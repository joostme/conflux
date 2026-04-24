package docker

import (
	"fmt"

	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/compose"
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
