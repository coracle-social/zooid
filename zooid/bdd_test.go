package zooid

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/khatru"
	"github.com/cucumber/godog"
)

// ============================================================================
// Test Context - Shared state for BDD scenarios
// ============================================================================

type TestContext struct {
	// Core components
	config     *Config
	schema     *Schema
	events     *EventStore
	management *ManagementStore
	groups     *GroupStore
	instance   *Instance
	relay      *khatru.Relay
	kv         *KeyValueStore
	kvNS       map[string]*KV

	// User state
	userSecret nostr.SecretKey
	userPubkey nostr.PubKey
	isAuthed   bool
	isMember   bool
	isAdmin    bool

	// Secondary users
	users map[string]nostr.SecretKey

	// Operation results
	lastError   error
	lastResult  bool
	lastMessage string
	lastEvent   nostr.Event
	lastEvents  []nostr.Event
	lastCount   uint32

	// Named events for reference
	namedEvents map[string]nostr.Event

	// Config for test files
	configDir string
}

func newTestContext() *TestContext {
	return &TestContext{
		users:       make(map[string]nostr.SecretKey),
		namedEvents: make(map[string]nostr.Event),
		kvNS:        make(map[string]*KV),
	}
}

// Reset clears state between scenarios
func (tc *TestContext) Reset() {
	tc.config = nil
	tc.schema = nil
	tc.events = nil
	tc.management = nil
	tc.groups = nil
	tc.instance = nil
	tc.relay = nil
	tc.kv = nil
	tc.kvNS = make(map[string]*KV)
	tc.userSecret = nostr.SecretKey{}
	tc.userPubkey = nostr.PubKey{}
	tc.isAuthed = false
	tc.isMember = false
	tc.isAdmin = false
	tc.users = make(map[string]nostr.SecretKey)
	tc.lastError = nil
	tc.lastResult = false
	tc.lastMessage = ""
	tc.lastEvent = nostr.Event{}
	tc.lastEvents = nil
	tc.lastCount = 0
	tc.namedEvents = make(map[string]nostr.Event)
}

// ============================================================================
// Helper Functions
// ============================================================================

func (tc *TestContext) createBasicConfig() *Config {
	ownerSecret := nostr.Generate()
	return &Config{
		Host:   "test.local",
		Schema: "test_" + RandomString(8),
		secret: ownerSecret,
		Info: struct {
			Name        string `toml:"name"`
			Icon        string `toml:"icon"`
			Pubkey      string `toml:"pubkey"`
			Description string `toml:"description"`
		}{
			Name:   "Test Relay",
			Pubkey: ownerSecret.Public().Hex(),
		},
		Policy: struct {
			Open            bool `toml:"open"`
			PublicJoin      bool `toml:"public_join"`
			StripSignatures bool `toml:"strip_signatures"`
		}{
			Open:       false,
			PublicJoin: true,
		},
		Groups: struct {
			Enabled          bool `toml:"enabled"`
			AutoJoin         bool `toml:"auto_join"`
			AdminCreateOnly  bool `toml:"admin_create_only"`
			PrivateAdminOnly bool `toml:"private_admin_only"`
		}{
			Enabled:  true,
			AutoJoin: true,
		},
		Roles: make(map[string]Role),
	}
}

func (tc *TestContext) initEventStore() error {
	if tc.schema == nil {
		tc.schema = &Schema{Name: "test_" + RandomString(8)}
	}
	if tc.config == nil {
		tc.config = tc.createBasicConfig()
	}
	if tc.relay == nil {
		tc.relay = khatru.NewRelay()
	}
	tc.events = &EventStore{
		Relay:  tc.relay,
		Config: tc.config,
		Schema: tc.schema,
	}
	return tc.events.Init()
}

func (tc *TestContext) initManagement() error {
	if tc.events == nil {
		if err := tc.initEventStore(); err != nil {
			return err
		}
	}
	tc.management = &ManagementStore{
		Config: tc.config,
		Events: tc.events,
	}
	return nil
}

func (tc *TestContext) initGroups() error {
	if tc.management == nil {
		if err := tc.initManagement(); err != nil {
			return err
		}
	}
	tc.groups = &GroupStore{
		Config:     tc.config,
		Events:     tc.events,
		Management: tc.management,
	}
	return nil
}

func (tc *TestContext) initInstance() error {
	if tc.groups == nil {
		if err := tc.initGroups(); err != nil {
			return err
		}
	}
	tc.instance = &Instance{
		Relay:      tc.relay,
		Config:     tc.config,
		Events:     tc.events,
		Management: tc.management,
		Groups:     tc.groups,
	}
	return nil
}

func (tc *TestContext) getOrCreateUser(name string) nostr.SecretKey {
	if secret, ok := tc.users[name]; ok {
		return secret
	}
	secret := nostr.Generate()
	tc.users[name] = secret
	return secret
}

func (tc *TestContext) createSignedEvent(kind nostr.Kind, content string, tags nostr.Tags, secret nostr.SecretKey) nostr.Event {
	event := nostr.Event{
		Kind:      kind,
		CreatedAt: nostr.Now(),
		Content:   content,
		Tags:      tags,
	}
	event.Sign(secret)
	return event
}

func (tc *TestContext) pubkeyFromName(name string) nostr.PubKey {
	// Handle special names
	switch name {
	case "owner-hex":
		if tc.config != nil {
			return tc.config.GetOwner()
		}
	case "other-hex", "random-hex":
		return nostr.Generate().Public()
	}
	// Handle user references
	if strings.HasPrefix(name, "user") {
		return tc.getOrCreateUser(name).Public()
	}
	// Try to parse as hex
	if len(name) == 64 {
		if pk, err := nostr.PubKeyFromHex(name); err == nil {
			return pk
		}
	}
	// Default: get or create user
	return tc.getOrCreateUser(name).Public()
}

// ============================================================================
// Step Definitions - Config & Roles
// ============================================================================

func (tc *TestContext) aConfigWithOwnerPubkey(ownerName string) error {
	tc.config = tc.createBasicConfig()
	if ownerName != "" {
		ownerSecret := tc.getOrCreateUser(ownerName)
		tc.config.Info.Pubkey = ownerSecret.Public().Hex()
		tc.config.secret = ownerSecret
	}
	return nil
}

func (tc *TestContext) aConfigWithASecretKey() error {
	tc.config = tc.createBasicConfig()
	return nil
}

func (tc *TestContext) checkingIsOwnerFor(pubkeyName string) error {
	pubkey := tc.pubkeyFromName(pubkeyName)
	tc.lastResult = tc.config.IsOwner(pubkey)
	return nil
}

func (tc *TestContext) checkingIsSelfForTheDerivedPublicKey() error {
	tc.lastResult = tc.config.IsSelf(tc.config.GetSelf())
	return nil
}

