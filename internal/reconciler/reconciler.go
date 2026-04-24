package reconciler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
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
	validate   func(context.Context, stacks.Stack, string) error
	up         func(context.Context, stacks.Stack, string) error
	down       func(context.Context, string) error
	prune      func(context.Context) error
	notify     func(context.Context, string, map[string]string) error
}

// Result summarizes the outcome of a reconciliation cycle.
type Result struct {
	Deployed        int
	DeployedStacks  []string
	Skipped         int
	Failed          int
	FailedStacks    []string
	RemovedStacks   int
	RemovedStackIDs []string
	RemovedNetworks int
}

// ValidationError reports a repo state that was rejected before reconcile.
type ValidationError struct {
	Summary      string
	FailedStacks []string
	Err          error
}

func (e *ValidationError) Error() string {
	if len(e.FailedStacks) == 0 {
		return fmt.Sprintf("%s: %v", e.Summary, e.Err)
	}
	return fmt.Sprintf("%s for stacks %s: %v", e.Summary, strings.Join(e.FailedStacks, ", "), e.Err)
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

func recordStackName(mu *sync.Mutex, names *[]string, name string) {
	mu.Lock()
	defer mu.Unlock()
	*names = append(*names, name)
}

// Changed reports whether the reconcile run applied any changes.
func (r Result) Changed() bool {
	return r.Deployed > 0 || r.RemovedStacks > 0 || r.RemovedNetworks > 0 || r.Failed > 0
}

func (r Result) notificationMessage() string {
	parts := []string{"Conflux run changed"}
	if r.Deployed > 0 {
		parts = append(parts, fmt.Sprintf("Deployed: %s", strings.Join(r.DeployedStacks, ", ")))
	}
	if r.RemovedStacks > 0 {
		parts = append(parts, fmt.Sprintf("Removed: %s", strings.Join(r.RemovedStackIDs, ", ")))
	}
	if r.RemovedNetworks > 0 {
		parts = append(parts, fmt.Sprintf("Removed networks: %d", r.RemovedNetworks))
	}
	if r.Failed > 0 {
		parts = append(parts, fmt.Sprintf("Failed: %s", strings.Join(r.FailedStacks, ", ")))
	}

	return strings.Join(parts, "\n")
}

// New creates a new Reconciler.
func New(repoDir, configFile string, dockerClient *docker.Client, networkSvc *networks.Manager, notify func(context.Context, string, map[string]string) error) *Reconciler {
	rec := &Reconciler{
		repoDir:    repoDir,
		configFile: configFile,
		networks:   networkSvc,
		state:      reconcilestate.New(),
		notify:     notify,
	}
	if dockerClient != nil {
		rec.validate = dockerClient.Compose().Validate
		rec.up = dockerClient.Compose().Up
		rec.down = dockerClient.Compose().Down
		rec.prune = dockerClient.Prune
	}

	return rec
}

// Validate checks whether the current repo state can be reconciled safely.
func (r *Reconciler) Validate(ctx context.Context) error {
	slog.Info("starting validation")

	cfg, err := config.Load(r.repoDir, r.configFile)
	if err != nil {
		return r.reportValidationFailure(ctx, "loading config", nil, err)
	}

	envResolver, err := envfiles.NewResolver(r.repoDir, cfg)
	if err != nil {
		return r.reportValidationFailure(ctx, "resolving global env files", nil, err)
	}
	defer func() {
		if err := envResolver.Cleanup(); err != nil {
			slog.Warn("failed to clean up env temp files", "error", err)
		}
	}()

	var validationErrs []error
	var failedStacks []string

	if err := networks.Validate(cfg.Networks); err != nil {
		slog.Error("network validation failed", "error", err)
		validationErrs = append(validationErrs, fmt.Errorf("validating networks: %w", err))
	}

	discovered, err := stacks.Discover(r.repoDir, cfg)
	if err != nil {
		slog.Error("stack discovery failed", "error", err)
		validationErrs = append(validationErrs, fmt.Errorf("discovering stacks: %w", err))
	} else if len(discovered) > 0 && r.validate == nil {
		slog.Error("stack validation unavailable", "error", "docker client not configured")
		validationErrs = append(validationErrs, fmt.Errorf("validating stack definitions: %w", fmt.Errorf("docker client not configured")))
	}

	if r.validate != nil {
		for _, stack := range discovered {
			if err := ctx.Err(); err != nil {
				return err
			}

			resolvedEnv, err := envResolver.ResolveForStack(stack.Dir, cfg.Stacks)
			if err != nil {
				slog.Error("failed to resolve env file during validation", "stack", stack.Name, "error", err)
				failedStacks = append(failedStacks, stack.Name)
				validationErrs = append(validationErrs, fmt.Errorf("stack %s env resolution: %w", stack.Name, err))
				continue
			}

			envFile, err := envResolver.FileFromContent(resolvedEnv.Content)
			if err != nil {
				slog.Error("failed to prepare env file during validation", "stack", stack.Name, "error", err)
				failedStacks = append(failedStacks, stack.Name)
				validationErrs = append(validationErrs, fmt.Errorf("stack %s env file: %w", stack.Name, err))
				continue
			}

			if err := r.validate(ctx, stack, envFile); err != nil {
				slog.Error("stack validation failed", "stack", stack.Name, "error", err)
				failedStacks = append(failedStacks, stack.Name)
				validationErrs = append(validationErrs, fmt.Errorf("stack %s: %w", stack.Name, err))
			}
		}
	}

	if len(validationErrs) > 0 {
		sort.Strings(failedStacks)
		return r.reportValidationFailure(ctx, "validation failed", failedStacks, errors.Join(validationErrs...))
	}

	slog.Info("validation complete", "stacks", len(discovered), "networks", len(cfg.Networks))
	return nil
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
func (r *Reconciler) Reconcile(ctx context.Context, removedStacks, removedNetworks []string) (Result, error) {
	slog.Info("starting reconciliation")
	result := Result{}

	// 1. Parse config
	cfg, err := config.Load(r.repoDir, r.configFile)
	if err != nil {
		return result, fmt.Errorf("loading config: %w", err)
	}
	slog.Info("config loaded",
		"stacks_dir", cfg.Stacks.Directory,
		"global_env", cfg.Global.Environment,
		"global_secrets", cfg.Global.Secrets,
		"networks", len(cfg.Networks),
	)

	envResolver, err := envfiles.NewResolver(r.repoDir, cfg)
	if err != nil {
		return result, fmt.Errorf("resolving global env files: %w", err)
	}
	defer func() {
		if err := envResolver.Cleanup(); err != nil {
			slog.Warn("failed to clean up env temp files", "error", err)
		}
	}()

	// 2. Ensure global networks exist before any stacks are deployed
	if len(cfg.Networks) > 0 {
		if r.networks == nil {
			return result, fmt.Errorf("network manager not configured")
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		slog.Info("ensuring networks", "count", len(cfg.Networks))
		if err := r.networks.Ensure(ctx, cfg.Networks); err != nil {
			return result, fmt.Errorf("ensuring networks: %w", err)
		}
	}

	// 3. Discover stacks
	discovered, err := stacks.Discover(r.repoDir, cfg)
	if err != nil {
		return result, fmt.Errorf("discovering stacks: %w", err)
	}
	slog.Info("stacks discovered", "count", len(discovered))

	// 4. Deploy stacks (optionally in parallel)
	if len(discovered) > 0 && r.up == nil {
		return result, fmt.Errorf("docker client not configured")
	}

	var deployed atomic.Int64
	var failed atomic.Int64
	var skipped atomic.Int64
	var resultMu sync.Mutex
	repoStateKey := repoKey(r.repoDir, r.configFile)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(cfg.Stacks.ParallelDeploy)

	for _, stack := range discovered {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}

			stackName := stack.Name

			slog.Info("processing stack", "stack", stackName)
			resolvedEnv, err := envResolver.ResolveForStack(stack.Dir, cfg.Stacks)
			if err != nil {
				slog.Error("failed to resolve env file for stack", "stack", stackName, "error", err)
				failed.Add(1)
				recordStackName(&resultMu, &result.FailedStacks, stackName)
				return nil
			}

			fingerprint, err := fingerprintStack(stack, resolvedEnv.Content)
			if err != nil {
				slog.Error("failed to fingerprint stack", "stack", stackName, "error", err)
				failed.Add(1)
				recordStackName(&resultMu, &result.FailedStacks, stackName)
				return nil
			}

			previousFingerprint, ok, err := r.state.Get(repoStateKey, stackName)
			if err != nil {
				slog.Error("failed to read reconcile state", "stack", stackName, "error", err)
				failed.Add(1)
				recordStackName(&resultMu, &result.FailedStacks, stackName)
				return nil
			}

			if ok && previousFingerprint == fingerprint {
				slog.Info("stack unchanged, skipping compose up", "stack", stackName)
				skipped.Add(1)
				return nil
			}

			envFile, err := envResolver.FileFromContent(resolvedEnv.Content)
			if err != nil {
				slog.Error("failed to write env file for stack", "stack", stackName, "error", err)
				failed.Add(1)
				recordStackName(&resultMu, &result.FailedStacks, stackName)
				return nil
			}

			if err := r.up(gctx, stack, envFile); err != nil {
				slog.Error("failed to deploy stack", "stack", stackName, "error", err)
				failed.Add(1)
				recordStackName(&resultMu, &result.FailedStacks, stackName)
				return nil
			}
			if err := r.state.Put(repoStateKey, stackName, fingerprint); err != nil {
				slog.Error("failed to persist reconcile state", "stack", stackName, "error", err)
				failed.Add(1)
				recordStackName(&resultMu, &result.FailedStacks, stackName)
				return nil
			}
			deployed.Add(1)
			recordStackName(&resultMu, &result.DeployedStacks, stackName)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		slog.Warn("reconciliation interrupted",
			"deployed", deployed.Load(),
			"failed", failed.Load(),
			"total", len(discovered),
		)
		return result, err
	}

	result.Deployed = int(deployed.Load())
	result.Failed = int(failed.Load())
	result.Skipped = int(skipped.Load())
	sort.Strings(result.DeployedStacks)
	sort.Strings(result.FailedStacks)

	// 5. Remove deleted stacks
	if len(removedStacks) > 0 && r.down == nil {
		return result, fmt.Errorf("docker client not configured")
	}
	for _, name := range removedStacks {
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted during stack removal")
			return result, err
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

	// 6. Remove deleted networks (after stacks so containers are torn down first)
	if len(removedNetworks) > 0 {
		if r.networks == nil {
			return result, fmt.Errorf("network manager not configured")
		}
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted before network removal")
			return result, err
		}
		slog.Info("removing networks", "count", len(removedNetworks))
		if err := r.networks.Remove(ctx, removedNetworks); err != nil {
			slog.Error("failed to remove networks", "error", err)
		} else {
			result.RemovedNetworks = len(removedNetworks)
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
		r.notifyChanged(ctx, result)
		return result, nil
	}

	if deployed.Load() > 0 && cfg.Stacks.AutoPrune {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := r.prune(ctx); err != nil {
			return result, fmt.Errorf("pruning Docker resources: %w", err)
		}
	}

	slog.Info("reconciliation complete",
		"deployed", deployed.Load(),
		"skipped", skipped.Load(),
		"failed", failed.Load(),
		"removed_stacks", len(removedStacks),
		"removed_networks", len(removedNetworks),
	)
	r.notifyChanged(ctx, result)
	return result, nil
}

func (r *Reconciler) notifyChanged(ctx context.Context, result Result) {
	if r.notify == nil || !result.Changed() {
		return
	}

	params := map[string]string{"title": "Conflux run changed"}
	if err := r.notify(ctx, result.notificationMessage(), params); err != nil {
		slog.Warn("failed to send notification", "error", err)
	}
}

func (r *Reconciler) reportValidationFailure(ctx context.Context, summary string, failedStacks []string, err error) error {
	validationErr := &ValidationError{
		Summary:      summary,
		FailedStacks: append([]string(nil), failedStacks...),
		Err:          err,
	}
	r.notifyValidationFailed(ctx, validationErr)
	return validationErr
}

func (r *Reconciler) notifyValidationFailed(ctx context.Context, err *ValidationError) {
	if r.notify == nil {
		return
	}

	lines := []string{"Conflux validation failed"}
	if len(err.FailedStacks) > 0 {
		lines = append(lines, fmt.Sprintf("Stacks: %s", strings.Join(err.FailedStacks, ", ")))
	} else {
		lines = append(lines, fmt.Sprintf("Error: %s", err.Summary))
	}
	if err.Err != nil {
		lines = append(lines, fmt.Sprintf("Details: %v", err.Err))
	}

	params := map[string]string{"title": "Conflux validation failed"}
	if notifyErr := r.notify(ctx, strings.Join(lines, "\n"), params); notifyErr != nil {
		slog.Warn("failed to send validation notification", "error", notifyErr)
	}
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
