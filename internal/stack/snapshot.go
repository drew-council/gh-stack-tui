package stack

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/drew-council/gh-stack-tui/internal/git"
)

// Snapshot is the fully resolved local state of one stack.
type Snapshot struct {
	Repository    string // host:owner/name, from the stack file
	Key           string
	Number        int
	Index         int // position within the stack file
	StackCount    int
	Trunk         string
	CurrentBranch string
	Dirty         bool
	Rebasing      bool
	// Branches are bottom first, matching the stack file.
	Branches []Branch
}

// Branch is a stack branch resolved against the local repository.
type Branch struct {
	Name string
	// Parent is the branch this one is stacked on, skipping merged branches.
	Parent string
	// Head is the live tip of the local branch; empty when it does not exist
	// locally (for example after a pruning sync).
	Head string
	// DiffBase is the commit the branch's own changes start from.
	DiffBase    string
	PR          *PullRequestRef
	Merged      bool
	NeedsRebase bool
	IsCurrent   bool
	Commits     []git.Commit
	Files       []git.FileChange
	Additions   int
	Deletions   int
}

// Load reads the stack file and resolves the selected stack against git.
// pinned and previous are stack keys as described by [File.Select].
func Load(repo *git.Repo, pinned, previous string) (*Snapshot, error) {
	f, err := ReadFile(repo.GitDir)
	if err != nil {
		return nil, err
	}
	current := repo.CurrentBranch()
	snap := &Snapshot{
		Repository:    f.Repository,
		CurrentBranch: current,
		StackCount:    len(f.Stacks),
		Dirty:         repo.Dirty(),
		Rebasing:      rebasing(repo.GitDir),
		Index:         f.Select(pinned, current, previous),
	}
	if snap.Index < 0 {
		return snap, nil
	}
	s := f.Stacks[snap.Index]
	snap.Key = s.Key()
	snap.Number = s.Number
	snap.Trunk = s.Trunk.Branch

	heads, err := repo.BranchHeads()
	if err != nil {
		return nil, err
	}

	snap.Branches = make([]Branch, len(s.Branches))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, ref := range s.Branches {
		snap.Branches[i] = Branch{
			Name:      ref.Branch,
			Parent:    activeParent(s, i),
			Head:      heads[ref.Branch],
			PR:        ref.PullRequest,
			Merged:    ref.Merged(),
			IsCurrent: ref.Branch == current,
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			resolveBranch(repo, &snap.Branches[i], ref.Base)
		})
	}
	wg.Wait()
	return snap, nil
}

// IndexOf returns the index of the named branch, or -1.
func (s *Snapshot) IndexOf(name string) int {
	for i, b := range s.Branches {
		if b.Name == name {
			return i
		}
	}
	return -1
}

// HasBranch reports whether the snapshot's stack contains branch.
func (s *Snapshot) HasBranch(name string) bool {
	return s.IndexOf(name) >= 0
}

// activeParent mirrors gh-stack's ActiveBaseBranch: the nearest unmerged
// branch below, or trunk.
func activeParent(s Stack, idx int) string {
	for j := idx - 1; j >= 0; j-- {
		if !s.Branches[j].Merged() {
			return s.Branches[j].Branch
		}
	}
	return s.Trunk.Branch
}

func resolveBranch(repo *git.Repo, b *Branch, recordedBase string) {
	if b.Head == "" {
		return
	}
	if !b.Merged {
		b.NeedsRebase = !repo.IsAncestor(b.Parent, b.Head)
	}
	b.DiffBase = diffBase(repo, b.Parent, b.Head, recordedBase)
	if b.DiffBase == "" {
		return
	}
	b.Commits, _ = repo.Log(b.DiffBase, b.Head)
	b.Files, _ = repo.Diff(b.DiffBase, b.Head)
	for _, f := range b.Files {
		b.Additions += f.Additions
		b.Deletions += f.Deletions
	}
}

// diffBase finds where a branch's own changes begin. The merge-base with the
// parent is exact while the parent is live, but once a parent is squash-merged
// the branch is compared against trunk and would absorb the parent's commits.
// gh-stack's recorded base (the parent tip at the last rebase) is closer in that
// case, so it is preferred whenever it is still in the branch's history and
// newer than the merge-base.
func diffBase(repo *git.Repo, parent, head, recorded string) string {
	mb, err := repo.MergeBase(parent, head)
	if err != nil {
		mb = ""
	}
	if recorded != "" && repo.IsAncestor(recorded, head) &&
		(mb == "" || repo.IsAncestor(mb, recorded)) {
		return recorded
	}
	return mb
}

func rebasing(gitDir string) bool {
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(gitDir, d)); err == nil {
			return true
		}
	}
	return false
}

// StateFiles are the paths whose modification should trigger a local reload in
// addition to ref movement.
func StateFiles(gitDir string) []string {
	return []string{
		filepath.Join(gitDir, FileName),
		filepath.Join(gitDir, "rebase-merge"),
		filepath.Join(gitDir, "rebase-apply"),
	}
}
