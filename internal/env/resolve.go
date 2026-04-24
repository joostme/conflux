package env

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/getsops/sops/v3/decrypt"
)

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
