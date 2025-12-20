package zooid

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/BurntSushi/toml"
	"github.com/nbd-wtf/go-nostr"
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
	} `toml:"info"`

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
	secret string
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

	secret := config.Secret
	if _, err := nostr.GetPublicKey(secret); err != nil {
		return nil, err
	}

	// Make the secret... secret
	config.Secret = ""
	config.secret = secret

	return &config, nil
}

func (config *Config) Sign(event *nostr.Event) error {
	return event.Sign(config.secret)
}

func (config *Config) GetSelf() string {
	pubkey, _ := nostr.GetPublicKey(config.secret)
	return pubkey
}

func (config *Config) IsSelf(pubkey string) bool {
	return pubkey == config.GetSelf()
}

func (config *Config) GetOwner() string {
	return config.Info.Pubkey
}

func (config *Config) IsOwner(pubkey string) bool {
	return pubkey == config.GetOwner()
}

func (config *Config) GetAssignedRoles(pubkey string) []Role {
	roles := make([]Role, 0)
	for _, role := range config.Roles {
		if slices.Contains(role.Pubkeys, pubkey) {
			roles = append(roles, role)
		}
	}

	return roles
}

func (config *Config) GetAllRoles(pubkey string) []Role {
	roles := make([]Role, 0)
	for name, role := range config.Roles {
		if name == "member" {
			roles = append(roles, role)
		} else if slices.Contains(role.Pubkeys, pubkey) {
			roles = append(roles, role)
		}
	}

	return roles
}

func (config *Config) CanInvite(pubkey string) bool {
	if config.IsOwner(pubkey) || config.IsSelf(pubkey) {
		return true
	}

	for _, role := range config.GetAllRoles(pubkey) {
		if role.CanInvite {
			return true
		}
	}

	return false
}

func (config *Config) CanManage(pubkey string) bool {
	if config.IsOwner(pubkey) || config.IsSelf(pubkey) {
		return true
	}

	for _, role := range config.GetAllRoles(pubkey) {
		if role.CanManage {
			return true
		}
	}

	return false
}
