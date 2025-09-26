package zooid

import (
	"testing"

	"fiatjaf.com/nostr"
)

func createTestInstance() *Instance {
	ownerSecret := nostr.Generate()
	ownerPubkey := ownerSecret.Public()

	config := &Config{
		Host:   "test.com",
		Secret: ownerSecret,
		Self: struct {
			Name        string `toml:"name"`
			Icon        string `toml:"icon"`
			Schema      string `toml:"schema"`
			Secret      string `toml:"secret"`
			Pubkey      string `toml:"pubkey"`
			Description string `toml:"description"`
		}{
			Name:   "Test Relay",
			Secret: ownerSecret.Hex(),
			Pubkey: ownerPubkey.Hex(),
			Schema: "test_relay",
		},
		Roles: map[string]Role{
			"admin": {
				Pubkeys:   []string{ownerPubkey.Hex()},
				CanManage: true,
				CanInvite: true,
			},
		},
	}

	schema := &Schema{Name: "test_" + RandomString(8)}
	events := &EventStore{
		Config: config,
		Schema: schema,
	}

	instance := &Instance{
		Config: config,
		Events: events,
		Management: &ManagementStore{
			Config: config,
			Events: events,
		},
	}

	instance.Events.Init()

	return instance
}

func TestInstance_IsAdmin(t *testing.T) {
	instance := createTestInstance()

	ownerPubkey := instance.Config.Secret.Public()
	otherPubkey := nostr.Generate().Public()

	// Test owner is admin
	if !instance.Config.IsAdmin(ownerPubkey) {
		t.Error("IsAdmin() should return true for owner")
	}

	// Test non-owner is not admin
	if instance.Config.IsAdmin(otherPubkey) {
		t.Error("IsAdmin() should return false for non-owner")
	}

	// Test user with manage permission is admin
	managerPubkey := nostr.Generate().Public()
	instance.Config.Roles["manager"] = Role{
		Pubkeys:   []string{managerPubkey.Hex()},
		CanManage: true,
	}

	if !instance.Config.IsAdmin(managerPubkey) {
		t.Error("IsAdmin() should return true for user with manage permissions")
	}
}

func TestInstance_HasAccess(t *testing.T) {
	instance := createTestInstance()

	ownerPubkey := instance.Config.Secret.Public()
	userSecret := nostr.Generate()
	userPubkey := userSecret.Public()

	// Test owner has access
	if !instance.HasAccess(ownerPubkey) {
		t.Error("HasAccess() should return true for owner")
	}

	// Test user without join event has no access
	if instance.HasAccess(userPubkey) {
		t.Error("HasAccess() should return false for user without join event")
	}

	// Add a join event for the user (must be signed by the user)
	joinEvent := nostr.Event{
		Kind:      AUTH_JOIN,
		CreatedAt: nostr.Now(),
		PubKey:    userPubkey,
		Tags:      nostr.Tags{{"claim", "test"}},
	}
	joinEvent.Sign(userSecret)

	instance.Events.SaveEvent(joinEvent)

	// Test user with join event has access
	if !instance.HasAccess(userPubkey) {
		t.Error("HasAccess() should return true for user with join event")
	}
}

func TestInstance_IsGroupMember(t *testing.T) {
	instance := createTestInstance()

	groupID := "test-group-123"
	userPubkey := nostr.Generate().Public()

	// Test user is not initially a member
	if instance.IsGroupMember(groupID, userPubkey) {
		t.Error("IsGroupMember() should return false for non-member")
	}

	// Add user to group
	putUserEvent := MakePutUserEvent(groupID, userPubkey)
	putUserEvent.Sign(instance.Config.Secret)
	instance.Events.SaveEvent(putUserEvent)

	// Test user is now a member
	if !instance.IsGroupMember(groupID, userPubkey) {
		t.Error("IsGroupMember() should return true after put user event")
	}

	// Remove user from group (with a later timestamp to ensure proper ordering)
	removeUserEvent := MakeRemoveUserEvent(groupID, userPubkey)
	removeUserEvent.CreatedAt = nostr.Now() + 1 // Make it newer
	removeUserEvent.Sign(instance.Config.Secret)
	instance.Events.SaveEvent(removeUserEvent)

	// Test user is no longer a member
	if instance.IsGroupMember(groupID, userPubkey) {
		t.Error("IsGroupMember() should return false after remove user event")
	}
}

