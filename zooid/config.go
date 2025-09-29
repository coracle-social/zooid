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
	Host   string `toml:"host"`
	Schema string `toml:"schema"`
	Secret string `toml:"secret"`
	Info   struct {
		Name        string `toml:"name"`
		Icon        string `toml:"icon"`
		Secret      string `toml:"secret"`
		Pubkey      string `toml:"pubkey"`
		Description string `toml:"description"`
	} `toml:"self"`

	Policy struct {
		StripSignatures bool `toml:"strip_signatures"`
	} `toml:"policy"`

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

	// Private/parsed values
	secret nostr.SecretKey
}

func LoadConfig(filename string) (*Config, error) {
	path := filepath.Join(Env("CONFIG"), filename)

	var config Config
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, fmt.Errorf("Failed to parse config file %s: %w", path, err)
	}

	if config.Host == "" {
		return nil, fmt.Errorf("host is required")
	}

	if config.Schema == "" {
		return nil, fmt.Errorf("schema is required")
	}

	secret, err := nostr.SecretKeyFromHex(config.Secret)
	if err != nil {
		return nil, err
	}

	// Make the secret... secret
	config.Secret = ""
	config.secret = secret

	return &config, nil
}

func (config *Config) GetSelf() nostr.PubKey {
	return config.secret.Public()
}

func (config *Config) IsSelf(pubkey nostr.PubKey) bool {
	return pubkey == config.GetSelf()
}

func (config *Config) Sign(event *nostr.Event) error {
	return event.Sign(config.secret)
}

func (config *Config) IsOwner(pubkey nostr.PubKey) bool {
	return pubkey.Hex() == config.Info.Pubkey
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

func (config *Config) CanManage(pubkey nostr.PubKey) bool {
	for _, role := range config.GetRolesForPubkey(pubkey) {
		if role.CanManage {
			return true
		}
	}

	return false
}

func (config *Config) CanInvite(pubkey nostr.PubKey) bool {
	for _, role := range config.GetRolesForPubkey(pubkey) {
		if role.CanInvite {
			return true
		}
	}

	return false
}

func (config *Config) IsAdmin(pubkey nostr.PubKey) bool {
	if config.IsOwner(pubkey) {
		return true
	}

	if config.IsSelf(pubkey) {
		return true
	}

	if config.CanManage(pubkey) {
		return true
	}

	return false
}
