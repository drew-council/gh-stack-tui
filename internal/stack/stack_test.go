package stack

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drew-council/gh-stack-tui/internal/git"
)

func TestSelect(t *testing.T) {
	f := &File{Stacks: []Stack{
		{Number: 1, Trunk: BranchRef{Branch: "main"}, Branches: []BranchRef{{Branch: "a"}}},
		{Number: 2, Trunk: BranchRef{Branch: "main"}, Branches: []BranchRef{{Branch: "b"}}},
		{Number: 3, Trunk: BranchRef{Branch: "main"}, Branches: []BranchRef{{Branch: "c"}}},
	}}
	cases := []struct {
		name                      string
		pinned, current, previous string
		want                      int
	}{
		{"follows checked out branch", "", "a", "n:2", 0},
		{"pinned wins", "n:3", "a", "", 2},
		{"keeps previous on trunk", "", "main", "n:2", 1},
		{"defaults to newest", "", "", "", 2},
	}
	for _, c := range cases {
		if got := f.Select(c.pinned, c.current, c.previous); got != c.want {
			t.Errorf("%s: Select = %d, want %d", c.name, got, c.want)
		}
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(
		os.Environ(),
		"GIT_AUTHOR_NAME=t",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t",
		"GIT_COMMITTER_EMAIL=t@t",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", file)
	gitRun(t, dir, "commit", "-qm", "add "+file)
	return gitRun(t, dir, "rev-parse", "HEAD")
}

// TestLoadAfterParentSquashMerge covers the case the recorded base exists for:
// once the bottom branch is squash-merged, the next branch is compared against
// trunk, which does not contain the bottom branch's original commits.
func TestLoadAfterParentSquashMerge(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	commit(t, dir, "root")
	gitRun(t, dir, "checkout", "-qb", "one")
	oneHead := commit(t, dir, "one")
	gitRun(t, dir, "checkout", "-qb", "two")
	commit(t, dir, "two")
	// Squash-merge "one" into main as a new commit.
	gitRun(t, dir, "checkout", "-q", "main")
	commit(t, dir, "one-squashed")
	gitRun(t, dir, "checkout", "-q", "two")

	f := File{SchemaVersion: 1, Repository: "github.com:o/r", Stacks: []Stack{{
		Number: 7,
		Trunk:  BranchRef{Branch: "main"},
		Branches: []BranchRef{
			{Branch: "one", PullRequest: &PullRequestRef{Number: 1, Merged: true}},
			{Branch: "two", Base: oneHead, PullRequest: &PullRequestRef{Number: 2}},
		},
	}}}
	data, _ := json.Marshal(f)
	if err := os.WriteFile(filepath.Join(dir, ".git", FileName), data, 0o644); err != nil {
		t.Fatal(err)
	}

	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Load(repo, "", "")
	if err != nil {
		t.Fatal(err)
	}
	two := snap.Branches[1]
	if !two.IsCurrent || two.Parent != "main" {
		t.Fatalf(
			"two: current=%v parent=%q, want current with parent main",
			two.IsCurrent,
			two.Parent,
		)
	}
	if !two.NeedsRebase {
		t.Error("two should need a rebase onto the new main")
	}
	if two.DiffBase != oneHead {
		t.Errorf("DiffBase = %s, want recorded base %s", two.DiffBase, oneHead)
	}
	if len(two.Commits) != 1 || len(two.Files) != 1 || two.Files[0].Path != "two" {
		t.Errorf(
			"two: %d commits, files %+v; want only its own change",
			len(two.Commits),
			two.Files,
		)
	}
}