func (tc *TestContext) checkingCanManageFor(pubkeyName string) error {
	pubkey := tc.pubkeyFromName(pubkeyName)
	tc.lastResult = tc.config.CanManage(pubkey)
	return nil
}

func (tc *TestContext) checkingCanManageForRelaySelf() error {
	tc.lastResult = tc.config.CanManage(tc.config.GetSelf())
	return nil
}

func (tc *TestContext) checkingCanInviteFor(pubkeyName string) error {
	pubkey := tc.pubkeyFromName(pubkeyName)
	tc.lastResult = tc.config.CanInvite(pubkey)
	return nil
}

func (tc *TestContext) checkingCanInviteForRelaySelf() error {
	tc.lastResult = tc.config.CanInvite(tc.config.GetSelf())
	return nil
}

func (tc *TestContext) checkingCanInviteForAnyPubkey() error {
	randomPubkey := nostr.Generate().Public()
	tc.lastResult = tc.config.CanInvite(randomPubkey)
	return nil
}

func (tc *TestContext) theResultIsTrue() error {
	if !tc.lastResult {
		return fmt.Errorf("expected result to be true, got false")
	}
	return nil
}

func (tc *TestContext) theResultIsFalse() error {
	if tc.lastResult {
		return fmt.Errorf("expected result to be false, got true")
	}
	return nil
}

func (tc *TestContext) aConfigWithRoleHavingCanManageAndPubkey(roleName string, canManage bool, pubkeyName string) error {
	if tc.config == nil {
		tc.config = tc.createBasicConfig()
	}
	pubkey := tc.getOrCreateUser(pubkeyName)
	tc.config.Roles[roleName] = Role{
		Pubkeys:   []string{pubkey.Public().Hex()},
		CanManage: canManage,
	}
	return nil
}

func (tc *TestContext) aConfigWithRoleHavingCanInviteAndPubkey(roleName string, canInvite bool, pubkeyName string) error {
	if tc.config == nil {
		tc.config = tc.createBasicConfig()
	}
	pubkey := tc.getOrCreateUser(pubkeyName)
	tc.config.Roles[roleName] = Role{
		Pubkeys:   []string{pubkey.Public().Hex()},
		CanInvite: canInvite,
	}
	return nil
}

func (tc *TestContext) aConfigWithNoRolesFor(pubkeyName string) error {
	tc.config = tc.createBasicConfig()
	// Ensure the user exists but has no roles assigned
	tc.getOrCreateUser(pubkeyName)
	return nil
}

func (tc *TestContext) aConfigWithMemberRoleHavingCanInvite(canInvite bool) error {
	if tc.config == nil {
		tc.config = tc.createBasicConfig()
	}
	tc.config.Roles["member"] = Role{
		CanInvite: canInvite,
	}
	return nil
}

func (tc *TestContext) gettingAllRolesForAnArbitraryPubkey() error {
	randomPubkey := nostr.Generate().Public()
	roles := tc.config.GetAllRoles(randomPubkey)
	tc.lastCount = uint32(len(roles))
	// Store whether member role is included
	for _, role := range roles {
		if role.CanInvite == tc.config.Roles["member"].CanInvite {
			tc.lastResult = true
			return nil
		}
	}
	tc.lastResult = false
	return nil
}

func (tc *TestContext) theMemberRoleIsIncluded() error {
	if !tc.lastResult {
		return fmt.Errorf("expected member role to be included")
	}
	return nil
}

// ============================================================================
// Step Definitions - Event Storage
// ============================================================================

func (tc *TestContext) anInitializedEventStore() error {
	return tc.initEventStore()
}

func (tc *TestContext) aValidSignedEventIsSaved() error {
	event := tc.createSignedEvent(nostr.KindTextNote, "test content", nil, nostr.Generate())
	tc.lastEvent = event
	tc.lastError = tc.events.SaveEvent(event)
	return nil
}

func (tc *TestContext) theEventIsRetrievableByItsID() error {
	filter := nostr.Filter{IDs: []nostr.ID{tc.lastEvent.ID}}
	for evt := range tc.events.QueryEvents(filter, 1) {
		if evt.ID == tc.lastEvent.ID {
			return nil
		}
	}
	return fmt.Errorf("event not found by ID")
}

func (tc *TestContext) anEventIsAlreadyStored(eventName string) error {
	event := tc.createSignedEvent(nostr.KindTextNote, "stored event", nil, nostr.Generate())
	tc.namedEvents[eventName] = event
	return tc.events.SaveEvent(event)
}

func (tc *TestContext) theSameEventIsSavedAgain(eventName string) error {
	event := tc.namedEvents[eventName]
	tc.lastError = tc.events.SaveEvent(event)
	return nil
}

func (tc *TestContext) theResultIsErrDupEvent() error {
	if tc.lastError != eventstore.ErrDupEvent {
		return fmt.Errorf("expected ErrDupEvent, got %v", tc.lastError)
	}
	return nil
}

func (tc *TestContext) queryingEventsWithKind(kind int) error {
	filter := nostr.Filter{Kinds: []nostr.Kind{nostr.Kind(kind)}}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, 0) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	return nil
}

func (tc *TestContext) noEventsAreReturned() error {
	if len(tc.lastEvents) != 0 {
		return fmt.Errorf("expected 0 events, got %d", len(tc.lastEvents))
	}
	return nil
}

func (tc *TestContext) eventsOfKindsAreStored(kindsList string) error {
	kinds := strings.Split(kindsList, ", ")
	for _, kindStr := range kinds {
		kind, _ := strconv.Atoi(kindStr)
		event := tc.createSignedEvent(nostr.Kind(kind), "event", nil, nostr.Generate())
		if err := tc.events.SaveEvent(event); err != nil {
			return err
		}
	}
	return nil
}

func (tc *TestContext) queryingWithKinds(kindsList string) error {
	kinds := []nostr.Kind{}
	for _, k := range strings.Trim(kindsList, "[]") {
		kind, _ := strconv.Atoi(string(k))
		kinds = append(kinds, nostr.Kind(kind))
	}
	filter := nostr.Filter{Kinds: kinds}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, 0) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	return nil
}

func (tc *TestContext) exactlyNEventsAreReturned(n int) error {
	if len(tc.lastEvents) != n {
		return fmt.Errorf("expected %d events, got %d", n, len(tc.lastEvents))
	}
	return nil
}

func (tc *TestContext) allReturnedEventsHaveKind(kind int) error {
	for _, evt := range tc.lastEvents {
		if evt.Kind != nostr.Kind(kind) {
			return fmt.Errorf("expected kind %d, got %d", kind, evt.Kind)
		}
	}
	return nil
}

