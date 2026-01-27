// +build integration

package zooid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"github.com/coder/websocket"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	KindGroupAdmins      = 39001
	KindGroupMetadata    = 39000
	KindGroupMembers     = 39002
	KindCreateGroup      = 9007
	KindDeleteGroup      = 9008
	KindJoinRequest      = 9021
	KindGroupChatMessage = 9
)

// Test keys
var (
	adminSecret    = nostr.MustSecretKeyFromHex("0000000000000000000000000000000000000000000000000000000000000001")
	adminPubkey    = adminSecret.Public()
	nonAdminSecret = nostr.MustSecretKeyFromHex("0000000000000000000000000000000000000000000000000000000000000002")
	nonAdminPubkey = nonAdminSecret.Public()
	relaySecret    = nostr.MustSecretKeyFromHex("0000000000000000000000000000000000000000000000000000000000000099")
)

type relayContainer struct {
	testcontainers.Container
	URI string
}

func setupRelay(ctx context.Context, t *testing.T, adminCreateOnly bool) *relayContainer {
	adminCreateOnlyStr := "false"
	if adminCreateOnly {
		adminCreateOnlyStr = "true"
	}

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    "..",
			Dockerfile: "Dockerfile",
		},
		ExposedPorts: []string{"3334/tcp"},
		Env: map[string]string{
			"RELAY_HOST":               "localhost",
			"RELAY_SECRET":             relaySecret.Hex(),
			"RELAY_PUBKEY":             adminPubkey.Hex(),
			"ADMIN_PUBKEYS":            fmt.Sprintf(`"%s"`, adminPubkey.Hex()),
			"GROUPS_ADMIN_CREATE_ONLY": adminCreateOnlyStr,
		},
		WaitingFor: wait.ForListeningPort("3334/tcp").WithStartupTimeout(30 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Failed to start relay container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Failed to get container host: %v", err)
	}

	mappedPort, err := container.MappedPort(ctx, "3334")
	if err != nil {
		t.Fatalf("Failed to get mapped port: %v", err)
	}

	uri := fmt.Sprintf("ws://%s:%s", host, mappedPort.Port())

	// Give relay a moment to fully initialize
	time.Sleep(2 * time.Second)

	// Log container output for debugging
	logs, err := container.Logs(ctx)
	if err == nil {
		logBytes, _ := io.ReadAll(logs)
		if len(logBytes) > 0 {
			t.Logf("Container logs:\n%s", string(logBytes))
		}
		logs.Close()
	}

	return &relayContainer{
		Container: container,
		URI:       uri,
	}
}

type nostrClient struct {
	conn   *websocket.Conn
	secret nostr.SecretKey
}

func newNostrClient(ctx context.Context, t *testing.T, uri string, secret nostr.SecretKey) *nostrClient {
	// Set Host header to match the relay's configured hostname (without port)
	opts := &websocket.DialOptions{
		Host: "localhost",
	}
	conn, _, err := websocket.Dial(ctx, uri, opts)
	if err != nil {
		t.Fatalf("Failed to connect to relay: %v", err)
	}

	client := &nostrClient{
		conn:   conn,
		secret: secret,
	}

	// Handle NIP-42 AUTH challenge
	client.authenticate(ctx, t)

	return client
}

func (c *nostrClient) authenticate(ctx context.Context, t *testing.T) {
	// Read the AUTH challenge from relay
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, respData, err := c.conn.Read(readCtx)
	if err != nil {
		t.Logf("No AUTH challenge received (may not be required): %v", err)
		return
	}

	var resp []json.RawMessage
	json.Unmarshal(respData, &resp)

	if len(resp) < 2 {
		t.Logf("Unexpected message format: %s", string(respData))
		return
	}

	var msgType string
	json.Unmarshal(resp[0], &msgType)

	if msgType != "AUTH" {
		t.Logf("Expected AUTH challenge, got: %s", msgType)
		return
	}

	var challenge string
	json.Unmarshal(resp[1], &challenge)

	// Create and sign AUTH response (NIP-42 kind 22242)
	// Use ws://localhost to match the relay's configured host (without dynamic port)
	authEvent := &nostr.Event{
		Kind:      nostr.Kind(22242),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"relay", "ws://localhost"},
			{"challenge", challenge},
		},
		Content: "",
	}
	authEvent.Sign(c.secret)

	// Send AUTH response
	msg := []interface{}{"AUTH", authEvent}
	data, _ := json.Marshal(msg)

	err = c.conn.Write(ctx, websocket.MessageText, data)
	if err != nil {
		t.Fatalf("Failed to send AUTH response: %v", err)
	}

	// Read OK response
	_, okData, err := c.conn.Read(readCtx)
	if err != nil {
		t.Logf("Failed to read AUTH OK response: %v", err)
		return
	}

	t.Logf("AUTH response: %s", string(okData))
}

