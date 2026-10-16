// Package ghstack is the slice of gh-stack (github.com/github/gh-stack) that
// gh-stack-tui reads: the stacks gh-stack records for a repository, loaded the
// way gh-stack's own commands load them.
//
// gh-stack keeps its packages under internal/, and Go lets any import path
// rooted at github.com/github/gh-stack import those packages, whichever module
// it is in. This module's path sits there for that reason alone. Everything it
// exposes is its own type, so it is the only code a gh-stack upgrade can break.
package ghstack

import (
	"fmt"
	"path/filepath"

	"github.com/github/gh-stack/internal/stack"
)

// Catalog is every stack gh-stack records for a repository.
type Catalog struct {
	// Repository is the GitHub repository, as host:owner/name.
	Repository string
	// Stacks are in the order gh-stack created them, oldest first.
	Stacks []Stack
}

// Stack is a single ordered chain of branches.
type Stack struct {
	// ID and Number are GitHub's identifiers for the stack, when it has them.
	ID     string
	Number int
	// Trunk is the branch the stack is based on.
	Trunk string
	// Branches are bottom first.
	Branches []Branch
}

// Branch is one branch of a stack.
type Branch struct {
	Name string
	// Parent is the branch this one is stacked on, skipping merged branches.
	Parent string
	// Base is the parent's tip when gh-stack last rebased this branch.
	Base   string
	Merged bool
	// PR is nil until the branch has a pull request.
	PR *PullRequest
}

// PullRequest is gh-stack's record of a branch's pull request.
type PullRequest struct {
	Number int
	URL    string
}

// CatalogPath is the file Load reads in commonDir, for callers that watch it
// for changes.
func CatalogPath(commonDir string) string {
	// gh-stack does not export the name; see stackFilePath in internal/stack.
	return filepath.Join(commonDir, "gh-stack")
}

// Load reads the stacks recorded in commonDir, the git directory every
// worktree of a repository shares (git rev-parse --git-common-dir). Like
// gh-stack's own commands, it first folds in catalogs that older gh-stack
// versions kept per worktree.
func Load(commonDir string) (*Catalog, error) {
	if err := migrate(commonDir); err != nil {
		return nil, fmt.Errorf("migrating gh-stack state: %w", err)
	}
	sf, err := stack.Load(commonDir)
	if err != nil {
		return nil, err
	}
	c := &Catalog{Repository: sf.Repository, Stacks: make([]Stack, len(sf.Stacks))}
	for i := range sf.Stacks {
		s := &sf.Stacks[i]
		out := Stack{
			ID:       s.ID,
			Number:   s.Number,
			Trunk:    s.Trunk.Branch,
			Branches: make([]Branch, len(s.Branches)),
		}
		for j := range s.Branches {
			b := &s.Branches[j]
			out.Branches[j] = Branch{
				Name:   b.Branch,
				Parent: s.ActiveBaseBranch(b.Branch),
				Base:   b.Base,
				Merged: b.IsMerged(),
			}
			if b.PullRequest != nil {
				out.Branches[j].PR = &PullRequest{
					Number: b.PullRequest.Number,
					URL:    b.PullRequest.URL,
				}
			}
		}
		c.Stacks[i] = out
	}
	return c, nil
}

// migrate mirrors what gh-stack's read-only commands do before loading (see
// stackStateDir in cmd/worktree_utils.go): only when legacy state exists, take
// the operation lock so no running command races the migration.
func migrate(commonDir string) error {
	legacy, err := stack.HasLegacyState(commonDir)
	if err != nil || !legacy {
		return err
	}
	lock, err := stack.LockOperation(commonDir)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	return stack.MigrateLegacyState(commonDir)
}
