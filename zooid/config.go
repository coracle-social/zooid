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

	Management struct {
		Enabled bool     `toml:"enabled"`
		Methods []string `toml:"methods"`
	} `toml:"management"`

	Blossom struct {
		Enabled   bool   `toml:"enabled"`
		Directory string `toml:"directory"`
	} `toml:"blossom"`

	Roles map[string]struct {
		Pubkeys      []string `toml:"pubkeys"`
		CanInvite    bool     `toml:"can_invite"`
	} `toml:"roles"`

	Data struct {
		Events string `toml:"events"`
		Blossom string `toml:"blossom"`
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