func (c *nostrClient) close() {
	c.conn.Close(websocket.StatusNormalClosure, "")
}

func (c *nostrClient) sendEvent(ctx context.Context, t *testing.T, event *nostr.Event) string {
	event.Sign(c.secret)

	msg := []interface{}{"EVENT", event}
	data, _ := json.Marshal(msg)

	err := c.conn.Write(ctx, websocket.MessageText, data)
	if err != nil {
		t.Fatalf("Failed to send event: %v", err)
	}

	// Read response
	_, respData, err := c.conn.Read(ctx)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	var resp []json.RawMessage
	json.Unmarshal(respData, &resp)

	if len(resp) < 3 {
		t.Fatalf("Invalid response: %s", string(respData))
	}

	var msgType string
	json.Unmarshal(resp[0], &msgType)

	if msgType == "OK" {
		var success bool
		json.Unmarshal(resp[2], &success)
		if !success {
			var reason string
			if len(resp) > 3 {
				json.Unmarshal(resp[3], &reason)
			}
			return "rejected:" + reason
		}
		return "ok"
	}

	return string(respData)
}

func (c *nostrClient) subscribe(ctx context.Context, t *testing.T, subID string, filter map[string]interface{}) []nostr.Event {
	msg := []interface{}{"REQ", subID, filter}
	data, _ := json.Marshal(msg)

	t.Logf("Sending subscription %s with filter: %+v", subID, filter)

	err := c.conn.Write(ctx, websocket.MessageText, data)
	if err != nil {
		t.Fatalf("Failed to send subscription: %v", err)
	}

	var events []nostr.Event
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	for {
		_, respData, err := c.conn.Read(timeoutCtx)
		if err != nil {
			t.Logf("Subscription %s read error: %v, received %d events", subID, err, len(events))
			return events
		}

		t.Logf("Subscription %s received: %s", subID, string(respData))

		var resp []json.RawMessage
		json.Unmarshal(respData, &resp)

		if len(resp) < 2 {
			continue
		}

		var msgType string
		json.Unmarshal(resp[0], &msgType)

		if msgType == "EVENT" && len(resp) >= 3 {
			var event nostr.Event
			if err := json.Unmarshal(resp[2], &event); err == nil {
				events = append(events, event)
			}
		} else if msgType == "EOSE" {
			t.Logf("Subscription %s complete, received %d events", subID, len(events))
			return events
		} else if msgType == "CLOSED" {
			var reason string
			if len(resp) >= 3 {
				json.Unmarshal(resp[2], &reason)
			}
			t.Logf("Subscription %s closed: %s", subID, reason)
			return events
		}
	}
}

func TestIntegration_RelayAdminListPublished(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelay(ctx, t, true)
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, adminSecret)
	defer client.close()

	// Query for relay-level admins (GROUP_ADMINS with d tag = "_")
	filter := map[string]interface{}{
		"kinds": []int{KindGroupAdmins},
		"#d":    []string{"_"},
	}

	events := client.subscribe(ctx, t, "admin-list", filter)

	if len(events) == 0 {
		t.Fatal("Expected relay to publish GROUP_ADMINS event with d='_', but got none")
	}

	// Verify the admin pubkey is in the event
	event := events[0]
	if event.Kind != nostr.Kind(KindGroupAdmins) {
		t.Errorf("Expected kind %d, got %d", KindGroupAdmins, event.Kind)
	}

	// Check d tag
	dTag := event.Tags.GetD()
	if dTag != "_" {
		t.Errorf("Expected d tag '_', got '%s'", dTag)
	}

	// Check p tags contain admin
	foundAdmin := false
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == adminPubkey.Hex() {
			foundAdmin = true
			break
		}
	}

	if !foundAdmin {
		t.Errorf("Admin pubkey not found in GROUP_ADMINS event p tags")
	}

	// Count p tags
	pTagCount := 0
	for range event.Tags.FindAll("p") {
		pTagCount++
	}
	t.Logf("Relay admin list contains %d admins", pTagCount)
}

