package zooid

import (
	"testing"

	"fiatjaf.com/nostr"
)

func createTestManagementStore() *ManagementStore {
	config := &Config{
		Host:   "test.com",
		Secret: nostr.Generate(),
	}
	schema := &Schema{Name: "test_" + RandomString(8)}
	events := &EventStore{
		Config: config,
		Schema: schema,
	}
	events.Init()

	return &ManagementStore{
		Config: config,
		Events: events,
	}
}

func TestManagementStore_BanPubkey(t *testing.T) {
	mgmt := createTestManagementStore()

	pubkey := nostr.Generate().Public()
	reason := "spam"

	// Note: BanPubkey might return "duplicate event" error due to implementation
	// but the banning should still work
	mgmt.BanPubkey(pubkey, reason)

	// Test that pubkey is now banned
	if !mgmt.PubkeyIsBanned(pubkey) {
		t.Error("PubkeyIsBanned() should return true after banning")
	}

	// Test banned pubkey list
	bannedPubkeys := mgmt.GetBannedPubkeys()
	found := false
	for _, banned := range bannedPubkeys {
		if banned == pubkey {
			found = true
			break
		}
	}
	if !found {
		t.Error("GetBannedPubkeys() should include banned pubkey")
	}

	// Test banned pubkey items
	bannedItems := mgmt.GetBannedPubkeyItems()
	itemFound := false
	for _, item := range bannedItems {
		if item.Pubkey == pubkey && item.Reason == reason {
			itemFound = true
			break
		}
	}
	if !itemFound {
		t.Error("GetBannedPubkeyItems() should include banned pubkey with reason")
	}
}

func TestManagementStore_AllowPubkey(t *testing.T) {
	mgmt := createTestManagementStore()

	pubkey := nostr.Generate().Public()

	// Ban then allow
	mgmt.BanPubkey(pubkey, "test")

	if !mgmt.PubkeyIsBanned(pubkey) {
		t.Error("Setup: pubkey should be banned")
	}

	mgmt.AllowPubkey(pubkey, "unbanned")

	if mgmt.PubkeyIsBanned(pubkey) {
		t.Error("PubkeyIsBanned() should return false after allowing")
	}
}

func TestManagementStore_BanEvent(t *testing.T) {
	mgmt := createTestManagementStore()

	eventID := nostr.MustIDFromHex("1234567890123456789012345678901234567890123456789012345678901234")
	reason := "inappropriate"

	mgmt.BanEvent(eventID, reason)

	// Test that event is now banned
	if !mgmt.EventIsBanned(eventID) {
		t.Error("EventIsBanned() should return true after banning")
	}

	// Test banned event list
	bannedEvents := mgmt.GetBannedEvents()
	found := false
	for _, banned := range bannedEvents {
		if banned == eventID {
			found = true
			break
		}
	}
	if !found {
		t.Error("GetBannedEvents() should include banned event")
	}

	// Test banned event items
	bannedItems := mgmt.GetBannedEventItems()
	itemFound := false
	for _, item := range bannedItems {
		if item.ID == eventID && item.Reason == reason {
			itemFound = true
			break
		}
	}
	if !itemFound {
		t.Error("GetBannedEventItems() should include banned event with reason")
	}
}

func TestManagementStore_AllowEvent(t *testing.T) {
	mgmt := createTestManagementStore()

	eventID := nostr.MustIDFromHex("1234567890123456789012345678901234567890123456789012345678901234")

	// Ban then allow
	mgmt.BanEvent(eventID, "test")

	if !mgmt.EventIsBanned(eventID) {
		t.Error("Setup: event should be banned")
	}

	mgmt.AllowEvent(eventID, "unbanned")

	if mgmt.EventIsBanned(eventID) {
		t.Error("EventIsBanned() should return false after allowing")
	}
}

func TestManagementStore_PubkeyIsBanned_NotBanned(t *testing.T) {
	mgmt := createTestManagementStore()

	pubkey := nostr.Generate().Public()

	if mgmt.PubkeyIsBanned(pubkey) {
		t.Error("PubkeyIsBanned() should return false for non-banned pubkey")
	}
}

func TestManagementStore_EventIsBanned_NotBanned(t *testing.T) {
	mgmt := createTestManagementStore()

	eventID := nostr.MustIDFromHex("abcdef1234567890123456789012345678901234567890123456789012345678")

	if mgmt.EventIsBanned(eventID) {
		t.Error("EventIsBanned() should return false for non-banned event")
	}
}