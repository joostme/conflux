package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/getsops/sops/v3/decrypt"
	"github.com/joostme/conflux/internal/config"
	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/stacks"
)

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
	compose    ComposeService
	networks   NetworkService
}

// New creates a new Reconciler.
func New(repoDir, configFile string, compose ComposeService, networkSvc NetworkService) *Reconciler {
	return &Reconciler{
		repoDir:    repoDir,
		configFile: configFile,
		compose:    compose,
		networks:   networkSvc,
	}
}

// Reconcile performs a full reconciliation cycle:
//  1. Ensure global networks
//  2. Decrypt global secrets
//  3. Discover and deploy stacks
//  4. Remove stacks/networks that disappeared from git
//
// Networks are ensured before stacks and removed after stacks so that
// dependencies are respected. Context cancellation is checked between
// each major phase and individual stack operation.
func (r *Reconciler) Reconcile(ctx context.Context, removedStacks, removedNetworks []string) error {
	slog.Info("starting reconciliation")

	// Track temp files for cleanup
	var tempFiles []string
	defer func() {
		for _, f := range tempFiles {
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				slog.Warn("failed to remove temp secret file", "file", f, "error", err)
			}
		}
		slog.Debug("cleaned up decrypted secret temp files", "count", len(tempFiles))
	}()

	// 1. Parse config
	cfg, err := r.loadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	slog.Info("config loaded",
		"stacks_dir", cfg.Stacks.Directory,
		"global_env", cfg.Global.Environment,
		"global_secrets", cfg.Global.Secrets,
		"networks", len(cfg.Networks),
	)

	// 2. Ensure global networks exist before any stacks are deployed
	if len(cfg.Networks) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		slog.Info("ensuring networks", "count", len(cfg.Networks))
		if err := r.networks.Ensure(ctx, cfg.Networks); err != nil {
			return fmt.Errorf("ensuring networks: %w", err)
		}
	}

	// 3. Process global env files (decrypt secrets, resolve paths)
	globalEnvFiles := r.resolveGlobalEnvFiles(cfg)

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
	slog.Info("stacks discovered", "count", len(discovered))

	// 5. Deploy each stack
	deployed := 0
	for _, stack := range discovered {
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted", "deployed", deployed, "remaining", len(discovered)-deployed)
			return err
		}
		stackTmpPaths, err := r.deployStack(ctx, stack, cfg, globalEnvFiles, globalSecretFiles)
		if err != nil {
			slog.Error("failed to deploy stack", "stack", stack.Name, "error", err)
			// Continue with other stacks
			deployed++
			continue
		}
		tempFiles = append(tempFiles, stackTmpPaths...)
		deployed++
	}

	// 6. Remove deleted stacks
	for _, name := range removedStacks {
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted during stack removal")
			return err
		}
		slog.Info("stack removed from repo, tearing down", "stack", name)
		if err := r.compose.Down(ctx, name); err != nil {
			slog.Error("failed to remove stack", "stack", name, "error", err)
		}
	}

	// 7. Remove deleted networks (after stacks so containers are torn down first)
	if len(removedNetworks) > 0 {
		if err := ctx.Err(); err != nil {
			slog.Warn("reconciliation interrupted before network removal")
			return err
		}
		slog.Info("removing networks", "count", len(removedNetworks))
		if err := r.networks.Remove(ctx, removedNetworks); err != nil {
			slog.Error("failed to remove networks", "error", err)
		}
	}

	slog.Info("reconciliation complete",
		"deployed", deployed,
		"removed_stacks", len(removedStacks),
		"removed_networks", len(removedNetworks),
	)
	return nil
}

// resolveGlobalEnvFiles returns absolute paths for global environment files, skipping missing ones.
func (r *Reconciler) resolveGlobalEnvFiles(cfg *config.Config) []string {
	var files []string
	for _, f := range cfg.Global.Environment {
		absPath := filepath.Join(r.repoDir, f)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			slog.Warn("global environment file not found, skipping", "file", f)
			continue
		}
		files = append(files, absPath)
	}
	return files
}

// decryptSecretFiles decrypts secret files and writes each to a temp file.
// Returns temp file paths for use as env-file arguments and cleanup.
func (r *Reconciler) decryptSecretFiles(secrets []string, baseDir string) (tmpPaths []string, err error) {
	for _, f := range secrets {
		srcPath := filepath.Join(baseDir, f)
		if _, statErr := os.Stat(srcPath); os.IsNotExist(statErr) {
			slog.Warn("secret file not found, skipping", "file", f)
			continue
		}

		data, decErr := decrypt.File(srcPath, "")
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

		if chmodErr := os.Chmod(tmpFile.Name(), 0600); chmodErr != nil {
			return tmpPaths, fmt.Errorf("setting permissions on temp file for %s: %w", f, chmodErr)
		}

		tmpPaths = append(tmpPaths, tmpFile.Name())
	}
	return tmpPaths, nil
}

// deployStack builds the env file list and deploys a single stack.
// Returns temp paths created for stack-level secrets.
//
// Env file priority (last --env-file wins in docker compose):
//  1. Global environment files
//  2. Global secret files (decrypted)
//  3. Stack-level environment files
//  4. Stack-level secret files (decrypted)
func (r *Reconciler) deployStack(ctx context.Context, stack stacks.Stack, cfg *config.Config, globalEnvFiles, globalSecretFiles []string) ([]string, error) {
	slog.Info("processing stack", "stack", stack.Name)

	var envFiles []string

	envFiles = append(envFiles, globalEnvFiles...)
	envFiles = append(envFiles, globalSecretFiles...)

	// Stack-level environment files
	if len(stack.EnvFiles) > 0 {
		slog.Debug("adding stack-level environment", "stack", stack.Name)
		envFiles = append(envFiles, stack.EnvFiles...)
	}

	// Stack-level secret files
	var tempPaths []string
	if len(stack.SecretFiles) > 0 {
		slog.Debug("adding stack-level secrets", "stack", stack.Name)
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

// loadConfig loads and returns the parsed configuration.
func (r *Reconciler) loadConfig() (*config.Config, error) {
	return config.Load(r.repoDir, r.configFile)
}

// Snapshot returns stack and network names from the current worktree.
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

// discoverStackNames returns stack names using an already-loaded config.
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