func TestIntegration_AdminCanCreateGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelay(ctx, t, true)
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, adminSecret)
	defer client.close()

	// Create group as admin
	event := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "testgroup"}},
		Content:   `{"name":"Test Group","about":"Integration test group"}`,
	}

	result := client.sendEvent(ctx, t, event)
	if result != "ok" {
		t.Fatalf("Admin should be able to create group, but got: %s", result)
	}

	// Verify group was created by querying metadata
	filter := map[string]interface{}{
		"kinds": []int{KindGroupMetadata},
		"#d":    []string{"testgroup"},
	}

	events := client.subscribe(ctx, t, "group-meta", filter)
	if len(events) == 0 {
		t.Fatal("Group metadata not found after creation")
	}

	// Verify the metadata contains the group name
	metaEvent := events[0]
	t.Logf("Group metadata content: %s", metaEvent.Content)

	if metaEvent.Content == "" {
		t.Error("Group metadata content should not be empty")
	}

	// Parse content to verify name is included
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(metaEvent.Content), &metadata); err != nil {
		t.Errorf("Failed to parse metadata content as JSON: %v", err)
	} else {
		name, ok := metadata["name"].(string)
		if !ok || name != "Test Group" {
			t.Errorf("Expected group name 'Test Group', got '%v'", metadata["name"])
		}
		about, ok := metadata["about"].(string)
		if !ok || about != "Integration test group" {
			t.Errorf("Expected about 'Integration test group', got '%v'", metadata["about"])
		}
	}

	// Verify the creator is added as a member
	membersFilter := map[string]interface{}{
		"kinds": []int{KindGroupMembers},
		"#d":    []string{"testgroup"},
	}

	memberEvents := client.subscribe(ctx, t, "group-members", membersFilter)
	if len(memberEvents) == 0 {
		t.Fatal("Group members list not found after creation")
	}

	memberEvent := memberEvents[0]
	pTagCount := 0
	creatorFound := false
	for _, tag := range memberEvent.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			pTagCount++
			if tag[1] == adminPubkey.Hex() {
				creatorFound = true
			}
		}
	}

	if pTagCount == 0 {
		t.Error("Group members list should have at least 1 member (the creator)")
	}
	if !creatorFound {
		t.Error("Group creator should be in the members list")
	}

	t.Logf("Group created successfully with correct metadata and %d member(s)", pTagCount)
}

func TestIntegration_NonAdminCannotCreateGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelay(ctx, t, true) // admin_create_only = true
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer client.close()

	// Try to create group as non-admin
	event := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "unauthorized-group"}},
		Content:   `{"name":"Unauthorized Group"}`,
	}

	result := client.sendEvent(ctx, t, event)
	if result == "ok" {
		t.Fatal("Non-admin should NOT be able to create group when admin_create_only=true")
	}

	if !strings.Contains(result, "restricted") && !strings.Contains(result, "admin") {
		t.Logf("Got rejection: %s", result)
	}

	t.Logf("Non-admin correctly rejected from creating group")
}

func TestIntegration_AdminCanDeleteGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelay(ctx, t, true)
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, adminSecret)
	defer client.close()

	// First create a group
	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "deleteme"}},
		Content:   `{"name":"To Be Deleted"}`,
	}

	result := client.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create group: %s", result)
	}

	// Wait a moment
	time.Sleep(100 * time.Millisecond)

	// Delete the group
	deleteEvent := &nostr.Event{
		Kind:      nostr.Kind(KindDeleteGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "deleteme"}},
		Content:   "",
	}

	result = client.sendEvent(ctx, t, deleteEvent)
	if result != "ok" {
		t.Fatalf("Admin should be able to delete group, but got: %s", result)
	}

	// Verify group was deleted by querying metadata
	filter := map[string]interface{}{
		"kinds": []int{KindGroupMetadata},
		"#d":    []string{"deleteme"},
	}

	events := client.subscribe(ctx, t, "deleted-group", filter)
	if len(events) > 0 {
		t.Fatal("Group should be deleted but metadata still exists")
	}

	t.Logf("Group deleted successfully")
}

func TestIntegration_NonAdminCanCreateGroupWhenNotRestricted(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelay(ctx, t, false) // admin_create_only = false
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer client.close()

	// Create group as non-admin (should work when admin_create_only=false)
	event := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "anyonecanmake"}},
		Content:   `{"name":"Open Group"}`,
	}

	result := client.sendEvent(ctx, t, event)
	if result != "ok" {
		t.Fatalf("Non-admin should be able to create group when admin_create_only=false, but got: %s", result)
	}

	t.Logf("Non-admin successfully created group when admin_create_only=false")
}
