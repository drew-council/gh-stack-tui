package stack

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drew-council/gh-stack-tui/internal/git"
	"github.com/github/gh-stack/shim/ghstack"
)

func TestSelect(t *testing.T) {
	f := &File{Stacks: []Stack{
		{Number: 1, Trunk: "main", Branches: []ghstack.Branch{{Name: "a"}}},
		{Number: 2, Trunk: "main", Branches: []ghstack.Branch{{Name: "b"}}},
		{Number: 3, Trunk: "main", Branches: []ghstack.Branch{{Name: "c"}}},
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
		if got := Select(f, c.pinned, c.current, c.previous); got != c.want {
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

// writeCatalog records stacks the way gh-stack does, in the common git
// directory of the repository at dir.
func writeCatalog(t *testing.T, dir, stacks string) {
	t.Helper()
	commonDir := gitRun(t, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	data := `{"schemaVersion": 1, "repository": "github.com:o/r", "stacks": ` + stacks + `}`
	if err := os.WriteFile(ghstack.CatalogPath(commonDir), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
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

	writeCatalog(t, dir, `[{"number": 7, "trunk": {"branch": "main"}, "branches": [
		{"branch": "one", "pullRequest": {"number": 1, "merged": true}},
		{"branch": "two", "base": "`+oneHead+`", "pullRequest": {"number": 2}}
	]}]`)

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

// TestLoadFromLinkedWorktree covers gh-stack keeping one stack file in the
// common git directory, which a linked worktree must read rather than its own.
func TestLoadFromLinkedWorktree(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	commit(t, dir, "root")
	wt := filepath.Join(t.TempDir(), "wt")
	gitRun(t, dir, "worktree", "add", "-qb", "feature", wt)

	writeCatalog(t, wt, `[{"number": 7, "trunk": {"branch": "main"}, "branches": [
		{"branch": "feature"}
	]}]`)

	repo, err := git.Open(wt)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Load(repo, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Number != 7 || len(snap.Branches) != 1 || !snap.Branches[0].IsCurrent {
		t.Fatalf("snapshot = %+v, want stack 7 with feature checked out", snap)
	}
}

func TestLoadOmitsGeneratedFilesFromDiffCounts(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(
		filepath.Join(dir, ".gitattributes"),
		[]byte(
			"*_gen.go linguist-generated=true\n*.pb.go -diff linguist-generated\n*.gen.txt generated=true\n",
		),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".gitattributes")
	gitRun(t, dir, "commit", "-qm", "init")

	gitRun(t, dir, "checkout", "-qb", "feature")
	commit(t, dir, "hand.go")
	commit(t, dir, "types_gen.go")
	commit(t, dir, "proto.pb.go")
	commit(t, dir, "doc.gen.txt")

	writeCatalog(t, dir, `[{"number": 1, "trunk": {"branch": "main"}, "branches": [
		{"branch": "feature"}
	]}]`)

	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := Load(repo, "", "")
	if err != nil {
		t.Fatal(err)
	}
	b := snap.Branches[0]
	if b.Additions != 1 || b.Deletions != 0 {
		t.Fatalf("diff stat = +%d -%d, want +1 -0 (non-generated only)", b.Additions, b.Deletions)
	}
	wantGen := map[string]bool{
		"hand.go":      false,
		"types_gen.go": true,
		"proto.pb.go":  true,
		"doc.gen.txt":  true,
	}
	if len(b.Files) != len(wantGen) {
		t.Fatalf("files = %+v, want %d files", b.Files, len(wantGen))
	}
	for _, f := range b.Files {
		if want, ok := wantGen[f.Path]; !ok || f.Generated != want {
			t.Errorf("file %s Generated = %v, want %v", f.Path, f.Generated, want)
		}
	}
}
