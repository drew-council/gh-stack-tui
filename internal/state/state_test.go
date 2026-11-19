package state

import (
	"testing"
	"time"

	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
)

var (
	// Open deletes snoozes that woke a while ago, by the real clock.
	now  = time.Now().Truncate(time.Millisecond)
	repo = github.Repo{Host: "github.com", Owner: "acme", Name: "auth-service"}
)

func open(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSnoozesOutliveTheStore(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	pr, stack := review.Target{Number: 325}, review.Target{Stack: true, Number: 12}
	if err := s.Snooze(repo, []review.Target{pr, stack}, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	// Snoozing again moves the wake.
	if err := s.Snooze(repo, []review.Target{pr}, now.Add(2*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s = open(t, dir)
	other := github.Repo{Host: "github.com", Owner: "ACME", Name: "Auth-Service"}
	got, err := s.Snoozes(other, now)
	if err != nil {
		t.Fatal(err)
	}
	want := review.Snoozes{pr: now.Add(2 * time.Hour), stack: now.Add(time.Hour)}
	if len(got) != len(want) || !got[pr].Equal(want[pr]) || !got[stack].Equal(want[stack]) {
		t.Fatalf("snoozes = %v, want %v", got, want)
	}

	if err := s.Wake(repo, []review.Target{stack}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Snoozes(repo, now); len(got) != 1 {
		t.Fatalf("after waking the stack, snoozes = %v", got)
	}
	// A snooze that woke is not returned.
	if got, _ := s.Snoozes(repo, now.Add(3*time.Hour)); len(got) != 0 {
		t.Fatalf("after the wake, snoozes = %v", got)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(dir); err == nil {
		t.Fatal("Open took a schema newer than its own")
	}
}
