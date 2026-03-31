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
// Previously, Ensure() and Remove() each created their own dockerclient.Client
// on every call. The Manager creates it once and reuses it.
type Manager struct {
	cli    *dockerclient.Client
	logger *slog.Logger
}

// NewManager creates a new network Manager with a shared Docker client.
func NewManager(logger *slog.Logger) (*Manager, error) {
	cli, err := dockerclient.New(dockerclient.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}
	return &Manager{cli: cli, logger: logger}, nil
}

// Close releases the underlying Docker client resources.
func (m *Manager) Close() error {
	return m.cli.Close()
}

// Ensure checks whether each configured network already exists on the Docker
// host (matched by name). Networks that already exist are skipped with an
// INFO log. Missing networks are created with the full set of options from
// the config.
func (m *Manager) Ensure(ctx context.Context, networks map[string]config.NetworkConfig) error {
	if len(networks) == 0 {
		return nil
	}

	// Build a set of existing network names for fast lookup.
	existing, err := listExistingNames(ctx, m.cli)
	if err != nil {
		return fmt.Errorf("listing networks: %w", err)
	}

	for key, cfg := range networks {
		name := ResolveName(key, cfg)

		if existing[name] {
			m.logger.Info("network already exists, skipping", "network", name)
			continue
		}

		opts, err := buildCreateOptions(cfg)
		if err != nil {
			return fmt.Errorf("building options for network %s: %w", name, err)
		}

		m.logger.Info("creating network", "network", name, "driver", opts.Driver)
		resp, err := m.cli.NetworkCreate(ctx, name, opts)
		if err != nil {
			return fmt.Errorf("creating network %s: %w", name, err)
		}

		m.logger.Info("network created", "network", name, "id", resp.ID[:12])
	}

	return nil
}

// Remove removes the given networks from the Docker host. Networks that
// don't exist are silently skipped. This only removes networks by the
// exact names provided — it will never touch networks not in the list.
func (m *Manager) Remove(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}

	for _, name := range names {
		m.logger.Info("removing network", "network", name)
		if _, err := m.cli.NetworkRemove(ctx, name, dockerclient.NetworkRemoveOptions{}); err != nil {
			// Log but continue — the network might already be gone, or
			// still in use by a container that hasn't been torn down yet.
			m.logger.Error("failed to remove network", "network", name, "error", err)
		}
	}

	return nil
}

// ResolveName returns the effective Docker network name for a config entry.
// The effective name is either the explicit "name" field or the map key,
// exactly like docker compose does it.
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

// convertIPAM turns our YAML-friendly IPAM struct into the Docker SDK type.
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
