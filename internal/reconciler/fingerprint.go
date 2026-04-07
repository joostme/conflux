package reconciler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

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

	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:]), nil
}
