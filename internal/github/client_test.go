package github

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

// fakeGitHub answers every request with status and body, and records the
// requests it saw.
type fakeGitHub struct {
	status   int
	body     string
	requests []string
}

func (f *fakeGitHub) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	f.requests = append(f.requests, req.Method+" "+req.URL.Path+" "+string(body))
	return &http.Response{
		StatusCode: f.status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Request:    req,
	}, nil
}

// fakeClient returns a client whose requests fake answers.
func fakeClient(t *testing.T, fake *fakeGitHub) *Client {
	t.Helper()
	c, err := NewClientWith(
		Repo{Host: "github.com", Owner: "o", Name: "r"},
		api.ClientOptions{
			Host: "github.com", AuthToken: "token", Transport: fake, LogIgnoreEnv: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFetchPRsKeepsDataFromPartialErrors(t *testing.T) {
	fake := &fakeGitHub{status: 200, body: `{
		"data": {"repository": {
			"b0": {"number": 2, "state": "OPEN", "isDraft": true,
				"author": {"login": "me"},
				"reviewRequests": {"nodes": [
					{"requestedReviewer": {"__typename": "Team", "slug": "go-readability"}},
					{"requestedReviewer": {"__typename": "User", "login": "amy"}}
				]},
				"latestReviews": {"nodes": [{"author": {"__typename": "User", "login": "me"}}]}},
			"b1": null
		}},
		"errors": [{"type": "NOT_FOUND", "message": "Could not resolve to a PullRequest"}]
	}`}
	prs, err := fakeClient(t, fake).FetchPRs(
		context.Background(),
		[]Lookup{{Branch: "two", Number: 2}, {Branch: "gone", Number: 9}},
	)
	if err != nil {
		t.Fatalf("partial errors should not fail the fetch: %v", err)
	}
	pr := prs["two"]
	if len(prs) != 1 || pr == nil || !pr.IsDraft {
		t.Fatalf("prs = %+v", prs)
	}
	// The author's own review does not count as a reviewer.
	if len(pr.Reviewers) != 1 || pr.Reviewers[0] != "amy" ||
		len(pr.RequestedTeams) != 1 || pr.RequestedTeams[0] != "go-readability" {
		t.Fatalf("reviewers %v teams %v", pr.Reviewers, pr.RequestedTeams)
	}
	if !strings.HasPrefix(fake.requests[0], "POST /graphql ") {
		t.Fatalf("request = %s", fake.requests[0])
	}
}

func TestFetchPRsFailsWithoutData(t *testing.T) {
	fake := &fakeGitHub{status: 401, body: `{"message": "Bad credentials"}`}
	_, err := fakeClient(t, fake).FetchPRs(context.Background(), []Lookup{{Branch: "two"}})
	if err == nil || !strings.Contains(err.Error(), "Bad credentials") {
		t.Fatalf("err = %v", err)
	}
}

func TestRequestReviewers(t *testing.T) {
	fake := &fakeGitHub{status: 201, body: `{}`}
	err := fakeClient(t, fake).RequestReviewers(context.Background(), 2, []string{"amy", "bob"})
	want := `POST /repos/o/r/pulls/2/requested_reviewers {"reviewers":["amy","bob"]}`
	if err != nil || len(fake.requests) != 1 || fake.requests[0] != want {
		t.Fatalf("err %v requests %q, want %s", err, fake.requests, want)
	}
}
