package zooid

import (
	"fiatjaf.com/nostr"
	"fmt"
	"github.com/BurntSushi/toml"
	"os"
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
		Pubkey      string `toml:"pubkey"`
		Description string `toml:"description"`
	} `toml:"info"`

	Policy struct {
		Open            bool `toml:"open"`             // Allow all authenticated users (no membership required)
		PublicJoin      bool `toml:"public_join"`
		StripSignatures bool `toml:"strip_signatures"`
	} `toml:"policy"`

	Groups struct {
		Enabled         bool `toml:"enabled"`
		AutoJoin        bool `toml:"auto_join"`
		AdminCreateOnly bool `toml:"admin_create_only"` // Only admins can create groups
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
	path   string
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

	// Save the path for later
	config.path = path

	// Make the secret... secret
	config.Secret = ""
	config.secret = secret

	return &config, nil
}

func (config *Config) Save() error {
	// Restore the secret key to the public field for saving
	config.Secret = config.secret.Hex()

	file, err := os.Create(config.path)
	if err != nil {
		return fmt.Errorf("Failed to open config file %s: %w", config.path, err)
	}
	defer file.Close()

	encoder := toml.NewEncoder(file)
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("Failed to encode config file %s: %w", config.path, err)
	}

	// Clear the secret again
	config.Secret = ""

	return nil
}

func (config *Config) SetName(name string) error {
	config.Info.Name = name

	return config.Save()
}

func (config *Config) SetDescription(description string) error {
	config.Info.Description = description

	return config.Save()
}

func (config *Config) SetIcon(icon string) error {
	config.Info.Icon = icon

	return config.Save()
}

func (config *Config) Sign(event *nostr.Event) error {
	return event.Sign(config.secret)
}

func (config *Config) GetSelf() nostr.PubKey {
	return config.secret.Public()
}

func (config *Config) IsSelf(pubkey nostr.PubKey) bool {
	return pubkey == config.GetSelf()
}

func (config *Config) GetOwner() nostr.PubKey {
	return nostr.MustPubKeyFromHex(config.Info.Pubkey)
}

func (config *Config) IsOwner(pubkey nostr.PubKey) bool {
	return pubkey == config.GetOwner()
}

func (config *Config) GetAssignedRoles(pubkey nostr.PubKey) []Role {
	roles := make([]Role, 0)
	for _, role := range config.Roles {
		if slices.Contains(role.Pubkeys, pubkey.Hex()) {
			roles = append(roles, role)
		}
	}

	return roles
}

func (config *Config) GetAllRoles(pubkey nostr.PubKey) []Role {
	roles := make([]Role, 0)
	for name, role := range config.Roles {
		if name == "member" {
			roles = append(roles, role)
		} else if slices.Contains(role.Pubkeys, pubkey.Hex()) {
			roles = append(roles, role)
		}
	}

	return roles
}

func (config *Config) CanInvite(pubkey nostr.PubKey) bool {
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

func (config *Config) CanManage(pubkey nostr.PubKey) bool {
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
