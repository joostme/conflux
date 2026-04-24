package reconciler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/joostme/conflux/internal/reconcilestate"
	stacktypes "github.com/joostme/conflux/internal/stacks"
)

type fakeCompose struct {
	mu                sync.Mutex
	upCalls           []string
	downCalls         []string
	validateCalls     []string
	upErrByName       map[string]error
	validateErrByName map[string]error
}

type fakeNotifier struct {
	mu       sync.Mutex
	messages []string
	params   []map[string]string
}

func (f *fakeNotifier) Send(_ context.Context, message string, params map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, message)
	f.params = append(f.params, params)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

func (f *fakeCompose) Up(_ context.Context, stack stacktypes.Stack, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upCalls = append(f.upCalls, stack.Name)
	if err := f.upErrByName[stack.Name]; err != nil {
		return err
	}
	return nil
}

func (f *fakeCompose) Validate(_ context.Context, stack stacktypes.Stack, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.validateCalls = append(f.validateCalls, stack.Name)
	if err := f.validateErrByName[stack.Name]; err != nil {
		return err
	}
	return nil
}

func (f *fakeCompose) Down(_ context.Context, stackName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downCalls = append(f.downCalls, stackName)
	return nil
}

func (f *fakeCompose) upCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, got := range f.upCalls {
		if got == name {
			count++
		}
	}
	return count
}

func newTestStateStore(t *testing.T) *reconcilestate.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reconcile-state.json")
	t.Setenv("CONFLUX_STATE_FILE", path)
	store := reconcilestate.New()
	storeDir := filepath.Dir(path)
	if err := os.MkdirAll(storeDir, 0755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", storeDir, err)
	}
	_ = os.Remove(path)
	return store
}

func newTestReconciler(repoDir string, compose *fakeCompose, store *reconcilestate.Store) *Reconciler {
	return &Reconciler{
		repoDir:    repoDir,
		configFile: "conflux.yaml",
		state:      store,
		validate:   compose.Validate,
		up:         compose.Up,
		down:       compose.Down,
		prune: func(context.Context) error {
			return nil
		},
	}
}

func newTestReconcilerWithNotifier(repoDir string, compose *fakeCompose, store *reconcilestate.Store, notifier *fakeNotifier) *Reconciler {
	rec := newTestReconciler(repoDir, compose, store)
	if notifier != nil {
		rec.notify = notifier.Send
	}
	return rec
}

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

func TestSnapshot_Basic(t *testing.T) {
	repoDir := setupRepoDirWithNetworks(t, []string{"nginx", "redis", "whoami"}, `
networks:
  proxy:
    driver: bridge
  internal:
    driver: bridge
`)

	rec := New(repoDir, "conflux.yaml", nil, nil, nil)
	stackNames, networkNames, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	if len(stackNames) != 3 {
		t.Fatalf("expected 3 stacks, got %d", len(stackNames))
	}
	for _, expected := range []string{"nginx", "redis", "whoami"} {
		if !stackNames[expected] {
			t.Errorf("missing stack %q", expected)
		}
	}

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

	rec := New(repoDir, "conflux.yaml", nil, nil, nil)
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

	rec := New(repoDir, "conflux.yaml", nil, nil, nil)
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
	repoDir := setupRepoDir(t)

	rec := New(repoDir, "conflux.yaml", nil, nil, nil)
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

	rec := New(repoDir, "conflux.yaml", nil, nil, nil)
	_, _, err := rec.Snapshot()
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
}

func TestSnapshot_AfterStackRemoval(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx", "redis", "whoami")

	rec := New(repoDir, "conflux.yaml", nil, nil, nil)

	before, _, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() before error = %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("expected 3 stacks before, got %d", len(before))
	}

	whoamiDir := filepath.Join(repoDir, "stacks", "whoami")
	if err := os.RemoveAll(whoamiDir); err != nil {
		t.Fatal(err)
	}

	after, _, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() after error = %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("expected 2 stacks after, got %d", len(after))
	}

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

	rec := New(repoDir, "conflux.yaml", nil, nil, nil)

	_, beforeNetworks, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() before error = %v", err)
	}
	if len(beforeNetworks) != 2 {
		t.Fatalf("expected 2 networks before, got %d", len(beforeNetworks))
	}

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

	_, afterNetworks, err := rec.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot() after error = %v", err)
	}
	if len(afterNetworks) != 1 {
		t.Fatalf("expected 1 network after, got %d", len(afterNetworks))
	}

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

func TestReconcile_SkipsUnchangedStacks(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx")
	compose := &fakeCompose{}
	rec := newTestReconciler(repoDir, compose, newTestStateStore(t))

	ctx := context.Background()
	if _, err := rec.Reconcile(ctx, nil, nil); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	if compose.upCount("nginx") != 1 {
		t.Fatalf("expected first reconcile to deploy stack once, got %d", compose.upCount("nginx"))
	}

	if _, err := rec.Reconcile(ctx, nil, nil); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if compose.upCount("nginx") != 1 {
		t.Fatalf("expected unchanged stack to be skipped, got %d deploys", compose.upCount("nginx"))
	}
}

