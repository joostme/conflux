package env

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/joostme/conflux/internal/config"
)

// Resolver resolves env and secret files for stacks, decrypting secrets in
// memory and producing a single merged env file per stack.
// It is safe for concurrent use by multiple goroutines.
type Resolver struct {
	globalContents [][]byte
	mu             sync.Mutex
	tempFiles      []string
}

// ResolvedEnv contains the canonical merged env content for a stack.
type ResolvedEnv struct {
	Content string
}

// NewResolver resolves global env/secret files upfront and returns a Resolver
// that can produce per-stack env files via FileForStack.
func NewResolver(repoDir string, cfg *config.Config) (*Resolver, error) {
	r := &Resolver{}

	global, err := resolveContents(repoDir, cfg.Global.Environment, cfg.Global.Secrets)
	if err != nil {
		return nil, err
	}
	r.globalContents = global

	return r, nil
}

// ResolveForStack returns the canonical merged env content for a stack. All env
// and secret contents are collected in precedence order (global env, global
// secrets, stack env, stack secrets), and variable references like ${VAR} are
// expanded.
func (r *Resolver) ResolveForStack(stackDir string, stacks config.StacksConfig) (ResolvedEnv, error) {
	contents := append([][]byte{}, r.globalContents...)

	stackContents, err := resolveContents(stackDir, stacks.Environment, stacks.Secrets)
	if err != nil {
		return ResolvedEnv{}, err
	}
	contents = append(contents, stackContents...)

	return r.mergeAndExpand(contents)
}

// FileFromContent writes canonical env content to a temporary file and returns
// its path for Compose consumption.
func (r *Resolver) FileFromContent(content string) (string, error) {
	if content == "" {
		return "", nil
	}

	tmp, err := os.CreateTemp("", "conflux-resolved-*.env")
	if err != nil {
		return "", fmt.Errorf("creating resolved env temp file: %w", err)
	}
	r.mu.Lock()
	r.tempFiles = append(r.tempFiles, tmp.Name())
	r.mu.Unlock()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("writing resolved env: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return "", err
	}

	return tmp.Name(), nil
}

// Cleanup removes all temporary resolved env files.
func (r *Resolver) Cleanup() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []error
	for _, f := range r.tempFiles {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	slog.Debug("cleaned up resolved env temp files", "count", len(r.tempFiles))
	r.tempFiles = nil
	return errors.Join(errs...)
}
