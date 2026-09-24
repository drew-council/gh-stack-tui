// Package stack reads gh-stack's local state and resolves it against git into
// a snapshot the TUI can render.
package stack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// FileName is the stack file gh-stack keeps inside the git directory.
const FileName = "gh-stack"

// File mirrors the schema of .git/gh-stack (schemaVersion 1).
type File struct {
	SchemaVersion int     `json:"schemaVersion"`
	Repository    string  `json:"repository"`
	Stacks        []Stack `json:"stacks"`
}

// Stack is a single ordered chain of branches, bottom first.
type Stack struct {
	ID       string      `json:"id,omitempty"`
	Number   int         `json:"number,omitempty"`
	Trunk    BranchRef   `json:"trunk"`
	Branches []BranchRef `json:"branches"`
}

// BranchRef is one branch of a stack as recorded by gh-stack.
type BranchRef struct {
	Branch      string          `json:"branch"`
	Head        string          `json:"head,omitempty"`
	Base        string          `json:"base,omitempty"`
	PullRequest *PullRequestRef `json:"pullRequest,omitempty"`
}

// PullRequestRef is gh-stack's cached view of a branch's PR.
type PullRequestRef struct {
	Number int    `json:"number"`
	ID     string `json:"id,omitempty"`
	URL    string `json:"url,omitempty"`
	Merged bool   `json:"merged,omitempty"`
}

// Merged reports whether gh-stack has recorded the branch's PR as merged.
func (b BranchRef) Merged() bool {
	return b.PullRequest != nil && b.PullRequest.Merged
}

// Key identifies a stack across reloads, even as branches are added or pruned.
func (s Stack) Key() string {
	switch {
	case s.ID != "":
		return "id:" + s.ID
	case s.Number != 0:
		return "n:" + strconv.Itoa(s.Number)
	case len(s.Branches) > 0:
		return "b:" + s.Branches[0].Branch
	default:
		return "t:" + s.Trunk.Branch
	}
}

// Contains reports whether branch is one of the stack's branches (not trunk).
func (s Stack) Contains(branch string) bool {
	for _, b := range s.Branches {
		if b.Branch == branch {
			return true
		}
	}
	return false
}

// ReadFile loads the stack file from gitDir. A missing file is reported as an
// empty stack list rather than an error.
func ReadFile(gitDir string) (*File, error) {
	data, err := os.ReadFile(filepath.Join(gitDir, FileName))
	if os.IsNotExist(err) {
		return &File{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", FileName, err)
	}
	return &f, nil
}

// RepositoryParts splits the "host:owner/name" repository string.
func (f *File) RepositoryParts() (host, owner, name string, ok bool) {
	host, rest, found := strings.Cut(f.Repository, ":")
	if !found {
		return "", "", "", false
	}
	owner, name, found = strings.Cut(rest, "/")
	if !found || owner == "" || name == "" {
		return "", "", "", false
	}
	return host, owner, name, true
}

// Select picks the stack to show, returning -1 when there are none. A stack
// the user explicitly switched to (pinned) wins. Otherwise the stack containing
// the checked out branch is shown, so checking out a branch elsewhere follows
// you. Failing that the previously shown stack is kept (HEAD is detached
// mid-rebase, or you are on trunk), then the most recently created stack.
func (f *File) Select(pinned, current, previous string) int {
	find := func(match func(Stack) bool) int {
		for i, s := range f.Stacks {
			if match(s) {
				return i
			}
		}
		return -1
	}
	if i := find(func(s Stack) bool { return pinned != "" && s.Key() == pinned }); i >= 0 {
		return i
	}
	if i := find(func(s Stack) bool { return current != "" && s.Contains(current) }); i >= 0 {
		return i
	}
	if i := find(func(s Stack) bool { return previous != "" && s.Key() == previous }); i >= 0 {
		return i
	}
	return len(f.Stacks) - 1
}

// FindNumber returns the index of the stack with the given number, or -1.
func (f *File) FindNumber(number int) int {
	for i, s := range f.Stacks {
		if s.Number == number {
			return i
		}
	}
	return -1
}
