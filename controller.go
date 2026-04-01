package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/joostme/conflux/internal/git"
	"github.com/joostme/conflux/internal/reconciler"
)

// Controller orchestrates the git-poll → snapshot → diff → reconcile loop.
type Controller struct {
	repo *git.Repo
	rec  *reconciler.Reconciler
}

// NewController creates a new Controller.
func NewController(repo *git.Repo, rec *reconciler.Reconciler) *Controller {
	return &Controller{repo: repo, rec: rec}
}

// InitialSync performs the first-run synchronisation. On a fresh clone it
// deploys everything; on an existing repo it fetches, diffs, and reconciles.
func (c *Controller) InitialSync(ctx context.Context) error {
	slog.Info("performing initial sync")

	freshClone, err := c.repo.CloneOrOpen()
	if err != nil {
		return err
	}

	if freshClone {
		slog.Info("fresh clone, deploying all stacks")
		return c.rec.Reconcile(ctx, nil, nil)
	}

	return c.fetchAndReconcile(ctx)
}

// RunLoop enters the main polling loop and blocks until ctx is cancelled.
func (c *Controller) RunLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("entering poll loop", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("conflux stopped")
			return
		case <-ticker.C:
			if err := c.fetchAndReconcile(ctx); err != nil {
				slog.Error("poll cycle failed", "error", err)
			}
		}
	}
}

// fetchAndReconcile snapshots state, fetches remote changes, and reconciles.
func (c *Controller) fetchAndReconcile(ctx context.Context) error {
	before := c.snapshotState()

	remoteHash, err := c.repo.Fetch()
	if err != nil {
		return err
	}
	if remoteHash == nil {
		// No changes — still reconcile to ensure all stacks are running.
		return c.rec.Reconcile(ctx, nil, nil)
	}

	if err := c.repo.Reset(*remoteHash); err != nil {
		return err
	}

	after := c.snapshotState()
	removedStacks := diffNames(before.stacks, after.stacks)
	removedNetworks := diffNames(before.networks, after.networks)

	slog.Info("changes detected, reconciling",
		"removed_stacks", len(removedStacks),
		"removed_networks", len(removedNetworks),
	)
	return c.rec.Reconcile(ctx, removedStacks, removedNetworks)
}

// repoState holds discovered resource names from a worktree snapshot.
type repoState struct {
	stacks   map[string]bool
	networks map[string]bool
}

// snapshotState reads the current worktree and returns managed resource names.
func (c *Controller) snapshotState() repoState {
	stackNames, networkNames, err := c.rec.Snapshot()
	if err != nil {
		slog.Warn("failed to snapshot state, skipping removals", "error", err)
		return repoState{}
	}
	return repoState{stacks: stackNames, networks: networkNames}
}

// diffNames returns names present in before but not in after.
// Returns nil if either set is nil to avoid accidental removals.
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