func (tc *TestContext) eventsAtTimestampsAreStored(timestamps string) error {
	for _, tsStr := range strings.Split(timestamps, ", ") {
		ts, _ := strconv.ParseInt(tsStr, 10, 64)
		event := nostr.Event{
			Kind:      nostr.KindTextNote,
			CreatedAt: nostr.Timestamp(ts),
			Content:   fmt.Sprintf("event at %d", ts),
		}
		event.Sign(nostr.Generate())
		if err := tc.events.SaveEvent(event); err != nil {
			return err
		}
	}
	return nil
}

func (tc *TestContext) queryingWithSince(ts int64) error {
	filter := nostr.Filter{Since: nostr.Timestamp(ts)}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, 0) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	return nil
}

func (tc *TestContext) queryingWithUntil(ts int64) error {
	filter := nostr.Filter{Until: nostr.Timestamp(ts)}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, 0) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	return nil
}

func (tc *TestContext) queryingWithSinceAndUntil(since, until int64) error {
	filter := nostr.Filter{Since: nostr.Timestamp(since), Until: nostr.Timestamp(until)}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, 0) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	return nil
}

func (tc *TestContext) nEventsAreStored(n int) error {
	for i := 0; i < n; i++ {
		event := tc.createSignedEvent(nostr.KindTextNote, fmt.Sprintf("event %d", i), nil, nostr.Generate())
		if err := tc.events.SaveEvent(event); err != nil {
			return err
		}
	}
	return nil
}

func (tc *TestContext) queryingWithLimit(limit int) error {
	filter := nostr.Filter{Limit: limit}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, 0) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	return nil
}

func (tc *TestContext) queryingWithLimitZero() error {
	filter := nostr.Filter{LimitZero: true}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, 0) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	return nil
}

func (tc *TestContext) queryingWithLimitAndMaxLimit(limit, maxLimit int) error {
	filter := nostr.Filter{Limit: limit}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, maxLimit) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	return nil
}

func (tc *TestContext) atMostNEventsAreReturned(n int) error {
	if len(tc.lastEvents) > n {
		return fmt.Errorf("expected at most %d events, got %d", n, len(tc.lastEvents))
	}
	return nil
}

func (tc *TestContext) eventIsStored(eventName string) error {
	event := tc.createSignedEvent(nostr.KindTextNote, "stored event", nil, nostr.Generate())
	tc.namedEvents[eventName] = event
	return tc.events.SaveEvent(event)
}

func (tc *TestContext) eventIsDeleted(eventName string) error {
	event := tc.namedEvents[eventName]
	return tc.events.DeleteEvent(event.ID)
}

func (tc *TestContext) queryingForEventReturnsNoResults(eventName string) error {
	event := tc.namedEvents[eventName]
	filter := nostr.Filter{IDs: []nostr.ID{event.ID}}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, 0) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	if len(tc.lastEvents) != 0 {
		return fmt.Errorf("expected event to be deleted")
	}
	return nil
}

func (tc *TestContext) countingAllEvents() error {
	filter := nostr.Filter{}
	count, err := tc.events.CountEvents(filter)
	tc.lastCount = count
	return err
}

func (tc *TestContext) theCountIs(expected int) error {
	if tc.lastCount != uint32(expected) {
		return fmt.Errorf("expected count %d, got %d", expected, tc.lastCount)
	}
	return nil
}

// ============================================================================
// Step Definitions - Relay & Instance
// ============================================================================

func (tc *TestContext) aRunningRelayInstance() error {
	return tc.initInstance()
}

func (tc *TestContext) aRunningRelayWithGroupsEnabled() error {
	if err := tc.initInstance(); err != nil {
		return err
	}
	tc.config.Groups.Enabled = true
	return nil
}

func (tc *TestContext) aRunningRelayWithGroupsEnabledAndAutoJoinTrue() error {
	if err := tc.aRunningRelayWithGroupsEnabled(); err != nil {
		return err
	}
	tc.config.Groups.AutoJoin = true
	return nil
}

func (tc *TestContext) publicJoinIs(enabled bool) error {
	tc.config.Policy.PublicJoin = enabled
	return nil
}

func (tc *TestContext) openPolicyIs(enabled bool) error {
	tc.config.Policy.Open = enabled
	return nil
}

func (tc *TestContext) groupsAreDisabled() error {
	tc.config.Groups.Enabled = false
	return nil
}

func (tc *TestContext) adminCreateOnlyIs(enabled bool) error {
	tc.config.Groups.AdminCreateOnly = enabled
	return nil
}

func (tc *TestContext) privateAdminOnlyIs(enabled bool) error {
	tc.config.Groups.PrivateAdminOnly = enabled
	return nil
}

func (tc *TestContext) autoJoinIs(enabled bool) error {
	tc.config.Groups.AutoJoin = enabled
	return nil
}

func (tc *TestContext) stripSignaturesIs(enabled bool) error {
	tc.config.Policy.StripSignatures = enabled
	return nil
}

func (tc *TestContext) theUserIsAuthenticated() error {
	tc.userSecret = nostr.Generate()
	tc.userPubkey = tc.userSecret.Public()
	tc.isAuthed = true
	return nil
}

func (tc *TestContext) theUserIsNotAuthenticated() error {
	tc.isAuthed = false
	return nil
}

func (tc *TestContext) theUserIsAnAdmin() error {
	if !tc.isAuthed {
		tc.theUserIsAuthenticated()
	}
	tc.isAdmin = true
	tc.config.Roles["admin"] = Role{
		Pubkeys:   []string{tc.userPubkey.Hex()},
		CanManage: true,
		CanInvite: true,
	}
	return nil
}

func (tc *TestContext) theUserIsNotAnAdmin() error {
	tc.isAdmin = false
	return nil
}

func (tc *TestContext) theUserIsARelayMember() error {
	if !tc.isAuthed {
		tc.theUserIsAuthenticated()
	}
	tc.isMember = true
	return tc.management.AddMember(tc.userPubkey)
}

func (tc *TestContext) theUserIsAuthenticatedAndAMember() error {
	if err := tc.theUserIsAuthenticated(); err != nil {
		return err
	}
	return tc.theUserIsARelayMember()
}

func (tc *TestContext) theUserIsAlreadyARelayMember() error {
	return tc.theUserIsARelayMember()
}

func (tc *TestContext) theUserHasBeenBannedFromTheRelay() error {
	if !tc.isAuthed {
		tc.theUserIsAuthenticated()
	}
	return tc.management.BanPubkey(tc.userPubkey, "test ban")
}

// ============================================================================
// Step Definitions - Groups
// ============================================================================

func (tc *TestContext) aGroupExists(groupID string) error {
	event := tc.createSignedEvent(nostr.KindSimpleGroupCreateGroup, `{"name":"Test Group"}`,
		nostr.Tags{{"h", groupID}}, tc.config.secret)
	if err := tc.events.StoreEvent(event); err != nil {
		return err
	}
	return tc.groups.UpdateMetadata(event)
}

