package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/joostme/conflux/internal/config"
	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/stacks"
)

// Decryptor decrypts SOPS-encrypted files and returns the plaintext content.
type Decryptor interface {
	Decrypt(srcPath string) ([]byte, error)
}

// ComposeService deploys and tears down Docker Compose stacks.
type ComposeService interface {
	Up(ctx context.Context, stack stacks.Stack, envFiles []string) error
	Down(ctx context.Context, stackName string) error
}

// NetworkService manages Docker networks.
type NetworkService interface {
	Ensure(ctx context.Context, networks map[string]config.NetworkConfig) error
	Remove(ctx context.Context, names []string) error
}

// Reconciler manages the reconciliation loop between git state and running stacks.
type Reconciler struct {
	repoDir    string
	configFile string
	decryptor  Decryptor
	compose    ComposeService
	networks   NetworkService
	logger     *slog.Logger
}

// New creates a new Reconciler. Dependencies are accepted as interfaces to
// allow testing with mocks — the concrete types (*sops.Decryptor,
// *stacks.ComposeClient, *networks.Manager) satisfy these interfaces implicitly.
func New(repoDir, configFile string, decryptor Decryptor, compose ComposeService, networkSvc NetworkService, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		repoDir:    repoDir,
		configFile: configFile,
		decryptor:  decryptor,
		compose:    compose,
		networks:   networkSvc,
		logger:     logger,
	}
}

// Reconcile performs a full reconciliation:
// 1. Parse config
// 2. Ensure global networks exist
// 3. Decrypt global secrets
// 4. Discover stacks
// 5. Deploy each stack
// 6. Remove stacks that were present before the pull but are now gone
// 7. Remove networks that were present before the pull but are now gone
//
// The removedStacks and removedNetworks parameters contain names that existed
// in the worktree before the latest git pull but no longer exist after it.
// These are the only resources that will be torn down — resources that were
// never managed by conflux are never touched. Pass nil to skip removals
// (e.g. on initial clone when there is no prior state to compare against).
//
// Networks are removed after stacks so that stacks still using a network
// are torn down before the network itself is removed.
//
// Decrypted secrets are held in memory and written to temporary files only
// for the duration of compose loading. Temp files are removed immediately
// after each reconciliation cycle completes.
func (r *Reconciler) Reconcile(ctx context.Context, removedStacks, removedNetworks []string) error {
	r.logger.Info("starting reconciliation")

	// Track temp files so we can clean them all up at the end
	var tempFiles []string
	defer func() {
		for _, f := range tempFiles {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				r.logger.Warn("failed to remove temp secret file", "file", f, "error", err)
			}
		}
		r.logger.Debug("cleaned up decrypted secret temp files", "count", len(tempFiles))
	}()

	// 1. Parse config
	cfg, err := r.loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	r.logger.Info("config loaded",
		"stacks_dir", cfg.Stacks.Directory,
		"global_env", cfg.Global.Environment,
		"global_secrets", cfg.Global.Secrets,
		"networks", len(cfg.Networks),
	)

	// 2. Ensure global networks exist before any stacks are deployed
	if len(cfg.Networks) > 0 {
		r.logger.Info("ensuring networks", "count", len(cfg.Networks))
		if err := r.networks.Ensure(ctx, cfg.Networks); err != nil {
			return fmt.Errorf("ensuring networks: %w", err)
		}
	}

	// 3. Process global env files (decrypt secrets, resolve paths)
	globalEnvFiles, err := r.resolveGlobalEnvFiles(cfg)
	if err != nil {
		return fmt.Errorf("resolving global env files: %w", err)
	}

	globalSecretFiles, err := r.decryptSecretFiles(cfg.Global.Secrets, r.repoDir)
	if err != nil {
		return fmt.Errorf("resolving global secret files: %w", err)
	}
	tempFiles = append(tempFiles, globalSecretFiles...)

	// 4. Discover stacks
	discovered, err := stacks.Discover(r.repoDir, cfg)
	if err != nil {
		return fmt.Errorf("discovering stacks: %w", err)
	}
	r.logger.Info("stacks discovered", "count", len(discovered))

	// 5. Deploy each stack
	for _, stack := range discovered {
		stackTmpPaths, err := r.deployStack(ctx, stack, cfg, globalEnvFiles, globalSecretFiles)
		if err != nil {
			r.logger.Error("failed to deploy stack", "stack", stack.Name, "error", err)
			// Continue with other stacks
			continue
		}
		tempFiles = append(tempFiles, stackTmpPaths...)
	}

	// 6. Remove stacks that were present before the pull but are now gone
	for _, name := range removedStacks {
		r.logger.Info("stack removed from repo, tearing down", "stack", name)
		if err := r.compose.Down(ctx, name); err != nil {
			r.logger.Error("failed to remove stack", "stack", name, "error", err)
		}
	}

	// 7. Remove networks that were present before the pull but are now gone.
	//    This happens after stack removal so containers are torn down first.
	if len(removedNetworks) > 0 {
		r.logger.Info("removing networks", "count", len(removedNetworks))
		if err := r.networks.Remove(ctx, removedNetworks); err != nil {
			r.logger.Error("failed to remove networks", "error", err)
		}
	}

	r.logger.Info("reconciliation complete",
		"deployed", len(discovered),
		"removed_stacks", len(removedStacks),
		"removed_networks", len(removedNetworks),
	)
	return nil
}

