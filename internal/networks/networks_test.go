package networks

import (
	"testing"

	"github.com/joostme/conflux/internal/config"
)

func TestResolveName_ExplicitName(t *testing.T) {
	cfg := config.NetworkConfig{Name: "my-custom-net"}
	name := ResolveName("proxy", cfg)
	if name != "my-custom-net" {
		t.Errorf("ResolveName() = %q, want %q", name, "my-custom-net")
	}
}

func TestResolveName_FallsBackToKey(t *testing.T) {
	cfg := config.NetworkConfig{}
	name := ResolveName("proxy", cfg)
	if name != "proxy" {
		t.Errorf("ResolveName() = %q, want %q", name, "proxy")
	}
}

func TestResolveNames_MixedConfig(t *testing.T) {
	networks := map[string]config.NetworkConfig{
		"proxy":    {Name: "my-proxy"},
		"internal": {},
		"backend":  {Name: "custom-backend"},
	}

	names := ResolveNames(networks)

	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if !names["my-proxy"] {
		t.Error("missing 'my-proxy'")
	}
	if !names["internal"] {
		t.Error("missing 'internal'")
	}
	if !names["custom-backend"] {
		t.Error("missing 'custom-backend'")
	}
	// The map key "proxy" should NOT appear — it's overridden by name: "my-proxy"
	if names["proxy"] {
		t.Error("'proxy' should not be in names — it was overridden by 'my-proxy'")
	}
}

func TestResolveNames_EmptyMap(t *testing.T) {
	names := ResolveNames(map[string]config.NetworkConfig{})
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %d", len(names))
	}
}

func TestResolveNames_NilMap(t *testing.T) {
	names := ResolveNames(nil)
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %d", len(names))
	}
}

func TestConvertIPAM_ValidConfig(t *testing.T) {
	src := &config.IPAM{
		Driver: "default",
		Config: []config.IPAMPool{
			{
				Subnet:  "172.28.0.0/16",
				IPRange: "172.28.5.0/24",
				Gateway: "172.28.5.254",
				AuxAddresses: map[string]string{
					"host1": "172.28.1.5",
					"host2": "172.28.1.6",
				},
			},
		},
		Options: map[string]string{"foo": "bar"},
	}

	ipam, err := convertIPAM(src)
	if err != nil {
		t.Fatalf("convertIPAM() error = %v", err)
	}

	if ipam.Driver != "default" {
		t.Errorf("driver = %q, want %q", ipam.Driver, "default")
	}
	if ipam.Options["foo"] != "bar" {
		t.Errorf("options[foo] = %q, want %q", ipam.Options["foo"], "bar")
	}
	if len(ipam.Config) != 1 {
		t.Fatalf("expected 1 config entry, got %d", len(ipam.Config))
	}

	cfg := ipam.Config[0]
	if cfg.Subnet.String() != "172.28.0.0/16" {
		t.Errorf("subnet = %q, want %q", cfg.Subnet.String(), "172.28.0.0/16")
	}
	if cfg.IPRange.String() != "172.28.5.0/24" {
		t.Errorf("ip_range = %q, want %q", cfg.IPRange.String(), "172.28.5.0/24")
	}
	if cfg.Gateway.String() != "172.28.5.254" {
		t.Errorf("gateway = %q, want %q", cfg.Gateway.String(), "172.28.5.254")
	}
	if len(cfg.AuxAddress) != 2 {
		t.Fatalf("expected 2 aux addresses, got %d", len(cfg.AuxAddress))
	}
	if cfg.AuxAddress["host1"].String() != "172.28.1.5" {
		t.Errorf("aux[host1] = %q", cfg.AuxAddress["host1"].String())
	}
}

func TestConvertIPAM_InvalidSubnet(t *testing.T) {
	src := &config.IPAM{
		Config: []config.IPAMPool{
			{Subnet: "not-a-cidr"},
		},
	}
	_, err := convertIPAM(src)
	if err == nil {
		t.Fatal("expected error for invalid subnet, got nil")
	}
}

func TestConvertIPAM_InvalidGateway(t *testing.T) {
	src := &config.IPAM{
		Config: []config.IPAMPool{
			{
				Subnet:  "172.28.0.0/16",
				Gateway: "not-an-ip",
			},
		},
	}
	_, err := convertIPAM(src)
	if err == nil {
		t.Fatal("expected error for invalid gateway, got nil")
	}
}

func TestConvertIPAM_InvalidIPRange(t *testing.T) {
	src := &config.IPAM{
		Config: []config.IPAMPool{
			{
				Subnet:  "172.28.0.0/16",
				IPRange: "garbage",
			},
		},
	}
	_, err := convertIPAM(src)
	if err == nil {
		t.Fatal("expected error for invalid ip_range, got nil")
	}
}

