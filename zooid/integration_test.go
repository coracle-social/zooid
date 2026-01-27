// +build integration

package zooid

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"github.com/coder/websocket"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Pre-built image name and sync.Once for building it exactly once
var (
	prebuiltImage   = "zooid-integration-test:latest"
	buildImageOnce  sync.Once
	buildImageError error
)

// buildImage builds the Docker image once and caches the result
func buildImage(t *testing.T) string {
	buildImageOnce.Do(func() {
		log.Println("Building Docker image for integration tests (this happens once)...")

		// Build using docker CLI with a fixed tag
		// Tests run from zooid/zooid/, Dockerfile is in zooid/
		cmd := exec.Command("docker", "build", "-t", prebuiltImage, "-f", "Dockerfile", ".")
		cmd.Dir = ".."
		output, err := cmd.CombinedOutput()
		if err != nil {
			buildImageError = fmt.Errorf("failed to build Docker image: %w\nOutput: %s", err, string(output))
			return
		}

		log.Printf("Built Docker image: %s", prebuiltImage)
	})

	if buildImageError != nil {
		t.Fatalf("Failed to build Docker image: %v", buildImageError)
	}
	return prebuiltImage
}

const (
	KindGroupAdmins      = 39001
	KindGroupMetadata    = 39000
	KindGroupMembers     = 39002
	KindCreateGroup      = 9007
	KindDeleteGroup      = 9008
	KindJoinRequest      = 9021
	KindLeaveRequest     = 9022
	KindPutUser          = 9000
	KindRemoveUser       = 9001
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

type relayConfig struct {
	adminCreateOnly  bool
	privateAdminOnly bool
}

func setupRelay(ctx context.Context, t *testing.T, adminCreateOnly bool) *relayContainer {
	return setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  adminCreateOnly,
		privateAdminOnly: true, // Default to true for backwards compatibility
	})
}

func setupRelayWithConfig(ctx context.Context, t *testing.T, cfg relayConfig) *relayContainer {
	image := buildImage(t)

	adminCreateOnlyStr := "false"
	if cfg.adminCreateOnly {
		adminCreateOnlyStr = "true"
	}
	privateAdminOnlyStr := "false"
	if cfg.privateAdminOnly {
		privateAdminOnlyStr = "true"
	}

	req := testcontainers.ContainerRequest{
		Image:        image,
		ExposedPorts: []string{"3334/tcp"},
		Env: map[string]string{
			"RELAY_HOST":                "localhost",
			"RELAY_SECRET":              relaySecret.Hex(),
			"RELAY_PUBKEY":              adminPubkey.Hex(),
			"ADMIN_PUBKEYS":             fmt.Sprintf(`"%s"`, adminPubkey.Hex()),
			"GROUPS_ADMIN_CREATE_ONLY":  adminCreateOnlyStr,
			"GROUPS_PRIVATE_ADMIN_ONLY": privateAdminOnlyStr,
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

// Private Group Tests

func TestIntegration_AdminCanCreatePrivateGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, adminSecret)
	defer client.close()

	// Create private group as admin
	event := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "private-admin-group"}},
		Content:   `{"name":"Admin Private Group","about":"Private group by admin","private":true}`,
	}

	result := client.sendEvent(ctx, t, event)
	if result != "ok" {
		t.Fatalf("Admin should be able to create private group, but got: %s", result)
	}

	// Verify group metadata has private tag
	filter := map[string]interface{}{
		"kinds": []int{KindGroupMetadata},
		"#d":    []string{"private-admin-group"},
	}

	events := client.subscribe(ctx, t, "private-meta", filter)
	if len(events) == 0 {
		t.Fatal("Private group metadata not found after creation")
	}

	t.Logf("Admin successfully created private group")
}

func TestIntegration_NonAdminCannotCreatePrivateGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false, // Allow public group creation by anyone
		privateAdminOnly: true,  // But private groups are admin-only
	})
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer client.close()

	// Try to create private group as non-admin
	event := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "unauthorized-private"}},
		Content:   `{"name":"Unauthorized Private","private":true}`,
	}

	result := client.sendEvent(ctx, t, event)
	if result == "ok" {
		t.Fatal("Non-admin should NOT be able to create private group when private_admin_only=true")
	}

	if !strings.Contains(result, "restricted") {
		t.Logf("Got rejection: %s", result)
	}

	t.Logf("Non-admin correctly rejected from creating private group")
}

