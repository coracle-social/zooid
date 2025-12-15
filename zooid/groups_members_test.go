package zooid

import (
	"testing"

	"fiatjaf.com/nostr"
)

func TestGroupStore_GetMembers_AddRemove(t *testing.T) {
	// Minimal store setup
	config := &Config{Host: "test.com", secret: nostr.Generate()}
	events := createTestEventStore()
	events.Config = config
	_ = events.Init()

	mgmt := &ManagementStore{Config: config, Events: events}
	g := &GroupStore{Config: config, Events: events, Management: mgmt}

	h := "group1"
	pk := nostr.Generate().Public()

	// Add member event
	add := nostr.Event{
		Kind:      nostr.KindSimpleGroupPutUser,
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"p", pk.Hex()}, {"h", h}},
	}
	if err := events.SignAndStoreEvent(&add, false); err != nil {
		t.Fatalf("failed to store add event: %v", err)
	}

	members := g.GetMembers(h)
	if len(members) != 1 || members[0] != pk {
		t.Fatalf("expected member present, got %v", members)
	}

	// Remove member event
	rem := nostr.Event{
		Kind:      nostr.KindSimpleGroupRemoveUser,
		CreatedAt: nostr.Now(),
		Tags:      nostr.Tags{{"p", pk.Hex()}, {"h", h}},
	}
	if err := events.SignAndStoreEvent(&rem, false); err != nil {
		t.Fatalf("failed to store remove event: %v", err)
	}

	members = g.GetMembers(h)
	if len(members) != 0 {
		t.Fatalf("expected member removed, got %v", members)
	}
}
