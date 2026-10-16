// Package stack reads gh-stack's local state and resolves it against git into
// a snapshot the TUI can render.
package stack

import (
	"slices"
	"strconv"

	"github.com/github/gh-stack/shim/ghstack"
)

// gh-stack's own reading of its state, through the shim in third_party.
type (
	File           = ghstack.Catalog
	Stack          = ghstack.Stack
	PullRequestRef = ghstack.PullRequest
)

// ReadFile loads the stacks gh-stack records in commonDir. A missing file is
// reported as an empty stack list rather than an error.
func ReadFile(commonDir string) (*File, error) {
	return ghstack.Load(commonDir)
}

// Key identifies a stack across reloads, even as branches are added or pruned.
func Key(s Stack) string {
	switch {
	case s.ID != "":
		return "id:" + s.ID
	case s.Number != 0:
		return "n:" + strconv.Itoa(s.Number)
	case len(s.Branches) > 0:
		return "b:" + s.Branches[0].Name
	default:
		return "t:" + s.Trunk
	}
}

// contains reports whether branch is one of the stack's branches (not trunk).
func contains(s Stack, branch string) bool {
	return slices.ContainsFunc(s.Branches, func(b ghstack.Branch) bool { return b.Name == branch })
}

// Select picks the stack to show, returning -1 when there are none. A stack
// the user explicitly switched to (pinned) wins. Otherwise the stack containing
// the checked out branch is shown, so checking out a branch elsewhere follows
// you. Failing that the previously shown stack is kept (HEAD is detached
// mid-rebase, or you are on trunk), then the most recently created stack.
func Select(f *File, pinned, current, previous string) int {
	find := func(match func(Stack) bool) int {
		return slices.IndexFunc(f.Stacks, match)
	}
	if i := find(func(s Stack) bool { return pinned != "" && Key(s) == pinned }); i >= 0 {
		return i
	}
	if i := find(func(s Stack) bool { return current != "" && contains(s, current) }); i >= 0 {
		return i
	}
	if i := find(func(s Stack) bool { return previous != "" && Key(s) == previous }); i >= 0 {
		return i
	}
	return len(f.Stacks) - 1
}

// FindNumber returns the index of the stack with the given number, or -1.
func FindNumber(f *File, number int) int {
	return slices.IndexFunc(f.Stacks, func(s Stack) bool { return s.Number == number })
}