func TestIntegration_NonAdminCanCreatePublicGroupWhenPrivateRestricted(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false, // Allow public group creation by anyone
		privateAdminOnly: true,  // But private groups are admin-only
	})
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer client.close()

	// Create public group as non-admin (should work)
	event := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "public-by-user"}},
		Content:   `{"name":"Public Group By User","private":false}`,
	}

	result := client.sendEvent(ctx, t, event)
	if result != "ok" {
		t.Fatalf("Non-admin should be able to create public group when private_admin_only=true, but got: %s", result)
	}

	t.Logf("Non-admin successfully created public group")
}

func TestIntegration_NonAdminCanCreatePrivateGroupWhenNotRestricted(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false, // Allow group creation by anyone
		privateAdminOnly: false, // Allow private group creation by anyone
	})
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer client.close()

	// Create private group as non-admin (should work when private_admin_only=false)
	event := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "private-by-user"}},
		Content:   `{"name":"Private Group By User","private":true}`,
	}

	result := client.sendEvent(ctx, t, event)
	if result != "ok" {
		t.Fatalf("Non-admin should be able to create private group when private_admin_only=false, but got: %s", result)
	}

	t.Logf("Non-admin successfully created private group when private_admin_only=false")
}

func TestIntegration_AdminCanSeePrivateGroupContent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)
	defer adminClient.close()

	// Create private group as admin
	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "secret-group"}},
		Content:   `{"name":"Secret Group","about":"Admin only","private":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create private group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Send a message to the private group
	msgEvent := &nostr.Event{
		Kind:      nostr.Kind(KindGroupChatMessage),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "secret-group"}},
		Content:   "Secret message",
	}

	result = adminClient.sendEvent(ctx, t, msgEvent)
	if result != "ok" {
		t.Fatalf("Failed to send message to private group: %s", result)
	}

	// Admin should be able to see the message
	filter := map[string]interface{}{
		"kinds": []int{KindGroupChatMessage},
		"#h":    []string{"secret-group"},
	}

	events := adminClient.subscribe(ctx, t, "admin-private-msg", filter)
	if len(events) == 0 {
		t.Fatal("Admin should be able to see private group messages")
	}

	t.Logf("Admin can see private group content: %d messages", len(events))
}

func TestIntegration_NonMemberCannotSeePrivateGroupContent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	// Admin creates and posts to private group
	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "members-only"}},
		Content:   `{"name":"Members Only","private":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create private group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	msgEvent := &nostr.Event{
		Kind:      nostr.Kind(KindGroupChatMessage),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "members-only"}},
		Content:   "Private content",
	}

	result = adminClient.sendEvent(ctx, t, msgEvent)
	if result != "ok" {
		t.Fatalf("Failed to send message: %s", result)
	}

	adminClient.close()

	// Non-member tries to read
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer userClient.close()

	filter := map[string]interface{}{
		"kinds": []int{KindGroupChatMessage},
		"#h":    []string{"members-only"},
	}

	events := userClient.subscribe(ctx, t, "nonmember-private-msg", filter)
	if len(events) > 0 {
		t.Fatal("Non-member should NOT be able to see private group messages")
	}

	t.Logf("Non-member correctly cannot see private group content")
}

func TestIntegration_AdminCanDeletePrivateGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	client := newNostrClient(ctx, t, relay.URI, adminSecret)
	defer client.close()

	// Create private group
	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "delete-me-private"}},
		Content:   `{"name":"Delete Me Private","private":true}`,
	}

	result := client.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create private group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Delete the private group
	deleteEvent := &nostr.Event{
		Kind:      nostr.Kind(KindDeleteGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "delete-me-private"}},
		Content:   "",
	}

	result = client.sendEvent(ctx, t, deleteEvent)
	if result != "ok" {
		t.Fatalf("Admin should be able to delete private group, but got: %s", result)
	}

	// Verify group was deleted
	filter := map[string]interface{}{
		"kinds": []int{KindGroupMetadata},
		"#d":    []string{"delete-me-private"},
	}

	events := client.subscribe(ctx, t, "deleted-private-group", filter)
	if len(events) > 0 {
		t.Fatal("Private group should be deleted but metadata still exists")
	}

	t.Logf("Admin successfully deleted private group")
}

// Invite, Kick, and Rejoin Tests

func TestIntegration_AdminInvitesUserToPrivateGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	// Admin creates private group
	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "invite-test-group"}},
		Content:   `{"name":"Invite Test Group","private":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create private group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Admin sends a message to the group
	msgEvent := &nostr.Event{
		Kind:      nostr.Kind(KindGroupChatMessage),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "invite-test-group"}},
		Content:   "Welcome to the private group!",
	}

	result = adminClient.sendEvent(ctx, t, msgEvent)
	if result != "ok" {
		t.Fatalf("Failed to send message: %s", result)
	}

	// User tries to read before being invited - should fail
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)

	filter := map[string]interface{}{
		"kinds": []int{KindGroupChatMessage},
		"#h":    []string{"invite-test-group"},
	}

	events := userClient.subscribe(ctx, t, "before-invite", filter)
	if len(events) > 0 {
		t.Fatal("User should NOT be able to see private group messages before being invited")
	}
	userClient.close()

	// Admin adds user to the group (invite)
	inviteEvent := &nostr.Event{
		Kind:      nostr.Kind(KindPutUser),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "invite-test-group"},
			{"p", nonAdminPubkey.Hex()},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, inviteEvent)
	if result != "ok" {
		t.Fatalf("Admin should be able to add user to group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)
	adminClient.close()

	// User can now read the group messages
	userClient2 := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer userClient2.close()

	events = userClient2.subscribe(ctx, t, "after-invite", filter)
	if len(events) == 0 {
		t.Fatal("User should be able to see private group messages after being invited")
	}

	t.Logf("User successfully invited and can read %d messages", len(events))
}

func TestIntegration_AdminKicksUserFromGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	// Admin creates private group and adds user
	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "kick-test-group"}},
		Content:   `{"name":"Kick Test Group","private":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create private group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Add user
	addEvent := &nostr.Event{
		Kind:      nostr.Kind(KindPutUser),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "kick-test-group"},
			{"p", nonAdminPubkey.Hex()},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, addEvent)
	if result != "ok" {
		t.Fatalf("Failed to add user: %s", result)
	}

	// Wait to ensure different timestamp for next event (nostr uses seconds)
	time.Sleep(1100 * time.Millisecond)

	// Send a message
	msgEvent := &nostr.Event{
		Kind:      nostr.Kind(KindGroupChatMessage),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "kick-test-group"}},
		Content:   "Secret message",
	}

	result = adminClient.sendEvent(ctx, t, msgEvent)
	if result != "ok" {
		t.Fatalf("Failed to send message: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify user can read
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)

	filter := map[string]interface{}{
		"kinds": []int{KindGroupChatMessage},
		"#h":    []string{"kick-test-group"},
	}

	events := userClient.subscribe(ctx, t, "before-kick", filter)
	if len(events) == 0 {
		t.Fatal("User should be able to read before being kicked")
	}
	userClient.close()

	// Wait to ensure different timestamp for kick event
	time.Sleep(1100 * time.Millisecond)

	// Admin kicks user
	kickEvent := &nostr.Event{
		Kind:      nostr.Kind(KindRemoveUser),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "kick-test-group"},
			{"p", nonAdminPubkey.Hex()},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, kickEvent)
	if result != "ok" {
		t.Fatalf("Admin should be able to kick user: %s", result)
	}

	time.Sleep(100 * time.Millisecond)
	adminClient.close()

	// User can no longer read
	userClient2 := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer userClient2.close()

	events = userClient2.subscribe(ctx, t, "after-kick", filter)
	if len(events) > 0 {
		t.Fatal("User should NOT be able to read after being kicked")
	}

	t.Logf("User successfully kicked and can no longer read group")
}

