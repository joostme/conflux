package reconciler

import (
	"context"
	"log/slog"
	"sync"

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

func recordStackName(mu *sync.Mutex, names *[]string, name string) {
	mu.Lock()
	defer mu.Unlock()
	*names = append(*names, name)
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
	setup, err := r.prepareReconcile(ctx)
	if err != nil {
		return result, err
	}
	defer r.cleanupEnvResolver(setup.envResolver)

	counts, err := r.deployStacks(ctx, setup, &result)
	if err != nil {
		return result, err
	}
	if err := r.removeDeletedStacks(ctx, setup.repoStateKey, removedStacks, &result); err != nil {
		return result, err
	}
	if err := r.removeDeletedNetworks(ctx, removedNetworks, &result); err != nil {
		return result, err
	}

	return r.finalizeReconcile(ctx, setup.cfg, counts, removedStacks, removedNetworks, result)
}

func (r *Reconciler) loadConfig() (*config.Config, error) {
	return config.Load(r.repoDir, r.configFile)
}

func (r *Reconciler) cleanupEnvResolver(envResolver *envfiles.Resolver) {
	if err := envResolver.Cleanup(); err != nil {
		slog.Warn("failed to clean up env temp files", "error", err)
	}
}
