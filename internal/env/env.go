package env

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/getsops/sops/v3/decrypt"
	"github.com/joostme/conflux/internal/config"
)

// Resolver resolves env and secret files for stacks, decrypting secrets into
// temporary files that are removed by Cleanup.
type Resolver struct {
	globalFiles []string
	tempFiles   []string
}

// NewResolver resolves global env/secret files upfront and returns a Resolver
// that can append per-stack files via FilesForStack.
func NewResolver(repoDir string, cfg *config.Config) (*Resolver, error) {
	r := &Resolver{}

	for _, name := range cfg.Global.Environment {
		if p := findFile(repoDir, name); p != "" {
			r.globalFiles = append(r.globalFiles, p)
		} else {
			slog.Warn("environment file not found, skipping", "file", name)
		}
	}

	for _, name := range cfg.Global.Secrets {
		p := findFile(repoDir, name)
		if p == "" {
			slog.Warn("secret file not found, skipping", "file", name)
			continue
		}
		tmp, err := r.decryptToTemp(p)
		if err != nil {
			return nil, err
		}
		r.globalFiles = append(r.globalFiles, tmp)
	}

	return r, nil
}

// FilesForStack returns env file paths for a stack in precedence order:
// global env, global secrets (decrypted), stack env, stack secrets (decrypted).
func (r *Resolver) FilesForStack(stackDir string, stacks config.StacksConfig) ([]string, error) {
	files := append([]string{}, r.globalFiles...)

	if p := findFile(stackDir, stacks.Environment); p != "" {
		files = append(files, p)
	}

	if p := findFile(stackDir, stacks.Secrets); p != "" {
		tmp, err := r.decryptToTemp(p)
		if err != nil {
			return nil, err
		}
		files = append(files, tmp)
	}

	return files, nil
}

// Cleanup removes all temporary decrypted files.
func (r *Resolver) Cleanup() error {
	var errs []error
	for _, f := range r.tempFiles {
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	slog.Debug("cleaned up decrypted secret temp files", "count", len(r.tempFiles))
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

func (r *Resolver) decryptToTemp(srcPath string) (string, error) {
	data, err := decrypt.File(srcPath, "")
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", "conflux-secret-*.env")
	if err != nil {
		return "", err
	}
	r.tempFiles = append(r.tempFiles, f.Name())

	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return "", err
	}
	return f.Name(), os.Chmod(f.Name(), 0600)
}