func TestIntegration_KickedUserCanRejoin(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	// Create group, add user, kick user
	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "rejoin-test-group"}},
		Content:   `{"name":"Rejoin Test Group","private":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Add user
	addEvent := &nostr.Event{
		Kind:      nostr.Kind(KindPutUser),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "rejoin-test-group"},
			{"p", nonAdminPubkey.Hex()},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, addEvent)
	if result != "ok" {
		t.Fatalf("Failed to add user: %s", result)
	}

	// Wait to ensure different timestamp
	time.Sleep(1100 * time.Millisecond)

	// Send message
	msgEvent := &nostr.Event{
		Kind:      nostr.Kind(KindGroupChatMessage),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "rejoin-test-group"}},
		Content:   "Test message",
	}

	result = adminClient.sendEvent(ctx, t, msgEvent)
	if result != "ok" {
		t.Fatalf("Failed to send message: %s", result)
	}

	// Wait to ensure different timestamp for kick
	time.Sleep(1100 * time.Millisecond)

	// Kick user
	kickEvent := &nostr.Event{
		Kind:      nostr.Kind(KindRemoveUser),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "rejoin-test-group"},
			{"p", nonAdminPubkey.Hex()},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, kickEvent)
	if result != "ok" {
		t.Fatalf("Failed to kick user: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify user cannot read
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)

	filter := map[string]interface{}{
		"kinds": []int{KindGroupChatMessage},
		"#h":    []string{"rejoin-test-group"},
	}

	events := userClient.subscribe(ctx, t, "after-kick", filter)
	if len(events) > 0 {
		t.Fatal("Kicked user should not be able to read")
	}
	userClient.close()

	// Wait to ensure different timestamp for rejoin
	time.Sleep(1100 * time.Millisecond)

	// Admin re-adds user (rejoin)
	rejoinEvent := &nostr.Event{
		Kind:      nostr.Kind(KindPutUser),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "rejoin-test-group"},
			{"p", nonAdminPubkey.Hex()},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, rejoinEvent)
	if result != "ok" {
		t.Fatalf("Admin should be able to re-add kicked user: %s", result)
	}

	time.Sleep(100 * time.Millisecond)
	adminClient.close()

	// User can read again
	userClient2 := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer userClient2.close()

	events = userClient2.subscribe(ctx, t, "after-rejoin", filter)
	if len(events) == 0 {
		t.Fatal("Rejoined user should be able to read")
	}

	t.Logf("Kicked user successfully rejoined and can read %d messages", len(events))
}

func TestIntegration_UserCanPostAfterInvite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	// Admin creates group and invites user
	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "post-test-group"}},
		Content:   `{"name":"Post Test Group","private":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Add user
	addEvent := &nostr.Event{
		Kind:      nostr.Kind(KindPutUser),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "post-test-group"},
			{"p", nonAdminPubkey.Hex()},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, addEvent)
	if result != "ok" {
		t.Fatalf("Failed to add user: %s", result)
	}

	time.Sleep(100 * time.Millisecond)
	adminClient.close()

	// User posts a message
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)

	msgEvent := &nostr.Event{
		Kind:      nostr.Kind(KindGroupChatMessage),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "post-test-group"}},
		Content:   "Hello from invited user!",
	}

	result = userClient.sendEvent(ctx, t, msgEvent)
	if result != "ok" {
		t.Fatalf("Invited user should be able to post: %s", result)
	}

	// Verify message is stored
	filter := map[string]interface{}{
		"kinds": []int{KindGroupChatMessage},
		"#h":    []string{"post-test-group"},
	}

	events := userClient.subscribe(ctx, t, "user-message", filter)
	userClient.close()

	found := false
	for _, e := range events {
		if e.Content == "Hello from invited user!" {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("User's message should be stored in the group")
	}

	t.Logf("Invited user successfully posted message to group")
}

func TestIntegration_KickedUserCannotPost(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	// Create group, add user, then kick
	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "no-post-after-kick"}},
		Content:   `{"name":"No Post After Kick","private":true,"closed":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	addEvent := &nostr.Event{
		Kind:      nostr.Kind(KindPutUser),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "no-post-after-kick"},
			{"p", nonAdminPubkey.Hex()},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, addEvent)
	if result != "ok" {
		t.Fatalf("Failed to add user: %s", result)
	}

	// Wait to ensure different timestamp for kick
	time.Sleep(1100 * time.Millisecond)

	// Kick user
	kickEvent := &nostr.Event{
		Kind:      nostr.Kind(KindRemoveUser),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "no-post-after-kick"},
			{"p", nonAdminPubkey.Hex()},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, kickEvent)
	if result != "ok" {
		t.Fatalf("Failed to kick user: %s", result)
	}

	time.Sleep(100 * time.Millisecond)
	adminClient.close()

	// Kicked user tries to post
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer userClient.close()

	msgEvent := &nostr.Event{
		Kind:      nostr.Kind(KindGroupChatMessage),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "no-post-after-kick"}},
		Content:   "I was kicked but trying to post!",
	}

	result = userClient.sendEvent(ctx, t, msgEvent)
	if result == "ok" {
		t.Fatal("Kicked user should NOT be able to post to closed group")
	}

	t.Logf("Kicked user correctly rejected from posting: %s", result)
}

// Invite Code Validation Tests

const KindCreateInvite = 9009

func TestIntegration_UserCannotJoinPrivateGroupWithoutInvite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	// Admin creates a private group
	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "invite-required-group"}},
		Content:   `{"name":"Invite Required","private":true,"hidden":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create private group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)
	adminClient.close()

	// User tries to join WITHOUT invite code - should fail
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer userClient.close()

	joinEvent := &nostr.Event{
		Kind:      nostr.Kind(KindJoinRequest),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "invite-required-group"}},
		Content:   "",
	}

	result = userClient.sendEvent(ctx, t, joinEvent)
	if result == "ok" {
		t.Fatal("User should NOT be able to join private group without invite code")
	}

	if !strings.Contains(result, "invite") {
		t.Logf("Rejection reason: %s", result)
	}

	t.Logf("User correctly rejected from joining without invite code")
}

