package zooid

import (
	"fmt"
	"github.com/BurntSushi/toml"
	"path/filepath"
)

type Config struct {
	Self struct {
		Name        string `toml:"name"`
		Icon        string `toml:"icon"`
		Secret      string `toml:"secret"`
		Pubkey      string `toml:"pubkey"`
		Description string `toml:"description"`
	} `toml:"self"`

	Groups struct {
		Enabled   bool `toml:"enabled"`
		AutoJoin  bool `toml:"auto_join"`
		AutoLeave bool `toml:"auto_leave"`
	} `toml:"groups"`

	Roles map[string]struct {
		Pubkeys      []string `toml:"pubkeys"`
		Nip86Methods []string `toml:"nip86_methods"`
		CanInvite    bool     `toml:"can_invite"`
	} `toml:"roles"`

	Data struct {
		Sqlite string `toml:"sqlite"`
		Media  string `toml:"media"`
	} `toml:"data"`
}

func LoadConfig(hostname string) (*Config, error) {
	path := filepath.Join("configs", hostname)

	var config Config
	if _, err := toml.DecodeFile(path, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	return &config, nil
}