// resolveGlobalEnvFiles returns absolute paths for global environment files.
func (r *Reconciler) resolveGlobalEnvFiles(cfg *config.Config) ([]string, error) {
	var files []string
	for _, f := range cfg.Global.Environment {
		absPath := filepath.Join(r.repoDir, f)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			r.logger.Warn("global environment file not found, skipping", "file", f)
			continue
		}
		files = append(files, absPath)
	}
	return files, nil
}

// decryptSecretFiles decrypts a list of secret files (relative to baseDir) and
// writes each to a temporary file. It returns the temp file paths which serve
// double duty as both env-file arguments and cleanup targets.
func (r *Reconciler) decryptSecretFiles(secrets []string, baseDir string) (tmpPaths []string, err error) {
	for _, f := range secrets {
		srcPath := filepath.Join(baseDir, f)
		if _, statErr := os.Stat(srcPath); os.IsNotExist(statErr) {
			r.logger.Warn("secret file not found, skipping", "file", f)
			continue
		}

		data, decErr := r.decryptor.Decrypt(srcPath)
		if decErr != nil {
			return tmpPaths, fmt.Errorf("decrypting secret %s: %w", f, decErr)
		}

		tmpFile, tmpErr := os.CreateTemp("", "conflux-secret-*.env")
		if tmpErr != nil {
			return tmpPaths, fmt.Errorf("creating temp file for %s: %w", f, tmpErr)
		}

		if _, writeErr := tmpFile.Write(data); writeErr != nil {
			tmpFile.Close()
			return tmpPaths, fmt.Errorf("writing temp file for %s: %w", f, writeErr)
		}
		tmpFile.Close()

		// Restrict permissions — only the current process needs to read this
		if chmodErr := os.Chmod(tmpFile.Name(), 0600); chmodErr != nil {
			return tmpPaths, fmt.Errorf("setting permissions on temp file for %s: %w", f, chmodErr)
		}

		tmpPaths = append(tmpPaths, tmpFile.Name())
	}
	return tmpPaths, nil
}

// deployStack builds the env file list and deploys a single stack.
// Returns the list of temp file paths created for stack-level secrets.
//
// Ordering (lowest → highest priority, last --env-file wins in docker compose):
//  1. Global environment files
//  2. Global secret files (decrypted)
//  3. Stack-level environment files (if present)
//  4. Stack-level secret files (if present, decrypted)
func (r *Reconciler) deployStack(ctx context.Context, stack stacks.Stack, cfg *config.Config, globalEnvFiles, globalSecretFiles []string) ([]string, error) {
	r.logger.Info("processing stack", "stack", stack.Name)

	var envFiles []string

	// Always include global environment files first (lowest priority)
	envFiles = append(envFiles, globalEnvFiles...)

	// Always include global secret files (override global env on conflict)
	envFiles = append(envFiles, globalSecretFiles...)

	// Stack-level environment files override globals if present
	if len(stack.EnvFiles) > 0 {
		r.logger.Debug("adding stack-level environment", "stack", stack.Name)
		envFiles = append(envFiles, stack.EnvFiles...)
	}

	// Stack-level secret files override everything if present
	var tempPaths []string
	if len(stack.SecretFiles) > 0 {
		r.logger.Debug("adding stack-level secrets", "stack", stack.Name)
		stackSecretPaths, err := r.decryptSecretFiles(
			toRelativePaths(stack.SecretFiles, stack.Dir),
			stack.Dir,
		)
		if err != nil {
			return nil, fmt.Errorf("decrypting stack secrets for %s: %w", stack.Name, err)
		}
		envFiles = append(envFiles, stackSecretPaths...)
		tempPaths = stackSecretPaths
	}

	if err := r.compose.Up(ctx, stack, envFiles); err != nil {
		return tempPaths, err
	}
	return tempPaths, nil
}

// loadConfig loads and returns the parsed configuration. This is the single
// entry point for config loading in the reconciler — previously each
// discovery method and Reconcile() independently called config.Load().
func (r *Reconciler) loadConfig() (*config.Config, error) {
	return config.Load(r.repoDir, r.configFile)
}

// Snapshot returns both stack names and network names from the current worktree
// by loading the config only once. Previously snapshotState called
// DiscoverStackNames + DiscoverNetworkNames which each loaded config
// independently — 2 redundant parses per snapshot, 4 per poll cycle.
func (r *Reconciler) Snapshot() (stackNames map[string]bool, networkNames map[string]bool, err error) {
	cfg, err := r.loadConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	stackNames, err = discoverStackNames(r.repoDir, cfg)
	if err != nil {
		return nil, nil, err
	}

	networkNames = networks.ResolveNames(cfg.Networks)
	return stackNames, networkNames, nil
}

// discoverStackNames is the internal helper that works with an already-loaded config.
func discoverStackNames(repoDir string, cfg *config.Config) (map[string]bool, error) {
	discovered, err := stacks.Discover(repoDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("discovering stacks: %w", err)
	}
	names := make(map[string]bool, len(discovered))
	for _, s := range discovered {
		names[s.Name] = true
	}
	return names, nil
}

// toRelativePaths converts absolute file paths to paths relative to baseDir.
func toRelativePaths(absPaths []string, baseDir string) []string {
	rel := make([]string, 0, len(absPaths))
	for _, p := range absPaths {
		r, err := filepath.Rel(baseDir, p)
		if err != nil {
			// Fall back to basename if Rel fails
			r = filepath.Base(p)
		}
		rel = append(rel, r)
	}
	return rel
}
