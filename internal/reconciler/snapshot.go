package reconciler

import (
	"fmt"

	"github.com/joostme/conflux/internal/networks"
	"github.com/joostme/conflux/internal/stacks"
)

// Snapshot returns stack and network names from the current worktree.
func (r *Reconciler) Snapshot() (stackNames map[string]bool, networkNames map[string]bool, err error) {
	cfg, err := r.loadConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	discovered, err := stacks.Discover(r.repoDir, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("discovering stacks: %w", err)
	}
	stackNames = make(map[string]bool, len(discovered))
	for _, s := range discovered {
		stackNames[s.Name] = true
	}

	networkNames = networks.ResolveNames(cfg.Networks)
	return stackNames, networkNames, nil
}
