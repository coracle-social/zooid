package zooid

import (
	"testing"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
)

func createTestInstance() *Instance {
	ownerSecret := nostr.Generate()
	ownerPubkey := ownerSecret.Public()

	config := &Config{
		Host:   "test.com",
		secret: ownerSecret,
		Info: struct {
			Name        string `toml:"name"`
			Icon        string `toml:"icon"`
			Pubkey      string `toml:"pubkey"`
			Description string `toml:"description"`
		}{
			Name:   "Test Relay",
			Pubkey: ownerPubkey.Hex(),
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

	relay := &khatru.Relay{}

	events := &EventStore{
		Relay:  relay,
		Config: config,
		Schema: schema,
	}

	management := &ManagementStore{
		Config: config,
		Events: events,
	}

	instance := &Instance{
		Relay:      relay,
		Config:     config,
		Events:     events,
		Management: management,
	}

	instance.Events.Init()

	return instance
}

func TestInstance_AllowRecipientEvent(t *testing.T) {
	instance := createTestInstance()

	userSecret := nostr.Generate()
	userPubkey := userSecret.Public()

	// Add user as member
	instance.Management.AddMember(userPubkey)

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
	if inviteEvent.Kind != RELAY_INVITE {
		t.Errorf("GenerateInviteEvent() kind = %v, want %v", inviteEvent.Kind, RELAY_INVITE)
	}

	if inviteEvent.PubKey != instance.Config.GetSelf() {
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
