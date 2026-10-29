package stack

import (
	"fmt"
	"os"

	"github.com/drew-council/gh-stack-tui/internal/git"
)

// Ref names one stack of the stack file.
type Ref struct {
	Key    string
	Number int
}

// Source reads the stacks of a git repository.
type Source struct {
	repo *git.Repo
}

// NewSource returns the stacks recorded in repo.
func NewSource(repo *git.Repo) *Source { return &Source{repo: repo} }

// Dir is the top level of the working tree, where commands run.
func (s *Source) Dir() string { return s.repo.Root }

// Load resolves the stack to show; see [Select] for pinned and previous.
func (s *Source) Load(pinned, previous string) (*Snapshot, error) {
	return Load(s.repo, pinned, previous)
}

// Stacks lists the stacks in the stack file's order.
func (s *Source) Stacks() ([]Ref, error) {
	f, err := ReadFile(s.repo.CommonDir)
	if err != nil {
		return nil, err
	}
	refs := make([]Ref, len(f.Stacks))
	for i, st := range f.Stacks {
		refs[i] = Ref{Key: Key(st), Number: st.Number}
	}
	return refs, nil
}

// Fingerprint cheaply summarizes the branch heads and the files that hold
// stack and rebase state. It changes whenever a reload would show something
// new.
func (s *Source) Fingerprint() (string, error) {
	var extra []string
	for _, p := range StateFiles(s.repo) {
		if fi, err := os.Stat(p); err == nil {
			extra = append(extra, fmt.Sprintf("%s %d %d", p, fi.ModTime().UnixNano(), fi.Size()))
		}
	}
	return s.repo.Fingerprint(extra...)
}

// CurrentBranch returns the checked out branch, or "" when HEAD is detached.
func (s *Source) CurrentBranch() string { return s.repo.CurrentBranch() }
