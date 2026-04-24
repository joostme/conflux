package docker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/compose/v5/pkg/api"
	"github.com/joostme/conflux/internal/stacks"
)

// ComposeClient wraps a compose service implementation.
type ComposeClient struct {
	service api.Compose
}

func projectLoadOptions(stack stacks.Stack, envFile string) api.ProjectLoadOptions {
	options := api.ProjectLoadOptions{
		ProjectName: stack.Name,
		ConfigPaths: []string{stack.ComposeFile},
		WorkingDir:  stack.Dir,
	}
	if envFile != "" {
		options.EnvFiles = []string{envFile}
	}

	return options
}

// Up loads the compose project and runs the equivalent of `docker compose up -d --remove-orphans`.
func (c *ComposeClient) Up(ctx context.Context, stack stacks.Stack, envFile string) error {
	slog.Info("deploying stack",
		"stack", stack.Name,
		"compose", stack.ComposeFile,
		"env_file", envFile,
	)

	project, err := c.service.LoadProject(ctx, projectLoadOptions(stack, envFile))
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

	slog.Info("stack deployed successfully", "stack", stack.Name)
	return nil
}

// Validate loads the compose project without applying any changes.
func (c *ComposeClient) Validate(ctx context.Context, stack stacks.Stack, envFile string) error {
	slog.Info("validating stack", "stack", stack.Name, "compose", stack.ComposeFile, "env_file", envFile)

	if _, err := c.service.LoadProject(ctx, projectLoadOptions(stack, envFile)); err != nil {
		return fmt.Errorf("loading project %s: %w", stack.Name, err)
	}

	slog.Info("stack validation succeeded", "stack", stack.Name)
	return nil
}

// Down runs the equivalent of `docker compose down --remove-orphans` for a project.
func (c *ComposeClient) Down(ctx context.Context, stackName string) error {
	slog.Info("removing stack", "stack", stackName)

	err := c.service.Down(ctx, stackName, api.DownOptions{
		RemoveOrphans: true,
	})
	if err != nil {
		return fmt.Errorf("compose down for %s: %w", stackName, err)
	}

	slog.Info("stack removed successfully", "stack", stackName)
	return nil
}
