package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joostme/conflux/internal/networks"
	"gopkg.in/yaml.v3"
)

// Config represents the conflux.yaml configuration file.
type Config struct {
	Global   GlobalConfig                      `yaml:"global"`
	Stacks   StacksConfig                      `yaml:"stacks"`
	Networks map[string]networks.NetworkConfig `yaml:"networks"`
}

// GlobalConfig defines global env and secret files applied to all stacks.
type GlobalConfig struct {
	Secrets     []string `yaml:"secrets"`
	Environment []string `yaml:"environment"`
}

// StacksConfig defines how stacks are discovered.
type StacksConfig struct {
	Directory   string `yaml:"directory"`
	File        string `yaml:"file"`
	Secrets     string `yaml:"secrets"`
	Environment string `yaml:"environment"`
}

// Load reads and parses the config file from the given repo directory.
func Load(repoDir, configFile string) (*Config, error) {
	path := filepath.Join(repoDir, configFile)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	// Apply defaults
	if cfg.Stacks.Directory == "" {
		cfg.Stacks.Directory = "stacks"
	}
	if cfg.Stacks.File == "" {
		cfg.Stacks.File = "compose.yaml"
	}
	if cfg.Stacks.Secrets == "" {
		cfg.Stacks.Secrets = "secrets.env"
	}
	if cfg.Stacks.Environment == "" {
		cfg.Stacks.Environment = "environment.env"
	}

	return cfg, nil
}
