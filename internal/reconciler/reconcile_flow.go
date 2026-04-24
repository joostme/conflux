package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/joostme/conflux/internal/config"
	envfiles "github.com/joostme/conflux/internal/env"
	"github.com/joostme/conflux/internal/stacks"
)

type reconcileSetup struct {
	cfg          *config.Config
	envResolver  *envfiles.Resolver
	discovered   []stacks.Stack
	repoStateKey string
}

type deployCounts struct {
	deployed atomic.Int64
	failed   atomic.Int64
	skipped  atomic.Int64
}

type deploySummary struct {
	deployed int
	failed   int
	skipped  int
}

func (r *Reconciler) prepareReconcile(ctx context.Context) (reconcileSetup, error) {
	setup := reconcileSetup{}

	cfg, err := r.loadConfig()
	if err != nil {
		return setup, fmt.Errorf("loading config: %w", err)
	}
	slog.Info("config loaded",
		"stacks_dir", cfg.Stacks.Directory,
		"global_env", cfg.Global.Environment,
		"global_secrets", cfg.Global.Secrets,
		"networks", len(cfg.Networks),
	)

	envResolver, err := envfiles.NewResolver(r.repoDir, cfg)
	if err != nil {
		return setup, fmt.Errorf("resolving global env files: %w", err)
	}

	if err := r.ensureNetworks(ctx, cfg); err != nil {
		r.cleanupEnvResolver(envResolver)
		return setup, err
	}

	discovered, err := stacks.Discover(r.repoDir, cfg)
	if err != nil {
		r.cleanupEnvResolver(envResolver)
		return setup, fmt.Errorf("discovering stacks: %w", err)
	}
	slog.Info("stacks discovered", "count", len(discovered))

	if len(discovered) > 0 && r.up == nil {
		r.cleanupEnvResolver(envResolver)
		return setup, fmt.Errorf("docker client not configured")
	}

	setup.cfg = cfg
	setup.envResolver = envResolver
	setup.discovered = discovered
	setup.repoStateKey = repoKey(r.repoDir, r.configFile)
	return setup, nil
}

func (r *Reconciler) ensureNetworks(ctx context.Context, cfg *config.Config) error {
	if len(cfg.Networks) == 0 {
		return nil
	}
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
	return nil
}

func (r *Reconciler) deployStacks(ctx context.Context, setup reconcileSetup, result *Result) (deploySummary, error) {
	counts := deployCounts{}
	summary := deploySummary{}
	var resultMu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(setup.cfg.Stacks.ParallelDeploy)

	for _, stack := range setup.discovered {
		g.Go(func() error {
			return r.deployStack(gctx, setup, stack, result, &resultMu, &counts)
		})
	}

	if err := g.Wait(); err != nil {
		slog.Warn("reconciliation interrupted",
			"deployed", counts.deployed.Load(),
			"failed", counts.failed.Load(),
			"total", len(setup.discovered),
		)
		return summary, err
	}

	summary.deployed = int(counts.deployed.Load())
	summary.failed = int(counts.failed.Load())
	summary.skipped = int(counts.skipped.Load())

	result.Deployed = summary.deployed
	result.Failed = summary.failed
	result.Skipped = summary.skipped
	sort.Strings(result.DeployedStacks)
	sort.Strings(result.FailedStacks)
	return summary, nil
}

