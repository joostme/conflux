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

func TestFingerprintStack_ChangesWhenReferencedFileChanges(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	compose := "services:\n  app:\n    image: nginx\nconfigs:\n  site:\n    file: ./site.conf\nsecrets:\n  key:\n    file: ${KEY_PATH}\n"
	if err := os.WriteFile(composeFile, []byte(compose), 0644); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "site.conf")
	if err := os.WriteFile(conf, []byte("v1"), 0644); err != nil {
		t.Fatal(err)
	}
	stack := stacks.Stack{Name: "app", Dir: dir, ComposeFile: composeFile}

	before, err := fingerprintStack(stack, "")
	if err != nil {
		t.Fatalf("fingerprintStack() error = %v", err)
	}
	if err := os.WriteFile(conf, []byte("v2"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := fingerprintStack(stack, "")
	if err != nil {
		t.Fatalf("fingerprintStack() error = %v", err)
	}
	if before == after {
		t.Fatal("fingerprint must change when a referenced config file changes")
	}
}

func TestFingerprintStack_MissingReferencedFileFails(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("configs:\n  site:\n    file: ./nope.conf\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := fingerprintStack(stacks.Stack{Name: "app", Dir: dir, ComposeFile: composeFile}, ""); err == nil {
		t.Fatal("expected error for missing referenced file")
	}
}