func TestValidate_SendsNotificationForInvalidStack(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx", "redis")
	compose := &fakeCompose{
		validateErrByName: map[string]error{"nginx": os.ErrInvalid},
	}
	notifier := &fakeNotifier{}
	rec := newTestReconcilerWithNotifier(repoDir, compose, newTestStateStore(t), notifier)

	err := rec.Validate(context.Background())
	if err == nil {
		t.Fatal("expected Validate() to fail, got nil")
	}
	if notifier.count() != 1 {
		t.Fatalf("expected 1 notification, got %d", notifier.count())
	}
	if !strings.Contains(notifier.messages[0], "Conflux validation failed") {
		t.Fatalf("expected validation failure notification, got %q", notifier.messages[0])
	}
	if !strings.Contains(notifier.messages[0], "Stacks: nginx") {
		t.Fatalf("expected failed stack in notification, got %q", notifier.messages[0])
	}
	if compose.upCount("nginx") != 0 || compose.upCount("redis") != 0 {
		t.Fatal("validation should not deploy stacks")
	}
}

func TestValidate_SendsGeneralNotificationForConfigError(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "conflux.yaml"), []byte("{{invalid"), 0644); err != nil {
		t.Fatal(err)
	}

	compose := &fakeCompose{}
	notifier := &fakeNotifier{}
	rec := newTestReconcilerWithNotifier(repoDir, compose, newTestStateStore(t), notifier)

	err := rec.Validate(context.Background())
	if err == nil {
		t.Fatal("expected Validate() to fail, got nil")
	}
	if notifier.count() != 1 {
		t.Fatalf("expected 1 notification, got %d", notifier.count())
	}
	if !strings.Contains(notifier.messages[0], "Error: loading config") {
		t.Fatalf("expected general validation error in notification, got %q", notifier.messages[0])
	}
}

