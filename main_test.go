package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/joostme/conflux/internal/reconciler"
)

type fakeControllerRepo struct {
	head       plumbing.Hash
	fetches    []*plumbing.Hash
	resets     []plumbing.Hash
	fetchIndex int
}

func (f *fakeControllerRepo) CloneOrOpen() (bool, error) {
	return false, nil
}

func (f *fakeControllerRepo) Fetch() (*plumbing.Hash, error) {
	if f.fetchIndex >= len(f.fetches) {
		return nil, nil
	}
	hash := f.fetches[f.fetchIndex]
	f.fetchIndex++
	return hash, nil
}

func (f *fakeControllerRepo) Head() (plumbing.Hash, error) {
	return f.head, nil
}

func (f *fakeControllerRepo) Reset(hash plumbing.Hash) error {
	f.resets = append(f.resets, hash)
	f.head = hash
	return nil
}

type fakeControllerReconciler struct {
	validateErr   error
	validateCalls int
	reconcileArgs []reconcileArgs
	snapshots     []repoState
	snapshotIndex int
}

type reconcileArgs struct {
	removedStacks   []string
	removedNetworks []string
}

func (f *fakeControllerReconciler) Validate(context.Context) error {
	f.validateCalls++
	return f.validateErr
}

func (f *fakeControllerReconciler) Reconcile(_ context.Context, removedStacks, removedNetworks []string) (reconciler.Result, error) {
	f.reconcileArgs = append(f.reconcileArgs, reconcileArgs{
		removedStacks:   append([]string(nil), removedStacks...),
		removedNetworks: append([]string(nil), removedNetworks...),
	})
	return reconciler.Result{}, nil
}

func (f *fakeControllerReconciler) Snapshot() (map[string]bool, map[string]bool, error) {
	if f.snapshotIndex >= len(f.snapshots) {
		return map[string]bool{}, map[string]bool{}, nil
	}
	state := f.snapshots[f.snapshotIndex]
	f.snapshotIndex++
	return state.stacks, state.networks, nil
}

func hashForTest(n int) plumbing.Hash {
	return plumbing.NewHash(fmt.Sprintf("%040d", n))
}

func hashPtr(hash plumbing.Hash) *plumbing.Hash {
	return &hash
}

func TestDiffNames_BasicRemoval(t *testing.T) {
	before := map[string]bool{"nginx": true, "whoami": true, "redis": true}
	after := map[string]bool{"nginx": true, "redis": true}

	removed := diffNames(before, after)

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(removed))
	}
	if removed[0] != "whoami" {
		t.Errorf("removed[0] = %q, want %q", removed[0], "whoami")
	}
}

func TestDiffNames_NoRemovals(t *testing.T) {
	before := map[string]bool{"nginx": true, "redis": true}
	after := map[string]bool{"nginx": true, "redis": true, "whoami": true}

	removed := diffNames(before, after)

	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d: %v", len(removed), removed)
	}
}

func TestDiffNames_AllRemoved(t *testing.T) {
	before := map[string]bool{"nginx": true, "redis": true}
	after := map[string]bool{}

	removed := diffNames(before, after)

	sort.Strings(removed)
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(removed))
	}
	if removed[0] != "nginx" || removed[1] != "redis" {
		t.Errorf("removed = %v, want [nginx redis]", removed)
	}
}

func TestDiffNames_BeforeNil_FailSafe(t *testing.T) {
	after := map[string]bool{"nginx": true}

	removed := diffNames(nil, after)

	if removed != nil {
		t.Errorf("expected nil when before is nil, got %v", removed)
	}
}

func TestDiffNames_AfterNil_FailSafe(t *testing.T) {
	before := map[string]bool{"nginx": true}

	removed := diffNames(before, nil)

	if removed != nil {
		t.Errorf("expected nil when after is nil, got %v", removed)
	}
}

func TestDiffNames_BothNil_FailSafe(t *testing.T) {
	removed := diffNames(nil, nil)

	if removed != nil {
		t.Errorf("expected nil when both are nil, got %v", removed)
	}
}

func TestDiffNames_BothEmpty(t *testing.T) {
	before := map[string]bool{}
	after := map[string]bool{}

	removed := diffNames(before, after)

	if len(removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(removed))
	}
}

