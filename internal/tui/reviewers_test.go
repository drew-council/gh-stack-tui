package tui

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cli/go-gh/v2/pkg/api"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// fakeGitHub answers every request with status, and records the requests.
type fakeGitHub struct {
	status   int
	requests []string
}

func (f *fakeGitHub) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	f.requests = append(f.requests, req.Method+" "+req.URL.Path+" "+string(body))
	return &http.Response{
		StatusCode: f.status,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"message": "Validation Failed"}`)),
		Request:    req,
	}, nil
}

// submit presses enter in the picker and feeds back the result of
// requesting reviewers. Batched commands run concurrently, since flashes
// batch a timer that would otherwise hold the test up.
func submit(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	results := make(chan tea.Msg, 8)
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if c != nil {
				go func() { results <- c() }()
			}
		}
	} else {
		results <- msg
	}
	for {
		select {
		case msg := <-results:
			if msg, ok := msg.(reviewersRequestedMsg); ok {
				next, _ = m.Update(msg)
				return next.(Model)
			}
		case <-time.After(2 * time.Second):
			return m
		}
	}
}

func draftModel(t *testing.T, fake *fakeGitHub) Model {
	t.Helper()
	m := testModel()
	client, err := github.NewClientWith(
		github.Repo{Host: "github.com", Owner: "o", Name: "r"},
		api.ClientOptions{
			Host: "github.com", AuthToken: "token", Transport: fake, LogIgnoreEnv: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	m.gh = client
	pr := m.prs["two"]
	pr.IsDraft = true
	pr.RequestedTeams = []string{"go-readability"}
	pr.Suggested = []string{"yuki"}
	m.reviewers = []github.Reviewer{
		{Login: "amy", Name: "Amy Adams"},
		{Login: "mbhatg", RecentPRs: 38, Recent: 12.8},
		{Login: "zachsheer", Name: "Zach Sheer", RecentPRs: 13, Recent: 7.1},
		{Login: "bob"},
		{Login: "yuki", Name: "Yuki Sato"},
	}
	m.reviewersLoaded = true
	// Keep ops queued instead of running gh.
	m.op = &opState{title: "busy"}
	return m
}

func keys(m Model, msgs ...tea.KeyPressMsg) Model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

var (
	keyTab   = tea.KeyPressMsg{Code: tea.KeyTab}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyCtrlS = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
)

func shown(m Model) []string {
	var logins []string
	for _, c := range m.picker.shown {
		logins = append(logins, c.Login)
	}
	return logins
}

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		query, s string
		ok       bool
	}{
		{"", "anything", true},
		{"zs", "zachsheer Zach Sheer", true},
		{"zach sheer", "zachsheer Zach Sheer", true},
		{"ZACH", "zachsheer", true},
		{"hz", "zachsheer", false},
	}
	for _, c := range cases {
		if _, _, ok := fuzzyMatch(c.query, c.s); ok != c.ok {
			t.Errorf("fuzzyMatch(%q, %q) ok = %v, want %v", c.query, c.s, ok, c.ok)
		}
	}
	// Word starts and runs beat scattered matches.
	prefix, _, _ := fuzzyMatch("sa", "sam Sato")
	scattered, _, _ := fuzzyMatch("sa", "mbhatsg sxa")
	if prefix <= scattered {
		t.Errorf("prefix score %d should beat scattered %d", prefix, scattered)
	}
	// Two word starts beat the s inside zachsheer.
	if _, pos, _ := fuzzyMatch(
		"ss",
		"zachsheer Sam Sato",
	); len(pos) != 2 || pos[0] != 10 ||
		pos[1] != 14 {
		t.Errorf("positions = %v", pos)
	}
}

func TestReadyWithOnlyTeamsOpensReviewerPicker(t *testing.T) {
	fake := &fakeGitHub{status: 201}
	m := press(draftModel(t, fake), "D")
	if m.mode != modeReviewers || m.picker == nil {
		t.Fatalf("D on a draft with only team reviewers should open the picker, flash %q", m.flash)
	}
	// GitHub's suggestions first, then recent reviewers, then A-Z.
	want := "yuki mbhatg zachsheer amy bob"
	if got := strings.Join(shown(m), " "); got != want {
		t.Fatalf("order = %s, want %s", got, want)
	}

	// Typing filters; q, j and k are text, not commands.
	m = press(m, "z", "s")
	if got := strings.Join(shown(m), " "); got != "zachsheer" {
		t.Fatalf("filtered = %s, want zachsheer", got)
	}
	m = keys(m, keyTab)
	m = press(m, "esc")
	if m.mode != modeNormal || m.picker != nil || len(m.queue) != 0 {
		t.Fatal("esc should cancel without marking ready")
	}

	// Pick two, then enter requests both, then marks the PR ready.
	m = press(m, "D")
	m = keys(m, keyDown, keyTab, keyTab)
	m = submit(t, m)
	wantReq := `POST /repos/o/r/pulls/2/requested_reviewers {"reviewers":["mbhatg","zachsheer"]}`
	if len(fake.requests) != 1 || fake.requests[0] != wantReq {
		t.Fatalf("requests = %q, want %s", fake.requests, wantReq)
	}
	if m.mode != modeNormal || len(m.queue) != 1 {
		t.Fatalf("the ready op should queue, mode %v queue %+v", m.mode, m.queue)
	}
	op := m.queue[0]
	if op.title != "ready #2 → mbhatg, zachsheer" ||
		op.name+" "+strings.Join(op.args, " ") != "gh pr ready 2" {
		t.Fatalf("op = %+v", op)
	}
}

func TestReviewerRequestFailureKeepsDraft(t *testing.T) {
	fake := &fakeGitHub{status: 422}
	m := press(draftModel(t, fake), "D")
	m = submit(t, m)
	if len(fake.requests) != 1 || len(m.queue) != 0 || !m.flashErr ||
		!strings.Contains(m.flash, "requesting review on #2") ||
		!strings.Contains(m.flash, "nothing marked ready") {
		t.Fatalf("a failed request should not mark ready, queue %+v flash %q", m.queue, m.flash)
	}
}

func TestReviewerPickerEnterTakesHovered(t *testing.T) {
	m := press(draftModel(t, &fakeGitHub{status: 201}), "D")
	m = press(m, "a", "m", "y")
	m = submit(t, m)
	if len(m.queue) != 1 || m.queue[0].title != "ready #2 → amy" {
		t.Fatalf("enter with nobody picked should take the hovered person, queue %+v", m.queue)
	}

	m = press(draftModel(t, &fakeGitHub{status: 201}), "D")
	m = keys(m, keyCtrlS)
	if len(m.queue) != 1 || m.queue[0].title != "ready #2" ||
		m.queue[0].name+" "+strings.Join(m.queue[0].args, " ") != "gh pr ready 2" {
		t.Fatalf("ctrl+s should mark ready without reviewers, queue %+v", m.queue)
	}
}

func TestReadySkipsPickerWhenSomeoneIsRequested(t *testing.T) {
	m := draftModel(t, &fakeGitHub{status: 201})
	m.prs["two"].Reviewers = []string{"mbhatg"}
	m = press(m, "D")
	if m.mode != modeNormal || len(m.queue) != 1 || m.queue[0].title != "ready #2" {
		t.Fatalf("a draft with a reviewer should just be marked ready, queue %+v", m.queue)
	}

	// Converting back to a draft never asks.
	m = draftModel(t, &fakeGitHub{status: 201})
	m.prs["two"].IsDraft = false
	m = press(m, "D")
	if m.mode != modeNormal || len(m.queue) != 1 || m.queue[0].title != "draft #2" {
		t.Fatalf("queue %+v", m.queue)
	}

	// Nor does a repository with nobody to ask.
	m = draftModel(t, &fakeGitHub{status: 201})
	m.reviewers = nil
	m = press(m, "D")
	if m.mode != modeNormal || len(m.queue) != 1 {
		t.Fatalf("queue %+v", m.queue)
	}
}

func TestReviewerPickerWaitsForReviewers(t *testing.T) {
	m := draftModel(t, &fakeGitHub{status: 201})
	loaded := m.reviewers
	m.reviewers, m.reviewersLoaded = nil, false
	m = press(m, "D")
	if m.mode != modeReviewers {
		t.Fatal("picker should open while reviewers are loading")
	}
	if screen := ansi.Strip(m.render()); !strings.Contains(screen, "loading reviewers") {
		t.Fatalf("picker should say reviewers are loading:\n%s", screen)
	}
	m = press(m, "b")
	next, _ := m.Update(reviewersLoadedMsg{reviewers: loaded})
	m = next.(Model)
	if got := strings.Join(shown(m), " "); got != "bob mbhatg" {
		t.Fatalf("loaded reviewers should be filtered by the query so far, got %s", got)
	}
	screen := ansi.Strip(m.render())
	for _, want := range []string{
		"Request reviewers  #2 Second layer",
		"only teams are requested: go-readability",
		"2/5",
		"▶ · bob",
		"38 recent PRs",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen is missing %q:\n%s", want, screen)
		}
	}
}