func TestValidate_AggregatesNetworkAndStackErrors(t *testing.T) {
	repoDir := setupRepoDirWithNetworks(t, []string{"nginx", "redis"}, `
networks:
  bad-a:
    driver: bridge
    ipam:
      config:
        - subnet: definitely-not-a-cidr
  bad-b:
    driver: bridge
    ipam:
      config:
        - gateway: definitely-not-an-ip
`)

	compose := &fakeCompose{
		validateErrByName: map[string]error{
			"nginx": errors.New("nginx broken"),
			"redis": errors.New("redis broken"),
		},
	}
	notifier := &fakeNotifier{}
	rec := newTestReconcilerWithNotifier(repoDir, compose, newTestStateStore(t), notifier)

	err := rec.Validate(context.Background())
	if err == nil {
		t.Fatal("expected Validate() to fail, got nil")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	for _, want := range []string{
		"network bad-a",
		"network bad-b",
		"stack nginx: nginx broken",
		"stack redis: redis broken",
	} {
		if !strings.Contains(validationErr.Error(), want) {
			t.Fatalf("expected validation error to contain %q, got %q", want, validationErr.Error())
		}
	}

	if len(compose.validateCalls) != 2 {
		t.Fatalf("expected both stacks to be validated, got %d calls", len(compose.validateCalls))
	}
	if notifier.count() != 1 {
		t.Fatalf("expected 1 notification, got %d", notifier.count())
	}
	if !strings.Contains(notifier.messages[0], "Details:") {
		t.Fatalf("expected notification to include validation details, got %q", notifier.messages[0])
	}
}

func TestReconcile_RedeploysWhenGlobalEnvChanges(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx", "redis")
	configYAML := `
global:
  environment:
    - global.env
stacks:
  directory: stacks
  file: compose.yaml
`
	if err := os.WriteFile(filepath.Join(repoDir, "conflux.yaml"), []byte(configYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "global.env"), []byte("TAG=1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	compose := &fakeCompose{}
	rec := newTestReconciler(repoDir, compose, newTestStateStore(t))
	ctx := context.Background()

	if _, err := rec.Reconcile(ctx, nil, nil); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	if compose.upCount("nginx") != 1 || compose.upCount("redis") != 1 {
		t.Fatalf("expected both stacks to deploy once, got nginx=%d redis=%d", compose.upCount("nginx"), compose.upCount("redis"))
	}

	if err := os.WriteFile(filepath.Join(repoDir, "global.env"), []byte("TAG=2\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := rec.Reconcile(ctx, nil, nil); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if compose.upCount("nginx") != 2 || compose.upCount("redis") != 2 {
		t.Fatalf("expected both stacks to redeploy after global env change, got nginx=%d redis=%d", compose.upCount("nginx"), compose.upCount("redis"))
	}
}

func TestReconcile_DoesNotAdvanceFingerprintOnDeployFailure(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx")
	compose := &fakeCompose{upErrByName: map[string]error{"nginx": os.ErrPermission}}
	rec := newTestReconciler(repoDir, compose, newTestStateStore(t))
	ctx := context.Background()

	if _, err := rec.Reconcile(ctx, nil, nil); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if compose.upCount("nginx") != 1 {
		t.Fatalf("expected failed reconcile to attempt deploy once, got %d", compose.upCount("nginx"))
	}

	compose.upErrByName = nil
	if _, err := rec.Reconcile(ctx, nil, nil); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if compose.upCount("nginx") != 2 {
		t.Fatalf("expected stack to deploy again after previous failure, got %d", compose.upCount("nginx"))
	}
}

func TestReconcile_RemovesStoredFingerprintWhenStackDeleted(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx")
	compose := &fakeCompose{}
	store := newTestStateStore(t)
	rec := newTestReconciler(repoDir, compose, store)
	ctx := context.Background()

	if _, err := rec.Reconcile(ctx, nil, nil); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}

	if _, err := rec.Reconcile(ctx, []string{"nginx"}, nil); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}

	key := repoKey(repoDir, "conflux.yaml")
	if _, ok, err := store.Get(key, "nginx"); err != nil {
		t.Fatalf("store.Get() error = %v", err)
	} else if ok {
		t.Fatal("expected fingerprint entry to be removed after stack deletion")
	}
}

func TestReconcile_SendsNotificationWhenChanged(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx")
	compose := &fakeCompose{}
	notifier := &fakeNotifier{}
	rec := newTestReconcilerWithNotifier(repoDir, compose, newTestStateStore(t), notifier)

	result, err := rec.Reconcile(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !result.Changed() {
		t.Fatal("expected reconcile result to report changes")
	}
	if notifier.count() != 1 {
		t.Fatalf("expected 1 notification, got %d", notifier.count())
	}
	if !strings.Contains(notifier.messages[0], "Deployed: nginx") {
		t.Fatalf("expected notification to include deployed stack name, got %q", notifier.messages[0])
	}
	if !strings.Contains(notifier.messages[0], "\n") {
		t.Fatalf("expected notification to use multiline format, got %q", notifier.messages[0])
	}
}

func TestReconcile_DoesNotNotifyWhenNothingChanged(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx")
	compose := &fakeCompose{}
	notifier := &fakeNotifier{}
	rec := newTestReconcilerWithNotifier(repoDir, compose, newTestStateStore(t), notifier)

	if _, err := rec.Reconcile(context.Background(), nil, nil); err != nil {
		t.Fatalf("first Reconcile() error = %v", err)
	}
	if _, err := rec.Reconcile(context.Background(), nil, nil); err != nil {
		t.Fatalf("second Reconcile() error = %v", err)
	}
	if notifier.count() != 1 {
		t.Fatalf("expected notification count to stay at 1, got %d", notifier.count())
	}
}

func TestReconcile_NotificationIncludesFailedAndRemovedStackNames(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx")
	compose := &fakeCompose{upErrByName: map[string]error{"nginx": os.ErrPermission}}
	notifier := &fakeNotifier{}
	rec := newTestReconcilerWithNotifier(repoDir, compose, newTestStateStore(t), notifier)

	if _, err := rec.Reconcile(context.Background(), []string{"old-stack"}, nil); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if notifier.count() != 1 {
		t.Fatalf("expected 1 notification, got %d", notifier.count())
	}
	message := notifier.messages[0]
	if !strings.Contains(message, "Removed: old-stack") {
		t.Fatalf("expected notification to include removed stack name, got %q", message)
	}
	if !strings.Contains(message, "Failed: nginx") {
		t.Fatalf("expected notification to include failed stack name, got %q", message)
	}
}

func TestReconcile_NotifiesWhenOnlyFailuresOccur(t *testing.T) {
	repoDir := setupRepoDir(t, "nginx")
	compose := &fakeCompose{upErrByName: map[string]error{"nginx": os.ErrPermission}}
	notifier := &fakeNotifier{}
	rec := newTestReconcilerWithNotifier(repoDir, compose, newTestStateStore(t), notifier)

	result, err := rec.Reconcile(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !result.Changed() {
		t.Fatal("expected failure-only reconcile result to report changes")
	}
	if notifier.count() != 1 {
		t.Fatalf("expected 1 notification, got %d", notifier.count())
	}
	if !strings.Contains(notifier.messages[0], "Failed: nginx") {
		t.Fatalf("expected failure-only notification to include failed stack name, got %q", notifier.messages[0])
	}
}
