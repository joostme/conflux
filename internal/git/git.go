package git

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// Repo manages git operations for a single repository using go-git.
type Repo struct {
	url    string
	branch string
	dir    string
	sshKey string
	logger *slog.Logger

	repo *gogit.Repository
	auth transport.AuthMethod
}

// NewRepo creates a new Repo instance.
func NewRepo(url, branch, dir, sshKey string, logger *slog.Logger) *Repo {
	return &Repo{
		url:    url,
		branch: branch,
		dir:    dir,
		sshKey: sshKey,
		logger: logger,
	}
}

// getAuth returns the authentication method, creating it lazily on first call.
func (r *Repo) getAuth() (transport.AuthMethod, error) {
	if r.auth != nil {
		return r.auth, nil
	}

	if r.sshKey == "" {
		return nil, nil
	}

	keys, err := gitssh.NewPublicKeysFromFile("git", r.sshKey, "")
	if err != nil {
		return nil, fmt.Errorf("loading SSH key %s: %w", r.sshKey, err)
	}

	// Accept any host key (equivalent to StrictHostKeyChecking=no).
	// In a GitOps context the repo URL is configured by the operator,
	// so TOFU-style host key checking adds no real security.
	keys.HostKeyCallback = gossh.InsecureIgnoreHostKey()

	r.auth = keys
	return r.auth, nil
}

// refName returns the full reference name for the configured branch.
func (r *Repo) refName() plumbing.ReferenceName {
	return plumbing.NewBranchReferenceName(r.branch)
}

// remoteRefName returns the remote-tracking reference name.
func (r *Repo) remoteRefName() plumbing.ReferenceName {
	return plumbing.NewRemoteReferenceName("origin", r.branch)
}

// CloneOrOpen clones the repo if it doesn't exist, or opens an existing one.
// Returns true if a fresh clone was performed (i.e. this is the first sync).
func (r *Repo) CloneOrOpen() (bool, error) {
	if r.repo != nil {
		return false, nil
	}

	// Try to open an existing repo first
	repo, err := gogit.PlainOpen(r.dir)
	if err == nil {
		r.repo = repo
		r.logger.Info("opened existing repository", "dir", r.dir)
		return false, nil
	}
	if !errors.Is(err, gogit.ErrRepositoryNotExists) {
		return false, fmt.Errorf("opening repo at %s: %w", r.dir, err)
	}

	// Not cloned yet — perform initial clone
	if err := r.clone(); err != nil {
		return false, err
	}
	return true, nil
}

// IsCloned returns true if the repository has been cloned or opened.
func (r *Repo) IsCloned() bool {
	return r.repo != nil
}

// Fetch downloads remote changes but does NOT update the worktree.
// Returns the remote commit hash if there are new changes, or nil if
// the local branch is already up to date.
func (r *Repo) Fetch() (*plumbing.Hash, error) {
	r.logger.Debug("fetching remote changes")

	auth, err := r.getAuth()
	if err != nil {
		return nil, err
	}

	// Get current HEAD hash before fetch
	head, err := r.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("getting HEAD: %w", err)
	}
	localHash := head.Hash()

	// Fetch from remote
	err = r.repo.Fetch(&gogit.FetchOptions{
		RemoteName: "origin",
		Auth:       auth,
		Force:      true,
	})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil, fmt.Errorf("fetching: %w", err)
	}

	// Resolve remote tracking reference to get the latest remote commit
	remoteRef, err := r.repo.Reference(r.remoteRefName(), true)
	if err != nil {
		return nil, fmt.Errorf("resolving remote ref %s: %w", r.remoteRefName(), err)
	}
	remoteHash := remoteRef.Hash()

	if localHash == remoteHash {
		r.logger.Debug("no changes detected", "commit", localHash.String()[:12])
		return nil, nil
	}

	r.logger.Info("changes detected",
		"local", localHash.String()[:12],
		"remote", remoteHash.String()[:12],
	)
	return &remoteHash, nil
}

// Reset hard-resets the worktree to the given commit and cleans untracked files.
func (r *Repo) Reset(hash plumbing.Hash) error {
	r.logger.Debug("resetting worktree", "commit", hash.String()[:12])

	wt, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	if err := wt.Reset(&gogit.ResetOptions{
		Commit: hash,
		Mode:   gogit.HardReset,
	}); err != nil {
		return fmt.Errorf("resetting to %s: %w", hash.String()[:12], err)
	}

	// Clean untracked files and directories
	if err := wt.Clean(&gogit.CleanOptions{Dir: true}); err != nil {
		return fmt.Errorf("cleaning worktree: %w", err)
	}

	r.logger.Info("repository updated", "commit", hash.String()[:12])
	return nil
}

// clone performs the initial git clone.
func (r *Repo) clone() error {
	r.logger.Info("cloning repository", "url", r.url, "branch", r.branch, "dir", r.dir)

	auth, err := r.getAuth()
	if err != nil {
		return err
	}

	repo, err := gogit.PlainClone(r.dir, false, &gogit.CloneOptions{
		URL:           r.url,
		Auth:          auth,
		ReferenceName: r.refName(),
		SingleBranch:  true,
		Depth:         1,
		Progress:      os.Stdout,
	})
	if err != nil {
		return fmt.Errorf("cloning %s: %w", r.url, err)
	}

	r.repo = repo
	r.logger.Info("repository cloned successfully")
	return nil
}
