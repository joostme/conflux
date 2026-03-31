package networks

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"

	dockernetwork "github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

// IPAMPool mirrors docker compose IPAM config entries.
type IPAMPool struct {
	Subnet       string            `yaml:"subnet"`
	IPRange      string            `yaml:"ip_range"`
	Gateway      string            `yaml:"gateway"`
	AuxAddresses map[string]string `yaml:"aux_addresses"`
}

// IPAM mirrors docker compose IPAM configuration.
type IPAM struct {
	Driver  string            `yaml:"driver"`
	Config  []IPAMPool        `yaml:"config"`
	Options map[string]string `yaml:"options"`
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

// Ensure checks whether each configured network already exists on the Docker
// host (matched by name). Networks that already exist are skipped with an
// INFO log. Missing networks are created with the full set of options from
// the config.
func Ensure(ctx context.Context, networks map[string]NetworkConfig, logger *slog.Logger) error {
	if len(networks) == 0 {
		return nil
	}

	cli, err := dockerclient.New(dockerclient.FromEnv)
	if err != nil {
		return fmt.Errorf("creating docker client: %w", err)
	}
	defer cli.Close()

	// Build a set of existing network names for fast lookup.
	existing, err := listExistingNames(ctx, cli)
	if err != nil {
		return fmt.Errorf("listing networks: %w", err)
	}

	for key, cfg := range networks {
		// The effective name is either the explicit "name" field or the
		// map key, exactly like docker compose does it.
		name := cfg.Name
		if name == "" {
			name = key
		}

		if existing[name] {
			logger.Info("network already exists, skipping", "network", name)
			continue
		}

		opts, err := buildCreateOptions(cfg)
		if err != nil {
			return fmt.Errorf("building options for network %s: %w", name, err)
		}

		logger.Info("creating network", "network", name, "driver", opts.Driver)
		resp, err := cli.NetworkCreate(ctx, name, opts)
		if err != nil {
			return fmt.Errorf("creating network %s: %w", name, err)
		}

		logger.Info("network created", "network", name, "id", resp.ID[:12])
	}

	return nil
}

// listExistingNames returns a set of all network names on the host.
func listExistingNames(ctx context.Context, cli *dockerclient.Client) (map[string]bool, error) {
	result, err := cli.NetworkList(ctx, dockerclient.NetworkListOptions{})
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(result.Items))
	for _, n := range result.Items {
		names[n.Name] = true
	}
	return names, nil
}

// buildCreateOptions converts our config struct into the Docker SDK's
// NetworkCreateOptions.
func buildCreateOptions(cfg NetworkConfig) (dockerclient.NetworkCreateOptions, error) {
	opts := dockerclient.NetworkCreateOptions{
		Driver:     cfg.Driver,
		Internal:   cfg.Internal,
		Attachable: cfg.Attachable,
		Options:    cfg.DriverOpts,
		Labels:     cfg.Labels,
		EnableIPv4: cfg.EnableIPv4,
		EnableIPv6: cfg.EnableIPv6,
	}

	if cfg.IPAM != nil {
		ipam, err := convertIPAM(cfg.IPAM)
		if err != nil {
			return opts, err
		}
		opts.IPAM = ipam
	}

	return opts, nil
}

// convertIPAM turns our YAML-friendly IPAM struct into the Docker SDK type.
func convertIPAM(src *IPAM) (*dockernetwork.IPAM, error) {
	ipam := &dockernetwork.IPAM{
		Driver:  src.Driver,
		Options: src.Options,
	}

	for i, pool := range src.Config {
		c := dockernetwork.IPAMConfig{}

		if pool.Subnet != "" {
			prefix, err := netip.ParsePrefix(pool.Subnet)
			if err != nil {
				return nil, fmt.Errorf("IPAM config[%d] subnet %q: %w", i, pool.Subnet, err)
			}
			c.Subnet = prefix
		}

		if pool.IPRange != "" {
			prefix, err := netip.ParsePrefix(pool.IPRange)
			if err != nil {
				return nil, fmt.Errorf("IPAM config[%d] ip_range %q: %w", i, pool.IPRange, err)
			}
			c.IPRange = prefix
		}

		if pool.Gateway != "" {
			addr, err := netip.ParseAddr(pool.Gateway)
			if err != nil {
				return nil, fmt.Errorf("IPAM config[%d] gateway %q: %w", i, pool.Gateway, err)
			}
			c.Gateway = addr
		}

		if len(pool.AuxAddresses) > 0 {
			c.AuxAddress = make(map[string]netip.Addr, len(pool.AuxAddresses))
			for host, addrStr := range pool.AuxAddresses {
				addr, err := netip.ParseAddr(addrStr)
				if err != nil {
					return nil, fmt.Errorf("IPAM config[%d] aux_address %s=%q: %w", i, host, addrStr, err)
				}
				c.AuxAddress[host] = addr
			}
		}

		ipam.Config = append(ipam.Config, c)
	}

	return ipam, nil
}
