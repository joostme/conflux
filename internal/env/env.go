package env

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/getsops/sops/v3/decrypt"
	"github.com/joho/godotenv"
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

// mergeAndExpand concatenates all env content slices, parses them as a single
// unit using godotenv (which expands ${VAR} references against previously
// parsed values).
func (r *Resolver) mergeAndExpand(contents [][]byte) (ResolvedEnv, error) {
	if len(contents) == 0 {
		return ResolvedEnv{}, nil
	}

	var combined []byte
	for _, data := range contents {
		// Ensure each chunk ends with a newline so keys don't merge.
		if len(data) > 0 && data[len(data)-1] != '\n' {
			combined = append(combined, data...)
			combined = append(combined, '\n')
		} else {
			combined = append(combined, data...)
		}
	}

	merged, err := godotenv.Unmarshal(string(combined))
	if err != nil {
		return ResolvedEnv{}, fmt.Errorf("parsing env files for variable expansion: %w", err)
	}

	return ResolvedEnv{Content: marshalComposeEnv(merged)}, nil
}

// marshalComposeEnv writes env values verbatim for Docker Compose.
// Keys are sorted so the generated content stays stable across reconcile loops,
// which keeps stack fingerprinting from triggering unnecessary redeploys.
func marshalComposeEnv(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(values[key])
		b.WriteByte('\n')
	}

	return b.String()
}

// resolveContents reads environment files from disk and decrypts secret files
// in memory, returning their contents in order: env then secrets. No temporary
// files are created.
func resolveContents(baseDir string, environment, secrets []string) ([][]byte, error) {
	var contents [][]byte

	for _, name := range environment {
		p := findFile(baseDir, name)
		if p == "" {
			slog.Warn("environment file not found, skipping", "file", name)
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("reading environment file %s: %w", p, err)
		}
		contents = append(contents, data)
	}

	for _, name := range secrets {
		p := findFile(baseDir, name)
		if p == "" {
			slog.Warn("secret file not found, skipping", "file", name)
			continue
		}
		data, err := decrypt.File(p, "")
		if err != nil {
			return nil, fmt.Errorf("decrypting secret file %s: %w", p, err)
		}
		contents = append(contents, data)
	}

	return contents, nil
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

// findFile returns the path if name is a regular file under baseDir, or "".
func findFile(baseDir, name string) string {
	if name == "" {
		return ""
	}
	path := filepath.Join(baseDir, name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}
