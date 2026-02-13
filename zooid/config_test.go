package zooid

import (
	"testing"

	"fiatjaf.com/nostr"
)

func TestConfig_IsOwner(t *testing.T) {
	ownerPubkey := nostr.MustPubKeyFromHex("1234567890123456789012345678901234567890123456789012345678901234")
	otherPubkey := nostr.MustPubKeyFromHex("abcdef1234567890123456789012345678901234567890123456789012345678")

	config := &Config{
		Info: struct {
			Name        string `toml:"name"`
			Icon        string `toml:"icon"`
			Pubkey      string `toml:"pubkey"`
			Description string `toml:"description"`
		}{
			Pubkey: ownerPubkey.Hex(),
		},
	}

	if !config.IsOwner(ownerPubkey) {
		t.Error("IsOwner() should return true for owner pubkey")
	}

	if config.IsOwner(otherPubkey) {
		t.Error("IsOwner() should return false for non-owner pubkey")
	}
}

func TestConfig_IsSelf(t *testing.T) {
	secret := nostr.Generate()
	selfPubkey := secret.Public()
	otherPubkey := nostr.MustPubKeyFromHex("abcdef1234567890123456789012345678901234567890123456789012345678")

	config := &Config{
		secret: secret,
	}

	if !config.IsSelf(selfPubkey) {
		t.Error("IsSelf() should return true for self pubkey")
	}

	if config.IsSelf(otherPubkey) {
		t.Error("IsSelf() should return false for non-self pubkey")
	}
}

func TestConfig_GetAllRoles(t *testing.T) {
	pubkey1 := nostr.MustPubKeyFromHex("1234567890123456789012345678901234567890123456789012345678901234")
	pubkey2 := nostr.MustPubKeyFromHex("abcdef1234567890123456789012345678901234567890123456789012345678")

	config := &Config{
		Roles: map[string]Role{
			"member": {
				Pubkeys:   []string{},
				CanInvite: true,
			},
			"admin": {
				Pubkeys:   []string{pubkey1.Hex()},
				CanManage: true,
			},
			"moderator": {
				Pubkeys:   []string{pubkey2.Hex()},
				CanInvite: true,
			},
		},
	}

	roles := config.GetAllRoles(pubkey1)
	if len(roles) != 2 {
		t.Errorf("GetAllRoles() returned %d roles, want 2", len(roles))
	}

	roles = config.GetAllRoles(pubkey2)
	if len(roles) != 2 {
		t.Errorf("GetAllRoles() returned %d roles, want 2", len(roles))
	}
}

func TestConfig_CanManage(t *testing.T) {
	ownerPubkey := nostr.MustPubKeyFromHex("9999999999999999999999999999999999999999999999999999999999999999")
	adminPubkey := nostr.MustPubKeyFromHex("1234567890123456789012345678901234567890123456789012345678901234")
	userPubkey := nostr.MustPubKeyFromHex("abcdef1234567890123456789012345678901234567890123456789012345678")

	config := &Config{
		secret: nostr.Generate(),
		Info: struct {
			Name        string `toml:"name"`
			Icon        string `toml:"icon"`
			Pubkey      string `toml:"pubkey"`
			Description string `toml:"description"`
		}{
			Pubkey: ownerPubkey.Hex(),
		},
		Roles: map[string]Role{
			"admin": {
				Pubkeys:   []string{adminPubkey.Hex()},
				CanManage: true,
			},
			"user": {
				Pubkeys:   []string{userPubkey.Hex()},
				CanManage: false,
			},
		},
	}

	if !config.CanManage(adminPubkey) {
		t.Error("CanManage() should return true for admin")
	}

	if config.CanManage(userPubkey) {
		t.Error("CanManage() should return false for regular user")
	}
}

func TestConfig_CanInvite(t *testing.T) {
	ownerPubkey := nostr.MustPubKeyFromHex("9999999999999999999999999999999999999999999999999999999999999999")
	inviterPubkey := nostr.MustPubKeyFromHex("1234567890123456789012345678901234567890123456789012345678901234")
	userPubkey := nostr.MustPubKeyFromHex("abcdef1234567890123456789012345678901234567890123456789012345678")

	config := &Config{
		secret: nostr.Generate(),
		Info: struct {
			Name        string `toml:"name"`
			Icon        string `toml:"icon"`
			Pubkey      string `toml:"pubkey"`
			Description string `toml:"description"`
		}{
			Pubkey: ownerPubkey.Hex(),
		},
		Roles: map[string]Role{
			"inviter": {
				Pubkeys:   []string{inviterPubkey.Hex()},
				CanInvite: true,
			},
			"user": {
				Pubkeys:   []string{userPubkey.Hex()},
				CanInvite: false,
			},
		},
	}

	if !config.CanInvite(inviterPubkey) {
		t.Error("CanInvite() should return true for inviter")
	}

	if config.CanInvite(userPubkey) {
		t.Error("CanInvite() should return false for regular user")
	}
}

func TestConfig_MemberRole(t *testing.T) {
	ownerPubkey := nostr.MustPubKeyFromHex("9999999999999999999999999999999999999999999999999999999999999999")
	anyPubkey := nostr.MustPubKeyFromHex("1234567890123456789012345678901234567890123456789012345678901234")

	config := &Config{
		secret: nostr.Generate(),
		Info: struct {
			Name        string `toml:"name"`
			Icon        string `toml:"icon"`
			Pubkey      string `toml:"pubkey"`
			Description string `toml:"description"`
		}{
			Pubkey: ownerPubkey.Hex(),
		},
		Roles: map[string]Role{
			"member": {
				Pubkeys:   []string{},
				CanInvite: true,
			},
		},
	}

	roles := config.GetAllRoles(anyPubkey)
	if len(roles) != 1 {
		t.Errorf("GetAllRoles() should return member role for any pubkey, got %d roles", len(roles))
	}

	if !config.CanInvite(anyPubkey) {
		t.Error("Any pubkey should have member role permissions")
	}
}
