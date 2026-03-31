package stacks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joostme/conflux/internal/config"
)

// helper to create a stack directory with optional files
func createStack(t *testing.T, baseDir, name string, files ...string) {
	t.Helper()
	stackDir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		path := filepath.Join(stackDir, f)
		if err := os.WriteFile(path, []byte("# "+f), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func defaultCfg() *config.Config {
	return &config.Config{
		Stacks: config.StacksConfig{
			Directory:   "stacks",
			File:        "compose.yaml",
			Secrets:     "secrets.enc.env",
			Environment: "environment.env",
		},
	}
}

func TestDiscover_BasicStacks(t *testing.T) {
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "stacks")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	createStack(t, stacksDir, "whoami", "compose.yaml")
	createStack(t, stacksDir, "nginx", "compose.yaml")

	stacks, err := Discover(repoDir, defaultCfg())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(stacks) != 2 {
		t.Fatalf("expected 2 stacks, got %d", len(stacks))
	}

	// Build a map for easier assertion
	byName := make(map[string]Stack)
	for _, s := range stacks {
		byName[s.Name] = s
	}

	whoami, ok := byName["whoami"]
	if !ok {
		t.Fatal("missing stack 'whoami'")
	}
	if whoami.Dir != filepath.Join(stacksDir, "whoami") {
		t.Errorf("whoami.Dir = %q", whoami.Dir)
	}
	if whoami.ComposeFile != filepath.Join(stacksDir, "whoami", "compose.yaml") {
		t.Errorf("whoami.ComposeFile = %q", whoami.ComposeFile)
	}
}

func TestDiscover_WithEnvAndSecretFiles(t *testing.T) {
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "stacks")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	createStack(t, stacksDir, "app", "compose.yaml", "environment.env", "secrets.enc.env")

	stacks, err := Discover(repoDir, defaultCfg())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}

	app := stacks[0]
	if len(app.EnvFiles) != 1 {
		t.Fatalf("expected 1 env file, got %d", len(app.EnvFiles))
	}
	expectedEnv := filepath.Join(stacksDir, "app", "environment.env")
	if app.EnvFiles[0] != expectedEnv {
		t.Errorf("env file = %q, want %q", app.EnvFiles[0], expectedEnv)
	}

	if len(app.SecretFiles) != 1 {
		t.Fatalf("expected 1 secret file, got %d", len(app.SecretFiles))
	}
	expectedSecret := filepath.Join(stacksDir, "app", "secrets.enc.env")
	if app.SecretFiles[0] != expectedSecret {
		t.Errorf("secret file = %q, want %q", app.SecretFiles[0], expectedSecret)
	}
}

func TestDiscover_NoEnvFiles(t *testing.T) {
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "stacks")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	createStack(t, stacksDir, "minimal", "compose.yaml")

	stacks, err := Discover(repoDir, defaultCfg())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}
	if len(stacks[0].EnvFiles) != 0 {
		t.Errorf("expected 0 env files, got %d", len(stacks[0].EnvFiles))
	}
	if len(stacks[0].SecretFiles) != 0 {
		t.Errorf("expected 0 secret files, got %d", len(stacks[0].SecretFiles))
	}
}

func TestDiscover_SkipsDirsWithoutComposeFile(t *testing.T) {
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "stacks")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	// This one has a compose file
	createStack(t, stacksDir, "valid", "compose.yaml")
	// This one doesn't
	createStack(t, stacksDir, "no-compose", "README.md")

	stacks, err := Discover(repoDir, defaultCfg())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}
	if stacks[0].Name != "valid" {
		t.Errorf("stack name = %q, want %q", stacks[0].Name, "valid")
	}
}

func TestDiscover_SkipsFiles(t *testing.T) {
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "stacks")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a file (not directory) in the stacks dir
	if err := os.WriteFile(filepath.Join(stacksDir, "README.md"), []byte("# Stacks"), 0644); err != nil {
		t.Fatal(err)
	}
	createStack(t, stacksDir, "app", "compose.yaml")

	stacks, err := Discover(repoDir, defaultCfg())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}
	if stacks[0].Name != "app" {
		t.Errorf("stack name = %q, want %q", stacks[0].Name, "app")
	}
}

func TestDiscover_EmptyStacksDir(t *testing.T) {
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "stacks")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	stacks, err := Discover(repoDir, defaultCfg())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(stacks) != 0 {
		t.Errorf("expected 0 stacks, got %d", len(stacks))
	}
}

func TestDiscover_NonexistentStacksDir(t *testing.T) {
	repoDir := t.TempDir()
	// Don't create the stacks directory

	_, err := Discover(repoDir, defaultCfg())
	if err == nil {
		t.Fatal("expected error for nonexistent stacks directory, got nil")
	}
}

func TestDiscover_CustomConfig(t *testing.T) {
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "apps")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	createStack(t, stacksDir, "web", "docker-compose.yml", "vars.env")

	cfg := &config.Config{
		Stacks: config.StacksConfig{
			Directory:   "apps",
			File:        "docker-compose.yml",
			Secrets:     "secrets.enc.env",
			Environment: "vars.env",
		},
	}

	stacks, err := Discover(repoDir, cfg)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}
	if stacks[0].Name != "web" {
		t.Errorf("stack name = %q, want %q", stacks[0].Name, "web")
	}
	if stacks[0].ComposeFile != filepath.Join(stacksDir, "web", "docker-compose.yml") {
		t.Errorf("compose file = %q", stacks[0].ComposeFile)
	}
	if len(stacks[0].EnvFiles) != 1 {
		t.Fatalf("expected 1 env file, got %d", len(stacks[0].EnvFiles))
	}
}
