package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_FullConfig(t *testing.T) {
	dir := t.TempDir()
	yaml := `
global:
  secrets:
    - secrets.enc.env
    - extra.enc.env
  environment:
    - environment.env

networks:
  proxy:
    driver: bridge
    attachable: true
  internal:
    driver: bridge
    internal: true
    ipam:
      config:
        - subnet: 172.28.0.0/16
          gateway: 172.28.0.1

stacks:
  directory: apps
  file: docker-compose.yml
  secrets: my-secrets.enc.env
  environment: my-env.env
`
	if err := os.WriteFile(filepath.Join(dir, "conflux.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir, "conflux.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Global
	if len(cfg.Global.Secrets) != 2 {
		t.Errorf("expected 2 global secrets, got %d", len(cfg.Global.Secrets))
	}
	if cfg.Global.Secrets[0] != "secrets.enc.env" {
		t.Errorf("global secrets[0] = %q, want %q", cfg.Global.Secrets[0], "secrets.enc.env")
	}
	if len(cfg.Global.Environment) != 1 {
		t.Errorf("expected 1 global env, got %d", len(cfg.Global.Environment))
	}

	// Networks
	if len(cfg.Networks) != 2 {
		t.Errorf("expected 2 networks, got %d", len(cfg.Networks))
	}
	proxy, ok := cfg.Networks["proxy"]
	if !ok {
		t.Fatal("missing network 'proxy'")
	}
	if proxy.Driver != "bridge" {
		t.Errorf("proxy.Driver = %q, want %q", proxy.Driver, "bridge")
	}
	if !proxy.Attachable {
		t.Error("proxy.Attachable should be true")
	}

	internal, ok := cfg.Networks["internal"]
	if !ok {
		t.Fatal("missing network 'internal'")
	}
	if !internal.Internal {
		t.Error("internal.Internal should be true")
	}
	if internal.IPAM == nil {
		t.Fatal("internal.IPAM should not be nil")
	}
	if len(internal.IPAM.Config) != 1 {
		t.Fatalf("expected 1 IPAM config, got %d", len(internal.IPAM.Config))
	}
	if internal.IPAM.Config[0].Subnet != "172.28.0.0/16" {
		t.Errorf("IPAM subnet = %q, want %q", internal.IPAM.Config[0].Subnet, "172.28.0.0/16")
	}

	// Stacks
	if cfg.Stacks.Directory != "apps" {
		t.Errorf("stacks.Directory = %q, want %q", cfg.Stacks.Directory, "apps")
	}
	if cfg.Stacks.File != "docker-compose.yml" {
		t.Errorf("stacks.File = %q, want %q", cfg.Stacks.File, "docker-compose.yml")
	}
	if cfg.Stacks.Secrets != "my-secrets.enc.env" {
		t.Errorf("stacks.Secrets = %q, want %q", cfg.Stacks.Secrets, "my-secrets.enc.env")
	}
	if cfg.Stacks.Environment != "my-env.env" {
		t.Errorf("stacks.Environment = %q, want %q", cfg.Stacks.Environment, "my-env.env")
	}
}

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	yaml := `
global:
  environment:
    - env.env
`
	if err := os.WriteFile(filepath.Join(dir, "conflux.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir, "conflux.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Stacks.Directory != "stacks" {
		t.Errorf("default directory = %q, want %q", cfg.Stacks.Directory, "stacks")
	}
	if cfg.Stacks.File != "compose.yaml" {
		t.Errorf("default file = %q, want %q", cfg.Stacks.File, "compose.yaml")
	}
	if cfg.Stacks.Secrets != "secrets.env" {
		t.Errorf("default secrets = %q, want %q", cfg.Stacks.Secrets, "secrets.env")
	}
	if cfg.Stacks.Environment != "environment.env" {
		t.Errorf("default environment = %q, want %q", cfg.Stacks.Environment, "environment.env")
	}
}

func TestLoad_EmptyConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "conflux.yaml"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir, "conflux.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Defaults should be applied
	if cfg.Stacks.Directory != "stacks" {
		t.Errorf("default directory = %q, want %q", cfg.Stacks.Directory, "stacks")
	}
	if cfg.Stacks.File != "compose.yaml" {
		t.Errorf("default file = %q, want %q", cfg.Stacks.File, "compose.yaml")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "conflux.yaml"), []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir, "conflux.yaml")
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := Load(dir, "nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_NetworksWithAllOptions(t *testing.T) {
	dir := t.TempDir()
	yaml := `
networks:
  custom:
    name: my-custom-net
    driver: overlay
    driver_opts:
      foo: bar
    enable_ipv6: true
    internal: true
    attachable: true
    labels:
      env: production
    ipam:
      driver: default
      config:
        - subnet: 10.0.0.0/8
          ip_range: 10.0.1.0/24
          gateway: 10.0.0.1
          aux_addresses:
            host1: 10.0.0.5
      options:
        opt1: val1
`
	if err := os.WriteFile(filepath.Join(dir, "conflux.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir, "conflux.yaml")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	net, ok := cfg.Networks["custom"]
	if !ok {
		t.Fatal("missing network 'custom'")
	}
	if net.Name != "my-custom-net" {
		t.Errorf("name = %q, want %q", net.Name, "my-custom-net")
	}
	if net.Driver != "overlay" {
		t.Errorf("driver = %q, want %q", net.Driver, "overlay")
	}
	if net.DriverOpts["foo"] != "bar" {
		t.Errorf("driver_opts[foo] = %q, want %q", net.DriverOpts["foo"], "bar")
	}
	if net.Labels["env"] != "production" {
		t.Errorf("labels[env] = %q, want %q", net.Labels["env"], "production")
	}
	if net.IPAM == nil {
		t.Fatal("IPAM should not be nil")
	}
	if net.IPAM.Driver != "default" {
		t.Errorf("IPAM driver = %q, want %q", net.IPAM.Driver, "default")
	}
	if len(net.IPAM.Config) != 1 {
		t.Fatalf("expected 1 IPAM config, got %d", len(net.IPAM.Config))
	}
	pool := net.IPAM.Config[0]
	if pool.Subnet != "10.0.0.0/8" {
		t.Errorf("subnet = %q", pool.Subnet)
	}
	if pool.IPRange != "10.0.1.0/24" {
		t.Errorf("ip_range = %q", pool.IPRange)
	}
	if pool.Gateway != "10.0.0.1" {
		t.Errorf("gateway = %q", pool.Gateway)
	}
	if pool.AuxAddresses["host1"] != "10.0.0.5" {
		t.Errorf("aux_addresses[host1] = %q", pool.AuxAddresses["host1"])
	}
	if net.IPAM.Options["opt1"] != "val1" {
		t.Errorf("IPAM options[opt1] = %q", net.IPAM.Options["opt1"])
	}
}
