package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/joostme/conflux/internal/config"
	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/sops"
	"github.com/joostme/conflux/internal/stacks"
)

// Reconciler manages the reconciliation loop between git state and running stacks.
type Reconciler struct {
	repoDir    string
	workDir    string
	configFile string
	decryptor  *sops.Decryptor
	compose    *stacks.ComposeClient
	logger     *slog.Logger

	// Track previously deployed stacks for removal detection.
	// Maps stack name → compose file path (in workDir).
	knownStacks map[string]string
}

// New creates a new Reconciler.
func New(repoDir, workDir, configFile string, decryptor *sops.Decryptor, compose *stacks.ComposeClient, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		repoDir:     repoDir,
		workDir:     workDir,
		configFile:  configFile,
		decryptor:   decryptor,
		compose:     compose,
		logger:      logger,
		knownStacks: make(map[string]string),
	}
}

// Reconcile performs a full reconciliation:
// 1. Parse config
// 2. Ensure global networks exist
// 3. Decrypt global secrets
// 4. Discover stacks
// 5. Deploy each stack
// 6. Remove stacks no longer in the repo
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.logger.Info("starting reconciliation")

	// Ensure work directory exists
	if err := os.MkdirAll(r.workDir, 0755); err != nil {
		return fmt.Errorf("creating work dir: %w", err)
	}

	// 1. Parse config
	cfg, err := config.Load(r.repoDir, r.configFile)
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
		if err := networks.Ensure(ctx, cfg.Networks, r.logger); err != nil {
			return fmt.Errorf("ensuring networks: %w", err)
		}
	}

	// 3. Process global env files (decrypt secrets, resolve paths)
	globalEnvFiles, err := r.resolveGlobalEnvFiles(cfg)
	if err != nil {
		return fmt.Errorf("resolving global env files: %w", err)
	}

	globalSecretFiles, err := r.resolveGlobalSecretFiles(cfg)
	if err != nil {
		return fmt.Errorf("resolving global secret files: %w", err)
	}

	// 4. Discover stacks
	discovered, err := stacks.Discover(r.repoDir, cfg)
	if err != nil {
		return fmt.Errorf("discovering stacks: %w", err)
	}
	r.logger.Info("stacks discovered", "count", len(discovered))

	// Track current stack names for removal detection
	currentStacks := make(map[string]bool)

	// 5. Deploy each stack
	for _, stack := range discovered {
		currentStacks[stack.Name] = true

		if err := r.deployStack(ctx, stack, cfg, globalEnvFiles, globalSecretFiles); err != nil {
			r.logger.Error("failed to deploy stack", "stack", stack.Name, "error", err)
			// Continue with other stacks
			continue
		}
	}

	// 6. Remove stacks that are no longer in the repo
	for name := range r.knownStacks {
		if currentStacks[name] {
			continue
		}

		r.logger.Info("stack removed from repo, tearing down", "stack", name)
		if err := r.compose.Down(ctx, name); err != nil {
			r.logger.Error("failed to remove stack", "stack", name, "error", err)
		}

		delete(r.knownStacks, name)
	}

	// Update known stacks
	for name := range currentStacks {
		r.knownStacks[name] = name
	}

	r.logger.Info("reconciliation complete", "deployed", len(discovered))
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

// resolveGlobalSecretFiles decrypts global secret files and returns paths to decrypted versions.
// All files referenced as secrets are assumed to be SOPS-encrypted and will be decrypted.
func (r *Reconciler) resolveGlobalSecretFiles(cfg *config.Config) ([]string, error) {
	var files []string
	for _, f := range cfg.Global.Secrets {
		srcPath := filepath.Join(r.repoDir, f)
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			r.logger.Warn("global secrets file not found, skipping", "file", f)
			continue
		}

		destPath := sops.DecryptedPath(r.workDir, f)
		if err := r.decryptor.DecryptFile(srcPath, destPath); err != nil {
			return nil, fmt.Errorf("decrypting global secret %s: %w", f, err)
		}
		files = append(files, destPath)
	}
	return files, nil
}

// deployStack builds the env file list and deploys a single stack.
//
// Ordering (lowest → highest priority, last --env-file wins in docker compose):
//  1. Global environment files
//  2. Global secret files (decrypted)
//  3. Stack-level environment files (if present)
//  4. Stack-level secret files (if present, decrypted)
func (r *Reconciler) deployStack(ctx context.Context, stack stacks.Stack, cfg *config.Config, globalEnvFiles, globalSecretFiles []string) error {
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
	if len(stack.SecretFiles) > 0 {
		r.logger.Debug("adding stack-level secrets", "stack", stack.Name)
		for _, sf := range stack.SecretFiles {
			destPath := sops.DecryptedPath(r.workDir, filepath.Join(cfg.Stacks.Directory, stack.Name, filepath.Base(sf)))
			if err := r.decryptor.DecryptFile(sf, destPath); err != nil {
				return fmt.Errorf("decrypting stack secret %s: %w", sf, err)
			}
			envFiles = append(envFiles, destPath)
		}
	}

	return r.compose.Up(ctx, stack, envFiles)
}
