package stacks

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/joostme/conflux/internal/config"
)

// Stack represents a discovered docker compose stack.
type Stack struct {
	Name        string
	Dir         string // absolute path to stack directory
	ComposeFile string // absolute path to compose file
	EnvFiles    []string
	SecretFiles []string
}

// ComposeClient wraps the docker compose SDK service.
type ComposeClient struct {
	service api.Compose
	logger  *slog.Logger
}

// NewComposeClient creates a compose SDK client.
func NewComposeClient(logger *slog.Logger) (*ComposeClient, error) {
	dockerCli, err := command.NewDockerCli()
	if err != nil {
		return nil, fmt.Errorf("creating docker CLI: %w", err)
	}

	if err := dockerCli.Initialize(flags.NewClientOptions()); err != nil {
		return nil, fmt.Errorf("initializing docker CLI: %w", err)
	}

	service, err := compose.NewComposeService(dockerCli)
	if err != nil {
		return nil, fmt.Errorf("creating compose service: %w", err)
	}

	return &ComposeClient{
		service: service,
		logger:  logger,
	}, nil
}

// Discover scans the stacks directory and returns all discovered stacks.
func Discover(repoDir string, cfg *config.Config) ([]Stack, error) {
	stacksDir := filepath.Join(repoDir, cfg.Stacks.Directory)

	entries, err := os.ReadDir(stacksDir)
	if err != nil {
		return nil, fmt.Errorf("reading stacks directory %s: %w", stacksDir, err)
	}

	var stacks []Stack
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackDir := filepath.Join(stacksDir, entry.Name())
		composeFile := filepath.Join(stackDir, cfg.Stacks.File)

		// Only include directories that contain a compose file
		if _, err := os.Stat(composeFile); os.IsNotExist(err) {
			continue
		}

		stack := Stack{
			Name:        entry.Name(),
			Dir:         stackDir,
			ComposeFile: composeFile,
		}

		// Check for stack-level environment file
		envFile := filepath.Join(stackDir, cfg.Stacks.Environment)
		if _, err := os.Stat(envFile); err == nil {
			stack.EnvFiles = []string{envFile}
		}

		// Check for stack-level secrets file
		secretsFile := filepath.Join(stackDir, cfg.Stacks.Secrets)
		if _, err := os.Stat(secretsFile); err == nil {
			stack.SecretFiles = []string{secretsFile}
		}

		stacks = append(stacks, stack)
	}

	return stacks, nil
}

// Up loads the compose project and runs the equivalent of `docker compose up -d --remove-orphans`.
func (c *ComposeClient) Up(ctx context.Context, stack Stack, envFiles []string) error {
	c.logger.Info("deploying stack",
		"stack", stack.Name,
		"compose", stack.ComposeFile,
		"env_files", envFiles,
	)

	project, err := c.service.LoadProject(ctx, api.ProjectLoadOptions{
		ProjectName: stack.Name,
		ConfigPaths: []string{stack.ComposeFile},
		WorkingDir:  stack.Dir,
		EnvFiles:    envFiles,
	})
	if err != nil {
		return fmt.Errorf("loading project %s: %w", stack.Name, err)
	}

	err = c.service.Up(ctx, project, api.UpOptions{
		Create: api.CreateOptions{
			RemoveOrphans: true,
		},
		Start: api.StartOptions{},
	})
	if err != nil {
		return fmt.Errorf("compose up for %s: %w", stack.Name, err)
	}

	c.logger.Info("stack deployed successfully", "stack", stack.Name)
	return nil
}

// Down runs the equivalent of `docker compose down --remove-orphans` for a project.
func (c *ComposeClient) Down(ctx context.Context, stackName string) error {
	c.logger.Info("removing stack", "stack", stackName)

	err := c.service.Down(ctx, stackName, api.DownOptions{
		RemoveOrphans: true,
	})
	if err != nil {
		return fmt.Errorf("compose down for %s: %w", stackName, err)
	}

	c.logger.Info("stack removed successfully", "stack", stackName)
	return nil
}
