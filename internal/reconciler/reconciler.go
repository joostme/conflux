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
	configFile string
	decryptor  *sops.Decryptor
	compose    *stacks.ComposeClient
	logger     *slog.Logger

	// Track previously deployed stacks for removal detection.
	// Maps stack name → compose file path.
	knownStacks map[string]string
}

// New creates a new Reconciler.
func New(repoDir, configFile string, decryptor *sops.Decryptor, compose *stacks.ComposeClient, logger *slog.Logger) *Reconciler {
	return &Reconciler{
		repoDir:     repoDir,
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
//
// Decrypted secrets are held in memory and written to temporary files only
// for the duration of compose loading. Temp files are removed immediately
// after each reconciliation cycle completes.
func (r *Reconciler) Reconcile(ctx context.Context) error {
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

	globalSecretFiles, tmpPaths, err := r.decryptSecretFiles(cfg.Global.Secrets, r.repoDir)
	if err != nil {
		return fmt.Errorf("resolving global secret files: %w", err)
	}
	tempFiles = append(tempFiles, tmpPaths...)

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

		stackTmpPaths, err := r.deployStack(ctx, stack, cfg, globalEnvFiles, globalSecretFiles)
		if err != nil {
			r.logger.Error("failed to deploy stack", "stack", stack.Name, "error", err)
			// Continue with other stacks
			continue
		}
		tempFiles = append(tempFiles, stackTmpPaths...)
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

// decryptSecretFiles decrypts a list of secret files (relative to baseDir) and
// writes each to a temporary file. It returns the temp file paths for use as
// env-files and also returns the list of temp paths for cleanup tracking.
func (r *Reconciler) decryptSecretFiles(secrets []string, baseDir string) (envPaths []string, tempPaths []string, err error) {
	for _, f := range secrets {
		srcPath := filepath.Join(baseDir, f)
		if _, statErr := os.Stat(srcPath); os.IsNotExist(statErr) {
			r.logger.Warn("secret file not found, skipping", "file", f)
			continue
		}

		data, decErr := r.decryptor.Decrypt(srcPath)
		if decErr != nil {
			return nil, tempPaths, fmt.Errorf("decrypting secret %s: %w", f, decErr)
		}

		tmpFile, tmpErr := os.CreateTemp("", "conflux-secret-*.env")
		if tmpErr != nil {
			return nil, tempPaths, fmt.Errorf("creating temp file for %s: %w", f, tmpErr)
		}

		if _, writeErr := tmpFile.Write(data); writeErr != nil {
			tmpFile.Close()
			return nil, tempPaths, fmt.Errorf("writing temp file for %s: %w", f, writeErr)
		}
		tmpFile.Close()

		// Restrict permissions — only the current process needs to read this
		if chmodErr := os.Chmod(tmpFile.Name(), 0600); chmodErr != nil {
			return nil, tempPaths, fmt.Errorf("setting permissions on temp file for %s: %w", f, chmodErr)
		}

		envPaths = append(envPaths, tmpFile.Name())
		tempPaths = append(tempPaths, tmpFile.Name())
	}
	return envPaths, tempPaths, nil
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
		stackSecretPaths, tmpPaths, err := r.decryptSecretFiles(
			toRelativePaths(stack.SecretFiles, stack.Dir),
			stack.Dir,
		)
		if err != nil {
			return nil, fmt.Errorf("decrypting stack secrets for %s: %w", stack.Name, err)
		}
		envFiles = append(envFiles, stackSecretPaths...)
		tempPaths = tmpPaths
	}

	if err := r.compose.Up(ctx, stack, envFiles); err != nil {
		return tempPaths, err
	}
	return tempPaths, nil
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
