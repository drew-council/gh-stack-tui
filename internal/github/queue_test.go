package github

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// queueFake answers the requests of FetchQueue: searches by what they search
// for, the stack list, the viewer's teams, and PRs fetched by number.
type queueFake struct {
	searches map[string]string // qualifier → search nodes
	stacks   string
	// stacksStatus overrides the status of the stack list when set.
	stacksStatus int
	// teams are the viewer's teams, as JSON. Empty fails the request.
	teams string
	prs   string // the repository object of a fetch by number
}

func (q queueFake) route(request string) (int, string, bool) {
	switch {
	case strings.HasPrefix(request, "GET /repos/o/r/stacks"):
		if q.stacksStatus != 0 {
			return q.stacksStatus, `{"message": "Not Found"}`, true
		}
		return 200, q.stacks, true
	case strings.Contains(request, "organization(login"):
		if q.teams == "" {
			// The owner is a user, not an organization.
			return 200, `{"data": {"organization": null},
				"errors": [{"type": "NOT_FOUND", "message": "Could not resolve"}]}`, true
		}
		return 200, `{"data": {"organization": {"teams": {"nodes": ` + q.teams + `}}}}`, true
	case strings.Contains(request, "search(query"):
		for qualifier, nodes := range q.searches {
			if strings.Contains(request, qualifier+`"`) {
				return 200, fmt.Sprintf(`{"data": {
					"viewer": {"login": "me"},
					"repository": {"defaultBranchRef": {"name": "main"}},
					"search": {"issueCount": 2, "pageInfo": {"hasNextPage": false}, "nodes": %s}
				}}`, nodes), true
			}
		}
		return 200, `{"data": {"viewer": {"login": "me"}, "search": {"nodes": []}}}`, true
	case strings.Contains(request, "pullRequest(number"):
		return 200, `{"data": {"repository": ` + q.prs + `}}`, true
	}
	return 0, "", false
}

const requestedPR = `{
	"number": 7, "url": "u7", "title": "Seven", "state": "OPEN", "mergeable": "CONFLICTING",
	"author": {"__typename": "User", "login": "amy"},
	"baseRefName": "main", "headRefName": "seven", "headRefOid": "head7",
	"createdAt": "2026-10-01T10:00:00Z", "updatedAt": "2026-10-03T10:00:00Z",
	"additions": 10, "deletions": 2, "changedFiles": 3,
	"labels": {"nodes": [{"name": "api"}]},
	"commits": {"totalCount": 4, "nodes": [{"commit": {
		"committedDate": "2026-10-03T09:00:00Z", "statusCheckRollup": {"state": "FAILURE"}}}]},
	"reviewRequests": {"nodes": [
		{"requestedReviewer": {"__typename": "Team", "slug": "go"}},
		{"requestedReviewer": {"__typename": "Team", "slug": "web"}},
		{"requestedReviewer": {"__typename": "User", "login": "bob"}}
	]},
	"latestOpinionatedReviews": {"nodes": [
		{"state": "APPROVED", "submittedAt": "2026-10-02T10:00:00Z",
			"author": {"__typename": "User", "login": "cat"}},
		{"state": "CHANGES_REQUESTED", "submittedAt": "2026-10-02T11:00:00Z",
			"author": {"__typename": "User", "login": "me"}}
	]},
	"viewerLatestReview": {"state": "CHANGES_REQUESTED",
		"submittedAt": "2026-10-02T11:00:00Z", "commit": {"oid": "old7"}},
	"timelineItems": {"nodes": [
		{"createdAt": "2026-10-01T10:00:00Z", "actor": {"login": "amy"},
			"requestedReviewer": {"__typename": "Team", "slug": "go"}},
		{"createdAt": "2026-10-03T08:00:00Z", "actor": {"login": "dan"},
			"requestedReviewer": {"__typename": "Team", "slug": "go"}},
		{"createdAt": "2026-10-03T09:30:00Z", "actor": {"login": "amy"},
			"requestedReviewer": {"__typename": "Team", "slug": "web"}},
		{"createdAt": "2026-10-03T09:40:00Z", "actor": {"login": "amy"},
			"requestedReviewer": {"__typename": "User", "login": "bob"}}
	]}
}`

