package reconcilestate

import (
	"os"
	"path/filepath"
	"testing"
)

func resetStoreFile(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reconcile-state.json")
	t.Setenv("CONFLUX_STATE_FILE", path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatalf("Remove(%s) error = %v", path, err)
	}
}

func TestStore_PutGetDelete(t *testing.T) {
	resetStoreFile(t)
	store := New()

	got, ok, err := store.Get("repo", "stack")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ok || got != "" {
		t.Fatalf("expected missing entry, got ok=%v fingerprint=%q", ok, got)
	}

	if err := store.Put("repo", "stack", "fingerprint-1"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, ok, err = store.Get("repo", "stack")
	if err != nil {
		t.Fatalf("Get() after Put error = %v", err)
	}
	if !ok || got != "fingerprint-1" {
		t.Fatalf("Get() = (%q, %v), want (%q, true)", got, ok, "fingerprint-1")
	}

	if err := store.Delete("repo", "stack"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	got, ok, err = store.Get("repo", "stack")
	if err != nil {
		t.Fatalf("Get() after Delete error = %v", err)
	}
	if ok || got != "" {
		t.Fatalf("expected missing entry after delete, got ok=%v fingerprint=%q", ok, got)
	}
}

func TestStore_SeparatesRepos(t *testing.T) {
	resetStoreFile(t)
	store := New()

	if err := store.Put("repo-a", "web", "fp-a"); err != nil {
		t.Fatalf("Put(repo-a) error = %v", err)
	}
	if err := store.Put("repo-b", "web", "fp-b"); err != nil {
		t.Fatalf("Put(repo-b) error = %v", err)
	}

	gotA, ok, err := store.Get("repo-a", "web")
	if err != nil {
		t.Fatalf("Get(repo-a) error = %v", err)
	}
	if !ok || gotA != "fp-a" {
		t.Fatalf("Get(repo-a) = (%q, %v), want (%q, true)", gotA, ok, "fp-a")
	}

	gotB, ok, err := store.Get("repo-b", "web")
	if err != nil {
		t.Fatalf("Get(repo-b) error = %v", err)
	}
	if !ok || gotB != "fp-b" {
		t.Fatalf("Get(repo-b) = (%q, %v), want (%q, true)", gotB, ok, "fp-b")
	}
}

func TestStore_CreatesStateDirectory(t *testing.T) {
	resetStoreFile(t)
	store := New()

	if err := store.Put("repo", "stack", "fingerprint"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	path := os.Getenv("CONFLUX_STATE_FILE")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected reconcile state file at %s: %v", path, err)
	}
}
