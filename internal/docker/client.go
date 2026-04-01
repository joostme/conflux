package docker

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/joostme/conflux/internal/stacks"
	mobyclient "github.com/moby/moby/client"
)

// Client wraps a shared Docker CLI instance and compose service.
type Client struct {
	cli     *command.DockerCli
	compose *ComposeClient
}

// New creates and initializes a shared Docker CLI client.
func New() (*Client, error) {
	cli, err := command.NewDockerCli()
	if err != nil {
		return nil, fmt.Errorf("creating docker CLI: %w", err)
	}

	if err := cli.Initialize(flags.NewClientOptions()); err != nil {
		return nil, fmt.Errorf("initializing docker CLI: %w", err)
	}

	composeService, err := compose.NewComposeService(cli)
	if err != nil {
		return nil, fmt.Errorf("creating compose service: %w", err)
	}

	return &Client{
		cli: cli,
		compose: &ComposeClient{
			service: composeService,
		},
	}, nil
}

// Compose returns the initialized compose client.
func (c *Client) Compose() *ComposeClient {
	return c.compose
}

// ComposeClient wraps a compose service implementation.
type ComposeClient struct {
	service api.Compose
}

// Up loads the compose project and runs the equivalent of `docker compose up -d --remove-orphans`.
func (c *ComposeClient) Up(ctx context.Context, stack stacks.Stack, envFiles []string) error {
	slog.Info("deploying stack",
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

	slog.Info("stack deployed successfully", "stack", stack.Name)
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

// APIClient returns the underlying Docker API client used by the CLI.
func (c *Client) APIClient() mobyclient.APIClient {
	return c.cli.Client()
}

// Close releases resources held by the underlying Docker API client.
func (c *Client) Close() error {
	if c == nil || c.cli == nil || c.cli.Client() == nil {
		return nil
	}
	return c.cli.Client().Close()
}