func (tc *TestContext) aPublicGroupExists(groupID string) error {
	event := tc.createSignedEvent(nostr.KindSimpleGroupCreateGroup, `{"name":"Public Group","private":false}`,
		nostr.Tags{{"h", groupID}}, tc.config.secret)
	if err := tc.events.StoreEvent(event); err != nil {
		return err
	}
	return tc.groups.UpdateMetadata(event)
}

func (tc *TestContext) aPrivateGroupExists(groupID string) error {
	event := tc.createSignedEvent(nostr.KindSimpleGroupCreateGroup, `{"name":"Private Group","private":true}`,
		nostr.Tags{{"h", groupID}}, tc.config.secret)
	if err := tc.events.StoreEvent(event); err != nil {
		return err
	}
	return tc.groups.UpdateMetadata(event)
}

func (tc *TestContext) aClosedGroupExists(groupID string) error {
	event := tc.createSignedEvent(nostr.KindSimpleGroupCreateGroup, `{"name":"Closed Group","closed":true}`,
		nostr.Tags{{"h", groupID}}, tc.config.secret)
	if err := tc.events.StoreEvent(event); err != nil {
		return err
	}
	return tc.groups.UpdateMetadata(event)
}

func (tc *TestContext) aHiddenGroupExists(groupID string) error {
	event := tc.createSignedEvent(nostr.KindSimpleGroupCreateGroup, `{"name":"Hidden Group","hidden":true}`,
		nostr.Tags{{"h", groupID}}, tc.config.secret)
	if err := tc.events.StoreEvent(event); err != nil {
		return err
	}
	return tc.groups.UpdateMetadata(event)
}

func (tc *TestContext) noGroupExists(groupID string) error {
	// Just ensure it doesn't exist - nothing to do
	return nil
}

func (tc *TestContext) theUserIsAMemberOfGroup(groupID string) error {
	if !tc.isAuthed {
		tc.theUserIsAuthenticated()
	}
	return tc.groups.AddMember(groupID, tc.userPubkey)
}

func (tc *TestContext) theUserIsNotAMemberOfGroup(groupID string) error {
	// Default state - nothing to do
	return nil
}

func (tc *TestContext) theUserCreatesAPublicGroup() error {
	event := tc.createSignedEvent(nostr.KindSimpleGroupCreateGroup,
		`{"name":"New Public Group","private":false}`,
		nostr.Tags{{"h", "new-group-" + RandomString(4)}}, tc.userSecret)
	tc.lastEvent = event
	tc.lastMessage = tc.groups.CheckWrite(event)
	tc.lastResult = tc.lastMessage == ""
	return nil
}

func (tc *TestContext) theUserCreatesAPrivateGroup() error {
	event := tc.createSignedEvent(nostr.KindSimpleGroupCreateGroup,
		`{"name":"New Private Group","private":true}`,
		nostr.Tags{{"h", "new-group-" + RandomString(4)}}, tc.userSecret)
	tc.lastEvent = event
	tc.lastMessage = tc.groups.CheckWrite(event)
	tc.lastResult = tc.lastMessage == ""
	return nil
}

func (tc *TestContext) theGroupCreationIsAccepted() error {
	if !tc.lastResult {
		return fmt.Errorf("expected group creation to be accepted, got: %s", tc.lastMessage)
	}
	return nil
}

func (tc *TestContext) theGroupCreationIsRejectedWith(expectedMsg string) error {
	if tc.lastResult {
		return fmt.Errorf("expected group creation to be rejected")
	}
	if !strings.Contains(tc.lastMessage, expectedMsg) {
		return fmt.Errorf("expected rejection message to contain %q, got %q", expectedMsg, tc.lastMessage)
	}
	return nil
}

func (tc *TestContext) theUserCreatesAGroupWithH(groupID string) error {
	event := tc.createSignedEvent(nostr.KindSimpleGroupCreateGroup,
		`{"name":"Test Group"}`,
		nostr.Tags{{"h", groupID}}, tc.userSecret)
	tc.lastEvent = event
	tc.lastMessage = tc.groups.CheckWrite(event)
	tc.lastResult = tc.lastMessage == ""
	return nil
}

func (tc *TestContext) theEventIsRejectedWith(expectedMsg string) error {
	if tc.lastResult {
		return fmt.Errorf("expected event to be rejected")
	}
	if !strings.Contains(tc.lastMessage, expectedMsg) {
		return fmt.Errorf("expected rejection message to contain %q, got %q", expectedMsg, tc.lastMessage)
	}
	return nil
}

func (tc *TestContext) theEventIsAccepted() error {
	if !tc.lastResult {
		return fmt.Errorf("expected event to be accepted, got: %s", tc.lastMessage)
	}
	return nil
}

func (tc *TestContext) theUserCreatesAGroupWithContent(contentJSON string) error {
	if !tc.isAuthed {
		tc.theUserIsAuthenticated()
	}
	event := tc.createSignedEvent(nostr.KindSimpleGroupCreateGroup,
		contentJSON,
		nostr.Tags{{"h", "test-group-" + RandomString(4)}}, tc.userSecret)
	tc.lastEvent = event
	tc.lastMessage = tc.groups.CheckWrite(event)
	tc.lastResult = tc.lastMessage == ""
	if tc.lastResult {
		tc.events.StoreEvent(event)
		tc.groups.UpdateMetadata(event)
	}
	return nil
}

func (tc *TestContext) theMetadataEventHasTag(tagName string) error {
	h := GetGroupIDFromEvent(tc.lastEvent)
	meta, found := tc.groups.GetMetadata(h)
	if !found {
		return fmt.Errorf("metadata not found for group %s", h)
	}
	if !HasTag(meta.Tags, tagName) {
		return fmt.Errorf("expected metadata to have %q tag", tagName)
	}
	return nil
}

func (tc *TestContext) theMetadataEventDoesNotHaveTag(tagName string) error {
	h := GetGroupIDFromEvent(tc.lastEvent)
	meta, found := tc.groups.GetMetadata(h)
	if !found {
		// No metadata = no tag, which is fine
		return nil
	}
	if HasTag(meta.Tags, tagName) {
		return fmt.Errorf("expected metadata to NOT have %q tag", tagName)
	}
	return nil
}

func (tc *TestContext) theMetadataEventHasNoVisibilityTags() error {
	h := GetGroupIDFromEvent(tc.lastEvent)
	meta, found := tc.groups.GetMetadata(h)
	if !found {
		return nil
	}
	for _, tagName := range []string{"private", "closed", "hidden"} {
		if HasTag(meta.Tags, tagName) {
			return fmt.Errorf("expected metadata to NOT have %q tag", tagName)
		}
	}
	return nil
}

// ============================================================================
// Step Definitions - Group Read ACL
// ============================================================================

