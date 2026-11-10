package demo

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/drew-council/gh-stack-tui/internal/desktop"
	"github.com/drew-council/gh-stack-tui/internal/github"
)

// Viewer is the login the demo review queue belongs to.
const Viewer = "drew"

// ReviewRemote serves a made-up review queue with pull requests in every
// state ghrv renders. It implements the review TUI's Remote.
type ReviewRemote struct {
	// Delay is how long each fetch takes, so loading states show.
	Delay time.Duration
	// Now is the clock the queue's times are relative to.
	Now func() time.Time
}

func (r *ReviewRemote) Repository() github.Repo {
	return github.Repo{Host: "github.com", Owner: "acme", Name: "auth-service"}
}

func (r *ReviewRemote) wait(ctx context.Context) error {
	select {
	case <-time.After(r.Delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *ReviewRemote) FetchQueue(ctx context.Context, _ github.QueueQuery) (*github.Queue, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	return Queue(r.Now()), nil
}

func (r *ReviewRemote) FetchReviewDetails(
	ctx context.Context,
	numbers []int,
) (map[int]*github.ReviewDetail, error) {
	if err := r.wait(ctx); err != nil {
		return nil, err
	}
	all := Details(r.Now())
	out := map[int]*github.ReviewDetail{}
	for _, n := range numbers {
		if d, ok := all[n]; ok {
			out[n] = d
		}
	}
	return out, nil
}

// Checkout is the made-up checkout the demo's reviews run in. It implements
// the review TUI's Local.
type Checkout struct{}

// Generated takes the protobuf code for generated.
func (Checkout) Generated(paths []string) map[string]struct{} {
	gen := map[string]struct{}{}
	for _, p := range paths {
		if strings.HasSuffix(p, ".pb.go") {
			gen[p] = struct{}{}
		}
	}
	return gen
}

// Queue is the demo review queue, with times relative to now.
func Queue(now time.Time) *github.Queue {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	const hour, day = time.Hour, 24 * time.Hour
	sha := func(n int) string { return fmt.Sprintf("%040x", n) }
	approved := func(login string, d time.Duration) github.Review {
		return github.Review{Login: login, State: "APPROVED", At: ago(d)}
	}
	changes := func(login string, d time.Duration) github.Review {
		return github.Review{Login: login, State: "CHANGES_REQUESTED", At: ago(d)}
	}
	// pr fills in what every demo PR shares.
	pr := func(p github.ReviewPR) *github.ReviewPR {
		p.URL = url(p.Number)
		if p.State == "" {
			p.State = "OPEN"
		}
		if p.BaseRef == "" {
			p.BaseRef = "main"
		}
		p.HeadSHA = sha(p.Number)
		if p.UpdatedAt.IsZero() {
			p.UpdatedAt = p.AskedAt
		}
		if p.PushedAt.IsZero() {
			p.PushedAt = p.UpdatedAt
		}
		if p.CreatedAt.IsZero() {
			p.CreatedAt = p.UpdatedAt.Add(-3 * hour)
		}
		if p.Commits == 0 {
			p.Commits = 1
		}
		if !p.AskedAt.IsZero() && p.AskedBy == "" {
			p.AskedBy = p.Author
		}
		p.RequestedTeams = append(p.RequestedTeams, p.Via...)
		return &p
	}

	prs := []*github.ReviewPR{
		// A stack with every kind of neighbor.
		pr(github.ReviewPR{
			Number: 301, Title: "Extract a money package", Author: "mkowalski",
			HeadRef: "billing/money", State: "MERGED", Merged: true,
			UpdatedAt: ago(6 * day), Additions: 310, Deletions: 122, ChangedFiles: 9,
			ReviewDecision: "APPROVED", Checks: github.CheckSuccess,
		}),
		pr(github.ReviewPR{
			Number: 302, Title: "Add the invoices schema migration", Author: "mkowalski",
			HeadRef: "billing/schema", UpdatedAt: ago(2 * day),
			Additions: 84, Deletions: 0, ChangedFiles: 3, Commits: 2,
			ReviewDecision: "APPROVED", Checks: github.CheckSuccess,
			Reviews: []github.Review{approved("tobiasr", 2*day)},
			Mine: &github.Review{
				Login: Viewer, State: "APPROVED", At: ago(3 * day), Commit: sha(302),
			},
		}),
		pr(github.ReviewPR{
			Number: 303, Title: "Model invoices and line items", Author: "mkowalski",
			HeadRef: "billing/model", BaseRef: "billing/schema",
			Requested: true, Via: []string{"go-readability"}, AskedAt: ago(3 * hour),
			CreatedAt: ago(4 * day), PushedAt: ago(4 * hour),
			Additions: 412, Deletions: 38, ChangedFiles: 7, Commits: 5,
			ReviewDecision: "CHANGES_REQUESTED", Checks: github.CheckSuccess,
			Reviews: []github.Review{approved("tobiasr", 26*hour)},
			Mine: &github.Review{
				Login: Viewer, State: "CHANGES_REQUESTED", At: ago(2 * day), Commit: sha(9303),
			},
		}),
		pr(github.ReviewPR{
			Number: 304, Title: "Serve invoices over the billing API", Author: "mkowalski",
			HeadRef: "billing/api", BaseRef: "billing/model",
			Requested: true, Via: []string{"api-readability", "go-readability"},
			AskedAt:   ago(26 * hour),
			Additions: 536, Deletions: 61, ChangedFiles: 12, Commits: 3,
			ReviewDecision: "REVIEW_REQUIRED", Checks: github.CheckFailure,
			RequestedUsers: []string{"tobiasr"},
			Labels:         []string{"api"},
		}),
		pr(github.ReviewPR{
			Number: 305, Title: "Post invoices to the ledger when they are finalized",
			Author: "mkowalski", HeadRef: "billing/ledger", BaseRef: "billing/api",
			Requested: true, Via: []string{"go-readability"}, AskedAt: ago(26 * hour),
			Additions: 198, Deletions: 14, ChangedFiles: 5, Commits: 2,
			ReviewDecision: "REVIEW_REQUIRED", Checks: github.CheckPending,
		}),
		pr(github.ReviewPR{
			Number: 306, Title: "Document the invoice lifecycle", Author: "mkowalski",
			HeadRef: "billing/docs", BaseRef: "billing/ledger", IsDraft: true,
			UpdatedAt: ago(5 * hour), Additions: 96, Deletions: 3, ChangedFiles: 2,
			Checks: github.CheckSuccess,
		}),

		// Asked by name, and small.
		pr(github.ReviewPR{
			Number: 322, Title: "Fix the flaky session expiry test", Author: "priya-n",
			HeadRef: "fix/session-expiry-flake", Requested: true, Direct: true,
			AskedAt: ago(40 * time.Minute), Additions: 12, Deletions: 4, ChangedFiles: 1,
			ReviewDecision: "REVIEW_REQUIRED", Checks: github.CheckSuccess,
		}),
		// Waiting for days, with reviewers who disagree, and conflicts.
		pr(github.ReviewPR{
			Number: 318, Title: "Cache team membership lookups for a minute",
			Author: "priya-n", HeadRef: "perf/team-cache",
			Requested: true, Via: []string{"go-readability"}, AskedAt: ago(4 * day),
			UpdatedAt: ago(20 * hour), Additions: 143, Deletions: 27, ChangedFiles: 4,
			Commits: 4, Conflicts: true,
			ReviewDecision: "CHANGES_REQUESTED", Checks: github.CheckSuccess,
			Reviews: []github.Review{approved("tobiasr", 3*day), changes("sam", 20*hour)},
		}),
		// Based on the branch of the PR above, outside any stack.
		pr(github.ReviewPR{
			Number: 324, Title: "Drop cached teams when a user leaves a team",
			Author: "priya-n", HeadRef: "perf/team-cache-evict", BaseRef: "perf/team-cache",
			Requested: true, Via: []string{"go-readability"}, AskedAt: ago(19 * hour),
			Additions: 58, Deletions: 9, ChangedFiles: 3, Commits: 2,
			ReviewDecision: "REVIEW_REQUIRED", Checks: github.CheckSuccess,
		}),

		// A long stack, most of it not for the viewer.
		pr(github.ReviewPR{
			Number: 332, Title: "Add a search index interface", Author: "tobiasr",
			HeadRef: "search/index", State: "MERGED", Merged: true, UpdatedAt: ago(9 * day),
			Additions: 120, Deletions: 8, ChangedFiles: 4,
		}),
		pr(github.ReviewPR{
			Number: 333, Title: "Index users as they change", Author: "tobiasr",
			HeadRef: "search/users", State: "MERGED", Merged: true, UpdatedAt: ago(8 * day),
			Additions: 260, Deletions: 31, ChangedFiles: 8,
		}),
		pr(github.ReviewPR{
			Number: 334, Title: "Index teams and their members", Author: "tobiasr",
			HeadRef: "search/teams", State: "MERGED", Merged: true, UpdatedAt: ago(8 * day),
			Additions: 231, Deletions: 19, ChangedFiles: 7,
		}),
		pr(github.ReviewPR{
			Number: 335, Title: "Backfill the index from Postgres", Author: "tobiasr",
			HeadRef: "search/backfill", State: "MERGED", Merged: true, UpdatedAt: ago(7 * day),
			Additions: 178, Deletions: 6, ChangedFiles: 5,
		}),
		pr(github.ReviewPR{
			Number: 336, Title: "Rank exact matches first", Author: "tobiasr",
			HeadRef: "search/ranking", UpdatedAt: ago(2 * day),
			Additions: 94, Deletions: 22, ChangedFiles: 3,
			ReviewDecision: "APPROVED", Checks: github.CheckSuccess,
			Reviews: []github.Review{approved("sam", 2*day)},
		}),
		pr(github.ReviewPR{
			Number: 337, Title: "Search users by name and email", Author: "tobiasr",
			HeadRef: "search/user-query", BaseRef: "search/ranking",
			Requested: true, Via: []string{"api-readability"}, AskedAt: ago(7 * hour),
			Additions: 287, Deletions: 12, ChangedFiles: 9, Commits: 3,
			ReviewDecision: "REVIEW_REQUIRED", Checks: github.CheckSuccess,
			Reviews: []github.Review{approved("elena-v", 5*hour)},
		}),
		pr(github.ReviewPR{
			Number: 338, Title: "Paginate search results with cursors", Author: "tobiasr",
			HeadRef: "search/pagination", BaseRef: "search/user-query",
			Requested: true, Via: []string{"api-readability"}, AskedAt: ago(7 * hour),
			Additions: 164, Deletions: 40, ChangedFiles: 6, Commits: 2,
			ReviewDecision: "REVIEW_REQUIRED", Checks: github.CheckSuccess,
		}),
		pr(github.ReviewPR{
			Number: 339, Title: "Highlight the matched text", Author: "tobiasr",
			HeadRef: "search/highlight", BaseRef: "search/pagination", UpdatedAt: ago(6 * hour),
			Additions: 73, Deletions: 5, ChangedFiles: 3,
			ReviewDecision: "REVIEW_REQUIRED", Checks: github.CheckPending,
			RequestedUsers: []string{"sam"},
		}),
		pr(github.ReviewPR{
			Number: 340, Title: "Search audit log entries", Author: "tobiasr",
			HeadRef: "search/audit", BaseRef: "search/highlight", IsDraft: true,
			UpdatedAt: ago(5 * hour), Additions: 210, Deletions: 0, ChangedFiles: 6,
		}),
		pr(github.ReviewPR{
			Number: 341, Title: "Add a search box to the admin console", Author: "tobiasr",
			HeadRef: "search/console", BaseRef: "search/audit", IsDraft: true,
			UpdatedAt: ago(5 * hour), Additions: 388, Deletions: 17, ChangedFiles: 11,
		}),

		// Large, from a team the viewer is in.
		pr(github.ReviewPR{
			Number: 325, Title: "Add structured logging to the gateway", Author: "elena-v",
			HeadRef: "gateway/slog", Requested: true, Via: []string{"platform"},
			AskedAt: ago(2 * day), Additions: 1840, Deletions: 220, ChangedFiles: 24,
			Commits: 9, ReviewDecision: "REVIEW_REQUIRED", Checks: github.CheckSuccess,
			Labels: []string{"observability", "needs-design"},
		}),

		// Left out of the queue until hidden PRs are shown.
		pr(github.ReviewPR{
			Number: 327, Title: "Sketch passkey enrollment", Author: "elena-v",
			HeadRef: "passkeys/enroll", IsDraft: true,
			Requested: true, Via: []string{"go-readability"}, AskedAt: ago(30 * hour),
			Additions: 520, Deletions: 12, ChangedFiles: 14, Checks: github.CheckFailure,
		}),
		pr(github.ReviewPR{
			Number: 329, Title: "Bump golang.org/x/crypto from 0.38.0 to 0.39.0",
			Author: "dependabot", AuthorBot: true, HeadRef: "dependabot/go_modules/x-crypto",
			Requested: true, Via: []string{"go-readability"}, AskedAt: ago(3 * day),
			Additions: 6, Deletions: 6, ChangedFiles: 2, Checks: github.CheckSuccess,
		}),
		pr(github.ReviewPR{
			Number: 315, Title: "Rotate signing keys on a schedule", Author: "sam",
			HeadRef:   "keys/rotation",
			Requested: true, Via: []string{"go-readability"}, AskedAt: ago(5 * day),
			UpdatedAt: ago(2 * day), Additions: 240, Deletions: 33, ChangedFiles: 6,
			ReviewDecision: "REVIEW_REQUIRED", Checks: github.CheckSuccess,
			Mine: &github.Review{
				Login: Viewer, State: "COMMENTED", At: ago(2 * day), Commit: sha(315),
			},
		}),
		pr(github.ReviewPR{
			Number: 312, Title: "Remove the legacy login form", Author: "sam",
			HeadRef:   "cleanup/legacy-login",
			Requested: true, Via: []string{"api-readability"}, AskedAt: ago(6 * day),
			UpdatedAt: ago(3 * day), Additions: 0, Deletions: 418, ChangedFiles: 5,
			ReviewDecision: "APPROVED", Checks: github.CheckSuccess,
			Mine: &github.Review{
				Login: Viewer, State: "APPROVED", At: ago(3 * day), Commit: sha(312),
			},
		}),
	}
	// A PR's totals are those of its files, generated ones included, as
	// GitHub counts them. #325 changes more files than its details list.
	for n, d := range Details(now) {
		for _, p := range prs {
			if p.Number != n || n == 325 {
				continue
			}
			p.ChangedFiles, p.Additions, p.Deletions = len(d.Files), 0, 0
			for _, f := range d.Files {
				p.Additions += f.Additions
				p.Deletions += f.Deletions
			}
		}
	}
	// The requested PRs come first, as GitHub's search returns them.
	slices.SortStableFunc(prs, func(a, b *github.ReviewPR) int {
		switch {
		case a.Requested && !b.Requested:
			return -1
		case b.Requested && !a.Requested:
			return 1
		}
		return 0
	})
	total := 0
	for _, p := range prs {
		if p.Requested {
			total++
		}
	}
	return &github.Queue{
		Viewer: Viewer,
		Trunk:  "main",
		Teams:  []string{"api-readability", "go-readability", "platform"},
		PRs:    prs,
		Stacks: []github.Stack{
			{
				Number: 342,
				Base:   "main",
				PRs:    []int{332, 333, 334, 335, 336, 337, 338, 339, 340, 341},
			},
			{Number: 307, Base: "main", PRs: []int{301, 302, 303, 304, 305, 306}},
		},
		Total: total,
	}
}

// Details are the details of the demo review queue's pull requests, by
// number.
func Details(now time.Time) map[int]*github.ReviewDetail {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	pass := func(names ...string) github.Workflow {
		w := github.Workflow{Name: names[0], State: github.CheckSuccess}
		for _, n := range names[1:] {
			w.Checks = append(w.Checks, github.Check{Name: n, State: github.CheckSuccess})
		}
		return w
	}
	green := github.Rollup{State: github.CheckSuccess, Workflows: []github.Workflow{
		pass("ci", "lint", "test", "build"),
		pass("CodeQL", "analyze (go)"),
	}}
	file := func(status, path string, add, del int) github.ChangedFile {
		return github.ChangedFile{Path: path, Status: status, Additions: add, Deletions: del}
	}
	commit := func(n int, subject string, d time.Duration) github.PRCommit {
		return github.PRCommit{SHA: fmt.Sprintf("%040x", n), Subject: subject, Time: ago(d)}
	}
	details := map[int]*github.ReviewDetail{
		303: {
			Body: "Invoices and their line items, with the totals worked out in the " +
				"money package from #301.\n\n" +
				"An invoice is immutable once finalized. Corrections are new invoices " +
				"that point at the one they replace, so the ledger never rewrites history.\n\n" +
				"Since the last review: line item totals no longer round per line, and " +
				"the currency is checked when an item is added rather than at finalize.",
			Files: []github.ChangedFile{
				file("A", "internal/billing/invoice.go", 148, 0),
				file("A", "internal/billing/invoice_test.go", 163, 0),
				file("A", "internal/billing/lineitem.go", 62, 0),
				file("M", "internal/billing/money/money.go", 21, 9),
				file("M", "internal/billing/store.go", 18, 29),
			},
			Commits: []github.PRCommit{
				commit(303, "Check the currency when an item is added", 4*time.Hour),
				commit(9304, "Round invoice totals once, not per line", 5*time.Hour),
				commit(9303, "Make finalized invoices immutable", 3*24*time.Hour),
			},
			Checks:     green,
			Unresolved: 2,
		},
		304: {
			Body: "List, get, and finalize invoices over the billing API.\n\n" +
				"The handlers are thin: they check the caller may see the account, " +
				"then call the billing package from the layer below.",
			Files: []github.ChangedFile{
				file("A", "api/billing/v1/invoices.proto", 96, 0),
				file("M", "api/billing/v1/invoices.pb.go", 214, 30),
				file("A", "internal/http/invoices.go", 132, 0),
				file("A", "internal/http/invoices_test.go", 88, 0),
				file("M", "internal/http/router.go", 6, 1),
			},
			Commits: []github.PRCommit{
				commit(304, "Finalize invoices over the API", 26*time.Hour),
				commit(9305, "List and get invoices", 28*time.Hour),
			},
			Checks: github.Rollup{State: github.CheckFailure, Workflows: []github.Workflow{
				{Name: "ci", State: github.CheckFailure, Checks: []github.Check{
					{Name: "test (ubuntu-latest)", State: github.CheckFailure},
					{Name: "buf breaking", State: github.CheckFailure},
					{Name: "lint", State: github.CheckSuccess},
					{Name: "build", State: github.CheckSuccess},
				}},
				pass("CodeQL", "analyze (go)"),
			}},
			UnresolvedBots: 1,
		},
		305: {
			Body: "Posts a ledger entry for each line item when an invoice is finalized.",
			Files: []github.ChangedFile{
				file("A", "internal/billing/ledger.go", 121, 0),
				file("A", "internal/billing/ledger_test.go", 70, 0),
				file("M", "internal/billing/invoice.go", 7, 14),
			},
			Commits: []github.PRCommit{
				commit(305, "Post invoices to the ledger", 26*time.Hour),
			},
			Checks: github.Rollup{State: github.CheckPending, Workflows: []github.Workflow{
				{Name: "ci", State: github.CheckPending, Checks: []github.Check{
					{Name: "test (ubuntu-latest)", State: github.CheckPending},
					{Name: "lint", State: github.CheckSuccess},
					{Name: "build", State: github.CheckSuccess},
				}},
			}},
		},
		322: {
			Body: "The test slept for the session's lifetime and then some, and lost " +
				"the race on a busy runner. It now moves a fake clock instead.",
			Files: []github.ChangedFile{
				file("M", "internal/auth/session_test.go", 12, 4),
			},
			Commits: []github.PRCommit{
				commit(322, "Use a fake clock in the session expiry test", 45*time.Minute),
			},
			Checks: green,
		},
		318: {
			Body: "Every request looked the caller's teams up in Postgres. This caches " +
				"them per user for a minute.\n\nMembership changes take up to a minute " +
				"to apply, which the security review accepted for everything but " +
				"removal from an admin team; that still checks the database.",
			Files: []github.ChangedFile{
				file("A", "internal/teams/cache.go", 84, 0),
				file("A", "internal/teams/cache_test.go", 51, 0),
				file("M", "internal/teams/lookup.go", 8, 22),
				file("M", "config/defaults.yaml", 0, 5),
			},
			Commits: []github.PRCommit{
				commit(318, "Check the database for admin teams", 20*time.Hour),
				commit(9318, "Cache team membership for a minute", 4*24*time.Hour),
			},
			Checks:     green,
			Unresolved: 1,
		},
		324: {
			Body: "## Why\n\nA user removed from a team kept its access for up to a " +
				"minute, until the cache from #318 expired.\n\n## What\n\n" +
				"- Evict the user's entry when membership changes.\n" +
				"- Publish the eviction, so every gateway instance drops it.\n\n" +
				"<!-- Screenshots, if the change is visible. -->",
			Files: []github.ChangedFile{
				file("M", "internal/teams/cache.go", 31, 6),
				file("M", "internal/teams/cache_test.go", 22, 0),
				file("M", "internal/teams/membership.go", 5, 3),
			},
			Commits: []github.PRCommit{
				commit(324, "Publish evictions to every instance", 19*time.Hour),
				commit(9324, "Evict a user's teams when membership changes", 20*time.Hour),
			},
			Checks: green,
		},
		337: {
			Body: "Adds GET /v1/users:search, which matches names and email addresses " +
				"by prefix and ranks exact matches first.",
			Files: []github.ChangedFile{
				file("A", "api/users/v1/search.proto", 48, 0),
				file("M", "api/users/v1/search.pb.go", 121, 0),
				file("A", "internal/search/users.go", 76, 0),
				file("A", "internal/search/users_test.go", 42, 12),
			},
			Commits: []github.PRCommit{
				commit(337, "Search users by name and email", 7*time.Hour),
			},
			Checks: green,
		},
		338: {
			Body: "Search results page with opaque cursors instead of offsets, so a " +
				"page stays stable while the index changes under it.",
			Files: []github.ChangedFile{
				file("M", "api/users/v1/search.proto", 14, 6),
				file("M", "internal/search/users.go", 58, 21),
				file("A", "internal/search/cursor.go", 61, 0),
				file("A", "internal/search/cursor_test.go", 31, 13),
			},
			Commits: []github.PRCommit{
				commit(338, "Paginate search results with cursors", 7*time.Hour),
			},
			Checks: green,
		},
		325: {
			Body: "Replaces the gateway's printf logging with log/slog.\n\n" +
				"Every request logs once, with the route, the caller, the status, and " +
				"how long it took. Handlers add attributes to the request's logger " +
				"instead of writing lines of their own.\n\n" +
				"Large because it touches every handler. The first commit adds the " +
				"middleware and is the one to read closely; the rest is mechanical.",
			Files: []github.ChangedFile{
				file("A", "internal/gateway/logging.go", 164, 0),
				file("A", "internal/gateway/logging_test.go", 212, 0),
				file("M", "internal/gateway/server.go", 41, 18),
				file("M", "internal/gateway/handlers/login.go", 22, 14),
				file("M", "internal/gateway/handlers/logout.go", 9, 7),
				file("M", "internal/gateway/handlers/session.go", 31, 20),
				file("M", "internal/gateway/handlers/token.go", 44, 29),
				file("M", "internal/gateway/handlers/users.go", 58, 37),
			},
			Commits: []github.PRCommit{
				commit(325, "Log from the users handlers with slog", 2*24*time.Hour),
				commit(9325, "Log each request once, with slog", 3*24*time.Hour),
			},
			Checks: green,
		},
	}
	for n, d := range details {
		d.Number = n
	}
	return details
}

// Tabs are made-up tabs that open nothing. They implement [desktop.Tabs].
type Tabs struct {
	mu   sync.Mutex
	next int
	tabs []desktop.Tab
}

// NewTabs returns the demo tabs, with one already open for each label.
func NewTabs(labels ...string) *Tabs {
	t := &Tabs{}
	for _, l := range labels {
		t.open(l)
	}
	return t
}

func (t *Tabs) open(label string) string {
	t.next++
	id := fmt.Sprintf("demo:%d", t.next)
	t.tabs = append(t.tabs, desktop.Tab{ID: id, Label: label})
	return id
}

func (t *Tabs) Noun() string { return "demo tab" }

func (t *Tabs) Open(_, label, _ string, _ bool) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.open(label), nil
}

func (t *Tabs) List() ([]desktop.Tab, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.tabs), nil
}

func (t *Tabs) Focus(string) error { return nil }
