package reconciler

import (
	"os"
	"path/filepath"
	"testing"
)

// setupRepoDir creates a temporary repo directory with a conflux.yaml config
// and the given stacks (each stack gets a compose.yaml file).
func setupRepoDir(t *testing.T, stackNames ...string) string {
	t.Helper()
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "stacks")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a minimal conflux.yaml
	configYAML := `
stacks:
  directory: stacks
  file: compose.yaml
`
	if err := os.WriteFile(filepath.Join(repoDir, "conflux.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Create each stack directory with a compose file
	for _, name := range stackNames {
		stackDir := filepath.Join(stacksDir, name)
		if err := os.MkdirAll(stackDir, 0755); err != nil {
			t.Fatal(err)
		}
		composeFile := filepath.Join(stackDir, "compose.yaml")
		if err := os.WriteFile(composeFile, []byte("# "+name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return repoDir
}

// setupRepoDirWithNetworks creates a temporary repo directory with a
// conflux.yaml config that includes the given stacks and networks.
func setupRepoDirWithNetworks(t *testing.T, stackNames []string, networkYAML string) string {
	t.Helper()
	repoDir := t.TempDir()
	stacksDir := filepath.Join(repoDir, "stacks")
	if err := os.MkdirAll(stacksDir, 0755); err != nil {
		t.Fatal(err)
	}

	configYAML := `
stacks:
  directory: stacks
  file: compose.yaml
` + networkYAML

	if err := os.WriteFile(filepath.Join(repoDir, "conflux.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}

	for _, name := range stackNames {
		stackDir := filepath.Join(stacksDir, name)
		if err := os.MkdirAll(stackDir, 0755); err != nil {
			t.Fatal(err)
		}
		composeFile := filepath.Join(stackDir, "compose.yaml")
		if err := os.WriteFile(composeFile, []byte("# "+name), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return repoDir
}

func TestDiscoverNetworkNames_Basic(t *testing.T) {
	repoDir := setupRepoDirWithNetworks(t, nil, `
networks:
  proxy:
    driver: bridge
  internal:
    driver: bridge
`)

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	names, err := rec.DiscoverNetworkNames()
	if err != nil {
		t.Fatalf("DiscoverNetworkNames() error = %v", err)
	}

	if len(names) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(names))
	}
	if !names["proxy"] {
		t.Error("missing 'proxy'")
	}
	if !names["internal"] {
		t.Error("missing 'internal'")
	}
}

func TestDiscoverNetworkNames_ExplicitName(t *testing.T) {
	repoDir := setupRepoDirWithNetworks(t, nil, `
networks:
  proxy:
    name: my-custom-proxy
    driver: bridge
`)

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	names, err := rec.DiscoverNetworkNames()
	if err != nil {
		t.Fatalf("DiscoverNetworkNames() error = %v", err)
	}

	if len(names) != 1 {
		t.Fatalf("expected 1 network, got %d", len(names))
	}
	if !names["my-custom-proxy"] {
		t.Error("missing 'my-custom-proxy'")
	}
	if names["proxy"] {
		t.Error("'proxy' (map key) should not be in names")
	}
}

func TestDiscoverNetworkNames_NoNetworks(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx")

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	names, err := rec.DiscoverNetworkNames()
	if err != nil {
		t.Fatalf("DiscoverNetworkNames() error = %v", err)
	}

	if len(names) != 0 {
		t.Errorf("expected 0 networks, got %d", len(names))
	}
}

func TestDiscoverNetworkNames_MissingConfig(t *testing.T) {
	repoDir := t.TempDir()

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	_, err := rec.DiscoverNetworkNames()
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
}

func TestDiscoverNetworkNames_AfterNetworkRemoval(t *testing.T) {
	repoDir := setupRepoDirWithNetworks(t, []string{"nginx"}, `
networks:
  proxy:
    driver: bridge
  internal:
    driver: bridge
`)

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)

	// Take "before" snapshot
	beforeNetworks, err := rec.DiscoverNetworkNames()
	if err != nil {
		t.Fatalf("DiscoverNetworkNames() before error = %v", err)
	}
	if len(beforeNetworks) != 2 {
		t.Fatalf("expected 2 networks before, got %d", len(beforeNetworks))
	}

	// Simulate git pull that removes the "internal" network from config
	newConfig := `
stacks:
  directory: stacks
  file: compose.yaml
networks:
  proxy:
    driver: bridge
`
	if err := os.WriteFile(filepath.Join(repoDir, "conflux.yaml"), []byte(newConfig), 0644); err != nil {
		t.Fatal(err)
	}

	// Take "after" snapshot
	afterNetworks, err := rec.DiscoverNetworkNames()
	if err != nil {
		t.Fatalf("DiscoverNetworkNames() after error = %v", err)
	}
	if len(afterNetworks) != 1 {
		t.Fatalf("expected 1 network after, got %d", len(afterNetworks))
	}

	// Verify the diff
	if !beforeNetworks["internal"] {
		t.Error("internal should be in before set")
	}
	if afterNetworks["internal"] {
		t.Error("internal should NOT be in after set")
	}
	if !afterNetworks["proxy"] {
		t.Error("proxy should still be in after set")
	}
}

func TestDiscoverStackNames_Basic(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx", "redis", "whoami")

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	names, err := rec.DiscoverStackNames()
	if err != nil {
		t.Fatalf("DiscoverStackNames() error = %v", err)
	}

	if len(names) != 3 {
		t.Fatalf("expected 3 stacks, got %d", len(names))
	}
	for _, expected := range []string{"nginx", "redis", "whoami"} {
		if !names[expected] {
			t.Errorf("missing stack %q", expected)
		}
	}
}

func TestDiscoverStackNames_Empty(t *testing.T) {
	repoDir := setupRepoDir(t) // no stacks

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	names, err := rec.DiscoverStackNames()
	if err != nil {
		t.Fatalf("DiscoverStackNames() error = %v", err)
	}

	if len(names) != 0 {
		t.Errorf("expected 0 stacks, got %d", len(names))
	}
}

func TestDiscoverStackNames_MissingConfig(t *testing.T) {
	repoDir := t.TempDir() // no conflux.yaml

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	_, err := rec.DiscoverStackNames()
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
}

func TestDiscoverStackNames_AfterStackRemoval(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx", "redis", "whoami")

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)

	// Take "before" snapshot
	before, err := rec.DiscoverStackNames()
	if err != nil {
		t.Fatalf("DiscoverStackNames() before error = %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("expected 3 stacks before, got %d", len(before))
	}

	// Simulate git pull removing "whoami" stack
	whoamiDir := filepath.Join(repoDir, "stacks", "whoami")
	if err := os.RemoveAll(whoamiDir); err != nil {
		t.Fatal(err)
	}

	// Take "after" snapshot
	after, err := rec.DiscoverStackNames()
	if err != nil {
		t.Fatalf("DiscoverStackNames() after error = %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("expected 2 stacks after, got %d", len(after))
	}

	// Verify the diff
	if !before["whoami"] {
		t.Error("whoami should be in before set")
	}
	if after["whoami"] {
		t.Error("whoami should NOT be in after set")
	}
	if !after["nginx"] || !after["redis"] {
		t.Error("nginx and redis should still be in after set")
	}
}
