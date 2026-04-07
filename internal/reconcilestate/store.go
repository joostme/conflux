package reconcilestate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultStatePath = "/data/reconcile-state.json"

// Store persists last-successful stack fingerprints outside the git worktree.
type Store struct {
	path string
	mu   sync.Mutex
}

type stateFile struct {
	Version int                  `json:"version"`
	Repos   map[string]repoState `json:"repos,omitempty"`
}

type repoState struct {
	Stacks map[string]stackState `json:"stacks,omitempty"`
}

type stackState struct {
	Fingerprint string    `json:"fingerprint"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// New creates a Store for the given application name under the user state dir.
func New() *Store {
	path := os.Getenv("CONFLUX_STATE_FILE")
	if path == "" {
		path = defaultStatePath
	}
	return &Store{path: path}
}

// Get returns the stored fingerprint for a repo/stack pair.
func (s *Store) Get(repoKey, stackName string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load()
	if err != nil {
		return "", false, err
	}

	repo, ok := state.Repos[repoKey]
	if !ok {
		return "", false, nil
	}

	stack, ok := repo.Stacks[stackName]
	if !ok {
		return "", false, nil
	}

	return stack.Fingerprint, true, nil
}

// Put stores the latest successful fingerprint for a repo/stack pair.
func (s *Store) Put(repoKey, stackName, fingerprint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load()
	if err != nil {
		return err
	}

	if state.Repos == nil {
		state.Repos = make(map[string]repoState)
	}

	repo := state.Repos[repoKey]
	if repo.Stacks == nil {
		repo.Stacks = make(map[string]stackState)
	}

	repo.Stacks[stackName] = stackState{
		Fingerprint: fingerprint,
		UpdatedAt:   time.Now().UTC(),
	}
	state.Repos[repoKey] = repo

	return s.write(state)
}

// Delete removes a stored fingerprint for a repo/stack pair.
func (s *Store) Delete(repoKey, stackName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.load()
	if err != nil {
		return err
	}

	repo, ok := state.Repos[repoKey]
	if !ok {
		return nil
	}

	delete(repo.Stacks, stackName)
	if len(repo.Stacks) == 0 {
		delete(state.Repos, repoKey)
	} else {
		state.Repos[repoKey] = repo
	}

	return s.write(state)
}

func (s *Store) load() (stateFile, error) {
	state := stateFile{Version: 1, Repos: make(map[string]repoState)}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return stateFile{}, fmt.Errorf("reading reconcile state %s: %w", s.path, err)
	}

	if len(data) == 0 {
		return state, nil
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return stateFile{}, fmt.Errorf("parsing reconcile state %s: %w", s.path, err)
	}

	if state.Version == 0 {
		state.Version = 1
	}
	if state.Repos == nil {
		state.Repos = make(map[string]repoState)
	}

	return state, nil
}

func (s *Store) write(state stateFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("creating reconcile state dir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding reconcile state: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), "reconcile-state-*.json")
	if err != nil {
		return fmt.Errorf("creating temp reconcile state file: %w", err)
	}
	tmpPath := tmp.Name()

	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("setting reconcile state file permissions: %w", err)
	}

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing reconcile state: %w", err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing reconcile state temp file: %w", err)
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replacing reconcile state file: %w", err)
	}

	return nil
}