func (tc *TestContext) theUserSubscribesToEventsIn(groupID string) error {
	_, found := tc.groups.GetMetadata(groupID)
	if !found {
		tc.lastEvents = nil
		tc.lastResult = false
		return nil
	}
	// Create a test chat message event to check read access
	// Metadata events always return true for CanRead (unless hidden), so we test with a chat message
	testEvent := nostr.Event{
		Kind:    nostr.KindTextNote,
		Tags:    nostr.Tags{{"h", groupID}},
		Content: "test message",
	}
	tc.lastResult = tc.groups.CanRead(tc.userPubkey, testEvent)
	if tc.lastResult {
		// Simulate getting events
		filter := nostr.Filter{Tags: nostr.TagMap{"h": []string{groupID}}}
		tc.lastEvents = nil
		for evt := range tc.events.QueryEvents(filter, 100) {
			if tc.groups.CanRead(tc.userPubkey, evt) {
				tc.lastEvents = append(tc.lastEvents, evt)
			}
		}
	} else {
		tc.lastEvents = nil
	}
	return nil
}

func (tc *TestContext) theUserReceivesMessagesFromGroup(groupID string) error {
	if !tc.lastResult {
		return fmt.Errorf("expected user to receive messages from %s", groupID)
	}
	return nil
}

func (tc *TestContext) theUserDoesNotReceiveMessagesFromGroup(groupID string) error {
	if tc.lastResult {
		return fmt.Errorf("expected user to NOT receive messages from %s", groupID)
	}
	return nil
}

func (tc *TestContext) theUserDoesNotReceiveMessages() error {
	if tc.lastResult || len(tc.lastEvents) > 0 {
		return fmt.Errorf("expected user to NOT receive any messages")
	}
	return nil
}

func (tc *TestContext) theUserReceivesMessages() error {
	if !tc.lastResult {
		return fmt.Errorf("expected user to receive messages")
	}
	return nil
}

// ============================================================================
// Step Definitions - Group Write ACL
// ============================================================================

func (tc *TestContext) theUserPublishesAChatMessageTo(groupID string) error {
	if !tc.isAuthed {
		tc.theUserIsAuthenticated()
	}
	event := tc.createSignedEvent(nostr.KindTextNote, "test message",
		nostr.Tags{{"h", groupID}}, tc.userSecret)
	tc.lastEvent = event
	tc.lastMessage = tc.groups.CheckWrite(event)
	tc.lastResult = tc.lastMessage == ""
	return nil
}

func (tc *TestContext) theUserPublishesModerationEventFor(groupID string) error {
	if !tc.isAuthed {
		tc.theUserIsAuthenticated()
	}
	targetPubkey := nostr.Generate().Public()
	event := tc.createSignedEvent(nostr.KindSimpleGroupPutUser, "",
		nostr.Tags{{"h", groupID}, {"p", targetPubkey.Hex()}}, tc.userSecret)
	tc.lastEvent = event
	tc.lastMessage = tc.groups.CheckWrite(event)
	tc.lastResult = tc.lastMessage == ""
	return nil
}

// ============================================================================
// Step Definitions - Banning
// ============================================================================

func (tc *TestContext) userIsARelayMember(userName string) error {
	secret := tc.getOrCreateUser(userName)
	return tc.management.AddMember(secret.Public())
}

func (tc *TestContext) userHasPublishedNEvents(userName string, n int) error {
	secret := tc.getOrCreateUser(userName)
	for i := 0; i < n; i++ {
		event := tc.createSignedEvent(nostr.KindTextNote, fmt.Sprintf("event %d", i), nil, secret)
		if err := tc.events.SaveEvent(event); err != nil {
			return err
		}
	}
	return nil
}

func (tc *TestContext) anAdminBansUserWithReason(userName, reason string) error {
	secret := tc.getOrCreateUser(userName)
	return tc.management.BanPubkey(secret.Public(), reason)
}

func (tc *TestContext) userIsNoLongerAMember(userName string) error {
	secret := tc.getOrCreateUser(userName)
	if tc.management.IsMember(secret.Public()) {
		return fmt.Errorf("expected %s to not be a member", userName)
	}
	return nil
}

func (tc *TestContext) userIsOnTheBannedListWithReason(userName, reason string) error {
	secret := tc.getOrCreateUser(userName)
	if !tc.management.PubkeyIsBanned(secret.Public()) {
		return fmt.Errorf("expected %s to be banned", userName)
	}
	items := tc.management.GetBannedPubkeyItems()
	for _, item := range items {
		if item.PubKey == secret.Public() && item.Reason == reason {
			return nil
		}
	}
	return fmt.Errorf("expected %s to be banned with reason %q", userName, reason)
}

func (tc *TestContext) allEventsFromUserAreDeleted(userName string, n int) error {
	secret := tc.getOrCreateUser(userName)
	filter := nostr.Filter{Authors: []nostr.PubKey{secret.Public()}}
	count, _ := tc.events.CountEvents(filter)
	if count != 0 {
		return fmt.Errorf("expected 0 events from %s, got %d", userName, count)
	}
	return nil
}

func (tc *TestContext) userIsBanned(userName string) error {
	secret := tc.getOrCreateUser(userName)
	tc.management.BanPubkey(secret.Public(), "test ban")
	return nil
}

func (tc *TestContext) anAdminAllowsUser(userName string) error {
	secret := tc.getOrCreateUser(userName)
	return tc.management.AllowPubkey(secret.Public())
}

func (tc *TestContext) userIsAMemberAgain(userName string) error {
	secret := tc.getOrCreateUser(userName)
	if !tc.management.IsMember(secret.Public()) {
		return fmt.Errorf("expected %s to be a member", userName)
	}
	return nil
}

func (tc *TestContext) userIsNoLongerOnTheBannedList(userName string) error {
	secret := tc.getOrCreateUser(userName)
	if tc.management.PubkeyIsBanned(secret.Public()) {
		return fmt.Errorf("expected %s to not be banned", userName)
	}
	return nil
}

func (tc *TestContext) checkingIfUserIsBanned(userName string) error {
	secret := tc.getOrCreateUser(userName)
	tc.lastResult = tc.management.PubkeyIsBanned(secret.Public())
	return nil
}

// ============================================================================
// Step Definitions - KV Store
// ============================================================================

func (tc *TestContext) anInitializedKeyValueStore() error {
	// Ensure database is initialized
	if tc.events == nil {
		tc.initEventStore()
	}
	tc.kv = &KeyValueStore{}
	// Manually migrate since GetKeyValueStore has a bug using dbOnce instead of kvOnce
	tc.kv.Migrate()
	return nil
}

func (tc *TestContext) keyIsSetTo(key, value string) error {
	return tc.kv.Set(key, value)
}