func TestInstance_HasGroupAccess(t *testing.T) {
	instance := createTestInstance()

	groupID := "test-group-456"
	userPubkey := nostr.Generate().Public()

	// Create open group metadata
	openGroupMeta := nostr.Event{
		Kind:      nostr.KindSimpleGroupMetadata,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"a", groupID},
			{"name", "Open Group"},
		},
	}
	openGroupMeta.Sign(instance.Config.Secret)
	instance.Events.SaveEvent(openGroupMeta)

	// Test access to open group
	if !instance.HasGroupAccess(groupID, userPubkey) {
		t.Error("HasGroupAccess() should return true for open group")
	}

	// Create closed group metadata
	closedGroupID := "closed-group-789"
	closedGroupMeta := nostr.Event{
		Kind:      nostr.KindSimpleGroupMetadata,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"a", closedGroupID},
			{"name", "Closed Group"},
			{"closed", ""},
		},
	}
	closedGroupMeta.Sign(instance.Config.Secret)
	instance.Events.SaveEvent(closedGroupMeta)

	// Test no access to closed group for non-member
	if instance.HasGroupAccess(closedGroupID, userPubkey) {
		t.Error("HasGroupAccess() should return false for closed group non-member")
	}

	// Add user as member to closed group
	putUserEvent := MakePutUserEvent(closedGroupID, userPubkey)
	putUserEvent.Sign(instance.Config.Secret)
	instance.Events.SaveEvent(putUserEvent)

	// Test access to closed group for member
	if !instance.HasGroupAccess(closedGroupID, userPubkey) {
		t.Error("HasGroupAccess() should return true for closed group member")
	}
}

