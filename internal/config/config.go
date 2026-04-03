package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the conflux.yaml configuration file.
type Config struct {
	Global   GlobalConfig             `yaml:"global"`
	Stacks   StacksConfig             `yaml:"stacks"`
	Networks map[string]NetworkConfig `yaml:"networks"`
}

// GlobalConfig defines global env and secret files applied to all stacks.
type GlobalConfig struct {
	Secrets     []string `yaml:"secrets"`
	Environment []string `yaml:"environment"`
}

// StacksConfig defines how stacks are discovered.
type StacksConfig struct {
	Directory      string   `yaml:"directory"`
	File           string   `yaml:"file"`
	Secrets        []string `yaml:"secrets"`
	Environment    []string `yaml:"environment"`
	ParallelDeploy int      `yaml:"parallel_deploy"`
}

// NetworkConfig represents a single network definition in conflux.yaml.
// The fields mirror the docker compose top-level network attributes.
type NetworkConfig struct {
	Name       string            `yaml:"name"`
	Driver     string            `yaml:"driver"`
	DriverOpts map[string]string `yaml:"driver_opts"`
	EnableIPv4 *bool             `yaml:"enable_ipv4"`
	EnableIPv6 *bool             `yaml:"enable_ipv6"`
	Internal   bool              `yaml:"internal"`
	Attachable bool              `yaml:"attachable"`
	Labels     map[string]string `yaml:"labels"`
	IPAM       *IPAM             `yaml:"ipam"`
}

// IPAM mirrors docker compose IPAM configuration.
type IPAM struct {
	Driver  string            `yaml:"driver"`
	Config  []IPAMPool        `yaml:"config"`
	Options map[string]string `yaml:"options"`
}

// IPAMPool mirrors docker compose IPAM config entries.
type IPAMPool struct {
	Subnet       string            `yaml:"subnet"`
	IPRange      string            `yaml:"ip_range"`
	Gateway      string            `yaml:"gateway"`
	AuxAddresses map[string]string `yaml:"aux_addresses"`
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
	if len(cfg.Stacks.Secrets) == 0 {
		cfg.Stacks.Secrets = []string{"secrets.env"}
	}
	if len(cfg.Stacks.Environment) == 0 {
		cfg.Stacks.Environment = []string{"environment.env"}
	}
	if cfg.Stacks.ParallelDeploy <= 0 {
		cfg.Stacks.ParallelDeploy = 1
	}

	return cfg, nil
}
