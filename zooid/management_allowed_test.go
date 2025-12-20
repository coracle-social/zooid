package zooid

import (
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestManagementStore_GetAllowedPubkeyItems_IncludesMembers(t *testing.T) {
	mgmt := createTestManagementStore()

	secret := nostr.GeneratePrivateKey()
	pk, _ := nostr.GetPublicKey(secret)
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
		t.Fatalf("expected allowed list to include added member %s", pk)
	}
}
