package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/joostme/conflux/internal/reconciler"
)

// Controller orchestrates the git-poll → snapshot → diff → reconcile loop.
type Controller struct {
	repo             gitRepo
	rec              reconcilerClient
	lastRejectedHash *plumbing.Hash
}

type gitRepo interface {
	CloneOrOpen() (bool, error)
	Fetch() (*plumbing.Hash, error)
	Head() (plumbing.Hash, error)
	Reset(plumbing.Hash) error
}

type reconcilerClient interface {
	Validate(context.Context) error
	Reconcile(context.Context, []string, []string) (reconciler.Result, error)
	Snapshot() (map[string]bool, map[string]bool, error)
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
		if err := c.rec.Validate(ctx); err != nil {
			return err
		}
		_, err := c.rec.Reconcile(ctx, nil, nil)
		return err
	}

	return c.fetchAndReconcile(ctx, true)
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
			if err := c.fetchAndReconcile(ctx, false); err != nil {
				slog.Error("poll cycle failed", "error", err)
			}
		}
	}
}

// fetchAndReconcile snapshots state, fetches remote changes, and reconciles.
// When ensureRunning is true the controller reconciles even when the remote
// has not changed, guaranteeing every stack is running (used on startup).
// When ensureRunning is false, reconciliation is skipped if there are no
// remote changes, avoiding unnecessary docker compose calls on every poll.
func (c *Controller) fetchAndReconcile(ctx context.Context, ensureRunning bool) error {
	remoteHash, err := c.repo.Fetch()
	if err != nil {
		return err
	}
	if remoteHash == nil {
		if !ensureRunning {
			slog.Debug("no changes detected, skipping reconciliation")
			return nil
		}
		slog.Info("no changes detected, reconciling to ensure all stacks are running")
		if err := c.rec.Validate(ctx); err != nil {
			return err
		}
		_, err := c.rec.Reconcile(ctx, nil, nil)
		return err
	}

	if c.lastRejectedHash != nil && *c.lastRejectedHash == *remoteHash {
		slog.Info("remote commit previously failed validation, skipping", "commit", remoteHash.String()[:12])
		return nil
	}

	before := c.snapshotState()

	previousHash, err := c.repo.Head()
	if err != nil {
		return err
	}

	if err := c.repo.Reset(*remoteHash); err != nil {
		return err
	}

	if err := c.rec.Validate(ctx); err != nil {
		rejectedHash := *remoteHash
		c.lastRejectedHash = &rejectedHash
		if resetErr := c.repo.Reset(previousHash); resetErr != nil {
			return fmt.Errorf("validation failed: %w; restoring previous checkout: %v", err, resetErr)
		}
		return err
	}
	c.lastRejectedHash = nil

	after := c.snapshotState()
	removedStacks := diffNames(before.stacks, after.stacks)
	removedNetworks := diffNames(before.networks, after.networks)

	slog.Info("changes detected, reconciling",
		"removed_stacks", len(removedStacks),
		"removed_networks", len(removedNetworks),
	)
	_, err = c.rec.Reconcile(ctx, removedStacks, removedNetworks)
	return err
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