const directPR = `{
	"number": 9, "state": "OPEN", "isDraft": true,
	"author": {"__typename": "Bot", "login": "dependabot"},
	"reviewRequests": {"nodes": [{"requestedReviewer": {"__typename": "User", "login": "me"}}]},
	"viewerLatestReview": {"state": "PENDING", "submittedAt": null, "commit": null},
	"timelineItems": {"nodes": [
		{"createdAt": "2026-10-04T08:00:00Z", "actor": null,
			"requestedReviewer": {"__typename": "User", "login": "me"}}
	]}
}`

func TestFetchQueue(t *testing.T) {
	fake := &fakeGitHub{route: queueFake{
		searches: map[string]string{
			// An issue the search matched decodes to nothing and is skipped.
			"review-requested:@me": "[" + requestedPR + ", {}, " + directPR + "]",
		},
		stacks: `[
			{"number": 30, "open": true, "base": {"ref": "main"},
				"pull_requests": [{"number": 5}, {"number": 7}, {"number": 8}]},
			{"number": 31, "open": false, "base": {"ref": "main"},
				"pull_requests": [{"number": 9}]},
			{"number": 32, "open": true, "base": {"ref": "main"},
				"pull_requests": [{"number": 1}, {"number": 2}]}
		]`,
		teams: `[{"slug": "go"}, {"slug": "ops"}]`,
		// #8 no longer resolves.
		prs: `{"p0": {"number": 5, "state": "MERGED", "merged": true,
			"mergeQueueEntry": null}, "p1": null}`,
	}.route}
	c := fakeClient(t, fake)
	queue, err := c.FetchQueue(context.Background(), QueueQuery{})
	if err != nil {
		t.Fatal(err)
	}

	if queue.Viewer != "me" || queue.Trunk != "main" || queue.Total != 2 ||
		!slices.Equal(queue.Teams, []string{"go", "ops"}) {
		t.Fatalf("queue %+v", queue)
	}
	// Only the open stack with a requested PR is kept, with every PR of it.
	if len(queue.Stacks) != 1 || queue.Stacks[0].Number != 30 ||
		!slices.Equal(queue.Stacks[0].PRs, []int{5, 7, 8}) {
		t.Fatalf("stacks %+v", queue.Stacks)
	}
	var numbers []int
	for _, pr := range queue.PRs {
		numbers = append(numbers, pr.Number)
	}
	if !slices.Equal(numbers, []int{7, 9, 5}) {
		t.Fatalf("PRs %v, want the requested ones and then the rest of the stack", numbers)
	}

	seven, nine, five := queue.PRs[0], queue.PRs[1], queue.PRs[2]
	if !seven.Requested || seven.Direct || !slices.Equal(seven.Via, []string{"go"}) ||
		!slices.Equal(seven.RequestedTeams, []string{"go", "web"}) ||
		!slices.Equal(seven.RequestedUsers, []string{"bob"}) {
		t.Fatalf("#7 requests %+v", seven)
	}
	// The latest request to the viewer's team, not the later ones to others.
	if want := time.Date(2026, 10, 3, 8, 0, 0, 0, time.UTC); !seven.AskedAt.Equal(want) ||
		seven.AskedBy != "dan" {
		t.Fatalf("#7 asked %v by %q", seven.AskedAt, seven.AskedBy)
	}
	if seven.Mine == nil || seven.Mine.State != "CHANGES_REQUESTED" ||
		seven.Mine.Commit != "old7" ||
		len(seven.Reviews) != 1 ||
		seven.Reviews[0].Login != "cat" {
		t.Fatalf("#7 mine %+v reviews %+v", seven.Mine, seven.Reviews)
	}
	if !seven.Conflicts || seven.Checks != CheckFailure || seven.Commits != 4 ||
		seven.HeadSHA != "head7" || seven.Author != "amy" || seven.AuthorBot ||
		!slices.Equal(seven.Labels, []string{"api"}) {
		t.Fatalf("#7 %+v", seven)
	}
	if !nine.Requested || !nine.Direct || !nine.IsDraft || !nine.AuthorBot ||
		nine.Mine == nil || nine.Mine.State != "PENDING" || nine.AskedBy != "" ||
		nine.AskedAt.IsZero() {
		t.Fatalf("#9 %+v mine %+v", nine, nine.Mine)
	}
	if five.Requested || !five.Merged {
		t.Fatalf("#5 %+v", five)
	}

	// The teams are asked for once.
	before := len(fake.requests)
	if _, err := c.FetchQueue(context.Background(), QueueQuery{}); err != nil {
		t.Fatal(err)
	}
	for _, r := range fake.requests[before:] {
		if strings.Contains(r, "organization(login") {
			t.Fatal("the viewer's teams should be fetched once")
		}
	}
}

