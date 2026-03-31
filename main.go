package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joostme/conflux/internal/git"
	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/reconciler"
	"github.com/joostme/conflux/internal/sops"
	"github.com/joostme/conflux/internal/stacks"
)

// appConfig holds all configuration loaded from environment variables.
// Required fields, defaults, and duration parsing are handled declaratively
// via struct tags — no manual getEnv/requireEnv/parseDuration needed.
type appConfig struct {
	GitURL       string        `env:"CONFLUX_GIT_URL,required"`
	GitBranch    string        `env:"CONFLUX_GIT_BRANCH"       envDefault:"main"`
	GitKey       string        `env:"CONFLUX_GIT_KEY"`
	PollInterval time.Duration `env:"CONFLUX_POLL_INTERVAL"    envDefault:"30s"`
	AgeKeyFile   string        `env:"CONFLUX_SOPS_AGE_KEY"`
	RepoDir      string        `env:"CONFLUX_REPO_DIR"         envDefault:"/data/repo"`
	ConfigFile   string        `env:"CONFLUX_CONFIG_FILE"      envDefault:"conflux.yaml"`
	LogLevel     string        `env:"CONFLUX_LOG_LEVEL"        envDefault:"info"`
}

// slogLevel converts the string log level to a slog.Level.
func (c appConfig) slogLevel() slog.Level {
	switch c.LogLevel {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	// Parse all configuration from environment variables in one step.
	// Required fields, defaults, and type conversions (e.g. duration)
	// are handled by the env library — no manual parsing needed.
	cfg, err := env.ParseAs[appConfig]()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.slogLevel(),
	}))

	logger.Info("conflux starting",
		"git_url", cfg.GitURL,
		"git_branch", cfg.GitBranch,
		"poll_interval", cfg.PollInterval,
		"repo_dir", cfg.RepoDir,
		"config_file", cfg.ConfigFile,
	)

	// Initialize components
	repo := git.NewRepo(cfg.GitURL, cfg.GitBranch, cfg.RepoDir, cfg.GitKey, logger)
	decryptor := sops.NewDecryptor(cfg.AgeKeyFile, logger)

	composeClient, err := stacks.NewComposeClient(logger)
	if err != nil {
		logger.Error("failed to create compose client", "error", err)
		os.Exit(1)
	}

	networkMgr, err := networks.NewManager(logger)
	if err != nil {
		logger.Error("failed to create network manager", "error", err)
		os.Exit(1)
	}
	defer networkMgr.Close()

	rec := reconciler.New(cfg.RepoDir, cfg.ConfigFile, decryptor, composeClient, networkMgr, logger)

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Initial clone/pull and reconcile
	logger.Info("performing initial sync")
	freshClone, err := repo.CloneOrOpen()
	if err != nil {
		logger.Error("initial git sync failed", "error", err)
		os.Exit(1)
	}

	if freshClone {
		// Fresh clone — no prior state to compare against, just deploy everything
		logger.Info("fresh clone, deploying all stacks")
		if err := rec.Reconcile(ctx, nil, nil); err != nil {
			logger.Error("initial reconciliation failed", "error", err)
			// Don't exit — keep polling, the repo might get fixed
		}
	} else {
		// Existing repo on disk — fetch and apply any pending changes
		remoteHash, err := repo.Fetch()
		if err != nil {
			logger.Error("initial fetch failed", "error", err)
			os.Exit(1)
		}

		if remoteHash != nil {
			// There are pending changes — snapshot before, reset, then reconcile
			before := snapshotState(rec, logger)

			if err := repo.Reset(*remoteHash); err != nil {
				logger.Error("initial reset failed", "error", err)
				os.Exit(1)
			}

			after := snapshotState(rec, logger)
			removedStacks := diffNames(before.stacks, after.stacks)
			removedNetworks := diffNames(before.networks, after.networks)

			if err := rec.Reconcile(ctx, removedStacks, removedNetworks); err != nil {
				logger.Error("initial reconciliation failed", "error", err)
			}
		} else {
			// No pending changes — just reconcile current state
			if err := rec.Reconcile(ctx, nil, nil); err != nil {
				logger.Error("initial reconciliation failed", "error", err)
			}
		}
	}

	// Main polling loop
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()

	logger.Info("entering poll loop", "interval", cfg.PollInterval)
	for {
		select {
		case <-ctx.Done():
			logger.Info("conflux stopped")
			return
		case <-ticker.C:
			// 1. Snapshot state from current worktree (before pull)
			before := snapshotState(rec, logger)

			// 2. Fetch remote changes
			remoteHash, err := repo.Fetch()
			if err != nil {
				logger.Error("git fetch failed", "error", err)
				continue
			}
			if remoteHash == nil {
				continue // no changes
			}

			// 3. Reset worktree to new commit
			if err := repo.Reset(*remoteHash); err != nil {
				logger.Error("git reset failed", "error", err)
				continue
			}

			// 4. Snapshot state from updated worktree (after pull)
			after := snapshotState(rec, logger)

			// 5. Compute removals and reconcile
			removedStacks := diffNames(before.stacks, after.stacks)
			removedNetworks := diffNames(before.networks, after.networks)

			logger.Info("changes detected, reconciling",
				"removed_stacks", len(removedStacks),
				"removed_networks", len(removedNetworks),
			)
			if err := rec.Reconcile(ctx, removedStacks, removedNetworks); err != nil {
				logger.Error("reconciliation failed", "error", err)
			}
		}
	}
}

// repoState holds the discovered resource names from a worktree snapshot.
// A nil field means discovery failed — the fail-safe in diffNames will
// prevent any removals for that resource type.
type repoState struct {
	stacks   map[string]bool
	networks map[string]bool
}

// snapshotState reads the current worktree and returns the set of managed
// resource names via the reconciler's Snapshot() method, which loads config
// only once for both stack and network discovery.
func snapshotState(rec *reconciler.Reconciler, logger *slog.Logger) repoState {
	stackNames, networkNames, err := rec.Snapshot()
	if err != nil {
		logger.Warn("failed to snapshot state, skipping removals", "error", err)
		return repoState{}
	}
	return repoState{stacks: stackNames, networks: networkNames}
}

// diffNames returns names that are in "before" but not in "after".
// Returns nil if either set is nil (fail-safe: don't remove anything when
// we don't have complete information).
func diffNames(before, after map[string]bool) []string {
	if before == nil || after == nil {
		return nil
	}
	var removed []string
	for name := range before {
		if !after[name] {
			removed = append(removed, name)
		}
	}
	return removed
}
