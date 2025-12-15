package zooid

import (
	"testing"

	"fiatjaf.com/nostr"
)

func TestManagementStore_GetAllowedPubkeyItems_IncludesMembers(t *testing.T) {
	mgmt := createTestManagementStore()

	pk := nostr.Generate().Public()
	if err := mgmt.AddMember(pk); err != nil {
		t.Fatalf("AddMember failed: %v", err)
	}

	items := mgmt.GetAllowedPubkeyItems()

	found := false
	for _, it := range items {
		if it.PubKey == pk && it.Reason == "relay member" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected allowed list to include added member %s", pk.Hex())
	}
}
