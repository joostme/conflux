package stacks

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joostme/conflux/internal/config"
)

// Stack represents a discovered docker compose stack.
type Stack struct {
	Name        string
	Dir         string // absolute path to stack directory
	ComposeFile string // absolute path to compose file
}

// Discover scans the stacks directory and returns all discovered stacks.
func Discover(repoDir string, cfg *config.Config) ([]Stack, error) {
	stacksDir := filepath.Join(repoDir, cfg.Stacks.Directory)

	entries, err := os.ReadDir(stacksDir)
	if err != nil {
		return nil, fmt.Errorf("reading stacks directory %s: %w", stacksDir, err)
	}

	var stacks []Stack
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		stackDir := filepath.Join(stacksDir, entry.Name())
		composeFile := filepath.Join(stackDir, cfg.Stacks.File)

		if _, err := os.Stat(composeFile); os.IsNotExist(err) {
			continue
		}

		stack := Stack{
			Name:        entry.Name(),
			Dir:         stackDir,
			ComposeFile: composeFile,
		}

		stacks = append(stacks, stack)
	}

	return stacks, nil
}