func TestDiffNames_CompleteSwap(t *testing.T) {
	before := map[string]bool{"nginx": true, "redis": true}
	after := map[string]bool{"postgres": true, "whoami": true}

	removed := diffNames(before, after)

	sort.Strings(removed)
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(removed))
	}
	if removed[0] != "nginx" || removed[1] != "redis" {
		t.Errorf("removed = %v, want [nginx redis]", removed)
	}
}

func TestFetchAndReconcile_RollsBackAndSkipsRejectedCommit(t *testing.T) {
	goodHash := hashForTest(1)
	rejectedHash := hashForTest(2)
	fixedHash := hashForTest(3)
	validationErr := errors.New("invalid compose")

	repo := &fakeControllerRepo{
		head: goodHash,
		fetches: []*plumbing.Hash{
			hashPtr(rejectedHash),
			hashPtr(rejectedHash),
			hashPtr(fixedHash),
		},
	}
	rec := &fakeControllerReconciler{
		validateErr: validationErr,
		snapshots: []repoState{
			{stacks: map[string]bool{"old": true, "web": true}},
			{stacks: map[string]bool{"old": true, "web": true}},
			{stacks: map[string]bool{"web": true}},
		},
	}
	ctrl := &Controller{repo: repo, rec: rec}

	if err := ctrl.fetchAndReconcile(context.Background(), false); !errors.Is(err, validationErr) {
		t.Fatalf("first fetchAndReconcile() error = %v, want validation error", err)
	}
	if repo.head != goodHash {
		t.Fatalf("expected checkout to roll back to good hash, got %s", repo.head)
	}
	if len(repo.resets) != 2 || repo.resets[0] != rejectedHash || repo.resets[1] != goodHash {
		t.Fatalf("expected reset to rejected then good hash, got %v", repo.resets)
	}
	if rec.validateCalls != 1 {
		t.Fatalf("expected one validation attempt, got %d", rec.validateCalls)
	}

	if err := ctrl.fetchAndReconcile(context.Background(), false); err != nil {
		t.Fatalf("second fetchAndReconcile() error = %v", err)
	}
	if len(repo.resets) != 2 {
		t.Fatalf("expected rejected hash to be skipped without another reset, got %v", repo.resets)
	}
	if rec.validateCalls != 1 {
		t.Fatalf("expected rejected hash to be skipped without validation, got %d calls", rec.validateCalls)
	}

	rec.validateErr = nil
	if err := ctrl.fetchAndReconcile(context.Background(), false); err != nil {
		t.Fatalf("third fetchAndReconcile() error = %v", err)
	}
	if repo.head != fixedHash {
		t.Fatalf("expected checkout to remain on fixed hash, got %s", repo.head)
	}
	if ctrl.lastRejectedHash != nil {
		t.Fatal("expected rejected hash to be cleared after successful validation")
	}
	if len(rec.reconcileArgs) != 1 {
		t.Fatalf("expected one reconcile call, got %d", len(rec.reconcileArgs))
	}
	if len(rec.reconcileArgs[0].removedStacks) != 1 || rec.reconcileArgs[0].removedStacks[0] != "old" {
		t.Fatalf("expected removed stack old, got %v", rec.reconcileArgs[0].removedStacks)
	}
}

func TestFetchAndReconcile_ValidatesBeforeEnsureRunningReconcile(t *testing.T) {
	repo := &fakeControllerRepo{head: hashForTest(1), fetches: []*plumbing.Hash{nil}}
	rec := &fakeControllerReconciler{
		validateErr: errors.New("invalid current checkout"),
		snapshots: []repoState{
			{stacks: map[string]bool{"web": true}},
		},
	}
	ctrl := &Controller{repo: repo, rec: rec}

	err := ctrl.fetchAndReconcile(context.Background(), true)
	if !errors.Is(err, rec.validateErr) {
		t.Fatalf("fetchAndReconcile() error = %v, want validation error", err)
	}
	if rec.validateCalls != 1 {
		t.Fatalf("expected validation before reconcile, got %d calls", rec.validateCalls)
	}
	if len(rec.reconcileArgs) != 0 {
		t.Fatalf("expected reconcile to be skipped after validation failure, got %d calls", len(rec.reconcileArgs))
	}
}
