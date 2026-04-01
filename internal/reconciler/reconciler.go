package reconciler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/joostme/conflux/internal/config"
	envfiles "github.com/joostme/conflux/internal/env"
	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/stacks"
)

// ComposeService deploys and tears down Docker Compose stacks.
type ComposeService interface {
	Up(ctx context.Context, stack stacks.Stack, envFiles []string) error
	Down(ctx context.Context, stackName string) error
}

// NetworkService manages Docker networks.
type NetworkService interface {
	Ensure(ctx context.Context, networks map[string]config.NetworkConfig) error
	Remove(ctx context.Context, names []string) error
}

// Reconciler manages the reconciliation loop between git state and running stacks.
type Reconciler struct {
	repoDir    string
	configFile string
	compose    ComposeService
	networks   NetworkService
}

// New creates a new Reconciler.
func New(repoDir, configFile string, compose ComposeService, networkSvc NetworkService) *Reconciler {
	return &Reconciler{
		repoDir:    repoDir,
		configFile: configFile,
		compose:    compose,
		networks:   networkSvc,
	}
}

// Reconcile performs a full reconciliation cycle:
//  1. Ensure global networks
//  2. Decrypt global secrets
//  3. Discover and deploy stacks
//  4. Remove stacks/networks that disappeared from git
//
// Networks are ensured before stacks and removed after stacks so that
// dependencies are respected. Context cancellation is checked between
// each major phase and individual stack operation.
func (r *Reconciler) Reconcile(ctx context.Context, removedStacks, removedNetworks []string) error {
	slog.Info("starting reconciliation")

	// 1. Parse config
	cfg, err := r.loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	slog.Info("config loaded",
		"stacks_dir", cfg.Stacks.Directory,
		"global_env", cfg.Global.Environment,
		"global_secrets", cfg.Global.Secrets,
		"networks", len(cfg.Networks),
	)

	envResolver, err := envfiles.NewResolver(r.repoDir, cfg)
	if err != nil {
		return fmt.Errorf("resolving global env files: %w", err)
	}
	defer func() {
		if err := envResolver.Cleanup(); err != nil {
			slog.Warn("failed to clean up env temp files", "error", err)
		}
	}()

	// 2. Ensure global networks exist before any stacks are deployed
	if len(cfg.Networks) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		slog.Info("ensuring networks", "count", len(cfg.Networks))
		if err := r.networks.Ensure(ctx, cfg.Networks); err != nil {
			return fmt.Errorf("ensuring networks: %w", err)
		}
	}

	// 3. Discover stacks
	discovered, err := stacks.Discover(r.repoDir, cfg)
	if err != nil {
		return fmt.Errorf("discovering stacks: %w", err)
	}
	slog.Info("stacks discovered", "count", len(discovered))

	// 4. Deploy each stack
	deployed := 0
	for _, stack := range discovered {
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted", "deployed", deployed, "remaining", len(discovered)-deployed)
			return err
		}
		err := r.deployStack(ctx, stack, cfg, envResolver)
		if err != nil {
			slog.Error("failed to deploy stack", "stack", stack.Name, "error", err)
			// Continue with other stacks
			deployed++
			continue
		}
		deployed++
	}

	// 5. Remove deleted stacks
	for _, name := range removedStacks {
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted during stack removal")
			return err
		}
		slog.Info("stack removed from repo, tearing down", "stack", name)
		if err := r.compose.Down(ctx, name); err != nil {
			slog.Error("failed to remove stack", "stack", name, "error", err)
		}
	}

	// 6. Remove deleted networks (after stacks so containers are torn down first)
	if len(removedNetworks) > 0 {
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted before network removal")
			return err
		}
		slog.Info("removing networks", "count", len(removedNetworks))
		if err := r.networks.Remove(ctx, removedNetworks); err != nil {
			slog.Error("failed to remove networks", "error", err)
		}
	}

	slog.Info("reconciliation complete",
		"deployed", deployed,
		"removed_stacks", len(removedStacks),
		"removed_networks", len(removedNetworks),
	)
	return nil
}

// deployStack resolves env files and deploys a single stack.
func (r *Reconciler) deployStack(ctx context.Context, stack stacks.Stack, cfg *config.Config, envResolver *envfiles.Resolver) error {
	slog.Info("processing stack", "stack", stack.Name)

	envFiles, err := envResolver.FilesForStack(stack.Dir, cfg.Stacks)
	if err != nil {
		return fmt.Errorf("resolving env files for %s: %w", stack.Name, err)
	}

	if err := r.compose.Up(ctx, stack, envFiles); err != nil {
		return err
	}
	return nil
}

// loadConfig loads and returns the parsed configuration.
func (r *Reconciler) loadConfig() (*config.Config, error) {
	return config.Load(r.repoDir, r.configFile)
}

// Snapshot returns stack and network names from the current worktree.
func (r *Reconciler) Snapshot() (stackNames map[string]bool, networkNames map[string]bool, err error) {
	cfg, err := r.loadConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	stackNames, err = discoverStackNames(r.repoDir, cfg)
	if err != nil {
		return nil, nil, err
	}

	networkNames = networks.ResolveNames(cfg.Networks)
	return stackNames, networkNames, nil
}

// discoverStackNames returns stack names using an already-loaded config.
func discoverStackNames(repoDir string, cfg *config.Config) (map[string]bool, error) {
	discovered, err := stacks.Discover(repoDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("discovering stacks: %w", err)
	}
	names := make(map[string]bool, len(discovered))
	for _, s := range discovered {
		names[s.Name] = true
	}
	return names, nil
}
