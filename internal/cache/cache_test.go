package cache

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

var repo = github.Repo{Host: "github.com", Owner: "o", Name: "r"}

// open opens a cache in a new directory, and returns the path of its file.
func open(t *testing.T) (*Cache, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sub")
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, filepath.Join(dir, fileName())
}

func TestPRsRoundTrip(t *testing.T) {
	c, path := open(t)
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("cache file mode %v err %v, want 0600", fi.Mode().Perm(), err)
	}
	at := time.UnixMilli(time.Now().UnixMilli())
	prs := map[string]*github.PR{
		"one": {Number: 1, State: "OPEN", Checks: github.Rollup{
			State: github.CheckFailure,
			Workflows: []github.Workflow{
				{Name: "ci", State: github.CheckFailure, Checks: []github.Check{
					{Name: "test", State: github.CheckFailure},
				}},
			},
		}},
		"two": {Number: 2, State: "OPEN", Reviewers: []string{"amy"}},
	}
	if err := c.SavePRs(repo, prs, at); err != nil {
		t.Fatal(err)
	}
	// Another spelling of the same repository finds the same rows.
	other := github.Repo{Host: "github.com", Owner: "O", Name: "R"}
	got, oldest, err := c.PRs(other, []github.Lookup{
		{Branch: "one", Number: 1}, {Branch: "two"}, {Branch: "three", Number: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["one"].Checks.Workflows[0].Checks[0].Name != "test" ||
		got["two"].Reviewers[0] != "amy" || !oldest.Equal(at) {
		t.Fatalf("got %+v oldest %v", got, oldest)
	}
}

func TestBranchLookupFindsNewestOpenPR(t *testing.T) {
	c, _ := open(t)
	at := time.Now()
	c.SavePRs(repo, map[string]*github.PR{"b": {Number: 5, State: "OPEN"}}, at)
	c.SavePRs(repo, map[string]*github.PR{"b": {Number: 9, State: "CLOSED"}}, at)
	got, _, err := c.PRs(repo, []github.Lookup{{Branch: "b"}})
	if err != nil || got["b"] == nil || got["b"].Number != 5 {
		t.Fatalf("got %+v err %v, want #5", got, err)
	}
}

func TestOlderFetchDoesNotOverwriteNewer(t *testing.T) {
	c, _ := open(t)
	now := time.Now()
	c.SavePRs(repo, map[string]*github.PR{"b": {Number: 1, Title: "new"}}, now)
	c.SavePRs(repo, map[string]*github.PR{"b": {Number: 1, Title: "old"}}, now.Add(-time.Minute))
	got, _, _ := c.PRs(repo, []github.Lookup{{Branch: "b", Number: 1}})
	if got["b"].Title != "new" {
		t.Fatalf("title = %q, want new", got["b"].Title)
	}
}

func TestReviewersAndSuggested(t *testing.T) {
	c, _ := open(t)
	if r, at, err := c.Reviewers(repo); r != nil || !at.IsZero() || err != nil {
		t.Fatalf("empty cache: %v %v %v", r, at, err)
	}
	at := time.UnixMilli(time.Now().UnixMilli())
	c.SaveReviewers(repo, []github.Reviewer{{Login: "amy", RecentPRs: 3, Recent: 2.5}}, at)
	r, rAt, err := c.Reviewers(repo)
	if err != nil || len(r) != 1 || r[0].Recent != 2.5 || !rAt.Equal(at) {
		t.Fatalf("reviewers %+v at %v err %v", r, rAt, err)
	}

	c.SaveSuggested(repo, map[int]Suggestion{2: {Head: "h2", Logins: []string{"yuki"}}}, at)
	s, err := c.Suggested(repo, []int{2, 3})
	if err != nil || len(s) != 1 || s[2].Head != "h2" || s[2].Logins[0] != "yuki" {
		t.Fatalf("suggested %+v err %v", s, err)
	}
}

func TestOpenDeletesOtherBuildsAndOldRows(t *testing.T) {
	c, path := open(t)
	dir := filepath.Dir(path)
	for _, name := range []string{"cache-000000000000.db", "cache-000000000000.db-wal"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	c.SavePRs(repo, map[string]*github.PR{"old": {Number: 1}}, time.Now().Add(-maxAge-time.Hour))
	c.SavePRs(repo, map[string]*github.PR{"new": {Number: 2}}, time.Now())
	c.Close()

	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	left, _ := filepath.Glob(filepath.Join(dir, "cache-0000*"))
	if len(left) != 0 {
		t.Fatalf("other builds' caches should be deleted: %v", left)
	}
	got, _, _ := c.PRs(
		repo,
		[]github.Lookup{{Branch: "old", Number: 1}, {Branch: "new", Number: 2}},
	)
	if got["old"] != nil || got["new"] == nil {
		t.Fatalf("got %+v, want only new", got)
	}
}

func TestShapeTracksStoredTypes(t *testing.T) {
	shape := func(v any) string {
		var b strings.Builder
		writeShape(&b, reflect.TypeOf(v))
		return b.String()
	}
	type a struct{ X []int }
	type renamed struct{ Y []int }
	type retyped struct{ X []string }
	type tagged struct {
		X []int `json:"x"`
	}
	for _, v := range []any{renamed{}, retyped{}, tagged{}} {
		if shape(v) == shape(a{}) {
			t.Errorf("%T has the same shape as a", v)
		}
	}
}

func TestNilCache(t *testing.T) {
	var c *Cache
	if prs, _, err := c.PRs(repo, []github.Lookup{{Branch: "b"}}); prs != nil || err != nil {
		t.Fatal("a nil cache should find nothing")
	}
	if err := c.SavePRs(repo, map[string]*github.PR{"b": {Number: 1}}, time.Now()); err != nil {
		t.Fatal(err)
	}
}
