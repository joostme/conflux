package reconciler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/joostme/conflux/internal/config"
	"github.com/joostme/conflux/internal/docker"
	envfiles "github.com/joostme/conflux/internal/env"
	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/stacks"
)

// Reconciler manages the reconciliation loop between git state and running stacks.
type Reconciler struct {
	repoDir    string
	configFile string
	compose    *docker.ComposeClient
	networks   *networks.Manager
}

// New creates a new Reconciler.
func New(repoDir, configFile string, compose *docker.ComposeClient, networkSvc *networks.Manager) *Reconciler {
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
	cfg, err := config.Load(r.repoDir, r.configFile)
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

		slog.Info("processing stack", "stack", stack.Name)
		envFiles, err := envResolver.FilesForStack(stack.Dir, cfg.Stacks)
		if err != nil {
			slog.Error("failed to resolve env files for stack", "stack", stack.Name, "error", err)
			deployed++
			continue
		}
		if err := r.compose.Up(ctx, stack, envFiles); err != nil {
			slog.Error("failed to deploy stack", "stack", stack.Name, "error", err)
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

// Snapshot returns stack and network names from the current worktree.
func (r *Reconciler) Snapshot() (stackNames map[string]bool, networkNames map[string]bool, err error) {
	cfg, err := config.Load(r.repoDir, r.configFile)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	discovered, err := stacks.Discover(r.repoDir, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("discovering stacks: %w", err)
	}
	stackNames = make(map[string]bool, len(discovered))
	for _, s := range discovered {
		stackNames[s.Name] = true
	}

	networkNames = networks.ResolveNames(cfg.Networks)
	return stackNames, networkNames, nil
}