func (r *Reconciler) deployStack(ctx context.Context, setup reconcileSetup, stack stacks.Stack, result *Result, resultMu *sync.Mutex, counts *deployCounts) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	stackName := stack.Name
	slog.Info("processing stack", "stack", stackName)

	resolvedEnv, err := setup.envResolver.ResolveForStack(stack.Dir, setup.cfg.Stacks)
	if err != nil {
		slog.Error("failed to resolve env file for stack", "stack", stackName, "error", err)
		counts.failed.Add(1)
		recordStackName(resultMu, &result.FailedStacks, stackName)
		return nil
	}

	fingerprint, err := fingerprintStack(stack, resolvedEnv.Content)
	if err != nil {
		slog.Error("failed to fingerprint stack", "stack", stackName, "error", err)
		counts.failed.Add(1)
		recordStackName(resultMu, &result.FailedStacks, stackName)
		return nil
	}

	previousFingerprint, ok, err := r.state.Get(setup.repoStateKey, stackName)
	if err != nil {
		slog.Error("failed to read reconcile state", "stack", stackName, "error", err)
		counts.failed.Add(1)
		recordStackName(resultMu, &result.FailedStacks, stackName)
		return nil
	}

	if ok && previousFingerprint == fingerprint {
		slog.Info("stack unchanged, skipping compose up", "stack", stackName)
		counts.skipped.Add(1)
		return nil
	}

	envFile, err := setup.envResolver.FileFromContent(resolvedEnv.Content)
	if err != nil {
		slog.Error("failed to write env file for stack", "stack", stackName, "error", err)
		counts.failed.Add(1)
		recordStackName(resultMu, &result.FailedStacks, stackName)
		return nil
	}

	if err := r.up(ctx, stack, envFile); err != nil {
		slog.Error("failed to deploy stack", "stack", stackName, "error", err)
		counts.failed.Add(1)
		recordStackName(resultMu, &result.FailedStacks, stackName)
		return nil
	}
	if err := r.state.Put(setup.repoStateKey, stackName, fingerprint); err != nil {
		slog.Error("failed to persist reconcile state", "stack", stackName, "error", err)
		counts.failed.Add(1)
		recordStackName(resultMu, &result.FailedStacks, stackName)
		return nil
	}

	counts.deployed.Add(1)
	recordStackName(resultMu, &result.DeployedStacks, stackName)
	return nil
}

func (r *Reconciler) removeDeletedStacks(ctx context.Context, repoStateKey string, removedStacks []string, result *Result) error {
	if len(removedStacks) == 0 {
		return nil
	}
	if r.down == nil {
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
		result.RemovedStacks++
		result.RemovedStackIDs = append(result.RemovedStackIDs, name)
		if err := r.state.Delete(repoStateKey, name); err != nil {
			slog.Error("failed to remove reconcile state for stack", "stack", name, "error", err)
		}
	}
	sort.Strings(result.RemovedStackIDs)
	return nil
}

func (r *Reconciler) removeDeletedNetworks(ctx context.Context, removedNetworks []string, result *Result) error {
	if len(removedNetworks) == 0 {
		return nil
	}
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
		return nil
	}
	result.RemovedNetworks = len(removedNetworks)
	return nil
}

func (r *Reconciler) finalizeReconcile(ctx context.Context, cfg *config.Config, summary deploySummary, removedStacks, removedNetworks []string, result Result) (Result, error) {
	failedCount := summary.failed
	if failedCount > 0 {
		if cfg.Stacks.AutoPrune {
			slog.Warn("skipping automatic Docker prune because stack deployments failed", "failed", failedCount)
		}
		slog.Warn("reconciliation completed with deployment failures",
			"deployed", summary.deployed,
			"skipped", summary.skipped,
			"failed", failedCount,
			"removed_stacks", len(removedStacks),
			"removed_networks", len(removedNetworks),
		)
		slog.Info("reconciliation complete",
			"deployed", summary.deployed,
			"skipped", summary.skipped,
			"failed", failedCount,
			"removed_stacks", len(removedStacks),
			"removed_networks", len(removedNetworks),
		)
		r.notifyChanged(ctx, result)
		return result, nil
	}

	if summary.deployed > 0 && cfg.Stacks.AutoPrune {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := r.prune(ctx); err != nil {
			return result, fmt.Errorf("pruning Docker resources: %w", err)
		}
	}

	slog.Info("reconciliation complete",
		"deployed", summary.deployed,
		"skipped", summary.skipped,
		"failed", summary.failed,
		"removed_stacks", len(removedStacks),
		"removed_networks", len(removedNetworks),
	)
	r.notifyChanged(ctx, result)
	return result, nil
}