func TestConvertIPAM_InvalidAuxAddress(t *testing.T) {
	src := &config.IPAM{
		Config: []config.IPAMPool{
			{
				Subnet:       "172.28.0.0/16",
				AuxAddresses: map[string]string{"bad": "not-an-ip"},
			},
		},
	}
	_, err := convertIPAM(src)
	if err == nil {
		t.Fatal("expected error for invalid aux address, got nil")
	}
}

func TestConvertIPAM_EmptyConfig(t *testing.T) {
	src := &config.IPAM{
		Driver: "custom",
	}
	ipam, err := convertIPAM(src)
	if err != nil {
		t.Fatalf("convertIPAM() error = %v", err)
	}
	if ipam.Driver != "custom" {
		t.Errorf("driver = %q, want %q", ipam.Driver, "custom")
	}
	if len(ipam.Config) != 0 {
		t.Errorf("expected 0 config entries, got %d", len(ipam.Config))
	}
}

func TestConvertIPAM_SubnetOnly(t *testing.T) {
	src := &config.IPAM{
		Config: []config.IPAMPool{
			{Subnet: "10.0.0.0/8"},
		},
	}
	ipam, err := convertIPAM(src)
	if err != nil {
		t.Fatalf("convertIPAM() error = %v", err)
	}
	if len(ipam.Config) != 1 {
		t.Fatalf("expected 1 config entry, got %d", len(ipam.Config))
	}
	if ipam.Config[0].Subnet.String() != "10.0.0.0/8" {
		t.Errorf("subnet = %q", ipam.Config[0].Subnet.String())
	}
	// Gateway and IPRange should be zero values
	if ipam.Config[0].Gateway.IsValid() {
		t.Error("gateway should be zero value")
	}
	if ipam.Config[0].IPRange.IsValid() {
		t.Error("ip_range should be zero value")
	}
}

func TestConvertIPAM_MultipleConfigs(t *testing.T) {
	src := &config.IPAM{
		Config: []config.IPAMPool{
			{Subnet: "10.0.0.0/8", Gateway: "10.0.0.1"},
			{Subnet: "172.16.0.0/12", Gateway: "172.16.0.1"},
		},
	}
	ipam, err := convertIPAM(src)
	if err != nil {
		t.Fatalf("convertIPAM() error = %v", err)
	}
	if len(ipam.Config) != 2 {
		t.Fatalf("expected 2 config entries, got %d", len(ipam.Config))
	}
}

func TestBuildCreateOptions_AllFields(t *testing.T) {
	enableIPv4 := true
	enableIPv6 := false
	cfg := config.NetworkConfig{
		Driver:     "bridge",
		DriverOpts: map[string]string{"opt1": "val1"},
		EnableIPv4: &enableIPv4,
		EnableIPv6: &enableIPv6,
		Internal:   true,
		Attachable: true,
		Labels:     map[string]string{"env": "test"},
		IPAM: &config.IPAM{
			Driver: "default",
			Config: []config.IPAMPool{
				{Subnet: "192.168.0.0/24"},
			},
		},
	}

	opts, err := buildCreateOptions(cfg)
	if err != nil {
		t.Fatalf("buildCreateOptions() error = %v", err)
	}

	if opts.Driver != "bridge" {
		t.Errorf("driver = %q", opts.Driver)
	}
	if !opts.Internal {
		t.Error("internal should be true")
	}
	if !opts.Attachable {
		t.Error("attachable should be true")
	}
	if opts.Options["opt1"] != "val1" {
		t.Errorf("options[opt1] = %q", opts.Options["opt1"])
	}
	if opts.Labels["env"] != "test" {
		t.Errorf("labels[env] = %q", opts.Labels["env"])
	}
	if opts.EnableIPv4 == nil || *opts.EnableIPv4 != true {
		t.Error("enable_ipv4 should be true")
	}
	if opts.EnableIPv6 == nil || *opts.EnableIPv6 != false {
		t.Error("enable_ipv6 should be false")
	}
	if opts.IPAM == nil {
		t.Fatal("IPAM should not be nil")
	}
}

func TestBuildCreateOptions_NilIPAM(t *testing.T) {
	cfg := config.NetworkConfig{
		Driver: "bridge",
	}

	opts, err := buildCreateOptions(cfg)
	if err != nil {
		t.Fatalf("buildCreateOptions() error = %v", err)
	}

	if opts.IPAM != nil {
		t.Error("IPAM should be nil when not configured")
	}
}

func TestBuildCreateOptions_InvalidIPAM(t *testing.T) {
	cfg := config.NetworkConfig{
		IPAM: &config.IPAM{
			Config: []config.IPAMPool{
				{Subnet: "invalid"},
			},
		},
	}

	_, err := buildCreateOptions(cfg)
	if err == nil {
		t.Fatal("expected error for invalid IPAM config, got nil")
	}
}