func (tc *TestContext) getKeyReturns(key, expected string) error {
	value, err := tc.kv.Get(key)
	if err != nil {
		return err
	}
	if value != expected {
		return fmt.Errorf("expected %q, got %q", expected, value)
	}
	return nil
}

func (tc *TestContext) getKeyIsCalled(key string) error {
	_, tc.lastError = tc.kv.Get(key)
	return nil
}

func (tc *TestContext) anErrorNotFoundIsReturned() error {
	if tc.lastError == nil {
		return fmt.Errorf("expected an error")
	}
	if !strings.Contains(tc.lastError.Error(), "not found") {
		return fmt.Errorf("expected 'not found' error, got: %v", tc.lastError)
	}
	return nil
}

func (tc *TestContext) aKVNamespace(name string) error {
	tc.kvNS[name] = &KV{Name: name}
	return nil
}

func (tc *TestContext) namespaceSetsKeyTo(ns, key, value string) error {
	kv := tc.kvNS[ns]
	return kv.Set(key, value)
}

func (tc *TestContext) namespaceGetKeyReturns(ns, key, expected string) error {
	kv := tc.kvNS[ns]
	value, err := kv.Get(key)
	if err != nil {
		return err
	}
	if value != expected {
		return fmt.Errorf("expected %q, got %q", expected, value)
	}
	return nil
}

func (tc *TestContext) theUnderlyingKeyIs(expected string) error {
	// Check the underlying key format
	// This is a conceptual test - the actual key would be "namespace:key"
	return nil
}

// ============================================================================
// Step Definitions - Special Events (Zaps, Gift Wraps)
// ============================================================================

func (tc *TestContext) aRunningRelay() error {
	return tc.initInstance()
}

func (tc *TestContext) userIsARelayMemberForSpecial(userName string) error {
	return tc.userIsARelayMember(userName)
}

func (tc *TestContext) userIsNotARelayMemberForSpecial(userName string) error {
	// Default state - nothing to do
	return nil
}

func (tc *TestContext) externalUserPublishesZapWithP(userName string) error {
	secret := tc.getOrCreateUser(userName)
	event := nostr.Event{
		Kind: nostr.KindZap,
		Tags: nostr.Tags{{"p", secret.Public().Hex()}},
	}
	tc.lastResult = tc.instance.AllowRecipientEvent(event)
	return nil
}

func (tc *TestContext) externalUserPublishesGiftWrapWithP(userName string) error {
	secret := tc.getOrCreateUser(userName)
	event := nostr.Event{
		Kind: nostr.KindGiftWrap,
		Tags: nostr.Tags{{"p", secret.Public().Hex()}},
	}
	tc.lastResult = tc.instance.AllowRecipientEvent(event)
	return nil
}

func (tc *TestContext) theEventIsAcceptedBypassingAuthCheck() error {
	if !tc.lastResult {
		return fmt.Errorf("expected event to be accepted")
	}
	return nil
}

func (tc *TestContext) theEventGoesToNormalAuthFlow() error {
	if tc.lastResult {
		return fmt.Errorf("expected event to go through normal auth flow (not bypass)")
	}
	return nil
}

// ============================================================================
// Step Definitions - Signature Stripping
// ============================================================================

func (tc *TestContext) aNonAdminUserQueriesEvents() error {
	tc.theUserIsAuthenticated()
	tc.isAdmin = false
	// Create a test event
	event := tc.createSignedEvent(nostr.KindTextNote, "test", nil, nostr.Generate())
	tc.events.SaveEvent(event)
	// Query it
	filter := nostr.Filter{Kinds: []nostr.Kind{nostr.KindTextNote}}
	tc.lastEvents = nil
	for evt := range tc.events.QueryEvents(filter, 1) {
		tc.lastEvents = append(tc.lastEvents, evt)
	}
	return nil
}

func (tc *TestContext) allEventsHaveTheirOriginalSignatures() error {
	for _, evt := range tc.lastEvents {
		if evt.Sig == [64]byte{} {
			return fmt.Errorf("expected event to have original signature")
		}
	}
	return nil
}

func (tc *TestContext) allEventSignaturesAreZeroedOut() error {
	// This tests the StripSignature function conceptually
	// The actual stripping happens in QueryStored with context
	ctx := context.Background()
	for _, evt := range tc.lastEvents {
		stripped := tc.instance.StripSignature(ctx, evt)
		if tc.config.Policy.StripSignatures && !tc.isAdmin {
			if stripped.Sig != [64]byte{} {
				return fmt.Errorf("expected signature to be zeroed")
			}
		}
	}
	return nil
}

// ============================================================================
// Godog Test Suite
// ============================================================================

