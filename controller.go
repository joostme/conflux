package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/joostme/conflux/internal/git"
	"github.com/joostme/conflux/internal/reconciler"
)

// Controller orchestrates the git-poll → snapshot → diff → reconcile loop.
// It extracts the duplicated logic that was previously inlined in main() for
// both the initial sync path and the recurring poll loop.
type Controller struct {
	repo   *git.Repo
	rec    *reconciler.Reconciler
	logger *slog.Logger
}

// NewController creates a new Controller.
func NewController(repo *git.Repo, rec *reconciler.Reconciler, logger *slog.Logger) *Controller {
	return &Controller{repo: repo, rec: rec, logger: logger}
}

// InitialSync performs the first-run synchronisation. On a fresh clone it
// deploys everything; on an existing repo it fetches, diffs, and reconciles.
func (c *Controller) InitialSync(ctx context.Context) error {
	c.logger.Info("performing initial sync")

	freshClone, err := c.repo.CloneOrOpen()
	if err != nil {
		return err
	}

	if freshClone {
		c.logger.Info("fresh clone, deploying all stacks")
		return c.rec.Reconcile(ctx, nil, nil)
	}

	// Existing repo — fetch and apply any pending changes.
	return c.fetchAndReconcile(ctx)
}

// RunLoop enters the main polling loop and blocks until ctx is cancelled.
func (c *Controller) RunLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.logger.Info("entering poll loop", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("conflux stopped")
			return
		case <-ticker.C:
			if err := c.fetchAndReconcile(ctx); err != nil {
				c.logger.Error("poll cycle failed", "error", err)
			}
		}
	}
}

// fetchAndReconcile is the single implementation of the
// snapshot → fetch → reset → snapshot → diff → reconcile sequence.
// Previously this logic was duplicated between the initial-sync path
// and the poll loop in main().
func (c *Controller) fetchAndReconcile(ctx context.Context) error {
	// 1. Snapshot state from current worktree (before pull)
	before := c.snapshotState()

	// 2. Fetch remote changes
	remoteHash, err := c.repo.Fetch()
	if err != nil {
		return err
	}
	if remoteHash == nil {
		// No changes — on initial sync this means current state is up-to-date,
		// so we still reconcile to ensure all stacks are deployed.
		if err := c.rec.Reconcile(ctx, nil, nil); err != nil {
			return err
		}
		return nil
	}

	// 3. Reset worktree to new commit
	if err := c.repo.Reset(*remoteHash); err != nil {
		return err
	}

	// 4. Snapshot state from updated worktree (after pull)
	after := c.snapshotState()

	// 5. Compute removals and reconcile
	removedStacks := diffNames(before.stacks, after.stacks)
	removedNetworks := diffNames(before.networks, after.networks)

	c.logger.Info("changes detected, reconciling",
		"removed_stacks", len(removedStacks),
		"removed_networks", len(removedNetworks),
	)
	return c.rec.Reconcile(ctx, removedStacks, removedNetworks)
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
func (c *Controller) snapshotState() repoState {
	stackNames, networkNames, err := c.rec.Snapshot()
	if err != nil {
		c.logger.Warn("failed to snapshot state, skipping removals", "error", err)
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
