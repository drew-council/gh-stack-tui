// Package demo is a made-up repository with two stacks and pull requests in
// every state the TUI renders. It backs ghst demo and the TUI's rendering
// tests.
package demo

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/drew-council/gh-stack-tui/internal/git"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/stack"
)

// Repository is the demo stacks' repository, as the stack file records it.
const Repository = "github.com:acme/auth-service"

// CurrentBranch is the demo's checked out branch.
const CurrentBranch = "auth/rate-limit"

// Source serves the demo stacks. It implements the TUI's Local.
type Source struct {
	now func() time.Time
	dir string
}

// NewSource returns the demo stacks, with commit times relative to now.
// Commands run in dir, which should be an empty directory so they fail
// without touching anything.
func NewSource(now func() time.Time, dir string) *Source {
	return &Source{now: now, dir: dir}
}

func (s *Source) Dir() string { return s.dir }

func (s *Source) CurrentBranch() string { return CurrentBranch }

// Fingerprint never changes: the demo stacks stay as they are.
func (s *Source) Fingerprint() (string, error) { return "demo", nil }

func (s *Source) Stacks() ([]stack.Ref, error) {
	var refs []stack.Ref
	for _, st := range s.snapshots() {
		refs = append(refs, stack.Ref{Key: st.Key, Number: st.Number})
	}
	return refs, nil
}

// Load picks a stack the way [stack.Select] does: pinned, then the one with
// the checked out branch.
func (s *Source) Load(pinned, previous string) (*stack.Snapshot, error) {
	all := s.snapshots()
	i := slices.IndexFunc(all, func(st *stack.Snapshot) bool { return st.Key == pinned })
	if i < 0 {
		i = slices.IndexFunc(
			all,
			func(st *stack.Snapshot) bool { return st.HasBranch(CurrentBranch) },
		)
	}
	return all[i], nil
}

func (s *Source) snapshots() []*stack.Snapshot {
	now := s.now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	commit := func(sha, subject string, d time.Duration) git.Commit {
		return git.Commit{SHA: sha, Subject: subject, Author: "Drew", Time: ago(d)}
	}
	auth := []stack.Branch{
		{Name: "auth/session-store", Parent: "main", Merged: true, PR: ref(101)},
		{Name: "auth/redis-sessions", Parent: "main", Merged: true, PR: ref(102)},
		{
			Name: "auth/token-refresh", Parent: "main", Head: "a1", DiffBase: "a0", PR: ref(103),
			Files: []git.FileChange{
				{Path: "internal/auth/refresh.go", Status: "M", Additions: 64, Deletions: 12},
				{Path: "internal/auth/refresh_test.go", Status: "A", Additions: 120},
			},
			Commits: []git.Commit{
				commit("3f9c2e1d0a", "Cover refresh races in tests", 26*time.Hour),
				commit("8b1d77a4c2", "Refresh access tokens before they expire", 28*time.Hour),
			},
		},
		{
			Name: "auth/middleware", Parent: "auth/token-refresh", Head: "b1", DiffBase: "a1",
			PR: ref(104),
			Files: []git.FileChange{
				{Path: "internal/http/middleware.go", Status: "A", Additions: 88},
				{Path: "internal/http/router.go", Status: "M", Additions: 14, Deletions: 6},
				{Path: "internal/http/middleware_test.go", Status: "A", Additions: 140},
				{
					Path: "api/gen/openapi.gen.go", Status: "M", Additions: 312, Deletions: 40,
					Generated: true,
				},
			},
			Commits: []git.Commit{
				commit("c41a09e7b3", "Regenerate the OpenAPI client", 5*time.Hour),
				commit("e7d2f3019a", "Exempt health checks from the session check", 6*time.Hour),
				commit("1a2b3c4d5e", "Require a session on every API route", 20*time.Hour),
			},
		},
		{
			Name: "auth/rate-limit", Parent: "auth/middleware", Head: "c1", DiffBase: "b1",
			IsCurrent: true, PR: ref(105),
			Files: []git.FileChange{
				{Path: "internal/ratelimit/limiter.go", Status: "A", Additions: 156},
				{Path: "internal/ratelimit/limiter_test.go", Status: "A", Additions: 210},
				{Path: "internal/auth/login.go", Status: "M", Additions: 22, Deletions: 8},
				{Path: "config/defaults.yaml", Status: "M", Additions: 6, Deletions: 1},
				{Path: "internal/auth/lockout.go", Status: "D", Deletions: 74},
				{
					Path: "internal/ratelimit/mock_store.go", Status: "A", Additions: 80,
					Generated: true,
				},
			},
			Commits: []git.Commit{
				commit("9e8d7c6b5a", "Address review: name the window option", 40*time.Minute),
				commit("5d4c3b2a19", "Drop the old lockout table", 3*time.Hour),
				commit("0f1e2d3c4b", "Limit attempts per account, not per IP", 4*time.Hour),
				commit("7a6b5c4d3e", "Add a token bucket limiter", 22*time.Hour),
			},
		},
		{
			Name: "auth/audit-log", Parent: "auth/rate-limit", Head: "d1", DiffBase: "c0",
			NeedsRebase: true, PR: ref(106),
			Files: []git.FileChange{
				{Path: "internal/audit/log.go", Status: "A", Additions: 97},
				{Path: "internal/auth/login.go", Status: "M", Additions: 9, Deletions: 2},
			},
			Commits: []git.Commit{
				commit("4e5f6a7b8c", "Write an audit log entry for each login", 2*24*time.Hour),
			},
		},
		{
			Name:     "auth/docs",
			Parent:   "auth/audit-log",
			Head:     "e1",
			DiffBase: "d1",
			Files: []git.FileChange{
				{Path: "docs/auth.md", Status: "M", Additions: 40, Deletions: 3},
			},
			Commits: []git.Commit{
				commit("2c3d4e5f6a", "Document sessions and limits", 15*time.Minute),
			},
		},
	}
	billing := []stack.Branch{
		{
			Name:     "billing/invoices",
			Parent:   "main",
			Head:     "f1",
			DiffBase: "f0",
			PR:       ref(110),
			Files: []git.FileChange{
				{Path: "internal/billing/invoice.go", Status: "A", Additions: 230},
			},
			Commits: []git.Commit{
				commit("6a7b8c9d0e", "Generate monthly invoices", 9*24*time.Hour),
			},
		},
		{Name: "billing/webhooks", Parent: "billing/invoices", PR: ref(111)},
	}
	return []*stack.Snapshot{
		snapshot("n:7", 7, 0, auth),
		snapshot("n:8", 8, 1, billing),
	}
}