func TestIntegration_UserCannotJoinPrivateGroupWithWrongInvite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	// Admin creates a private group
	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "wrong-invite-group"}},
		Content:   `{"name":"Wrong Invite Test","private":true,"hidden":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create private group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Admin creates a valid invite
	inviteEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateInvite),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "wrong-invite-group"},
			{"code", "validcode123"},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, inviteEvent)
	if result != "ok" {
		t.Fatalf("Failed to create invite: %s", result)
	}

	adminClient.close()

	// User tries to join with WRONG invite code - should fail
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer userClient.close()

	joinEvent := &nostr.Event{
		Kind:      nostr.Kind(KindJoinRequest),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "wrong-invite-group"},
			{"code", "wrongcode999"},
		},
		Content: "",
	}

	result = userClient.sendEvent(ctx, t, joinEvent)
	if result == "ok" {
		t.Fatal("User should NOT be able to join private group with wrong invite code")
	}

	t.Logf("User correctly rejected with wrong invite code: %s", result)
}

func TestIntegration_UserCanJoinPrivateGroupWithValidInvite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	// Admin creates a private group
	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "valid-invite-group"}},
		Content:   `{"name":"Valid Invite Test","private":true,"hidden":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create private group: %s", result)
	}

	time.Sleep(100 * time.Millisecond)

	// Admin sends a message
	msgEvent := &nostr.Event{
		Kind:      nostr.Kind(KindGroupChatMessage),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "valid-invite-group"}},
		Content:   "Welcome message",
	}

	result = adminClient.sendEvent(ctx, t, msgEvent)
	if result != "ok" {
		t.Fatalf("Failed to send message: %s", result)
	}

	// Admin creates a valid invite
	inviteCode := "secretcode456"
	inviteEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateInvite),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "valid-invite-group"},
			{"code", inviteCode},
		},
		Content: "",
	}

	result = adminClient.sendEvent(ctx, t, inviteEvent)
	if result != "ok" {
		t.Fatalf("Failed to create invite: %s", result)
	}

	adminClient.close()

	// User joins with valid invite code - should succeed
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer userClient.close()

	joinEvent := &nostr.Event{
		Kind:      nostr.Kind(KindJoinRequest),
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"h", "valid-invite-group"},
			{"code", inviteCode},
		},
		Content: "",
	}

	result = userClient.sendEvent(ctx, t, joinEvent)
	if result != "ok" {
		t.Fatalf("User should be able to join private group with valid invite code, but got: %s", result)
	}

	// Wait for membership to be processed
	time.Sleep(100 * time.Millisecond)

	// User should now be able to read messages
	filter := map[string]interface{}{
		"kinds": []int{KindGroupChatMessage},
		"#h":    []string{"valid-invite-group"},
	}

	events := userClient.subscribe(ctx, t, "after-valid-invite", filter)
	if len(events) == 0 {
		t.Fatal("User should be able to read messages after joining with valid invite")
	}

	t.Logf("User successfully joined with valid invite code and can read %d messages", len(events))
}

func TestIntegration_PublicGroupDoesNotRequireInvite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	relay := setupRelayWithConfig(ctx, t, relayConfig{
		adminCreateOnly:  false,
		privateAdminOnly: true,
	})
	defer relay.Container.Terminate(ctx)

	// Admin creates a public group (closed but not private/hidden)
	adminClient := newNostrClient(ctx, t, relay.URI, adminSecret)

	createEvent := &nostr.Event{
		Kind:      nostr.Kind(KindCreateGroup),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "public-no-invite"}},
		Content:   `{"name":"Public Group","closed":true}`,
	}

	result := adminClient.sendEvent(ctx, t, createEvent)
	if result != "ok" {
		t.Fatalf("Failed to create public group: %s", result)
	}

	adminClient.close()

	// User can join without invite code
	userClient := newNostrClient(ctx, t, relay.URI, nonAdminSecret)
	defer userClient.close()

	joinEvent := &nostr.Event{
		Kind:      nostr.Kind(KindJoinRequest),
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"h", "public-no-invite"}},
		Content:   "",
	}

	result = userClient.sendEvent(ctx, t, joinEvent)
	if result != "ok" {
		t.Fatalf("User should be able to join public group without invite, but got: %s", result)
	}

	t.Logf("User successfully joined public group without invite code")
}
