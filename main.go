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
	"github.com/joostme/conflux/internal/docker"
	"github.com/joostme/conflux/internal/git"
	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/reconciler"
)

// appConfig holds all configuration loaded from environment variables.
type appConfig struct {
	GitURL       string        `env:"CONFLUX_GIT_URL,required"`
	GitBranch    string        `env:"CONFLUX_GIT_BRANCH"       envDefault:"main"`
	GitKey       string        `env:"CONFLUX_GIT_KEY"`
	PollInterval time.Duration `env:"CONFLUX_POLL_INTERVAL"    envDefault:"3600s"`
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
	cfg, err := env.ParseAs[appConfig]()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.slogLevel(),
	}))
	slog.SetDefault(logger)

	slog.Info("conflux starting",
		"git_url", cfg.GitURL,
		"git_branch", cfg.GitBranch,
		"poll_interval", cfg.PollInterval,
		"repo_dir", cfg.RepoDir,
		"config_file", cfg.ConfigFile,
	)

	repo := git.NewRepo(cfg.GitURL, cfg.GitBranch, cfg.RepoDir, cfg.GitKey)

	dockerClient, err := docker.New()
	if err != nil {
		slog.Error("failed to create docker client", "error", err)
		os.Exit(1)
	}
	defer dockerClient.Close()

	networkMgr := networks.NewManager(dockerClient.APIClient())

	rec := reconciler.New(cfg.RepoDir, cfg.ConfigFile, dockerClient.Compose(), networkMgr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ctrl := &Controller{repo: repo, rec: rec}

	if err := ctrl.InitialSync(ctx); err != nil {
		slog.Error("initial sync failed", "error", err)
		// Don't exit — keep polling, the repo might get fixed
	}

	ctrl.RunLoop(ctx, cfg.PollInterval)
}