func snapshot(key string, number, index int, branches []stack.Branch) *stack.Snapshot {
	for i := range branches {
		for _, f := range branches[i].Files {
			if !f.Generated {
				branches[i].Additions += f.Additions
				branches[i].Deletions += f.Deletions
			}
		}
	}
	return &stack.Snapshot{
		Repository:    Repository,
		Key:           key,
		Number:        number,
		Index:         index,
		StackCount:    2,
		Trunk:         "main",
		CurrentBranch: CurrentBranch,
		Branches:      branches,
	}
}

func ref(n int) *stack.PullRequestRef {
	return &stack.PullRequestRef{Number: n, URL: url(n)}
}

func url(n int) string { return fmt.Sprintf("https://github.com/acme/auth-service/pull/%d", n) }

// Remote serves the demo pull requests. It implements the TUI's Remote.
type Remote struct {
	// Delay is how long each fetch takes, so loading states show.
	Delay time.Duration
}

func (r *Remote) Repository() github.Repo {
	return github.Repo{Host: "github.com", Owner: "acme", Name: "auth-service"}
}

func (r *Remote) wait(ctx context.Context) error {
	select {
	case <-time.After(r.Delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Remote) FetchPRs(
	ctx context.Context,
	lookups []github.Lookup,
) (map[string]*github.PR, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	all := PRs()
	out := map[string]*github.PR{}
	for _, l := range lookups {
		if pr, ok := all[l.Branch]; ok {
			out[l.Branch] = pr
		}
	}
	return out, nil
}

func (r *Remote) FetchReviewers(ctx context.Context) ([]github.Reviewer, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return []github.Reviewer{
		{Login: "mkowalski", Name: "Maria Kowalski", RecentPRs: 31, Recent: 12.4},
		{Login: "tobiasr", Name: "Tobias Reyes", RecentPRs: 17, Recent: 6.2},
		{Login: "priya-n", Name: "Priya Natarajan", RecentPRs: 9, Recent: 4.8},
		{Login: "sam", RecentPRs: 2, Recent: 0.5},
		{Login: "elena-v", Name: "Elena Vasquez"},
		{Login: "jun", Name: "Jun Park"},
	}, nil
}

func (r *Remote) FetchSuggested(ctx context.Context, numbers []int) (map[int][]string, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	out := map[int][]string{}
	for _, n := range numbers {
		out[n] = []string{"priya-n", "tobiasr"}
	}
	return out, nil
}

// RequestReviewers pretends to ask logins for review.
func (r *Remote) RequestReviewers(ctx context.Context, number int, logins []string) error {
	return r.wait(ctx)
}

// PRs are the demo pull requests, keyed by branch.
func PRs() map[string]*github.PR {
	pass := func(names ...string) github.Workflow {
		w := github.Workflow{Name: names[0], State: github.CheckSuccess}
		for _, n := range names[1:] {
			w.Checks = append(w.Checks, github.Check{Name: n, State: github.CheckSuccess})
		}
		return w
	}
	return map[string]*github.PR{
		"auth/session-store": {
			Number: 101, URL: url(101), Title: "Extract a session store interface",
			State: "MERGED", Merged: true,
		},
		"auth/redis-sessions": {
			Number: 102, URL: url(102), Title: "Back sessions with Redis",
			State: "MERGED", Merged: true,
		},
		"auth/token-refresh": {
			Number: 103, URL: url(103), Title: "Refresh access tokens before they expire",
			State: "OPEN", Queued: true, ReviewDecision: "APPROVED", Approvals: 2,
			Reviewers: []string{"mkowalski", "tobiasr"},
			Checks: github.Rollup{State: github.CheckSuccess, Workflows: []github.Workflow{
				pass("ci", "lint", "test", "build"),
			}},
		},
		"auth/middleware": {
			Number: 104, URL: url(104), Title: "Require a session on every API route",
			State: "OPEN", ReviewDecision: "APPROVED", Approvals: 1, Unresolved: 1,
			MergeState: "BLOCKED", Reviewers: []string{"mkowalski"},
			Checks: github.Rollup{State: github.CheckSuccess, Workflows: []github.Workflow{
				pass("ci", "lint", "test", "build"),
				pass("CodeQL", "analyze (go)"),
			}},
		},
		"auth/rate-limit": {
			Number: 105, URL: url(105), Title: "Rate limit login attempts per account",
			State: "OPEN", ReviewDecision: "CHANGES_REQUESTED", ChangesRequested: 1,
			MergeState: "DIRTY", Unresolved: 3, UnresolvedBots: 1,
			Reviewers: []string{"tobiasr", "priya-n"},
			Checks: github.Rollup{State: github.CheckFailure, Workflows: []github.Workflow{
				{Name: "ci", State: github.CheckFailure, Checks: []github.Check{
					{Name: "lint", State: github.CheckSuccess},
					{Name: "test (ubuntu-latest)", State: github.CheckFailure},
					{Name: "test (macos-latest)", State: github.CheckFailure},
					{Name: "build", State: github.CheckSuccess},
				}},
				{Name: "e2e", State: github.CheckPending, Checks: []github.Check{
					{Name: "playwright", State: github.CheckPending},
				}},
				pass("CodeQL", "analyze (go)"),
			}},
		},
		"auth/audit-log": {
			Number: 106, URL: url(106), Title: "Write an audit log entry for each login",
			State: "OPEN", IsDraft: true, RequestedTeams: []string{"security"},
			Checks: github.Rollup{State: github.CheckPending, Workflows: []github.Workflow{
				{Name: "ci", State: github.CheckPending, Checks: []github.Check{
					{Name: "lint", State: github.CheckSuccess},
					{Name: "test", State: github.CheckPending},
					{Name: "build", State: github.CheckPending},
				}},
			}},
		},
		"billing/invoices": {
			Number: 110, URL: url(110), Title: "Generate monthly invoices",
			State: "OPEN", MergeState: "BEHIND", Reviewers: []string{"sam"},
			Checks: github.Rollup{State: github.CheckSuccess, Workflows: []github.Workflow{
				pass("ci", "lint", "test", "build"),
			}},
		},
		"billing/webhooks": {
			Number: 111, URL: url(111), Title: "Retry failed webhooks with backoff",
			State: "CLOSED",
		},
	}
}
