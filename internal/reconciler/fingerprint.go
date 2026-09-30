package reconciler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/joostme/conflux/internal/stacks"
)

// repoKey returns a stable local key for persisted reconcile state.
func repoKey(repoDir, configFile string) string {
	sum := sha256.Sum256([]byte(repoDir + "\n" + configFile))
	return hex.EncodeToString(sum[:])
}

// fingerprintStack returns a deterministic hash of the desired stack state.
func fingerprintStack(stack stacks.Stack, resolvedEnv string) (string, error) {
	composeContent, err := os.ReadFile(stack.ComposeFile)
	if err != nil {
		return "", fmt.Errorf("reading compose file %s: %w", stack.ComposeFile, err)
	}

	payload := fmt.Sprintf(
		"version=1\nstack=%s\ncompose:\n%s\nenv:\n%s",
		stack.Name,
		composeContent,
		resolvedEnv,
	)

	referenced, err := referencedFiles(stack)
	if err != nil {
		return "", err
	}
	for _, f := range referenced {
		payload += fmt.Sprintf("\nfile %s:\n%s", f.name, f.content)
	}

	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:]), nil
}

type referencedFile struct {
	name    string
	content []byte
}

// referencedFiles returns the files behind top-level `configs:` and `secrets:`
// entries of the compose file, so edits to them change the stack fingerprint.
// Entries whose path uses variable interpolation are skipped.
func referencedFiles(stack stacks.Stack) ([]referencedFile, error) {
	raw, err := os.ReadFile(stack.ComposeFile)
	if err != nil {
		return nil, fmt.Errorf("reading compose file %s: %w", stack.ComposeFile, err)
	}

	var doc struct {
		Configs map[string]struct{ File string } `yaml:"configs"`
		Secrets map[string]struct{ File string } `yaml:"secrets"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing compose file %s: %w", stack.ComposeFile, err)
	}

	var files []referencedFile
	add := func(kind, name, file string) error {
		if file == "" || strings.Contains(file, "$") {
			return nil
		}
		path := file
		if !filepath.IsAbs(path) {
			path = filepath.Join(stack.Dir, path)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s %s file %s: %w", kind, name, path, err)
		}
		files = append(files, referencedFile{name: kind + "/" + name, content: content})
		return nil
	}
	for name, c := range doc.Configs {
		if err := add("config", name, c.File); err != nil {
			return nil, err
		}
	}
	for name, s := range doc.Secrets {
		if err := add("secret", name, s.File); err != nil {
			return nil, err
		}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}
