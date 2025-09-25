package zooid

import (
	"fiatjaf.com/nostr"
	"fmt"
	"github.com/BurntSushi/toml"
	"path/filepath"
	"slices"
)

type Role struct {
	Pubkeys   []string `toml:"pubkeys"`
	CanInvite bool     `toml:"can_invite"`
	CanManage bool     `toml:"can_manage"`
}

type Config struct {
	Self struct {
		Name        string `toml:"name"`
		Icon        string `toml:"icon"`
		Schema      string `toml:"schema"`
		Secret      string `toml:"secret"`
		Pubkey      string `toml:"pubkey"`
		Description string `toml:"description"`
	} `toml:"self"`

	Groups struct {
		Enabled   bool `toml:"enabled"`
		AutoJoin  bool `toml:"auto_join"`
		AutoLeave bool `toml:"auto_leave"`
	} `toml:"groups"`

	Management struct {
		Enabled bool     `toml:"enabled"`
		Methods []string `toml:"methods"`
	} `toml:"management"`

	Blossom struct {
		Enabled bool `toml:"enabled"`
	} `toml:"blossom"`

	Roles map[string]Role `toml:"roles"`
}

func LoadConfig(hostname string) (*Config, error) {
	path := filepath.Join("configs", hostname)

	var config Config
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	return &config, nil
}

func (config *Config) IsSelf(pubkey nostr.PubKey) bool {
	return pubkey == nostr.MustSecretKeyFromHex(config.Self.Secret).Public()
}

func (config *Config) IsOwner(pubkey nostr.PubKey) bool {
	return pubkey == nostr.MustPubKeyFromHex(config.Self.Pubkey)
}

func (config *Config) GetRolesForPubkey(pubkey nostr.PubKey) []Role {
	roles := make([]Role, 0)
	for name, role := range config.Roles {
		if name == "member" {
			roles = append(roles, role)
		}

		if slices.Contains(role.Pubkeys, pubkey.Hex()) {
			roles = append(roles, role)
		}
	}

	return roles
}

func (config *Config) CanManage(roles []Role) bool {
	for _, role := range roles {
		if role.CanManage {
			return true
		}
	}

	return false
}
