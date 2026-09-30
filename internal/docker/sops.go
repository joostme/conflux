package docker

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/getsops/sops/v3/decrypt"
)

// SOPSExtension marks a compose config or secret whose `file:` is SOPS-encrypted.
const SOPSExtension = "x-conflux-sops"

var decryptFile = func(path string) ([]byte, error) { return decrypt.File(path, "") }

// decryptMarkedFiles decrypts every config and secret flagged with
// `x-conflux-sops: true` into a private temp dir and repoints the project at
// the plaintext copies. The returned cleanup removes them.
func decryptMarkedFiles(project *types.Project) (func(), error) {
	noop := func() {}
	var dir string
	cleanup := func() {
		if dir != "" {
			_ = os.RemoveAll(dir)
		}
	}

	rewrite := func(kind, name, file string, ext types.Extensions) (string, error) {
		if marked, _ := ext[SOPSExtension].(bool); !marked || file == "" {
			return file, nil
		}
		data, err := decryptFile(file)
		if err != nil {
			return "", fmt.Errorf("decrypting %s %s (%s): %w", kind, name, file, err)
		}
		if dir == "" {
			if dir, err = os.MkdirTemp("", "conflux-sops-*"); err != nil {
				return "", fmt.Errorf("creating temp dir: %w", err)
			}
		}
		out := filepath.Join(dir, kind+"-"+name)
		if err := os.WriteFile(out, data, 0o600); err != nil {
			return "", fmt.Errorf("writing decrypted %s %s: %w", kind, name, err)
		}
		return out, nil
	}

	for name, c := range project.Configs {
		file, err := rewrite("config", name, c.File, c.Extensions)
		if err != nil {
			cleanup()
			return noop, err
		}
		c.File = file
		project.Configs[name] = c
	}
	for name, s := range project.Secrets {
		file, err := rewrite("secret", name, s.File, s.Extensions)
		if err != nil {
			cleanup()
			return noop, err
		}
		s.File = file
		project.Secrets[name] = s
	}
	return cleanup, nil
}
