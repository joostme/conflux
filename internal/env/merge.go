package env

import (
	"fmt"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

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