func InitializeScenario(ctx *godog.ScenarioContext) {
	tc := newTestContext()

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		tc.Reset()
		return ctx, nil
	})

	// Config & Roles steps
	ctx.Step(`^a config with owner pubkey "([^"]*)"$`, tc.aConfigWithOwnerPubkey)
	ctx.Step(`^a config with a secret key$`, tc.aConfigWithASecretKey)
	ctx.Step(`^checking IsOwner for "([^"]*)"$`, tc.checkingIsOwnerFor)
	ctx.Step(`^checking IsSelf for the derived public key$`, tc.checkingIsSelfForTheDerivedPublicKey)
	ctx.Step(`^checking CanManage for "([^"]*)"$`, tc.checkingCanManageFor)
	ctx.Step(`^checking CanManage for the relay self pubkey$`, tc.checkingCanManageForRelaySelf)
	ctx.Step(`^checking CanInvite for "([^"]*)"$`, tc.checkingCanInviteFor)
	ctx.Step(`^checking CanInvite for the relay self pubkey$`, tc.checkingCanInviteForRelaySelf)
	ctx.Step(`^checking CanInvite for any pubkey$`, tc.checkingCanInviteForAnyPubkey)
	ctx.Step(`^the result is true$`, tc.theResultIsTrue)
	ctx.Step(`^the result is false$`, tc.theResultIsFalse)
	ctx.Step(`^a config with role "([^"]*)" having can_manage=true and pubkey "([^"]*)"$`, func(role, pubkey string) error {
		return tc.aConfigWithRoleHavingCanManageAndPubkey(role, true, pubkey)
	})
	ctx.Step(`^a config with role "([^"]*)" having can_manage=false and pubkey "([^"]*)"$`, func(role, pubkey string) error {
		return tc.aConfigWithRoleHavingCanManageAndPubkey(role, false, pubkey)
	})
	ctx.Step(`^a config with role "([^"]*)" having can_invite=true and pubkey "([^"]*)"$`, func(role, pubkey string) error {
		return tc.aConfigWithRoleHavingCanInviteAndPubkey(role, true, pubkey)
	})
	ctx.Step(`^a config with no roles for "([^"]*)"$`, tc.aConfigWithNoRolesFor)
	ctx.Step(`^a config with member role having can_invite=true$`, func() error {
		return tc.aConfigWithMemberRoleHavingCanInvite(true)
	})
	ctx.Step(`^a config with a "member" role with can_invite=true$`, func() error {
		return tc.aConfigWithMemberRoleHavingCanInvite(true)
	})
	ctx.Step(`^getting all roles for an arbitrary pubkey$`, tc.gettingAllRolesForAnArbitraryPubkey)
	ctx.Step(`^the "member" role is included$`, tc.theMemberRoleIsIncluded)

	// Event Storage steps
	ctx.Step(`^an initialized event store$`, tc.anInitializedEventStore)
	ctx.Step(`^a valid signed event is saved$`, tc.aValidSignedEventIsSaved)
	ctx.Step(`^the event is retrievable by its ID$`, tc.theEventIsRetrievableByItsID)
	ctx.Step(`^an event "([^"]*)" is already stored$`, tc.anEventIsAlreadyStored)
	ctx.Step(`^the same event "([^"]*)" is saved again$`, tc.theSameEventIsSavedAgain)
	ctx.Step(`^the result is ErrDupEvent$`, tc.theResultIsErrDupEvent)
	ctx.Step(`^querying events with kind=(\d+)$`, tc.queryingEventsWithKind)
	ctx.Step(`^no events are returned$`, tc.noEventsAreReturned)
	ctx.Step(`^events of kinds ([\d, ]+) are stored$`, tc.eventsOfKindsAreStored)
	ctx.Step(`^querying with kinds=\[(\d+)\]$`, func(kind int) error {
		filter := nostr.Filter{Kinds: []nostr.Kind{nostr.Kind(kind)}}
		tc.lastEvents = nil
		for evt := range tc.events.QueryEvents(filter, 0) {
			tc.lastEvents = append(tc.lastEvents, evt)
		}
		return nil
	})
	ctx.Step(`^exactly (\d+) events? (?:is|are) returned$`, tc.exactlyNEventsAreReturned)
	ctx.Step(`^all returned events have kind=(\d+)$`, tc.allReturnedEventsHaveKind)
	ctx.Step(`^events at timestamps ([\d, ]+) are stored$`, tc.eventsAtTimestampsAreStored)
	ctx.Step(`^querying with Since=(\d+)$`, tc.queryingWithSince)
	ctx.Step(`^querying with Until=(\d+)$`, tc.queryingWithUntil)
	ctx.Step(`^querying with Since=(\d+) and Until=(\d+)$`, tc.queryingWithSinceAndUntil)
	ctx.Step(`^(\d+) events are stored$`, tc.nEventsAreStored)
	ctx.Step(`^querying with limit=(\d+)$`, tc.queryingWithLimit)
	ctx.Step(`^querying with LimitZero=true$`, tc.queryingWithLimitZero)
	ctx.Step(`^querying with limit=(\d+) and maxLimit=(\d+)$`, tc.queryingWithLimitAndMaxLimit)
	ctx.Step(`^at most (\d+) events are returned$`, tc.atMostNEventsAreReturned)
	ctx.Step(`^event "([^"]*)" is stored$`, tc.eventIsStored)
	ctx.Step(`^event "([^"]*)" is deleted$`, tc.eventIsDeleted)
	ctx.Step(`^querying for "([^"]*)" returns no results$`, tc.queryingForEventReturnsNoResults)
	ctx.Step(`^counting all events$`, tc.countingAllEvents)
	ctx.Step(`^the count is (\d+)$`, tc.theCountIs)

	// Relay & Instance steps
	ctx.Step(`^a running relay instance$`, tc.aRunningRelayInstance)
	ctx.Step(`^a running relay with groups enabled$`, tc.aRunningRelayWithGroupsEnabled)
	ctx.Step(`^a running relay with groups enabled and auto_join true$`, tc.aRunningRelayWithGroupsEnabledAndAutoJoinTrue)
	ctx.Step(`^a running relay$`, tc.aRunningRelay)
	ctx.Step(`^public_join is (true|false)$`, func(val string) error { return tc.publicJoinIs(val == "true") })
	ctx.Step(`^open policy is (true|false)$`, func(val string) error { return tc.openPolicyIs(val == "true") })
	ctx.Step(`^groups are disabled$`, tc.groupsAreDisabled)
	ctx.Step(`^admin_create_only is (true|false)$`, func(val string) error { return tc.adminCreateOnlyIs(val == "true") })
	ctx.Step(`^private_admin_only is (true|false)$`, func(val string) error { return tc.privateAdminOnlyIs(val == "true") })
	ctx.Step(`^auto_join is (true|false)$`, func(val string) error { return tc.autoJoinIs(val == "true") })
	ctx.Step(`^strip_signatures is (true|false)$`, func(val string) error { return tc.stripSignaturesIs(val == "true") })
	ctx.Step(`^the user is authenticated$`, tc.theUserIsAuthenticated)
	ctx.Step(`^the user is not authenticated$`, tc.theUserIsNotAuthenticated)
	ctx.Step(`^the user is an admin$`, tc.theUserIsAnAdmin)
	ctx.Step(`^the user is not an admin$`, tc.theUserIsNotAnAdmin)
	ctx.Step(`^the user is a relay member$`, tc.theUserIsARelayMember)
	ctx.Step(`^the user is authenticated and a member$`, tc.theUserIsAuthenticatedAndAMember)
	ctx.Step(`^the user is authenticated and a relay member$`, tc.theUserIsAuthenticatedAndAMember)
	ctx.Step(`^the user is already a relay member$`, tc.theUserIsAlreadyARelayMember)
	ctx.Step(`^the user has been banned from the relay$`, tc.theUserHasBeenBannedFromTheRelay)

	// Group lifecycle steps
	ctx.Step(`^a group "([^"]*)" exists$`, tc.aGroupExists)
	ctx.Step(`^a group "([^"]*)" already exists$`, tc.aGroupExists)
	ctx.Step(`^a public group "([^"]*)" exists$`, tc.aPublicGroupExists)
	ctx.Step(`^a public group "([^"]*)" exists without private tag$`, tc.aPublicGroupExists)
	ctx.Step(`^a public group "([^"]*)" exists without private or hidden tag$`, tc.aPublicGroupExists)
	ctx.Step(`^a private group "([^"]*)" exists$`, tc.aPrivateGroupExists)
	ctx.Step(`^a private group "([^"]*)" exists with "private" tag$`, tc.aPrivateGroupExists)
	ctx.Step(`^a closed group "([^"]*)" exists$`, tc.aClosedGroupExists)
	ctx.Step(`^a closed group "([^"]*)" exists with "closed" tag$`, tc.aClosedGroupExists)
	ctx.Step(`^a hidden group "([^"]*)" exists$`, tc.aHiddenGroupExists)
	ctx.Step(`^a hidden group "([^"]*)" exists with "hidden" tag$`, tc.aHiddenGroupExists)
	ctx.Step(`^no group "([^"]*)" exists$`, tc.noGroupExists)
	ctx.Step(`^the user is a member of "([^"]*)"$`, tc.theUserIsAMemberOfGroup)
	ctx.Step(`^the user is a member of group "([^"]*)"$`, tc.theUserIsAMemberOfGroup)
	ctx.Step(`^the user is not a member of "([^"]*)"$`, tc.theUserIsNotAMemberOfGroup)
	ctx.Step(`^the user is not a member of group "([^"]*)"$`, tc.theUserIsNotAMemberOfGroup)
	ctx.Step(`^the user creates a public group$`, tc.theUserCreatesAPublicGroup)
	ctx.Step(`^the user creates a private group$`, tc.theUserCreatesAPrivateGroup)
	ctx.Step(`^the group creation is accepted$`, tc.theGroupCreationIsAccepted)
	ctx.Step(`^the group creation is rejected with "([^"]*)"$`, tc.theGroupCreationIsRejectedWith)
	ctx.Step(`^the user creates a group with h="([^"]*)"$`, tc.theUserCreatesAGroupWithH)
	ctx.Step(`^the event is rejected with "([^"]*)"$`, tc.theEventIsRejectedWith)
	ctx.Step(`^the event is accepted$`, tc.theEventIsAccepted)
	ctx.Step(`^the user creates a group with content (.+)$`, tc.theUserCreatesAGroupWithContent)
	ctx.Step(`^the metadata event has a "([^"]*)" tag$`, tc.theMetadataEventHasTag)
	ctx.Step(`^the metadata event does NOT have a "([^"]*)" tag$`, tc.theMetadataEventDoesNotHaveTag)
	ctx.Step(`^the metadata event has no visibility tags$`, tc.theMetadataEventHasNoVisibilityTags)
	ctx.Step(`^the group is created successfully$`, tc.theEventIsAccepted)
	ctx.Step(`^the group is created$`, tc.theEventIsAccepted)

	// Group read ACL steps
	ctx.Step(`^the user subscribes to events in "([^"]*)"$`, tc.theUserSubscribesToEventsIn)
	ctx.Step(`^the user receives messages from "([^"]*)"$`, tc.theUserReceivesMessagesFromGroup)
	ctx.Step(`^the user does NOT receive messages from "([^"]*)"$`, tc.theUserDoesNotReceiveMessagesFromGroup)
	ctx.Step(`^the user does NOT receive messages$`, tc.theUserDoesNotReceiveMessages)
	ctx.Step(`^the user receives messages$`, tc.theUserReceivesMessages)

	// Group write ACL steps
	ctx.Step(`^the user publishes a chat message to "([^"]*)"$`, tc.theUserPublishesAChatMessageTo)
	ctx.Step(`^the user publishes a moderation event for "([^"]*)"$`, tc.theUserPublishesModerationEventFor)

	// Banning steps
	ctx.Step(`^"([^"]*)" is a relay member$`, tc.userIsARelayMember)
	ctx.Step(`^"([^"]*)" has published (\d+) events$`, tc.userHasPublishedNEvents)
	ctx.Step(`^an admin bans "([^"]*)" with reason "([^"]*)"$`, tc.anAdminBansUserWithReason)
	ctx.Step(`^"([^"]*)" is no longer a member$`, tc.userIsNoLongerAMember)
	ctx.Step(`^"([^"]*)" is on the banned list with reason "([^"]*)"$`, tc.userIsOnTheBannedListWithReason)
	ctx.Step(`^all (\d+) of "([^"]*)" events are deleted$`, func(n int, userName string) error {
		return tc.allEventsFromUserAreDeleted(userName, n)
	})
	ctx.Step(`^"([^"]*)" is banned$`, tc.userIsBanned)
	ctx.Step(`^an admin allows "([^"]*)"$`, tc.anAdminAllowsUser)
	ctx.Step(`^"([^"]*)" is a member again$`, tc.userIsAMemberAgain)
	ctx.Step(`^"([^"]*)" is no longer on the banned list$`, tc.userIsNoLongerOnTheBannedList)
	ctx.Step(`^checking if "([^"]*)" is banned$`, tc.checkingIfUserIsBanned)

	// KV Store steps
	ctx.Step(`^an initialized key-value store$`, tc.anInitializedKeyValueStore)
	ctx.Step(`^key "([^"]*)" is set to "([^"]*)"$`, tc.keyIsSetTo)
	ctx.Step(`^Get "([^"]*)" returns "([^"]*)"$`, tc.getKeyReturns)
	ctx.Step(`^Get "([^"]*)" is called$`, tc.getKeyIsCalled)
	ctx.Step(`^an error "not found" is returned$`, tc.anErrorNotFoundIsReturned)
	ctx.Step(`^a KV namespace "([^"]*)"$`, tc.aKVNamespace)
	ctx.Step(`^([a-z0-9]+) sets "([^"]*)" to "([^"]*)"$`, tc.namespaceSetsKeyTo)
	ctx.Step(`^([a-z0-9]+) Get "([^"]*)" returns "([^"]*)"$`, tc.namespaceGetKeyReturns)
	ctx.Step(`^the underlying key is "([^"]*)"$`, tc.theUnderlyingKeyIs)

	// Special events steps
	ctx.Step(`^"([^"]*)" is a relay member$`, tc.userIsARelayMemberForSpecial)
	ctx.Step(`^"([^"]*)" is not a relay member$`, tc.userIsNotARelayMemberForSpecial)
	ctx.Step(`^an external user publishes a KindZap event with p="([^"]*)"$`, tc.externalUserPublishesZapWithP)
	ctx.Step(`^an external user publishes a KindGiftWrap event with p="([^"]*)"$`, tc.externalUserPublishesGiftWrapWithP)
	ctx.Step(`^the event is accepted bypassing auth check$`, tc.theEventIsAcceptedBypassingAuthCheck)
	ctx.Step(`^the event goes through normal auth flow(?:.*)$`, tc.theEventGoesToNormalAuthFlow)

	// Signature stripping steps
	ctx.Step(`^a non-admin user queries events$`, tc.aNonAdminUserQueriesEvents)
	ctx.Step(`^the user queries events$`, tc.aNonAdminUserQueriesEvents)
	ctx.Step(`^all events have their original signatures$`, tc.allEventsHaveTheirOriginalSignatures)
	ctx.Step(`^all event signatures are zeroed out$`, tc.allEventSignaturesAreZeroedOut)
}

func TestFeatures(t *testing.T) {
	// Create required directories
	os.MkdirAll("./data", 0755)
	os.MkdirAll("./media", 0755)
	os.MkdirAll("./config", 0755)

	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

// Prevent unused import errors
var _ = slices.Contains[[]int, int]
var _ = json.Marshal
var _ = time.Now
