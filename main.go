package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joostme/conflux/internal/git"
	"github.com/joostme/conflux/internal/reconciler"
	"github.com/joostme/conflux/internal/sops"
	"github.com/joostme/conflux/internal/stacks"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: getLogLevel(),
	}))

	// Load configuration from environment variables
	gitURL := requireEnv("CONFLUX_GIT_URL", logger)
	gitBranch := getEnv("CONFLUX_GIT_BRANCH", "main")
	gitKey := getEnv("CONFLUX_GIT_KEY", "")
	pollInterval := parseDuration(getEnv("CONFLUX_POLL_INTERVAL", "30s"), logger)
	ageKeyFile := getEnv("CONFLUX_SOPS_AGE_KEY", "")
	repoDir := getEnv("CONFLUX_REPO_DIR", "/data/repo")
	configFile := getEnv("CONFLUX_CONFIG_FILE", "conflux.yaml")

	logger.Info("conflux starting",
		"git_url", gitURL,
		"git_branch", gitBranch,
		"poll_interval", pollInterval,
		"repo_dir", repoDir,
		"config_file", configFile,
	)

	// Initialize components
	repo := git.NewRepo(gitURL, gitBranch, repoDir, gitKey, logger)
	decryptor := sops.NewDecryptor(ageKeyFile, logger)

	composeClient, err := stacks.NewComposeClient(logger)
	if err != nil {
		logger.Error("failed to create compose client", "error", err)
		os.Exit(1)
	}

	rec := reconciler.New(repoDir, configFile, decryptor, composeClient, logger)

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
	if _, err := repo.CloneOrPull(); err != nil {
		logger.Error("initial git sync failed", "error", err)
		os.Exit(1)
	}

	if err := rec.Reconcile(ctx); err != nil {
		logger.Error("initial reconciliation failed", "error", err)
		// Don't exit — keep polling, the repo might get fixed
	}

	// Main polling loop
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	logger.Info("entering poll loop", "interval", pollInterval)
	for {
		select {
		case <-ctx.Done():
			logger.Info("conflux stopped")
			return
		case <-ticker.C:
			changed, err := repo.CloneOrPull()
			if err != nil {
				logger.Error("git poll failed", "error", err)
				continue
			}

			if !changed {
				continue
			}

			logger.Info("changes detected, reconciling")
			if err := rec.Reconcile(ctx); err != nil {
				logger.Error("reconciliation failed", "error", err)
			}
		}
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requireEnv(key string, logger *slog.Logger) string {
	v := os.Getenv(key)
	if v == "" {
		logger.Error("required environment variable not set", "var", key)
		os.Exit(1)
	}
	return v
}

func parseDuration(s string, logger *slog.Logger) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		logger.Error("invalid duration", "value", s, "error", err)
		os.Exit(1)
	}
	return d
}

func getLogLevel() slog.Level {
	switch os.Getenv("CONFLUX_LOG_LEVEL") {
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
