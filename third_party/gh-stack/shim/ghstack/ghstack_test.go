package ghstack

import (
	"os"
	"path/filepath"
	"testing"
)

const catalog = `{
  "schemaVersion": 1,
  "repository": "github.com:o/r",
  "stacks": [{
    "number": 7,
    "trunk": {"branch": "main"},
    "branches": [
      {"branch": "one", "pullRequest": {"number": 1, "merged": true}},
      {"branch": "two", "base": "abc", "pullRequest": {"number": 2, "url": "u"}},
      {"branch": "three"}
    ]
  }]
}`

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func check(t *testing.T, c *Catalog) {
	t.Helper()
	if c.Repository != "github.com:o/r" || len(c.Stacks) != 1 {
		t.Fatalf("catalog = %+v, want one stack in github.com:o/r", c)
	}
	s := c.Stacks[0]
	if s.Number != 7 || s.Trunk != "main" || len(s.Branches) != 3 {
		t.Fatalf("stack = %+v, want #7 on main with three branches", s)
	}
	one, two, three := s.Branches[0], s.Branches[1], s.Branches[2]
	if !one.Merged || one.Parent != "main" {
		t.Errorf("one = %+v, want merged onto main", one)
	}
	if two.Merged || two.Parent != "main" || two.Base != "abc" ||
		two.PR == nil || two.PR.Number != 2 || two.PR.URL != "u" {
		t.Errorf("two = %+v, want open PR #2 stacked on main past merged one", two)
	}
	if three.Parent != "two" || three.PR != nil {
		t.Errorf("three = %+v, want no PR stacked on two", three)
	}
}

// TestLoad pins CatalogPath to the file gh-stack actually reads.
func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write(t, CatalogPath(dir), catalog)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	check(t, c)
}

// TestLoadMigratesWorktreeCatalog covers a catalog an older gh-stack left in a
// linked worktree's own git directory.
func TestLoadMigratesWorktreeCatalog(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "worktrees", "wt", "gh-stack"), catalog)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	check(t, c)
	if _, err := os.Stat(CatalogPath(dir)); err != nil {
		t.Errorf("catalog was not migrated to the common directory: %v", err)
	}
}

func TestLoadMissing(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil || len(c.Stacks) != 0 {
		t.Fatalf("Load = %+v, %v; want no stacks", c, err)
	}
}
