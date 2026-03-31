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

// --- Snapshot tests (replaces separate DiscoverStackNames / DiscoverNetworkNames tests) ---

func TestSnapshot_Basic(t *testing.T) {
	repoDir := setupRepoDirWithNetworks(t, []string{"nginx", "redis", "whoami"}, `
networks:
  proxy:
    driver: bridge
  internal:
    driver: bridge
`)

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	stackNames, networkNames, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	// Verify stacks
	if len(stackNames) != 3 {
		t.Fatalf("expected 3 stacks, got %d", len(stackNames))
	}
	for _, expected := range []string{"nginx", "redis", "whoami"} {
		if !stackNames[expected] {
			t.Errorf("missing stack %q", expected)
		}
	}

	// Verify networks
	if len(networkNames) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(networkNames))
	}
	if !networkNames["proxy"] {
		t.Error("missing network 'proxy'")
	}
	if !networkNames["internal"] {
		t.Error("missing network 'internal'")
	}
}

func TestSnapshot_ExplicitNetworkName(t *testing.T) {
	repoDir := setupRepoDirWithNetworks(t, nil, `
networks:
  proxy:
    name: my-custom-proxy
    driver: bridge
`)

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	_, networkNames, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if len(networkNames) != 1 {
		t.Fatalf("expected 1 network, got %d", len(networkNames))
	}
	if !networkNames["my-custom-proxy"] {
		t.Error("missing 'my-custom-proxy'")
	}
	if networkNames["proxy"] {
		t.Error("'proxy' (map key) should not be in names")
	}
}

func TestSnapshot_NoNetworks(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx")

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	stackNames, networkNames, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if len(stackNames) != 1 {
		t.Errorf("expected 1 stack, got %d", len(stackNames))
	}
	if len(networkNames) != 0 {
		t.Errorf("expected 0 networks, got %d", len(networkNames))
	}
}

func TestSnapshot_EmptyStacks(t *testing.T) {
	repoDir := setupRepoDir(t) // no stacks

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	stackNames, _, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if len(stackNames) != 0 {
		t.Errorf("expected 0 stacks, got %d", len(stackNames))
	}
}

func TestSnapshot_MissingConfig(t *testing.T) {
	repoDir := t.TempDir()

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)
	_, _, err := rec.Snapshot()
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
}

func TestSnapshot_AfterStackRemoval(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx", "redis", "whoami")

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)

	// Take "before" snapshot
	before, _, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() before error = %v", err)
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
	after, _, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() after error = %v", err)
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

func TestSnapshot_AfterNetworkRemoval(t *testing.T) {
	repoDir := setupRepoDirWithNetworks(t, []string{"nginx"}, `
networks:
  proxy:
    driver: bridge
  internal:
    driver: bridge
`)

	rec := New(repoDir, "conflux.yaml", nil, nil, nil, nil)

	// Take "before" snapshot
	_, beforeNetworks, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() before error = %v", err)
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
	_, afterNetworks, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() after error = %v", err)
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