// With teams named, each team and the viewer are searched for, and a PR that
// several searches find is listed once.
func TestFetchQueueForTeams(t *testing.T) {
	fake := &fakeGitHub{route: queueFake{
		searches: map[string]string{
			"user-review-requested:@me":   "[" + directPR + "]",
			"team-review-requested:o/go":  "[" + requestedPR + "]",
			"team-review-requested:o/web": "[" + requestedPR + "]",
		},
		stacks: `[]`,
		teams:  `[]`,
	}.route}
	queue, err := fakeClient(t, fake).FetchQueue(
		context.Background(), QueueQuery{Teams: []string{"go", "web"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.PRs) != 2 || queue.PRs[0].Number != 9 || queue.PRs[1].Number != 7 {
		t.Fatalf("PRs %+v", queue.PRs)
	}
	// The viewer is in no team, so none of the requested teams is theirs.
	if len(queue.PRs[1].Via) != 0 || queue.Teams == nil {
		t.Fatalf("via %v teams %v", queue.PRs[1].Via, queue.Teams)
	}
	for _, r := range fake.requests {
		if strings.Contains(r, "search(query") && !strings.Contains(r, "-author:@me") {
			t.Fatalf("searches should leave out the viewer's own PRs: %s", r)
		}
	}
}

// A repository without stacked pull requests has a queue without stacks, and
// teams that cannot be read make every requested team count.
func TestFetchQueueWithoutStacksOrTeams(t *testing.T) {
	fake := &fakeGitHub{route: queueFake{
		searches:     map[string]string{"review-requested:@me": "[" + requestedPR + "]"},
		stacksStatus: 404,
	}.route}
	queue, err := fakeClient(t, fake).FetchQueue(context.Background(), QueueQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.Stacks) != 0 || queue.Teams != nil ||
		!slices.Equal(queue.PRs[0].Via, []string{"go", "web"}) {
		t.Fatalf("stacks %v teams %v via %v", queue.Stacks, queue.Teams, queue.PRs[0].Via)
	}
}

func TestFetchQueueFailsWhenStacksFail(t *testing.T) {
	fake := &fakeGitHub{route: queueFake{
		searches:     map[string]string{"review-requested:@me": "[]"},
		stacksStatus: 401,
	}.route}
	if _, err := fakeClient(t, fake).FetchQueue(context.Background(), QueueQuery{}); err == nil ||
		!strings.Contains(err.Error(), "listing stacks") {
		t.Fatalf("err = %v", err)
	}
}

func TestQueueQueryKey(t *testing.T) {
	a := QueueQuery{Teams: []string{"Go", "api"}}
	b := QueueQuery{Teams: []string{"api", "go"}}
	if a.Key() != b.Key() || a.Key() == (QueueQuery{}).Key() {
		t.Fatalf("keys %q %q", a.Key(), b.Key())
	}
	if !slices.Equal(a.Teams, []string{"Go", "api"}) {
		t.Fatal("Key should not reorder the query's teams")
	}
}

func TestFetchReviewDetails(t *testing.T) {
	fake := &fakeGitHub{status: 200, body: `{"data": {"repository": {
		"d0": {"number": 7, "body": "  Why\n\nBecause.  ",
			"files": {"nodes": [
				{"path": "a.go", "additions": 3, "deletions": 1, "changeType": "MODIFIED"},
				{"path": "b.go", "additions": 9, "changeType": "ADDED"},
				{"path": "c.go", "deletions": 4, "changeType": "DELETED"},
				{"path": "d.go", "changeType": "RENAMED"}
			]},
			"reviewThreads": {"nodes": [
				{"isResolved": false, "comments": {"nodes": [
					{"author": {"__typename": "User", "login": "cat"}}]}},
				{"isResolved": false, "comments": {"nodes": [
					{"author": {"__typename": "Bot", "login": "copilot"}}]}},
				{"isResolved": true, "comments": {"nodes": []}}
			]},
			"recent": {"nodes": [
				{"commit": {"oid": "one", "messageHeadline": "First"}},
				{"commit": {"oid": "two", "messageHeadline": "Second"}}
			]},
			"commits": {"nodes": [{"commit": {"oid": "two", "statusCheckRollup": {
				"state": "FAILURE", "contexts": {"nodes": [
					{"__typename": "CheckRun", "name": "test", "status": "COMPLETED",
						"conclusion": "CANCELLED"}
				]}}}}]}},
		"d1": null
	}}, "errors": [{"type": "NOT_FOUND", "message": "Could not resolve to a PullRequest"}]}`}
	got, err := fakeClient(t, fake).FetchReviewDetails(context.Background(), []int{7, 9})
	if err != nil {
		t.Fatalf("partial errors should not fail the fetch: %v", err)
	}
	d := got[7]
	if len(got) != 1 || d == nil || d.Body != "Why\n\nBecause." {
		t.Fatalf("details %+v", got)
	}
	var files []string
	for _, f := range d.Files {
		files = append(files, f.Status+" "+f.Path)
	}
	if !slices.Equal(files, []string{"M a.go", "A b.go", "D c.go", "R d.go"}) {
		t.Fatalf("files %v", files)
	}
	// Newest first, and a cancelled check is not a failure.
	if len(d.Commits) != 2 || d.Commits[0].SHA != "two" || d.Commits[0].Subject != "Second" ||
		d.Checks.State != CheckCancelled || d.Unresolved != 1 || d.UnresolvedBots != 1 {
		t.Fatalf("detail %+v", d)
	}
}

// A PR with more files than one page holds is asked for the rest.
func TestFetchReviewDetailsPagesFiles(t *testing.T) {
	page := func(path string, more bool, cursor string) string {
		return fmt.Sprintf(`{"pageInfo": {"hasNextPage": %v, "endCursor": %q},
			"nodes": [{"path": %q, "additions": 1, "changeType": "ADDED"}]}`, more, cursor, path)
	}
	fake := &fakeGitHub{route: func(request string) (int, string, bool) {
		rest := `{"data": {"repository": {"pullRequest": {"files": %s}}}}`
		switch {
		case strings.Contains(request, `"after":"c1"`):
			return 200, fmt.Sprintf(rest, page("b.go", true, "c2")), true
		case strings.Contains(request, `"after":"c2"`):
			return 200, fmt.Sprintf(rest, page("c.go", false, "")), true
		}
		return 200, `{"data": {"repository": {"d0": {"number": 7, "files": ` +
			page("a.go", true, "c1") + `}}}}`, true
	}}
	got, err := fakeClient(t, fake).FetchReviewDetails(context.Background(), []int{7})
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range got[7].Files {
		paths = append(paths, f.Path)
	}
	if !slices.Equal(paths, []string{"a.go", "b.go", "c.go"}) || len(fake.requests) != 3 {
		t.Fatalf("files %v after %d requests", paths, len(fake.requests))
	}
}

func TestFetchReviewDetailsCutsLongBodies(t *testing.T) {
	body := strings.Repeat("é", maxBody+10)
	fake := &fakeGitHub{status: 200, body: fmt.Sprintf(
		`{"data": {"repository": {"d0": {"number": 7, "body": %q}}}}`, body,
	)}
	got, err := fakeClient(t, fake).FetchReviewDetails(context.Background(), []int{7})
	if err != nil {
		t.Fatal(err)
	}
	if r := []rune(got[7].Body); len(r) != maxBody+1 || r[maxBody] != '…' {
		t.Fatalf("body is %d runes", len(r))
	}
}