func TestInstance_AllowRecipientEvent(t *testing.T) {
	instance := createTestInstance()

	userSecret := nostr.Generate()
	userPubkey := userSecret.Public()

	// Add user access
	joinEvent := nostr.Event{
		Kind:      AUTH_JOIN,
		CreatedAt: nostr.Now(),
		PubKey:    userPubkey,
		Tags:      nostr.Tags{{"claim", "test"}},
	}
	joinEvent.Sign(userSecret)
	instance.Events.SaveEvent(joinEvent)

	tests := []struct {
		name  string
		event nostr.Event
		want  bool
	}{
		{
			name: "zap event with valid recipient",
			event: nostr.Event{
				Kind: nostr.KindZap,
				Tags: nostr.Tags{{"p", userPubkey.Hex()}},
			},
			want: true,
		},
		{
			name: "gift wrap event with valid recipient",
			event: nostr.Event{
				Kind: nostr.KindGiftWrap,
				Tags: nostr.Tags{{"p", userPubkey.Hex()}},
			},
			want: true,
		},
		{
			name: "zap event with invalid recipient",
			event: nostr.Event{
				Kind: nostr.KindZap,
				Tags: nostr.Tags{{"p", nostr.Generate().Public().Hex()}},
			},
			want: false,
		},
		{
			name: "text note event",
			event: nostr.Event{
				Kind: nostr.KindTextNote,
				Tags: nostr.Tags{{"p", userPubkey.Hex()}},
			},
			want: false,
		},
		{
			name: "zap event without p tag",
			event: nostr.Event{
				Kind: nostr.KindZap,
				Tags: nostr.Tags{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := instance.AllowRecipientEvent(tt.event)
			if result != tt.want {
				t.Errorf("AllowRecipientEvent() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestInstance_GenerateInviteEvent(t *testing.T) {
	instance := createTestInstance()

	userPubkey := nostr.Generate().Public()

	// Generate invite event
	inviteEvent := instance.GenerateInviteEvent(userPubkey)

	// Test event properties
	if inviteEvent.Kind != AUTH_INVITE {
		t.Errorf("GenerateInviteEvent() kind = %v, want %v", inviteEvent.Kind, AUTH_INVITE)
	}

	if inviteEvent.PubKey != instance.Config.Secret.Public() {
		t.Error("GenerateInviteEvent() should be signed by instance")
	}

	// Test tags
	claimTag := inviteEvent.Tags.Find("claim")
	if claimTag == nil {
		t.Error("GenerateInviteEvent() should have claim tag")
	}

	pTag := inviteEvent.Tags.Find("p")
	if pTag == nil || pTag[1] != userPubkey.Hex() {
		t.Error("GenerateInviteEvent() should have correct p tag")
	}

	// Note: The GenerateInviteEvent function actually looks for existing events
	// by the target pubkey as author, but creates events signed by instance.
	// This seems to be a bug in the implementation, but we test the current behavior.
	// Each call will generate a new event since the query won't find a match.
	inviteEvent2 := instance.GenerateInviteEvent(userPubkey)
	if inviteEvent.ID == inviteEvent2.ID {
		t.Error("GenerateInviteEvent() generates new events each time due to query mismatch")
	}
}

func TestInstance_OnJoinEvent(t *testing.T) {
	instance := createTestInstance()

	userPubkey := nostr.Generate().Public()

	// Generate an invite first
	inviteEvent := instance.GenerateInviteEvent(userPubkey)
	claimTag := inviteEvent.Tags.Find("claim")

	tests := []struct {
		name       string
		joinEvent  nostr.Event
		wantReject bool
		wantMsg    string
	}{
		{
			name: "valid join event",
			joinEvent: nostr.Event{
				Kind: AUTH_JOIN,
				Tags: nostr.Tags{{"claim", claimTag[1]}},
			},
			wantReject: false,
			wantMsg:    "",
		},
		{
			name: "join event without claim",
			joinEvent: nostr.Event{
				Kind: AUTH_JOIN,
				Tags: nostr.Tags{},
			},
			wantReject: true,
			wantMsg:    "invalid: no claim tag",
		},
		{
			name: "join event with invalid claim",
			joinEvent: nostr.Event{
				Kind: AUTH_JOIN,
				Tags: nostr.Tags{{"claim", "invalid-claim"}},
			},
			wantReject: true,
			wantMsg:    "invalid: failed to validate invite code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reject, msg := instance.OnJoinEvent(tt.joinEvent)
			if reject != tt.wantReject {
				t.Errorf("OnJoinEvent() reject = %v, want %v", reject, tt.wantReject)
			}
			if msg != tt.wantMsg {
				t.Errorf("OnJoinEvent() msg = %v, want %v", msg, tt.wantMsg)
			}
		})
	}
}

func TestInstance_GetGroupMetadataEvent(t *testing.T) {
	instance := createTestInstance()

	groupID := "test-group-metadata"

	// Test with no metadata event
	metaEvent := instance.GetGroupMetadataEvent(groupID)
	if !IsEmptyEvent(metaEvent) {
		t.Error("GetGroupMetadataEvent() should return empty event when no metadata exists")
	}

	// Create metadata event
	originalMeta := nostr.Event{
		Kind:      nostr.KindSimpleGroupMetadata,
		CreatedAt: nostr.Now(),
		Tags: nostr.Tags{
			{"a", groupID},
			{"name", "Test Group"},
		},
	}
	originalMeta.Sign(instance.Config.Secret)
	instance.Events.SaveEvent(originalMeta)

	// Test with metadata event
	metaEvent = instance.GetGroupMetadataEvent(groupID)
	if IsEmptyEvent(metaEvent) {
		t.Error("GetGroupMetadataEvent() should return metadata event")
	}

	if metaEvent.ID != originalMeta.ID {
		t.Error("GetGroupMetadataEvent() should return correct metadata event")
	}
}

func TestInstance_IsInternalEvent(t *testing.T) {
	instance := createTestInstance()

	tests := []struct {
		name  string
		event nostr.Event
		want  bool
	}{
		{
			name: "internal zooid event",
			event: nostr.Event{
				Kind: nostr.KindApplicationSpecificData,
				Tags: nostr.Tags{{"d", "zooid/banned_pubkeys"}},
			},
			want: true,
		},
		{
			name: "internal zooid event with different data",
			event: nostr.Event{
				Kind: nostr.KindApplicationSpecificData,
				Tags: nostr.Tags{{"d", "zooid/some_data"}},
			},
			want: true,
		},
		{
			name: "non-internal event",
			event: nostr.Event{
				Kind: nostr.KindApplicationSpecificData,
				Tags: nostr.Tags{{"d", "external/data"}},
			},
			want: false,
		},
		{
			name: "wrong kind",
			event: nostr.Event{
				Kind: nostr.KindTextNote,
				Tags: nostr.Tags{{"d", "zooid/data"}},
			},
			want: false,
		},
		{
			name: "no d tag",
			event: nostr.Event{
				Kind: nostr.KindApplicationSpecificData,
				Tags: nostr.Tags{{"t", "tag"}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := instance.IsInternalEvent(tt.event)
			if result != tt.want {
				t.Errorf("IsInternalEvent() = %v, want %v", result, tt.want)
			}
		})
	}
}

func TestInstance_HasAccess_WithBannedUser(t *testing.T) {
	instance := createTestInstance()

	userSecret := nostr.Generate()
	userPubkey := userSecret.Public()

	// Add user to banned list
	instance.Management.BanPubkey(userPubkey, "test ban")

	// Test banned user has no access even with join event
	joinEvent := nostr.Event{
		Kind:      AUTH_JOIN,
		CreatedAt: nostr.Now(),
		PubKey:    userPubkey,
		Tags:      nostr.Tags{{"claim", "test"}},
	}
	joinEvent.Sign(userSecret)
	instance.Events.SaveEvent(joinEvent)

	if instance.HasAccess(userPubkey) {
		t.Error("HasAccess() should return false for banned user even with join event")
	}
}
