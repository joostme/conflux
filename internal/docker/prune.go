package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	mobyclient "github.com/moby/moby/client"
)

// Prune removes unused daemon-wide images, volumes, and networks.
func (c *Client) Prune(ctx context.Context) error {
	if c == nil || c.cli == nil || c.cli.Client() == nil {
		return nil
	}

	apiClient := c.cli.Client()
	slog.Info("pruning unused Docker resources")

	var errs []error
	imageFilters := mobyclient.Filters{}
	imageFilters.Add("dangling", "false")

	imageResult, err := apiClient.ImagePrune(ctx, mobyclient.ImagePruneOptions{Filters: imageFilters})
	if err != nil {
		errs = append(errs, fmt.Errorf("pruning images: %w", err))
	} else {
		slog.Info("unused Docker images pruned",
			"deleted", len(imageResult.Report.ImagesDeleted),
			"space_reclaimed_bytes", imageResult.Report.SpaceReclaimed,
		)
	}

	volumeResult, err := apiClient.VolumePrune(ctx, mobyclient.VolumePruneOptions{All: true})
	if err != nil {
		errs = append(errs, fmt.Errorf("pruning volumes: %w", err))
	} else {
		slog.Info("unused Docker volumes pruned",
			"deleted", len(volumeResult.Report.VolumesDeleted),
			"space_reclaimed_bytes", volumeResult.Report.SpaceReclaimed,
		)
	}

	networkResult, err := apiClient.NetworkPrune(ctx, mobyclient.NetworkPruneOptions{})
	if err != nil {
		errs = append(errs, fmt.Errorf("pruning networks: %w", err))
	} else {
		slog.Info("unused Docker networks pruned",
			"deleted", len(networkResult.Report.NetworksDeleted),
		)
	}

	return errors.Join(errs...)
}
