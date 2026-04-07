package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/joostme/conflux/internal/config"
	"github.com/joostme/conflux/internal/docker"
	envfiles "github.com/joostme/conflux/internal/env"
	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/reconcilestate"
	"github.com/joostme/conflux/internal/stacks"
)

// Reconciler manages the reconciliation loop between git state and running stacks.
type Reconciler struct {
	repoDir    string
	configFile string
	networks   *networks.Manager
	state      *reconcilestate.Store
	up         func(context.Context, stacks.Stack, string) error
	down       func(context.Context, string) error
	prune      func(context.Context) error
}

// New creates a new Reconciler.
func New(repoDir, configFile string, dockerClient *docker.Client, networkSvc *networks.Manager) *Reconciler {
	rec := &Reconciler{
		repoDir:    repoDir,
		configFile: configFile,
		networks:   networkSvc,
		state:      reconcilestate.New(),
	}
	if dockerClient != nil {
		rec.up = dockerClient.Compose().Up
		rec.down = dockerClient.Compose().Down
		rec.prune = dockerClient.Prune
	}

	return rec
}

// Reconcile performs a full reconciliation cycle:
//  1. Ensure global networks
//  2. Decrypt global secrets
//  3. Discover and deploy stacks
//  4. Remove stacks/networks that disappeared from git
//  5. Optionally prune unused Docker resources after a fully successful deploy
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
		if r.networks == nil {
			return fmt.Errorf("network manager not configured")
		}
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

	// 4. Deploy stacks (optionally in parallel)
	if len(discovered) > 0 && r.up == nil {
		return fmt.Errorf("docker client not configured")
	}

	var deployed atomic.Int64
	var failed atomic.Int64
	var skipped atomic.Int64
	repoStateKey := repoKey(r.repoDir, r.configFile)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(cfg.Stacks.ParallelDeploy)

	for _, stack := range discovered {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}

			slog.Info("processing stack", "stack", stack.Name)
			resolvedEnv, err := envResolver.ResolveForStack(stack.Dir, cfg.Stacks)
			if err != nil {
				slog.Error("failed to resolve env file for stack", "stack", stack.Name, "error", err)
				failed.Add(1)
				return nil
			}

			fingerprint, err := fingerprintStack(stack, resolvedEnv.Content)
			if err != nil {
				slog.Error("failed to fingerprint stack", "stack", stack.Name, "error", err)
				failed.Add(1)
				return nil
			}

			previousFingerprint, ok, err := r.state.Get(repoStateKey, stack.Name)
			if err != nil {
				slog.Error("failed to read reconcile state", "stack", stack.Name, "error", err)
				failed.Add(1)
				return nil
			}

			if ok && previousFingerprint == fingerprint {
				slog.Info("stack unchanged, skipping compose up", "stack", stack.Name)
				skipped.Add(1)
				return nil
			}

			envFile, err := envResolver.FileFromContent(resolvedEnv.Content)
			if err != nil {
				slog.Error("failed to write env file for stack", "stack", stack.Name, "error", err)
				failed.Add(1)
				return nil
			}

			if err := r.up(gctx, stack, envFile); err != nil {
				slog.Error("failed to deploy stack", "stack", stack.Name, "error", err)
				failed.Add(1)
				return nil
			}
			if err := r.state.Put(repoStateKey, stack.Name, fingerprint); err != nil {
				slog.Error("failed to persist reconcile state", "stack", stack.Name, "error", err)
				failed.Add(1)
				return nil
			}
			deployed.Add(1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		slog.Warn("reconciliation interrupted",
			"deployed", deployed.Load(),
			"failed", failed.Load(),
			"total", len(discovered),
		)
		return err
	}

	// 5. Remove deleted stacks
	if len(removedStacks) > 0 && r.down == nil {
		return fmt.Errorf("docker client not configured")
	}
	for _, name := range removedStacks {
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted during stack removal")
			return err
		}
		slog.Info("stack removed from repo, tearing down", "stack", name)
		if err := r.down(ctx, name); err != nil {
			slog.Error("failed to remove stack", "stack", name, "error", err)
			continue
		}
		if err := r.state.Delete(repoStateKey, name); err != nil {
			slog.Error("failed to remove reconcile state for stack", "stack", name, "error", err)
		}
	}

	// 6. Remove deleted networks (after stacks so containers are torn down first)
	if len(removedNetworks) > 0 {
		if r.networks == nil {
			return fmt.Errorf("network manager not configured")
		}
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted before network removal")
			return err
		}
		slog.Info("removing networks", "count", len(removedNetworks))
		if err := r.networks.Remove(ctx, removedNetworks); err != nil {
			slog.Error("failed to remove networks", "error", err)
		}
	}

	if failedCount := failed.Load(); failedCount > 0 {
		if cfg.Stacks.AutoPrune {
			slog.Warn("skipping automatic Docker prune because stack deployments failed", "failed", failedCount)
		}
		slog.Warn("reconciliation completed with deployment failures",
			"deployed", deployed.Load(),
			"skipped", skipped.Load(),
			"failed", failedCount,
			"removed_stacks", len(removedStacks),
			"removed_networks", len(removedNetworks),
		)
		slog.Info("reconciliation complete",
			"deployed", deployed.Load(),
			"skipped", skipped.Load(),
			"failed", failedCount,
			"removed_stacks", len(removedStacks),
			"removed_networks", len(removedNetworks),
		)
		return nil
	}

	if deployed.Load() > 0 && cfg.Stacks.AutoPrune {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.prune(ctx); err != nil {
			return fmt.Errorf("pruning Docker resources: %w", err)
		}
	}

	slog.Info("reconciliation complete",
		"deployed", deployed.Load(),
		"skipped", skipped.Load(),
		"failed", failed.Load(),
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
