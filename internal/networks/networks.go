package networks

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"

	"github.com/joostme/conflux/internal/config"
	dockernetwork "github.com/moby/moby/api/types/network"
	dockerclient "github.com/moby/moby/client"
)

// Manager manages Docker network operations using a shared client.
type Manager struct {
	cli dockerclient.APIClient
}

// NewManager creates a new network Manager with an injected Docker API client.
func NewManager(cli dockerclient.APIClient) *Manager {
	return &Manager{cli: cli}
}

// Ensure creates any configured networks that don't already exist.
func (m *Manager) Ensure(ctx context.Context, networks map[string]config.NetworkConfig) error {
	if len(networks) == 0 {
		return nil
	}

	existing, err := listExistingNames(ctx, m.cli)
	if err != nil {
		return fmt.Errorf("listing networks: %w", err)
	}

	for key, cfg := range networks {
		name := ResolveName(key, cfg)

		if existing[name] {
			slog.Info("network already exists, skipping", "network", name)
			continue
		}

		opts, err := buildCreateOptions(cfg)
		if err != nil {
			return fmt.Errorf("building options for network %s: %w", name, err)
		}

		slog.Info("creating network", "network", name, "driver", opts.Driver)
		resp, err := m.cli.NetworkCreate(ctx, name, opts)
		if err != nil {
			return fmt.Errorf("creating network %s: %w", name, err)
		}

		slog.Info("network created", "network", name, "id", resp.ID[:12])
	}

	return nil
}

// Remove removes the given networks. Errors are logged but don't halt removal of remaining networks.
func (m *Manager) Remove(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}

	for _, name := range names {
		slog.Info("removing network", "network", name)
		if _, err := m.cli.NetworkRemove(ctx, name, dockerclient.NetworkRemoveOptions{}); err != nil {
			slog.Error("failed to remove network", "network", name, "error", err)
		}
	}

	return nil
}

// ResolveName returns the effective Docker network name: the explicit "name" field or the map key.
func ResolveName(key string, cfg config.NetworkConfig) string {
	if cfg.Name != "" {
		return cfg.Name
	}
	return key
}

// ResolveNames returns the set of effective network names from a config map.
func ResolveNames(networks map[string]config.NetworkConfig) map[string]bool {
	names := make(map[string]bool, len(networks))
	for key, cfg := range networks {
		names[ResolveName(key, cfg)] = true
	}
	return names
}

// listExistingNames returns a set of all network names on the host.
func listExistingNames(ctx context.Context, cli dockerclient.APIClient) (map[string]bool, error) {
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

// buildCreateOptions converts a NetworkConfig into Docker SDK options.
func buildCreateOptions(cfg config.NetworkConfig) (dockerclient.NetworkCreateOptions, error) {
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

// convertIPAM converts a config IPAM struct into the Docker SDK type.
func convertIPAM(src *config.IPAM) (*dockernetwork.IPAM, error) {
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
