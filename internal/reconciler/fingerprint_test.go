package reconciler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joostme/conflux/internal/stacks"
)

func TestFingerprintStack_SameInputsStable(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services:\n  app:\n    image: nginx\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stack := stacks.Stack{Name: "app", Dir: dir, ComposeFile: composeFile}

	first, err := fingerprintStack(stack, "A=1\nB=2\n")
	if err != nil {
		t.Fatalf("first fingerprintStack() error = %v", err)
	}
	second, err := fingerprintStack(stack, "A=1\nB=2\n")
	if err != nil {
		t.Fatalf("second fingerprintStack() error = %v", err)
	}

	if first != second {
		t.Fatalf("fingerprint changed for same inputs: %q != %q", first, second)
	}
}

func TestFingerprintStack_ChangesWithComposeContent(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services:\n  app:\n    image: nginx\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stack := stacks.Stack{Name: "app", Dir: dir, ComposeFile: composeFile}
	before, err := fingerprintStack(stack, "A=1\n")
	if err != nil {
		t.Fatalf("before fingerprintStack() error = %v", err)
	}

	if err := os.WriteFile(composeFile, []byte("services:\n  app:\n    image: busybox\n"), 0644); err != nil {
		t.Fatal(err)
	}

	after, err := fingerprintStack(stack, "A=1\n")
	if err != nil {
		t.Fatalf("after fingerprintStack() error = %v", err)
	}

	if before == after {
		t.Fatal("expected fingerprint to change when compose file changes")
	}
}

func TestFingerprintStack_ChangesWithEnvContent(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services:\n  app:\n    image: nginx\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stack := stacks.Stack{Name: "app", Dir: dir, ComposeFile: composeFile}
	first, err := fingerprintStack(stack, "A=1\n")
	if err != nil {
		t.Fatalf("first fingerprintStack() error = %v", err)
	}
	second, err := fingerprintStack(stack, "A=2\n")
	if err != nil {
		t.Fatalf("second fingerprintStack() error = %v", err)
	}

	if first == second {
		t.Fatal("expected fingerprint to change when env content changes")
	}
}
