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
	m.suggested = map[int]suggestion{2: {head: pr.HeadSHA, logins: []string{"yuki"}}}
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

func TestSuggestionsRerankOpenPicker(t *testing.T) {
	m := draftModel(t, &fakeGitHub{status: 201})
	m.suggested = nil
	m = press(m, "D")
	if !m.loadingSuggested {
		t.Fatal("opening the picker without suggestions should load them")
	}
	if got := strings.Join(shown(m), " "); got != "mbhatg zachsheer amy bob yuki" {
		t.Fatalf("order = %s", got)
	}
	m = keys(m, keyDown)

	next, _ := m.Update(suggestedLoadedMsg{
		heads:  map[int]string{2: m.prs["two"].HeadSHA},
		logins: map[int][]string{2: {"yuki"}},
	})
	m = next.(Model)
	if got := strings.Join(shown(m), " "); got != "yuki mbhatg zachsheer amy bob" {
		t.Fatalf("order = %s", got)
	}
	if c, _ := m.picker.hovered(); c.Login != "zachsheer" {
		t.Fatalf("hovered = %s, want zachsheer to stay hovered", c.Login)
	}
	if m.requestSuggested() != nil {
		t.Fatal("suggestions for an unchanged head should not be fetched again")
	}
}

func TestReviewerRequestFailureKeepsDraft(t *testing.T) {
	fake := &fakeGitHub{status: 422}
	m := press(draftModel(t, fake), "D")
	m = submit(t, m)
	if len(fake.requests) != 1 || len(m.queue) != 0 || !m.flashErr ||
		!strings.Contains(m.flash, "requesting review on #2") ||
		!strings.Contains(m.flash, "left as a draft") {
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

	// Converting back to a draft asks first, but never for reviewers.
	m = draftModel(t, &fakeGitHub{status: 201})
	m.prs["two"].IsDraft = false
	m = press(m, "D")
	if m.mode != modeConfirm || len(m.queue) != 0 {
		t.Fatalf("converting to a draft should confirm first, mode %v queue %+v", m.mode, m.queue)
	}
	m = press(m, "y")
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

// draftPRs makes branches two and three drafts that only have team reviewers.
func draftPRs(t *testing.T, fake *fakeGitHub) Model {
	t.Helper()
	m := draftModel(t, fake)
	m.prs["three"] = &github.PR{
		Number: 3, State: "OPEN", IsDraft: true, Title: "Third layer", HeadSHA: "h3",
		RequestedTeams: []string{"go-readability"},
	}
	m.suggested[3] = suggestion{head: "h3", logins: []string{"yuki"}}
	m.marks["two"], m.marks["three"] = true, true
	return m
}

func TestReadyWizardWalksEachDraft(t *testing.T) {
	fake := &fakeGitHub{status: 201}
	m := press(draftPRs(t, fake), "D")
	if m.mode != modeReviewers || m.picker.step.pr.Number != 2 {
		t.Fatalf("first step should ask for #2, mode %v", m.mode)
	}
	if got := ansi.Strip(m.render()); !strings.Contains(got, "#2 Second layer") ||
		!strings.Contains(got, "two · step 1/2 · only teams are requested") {
		t.Fatalf("the first step should show its branch, number, title, and progress:\n%s", got)
	}

	m = submit(t, m)
	if m.mode != modeReviewers || m.picker.step.pr.Number != 3 {
		t.Fatalf("the wizard should move to #3, mode %v", m.mode)
	}
	if got := ansi.Strip(m.render()); !strings.Contains(got, "#3 Third layer") ||
		!strings.Contains(got, "three · step 2/2 · only teams are requested") {
		t.Fatalf("the second step should show its branch, number, title, and progress:\n%s", got)
	}

	m = submit(t, m)
	if m.mode != modeNormal || m.wizard != nil || len(m.queue) != 2 {
		t.Fatalf("both drafts should be marked ready, mode %v wizard %+v queue %+v",
			m.mode, m.wizard, m.queue)
	}
	if m.queue[0].title != "ready #2 → yuki" || m.queue[1].title != "ready #3 → yuki" {
		t.Fatalf("queue = %+v", m.queue)
	}
	if len(fake.requests) != 2 ||
		!strings.Contains(fake.requests[0], "/pulls/2/requested_reviewers") ||
		!strings.Contains(fake.requests[1], "/pulls/3/requested_reviewers") {
		t.Fatalf("requests = %q", fake.requests)
	}
}

func TestReviewerWizardSkipMovesOn(t *testing.T) {
	m := press(draftPRs(t, &fakeGitHub{status: 201}), "D")
	m = press(m, "esc")
	if m.mode != modeReviewers || m.picker.step.pr.Number != 3 {
		t.Fatalf("skipping #2 should ask about #3, mode %v", m.mode)
	}
	m = press(m, "esc")
	if m.mode != modeNormal || m.wizard != nil || len(m.queue) != 0 {
		t.Fatalf("skipping the last PR should end the wizard, mode %v queue %+v", m.mode, m.queue)
	}
}

func TestDraftMixedSelectionOnlyMarksDraftsReady(t *testing.T) {
	m := draftModel(t, &fakeGitHub{status: 201})
	m.prs["three"] = &github.PR{
		Number: 3, State: "OPEN", Title: "Third layer", HeadSHA: "h3",
	}
	m.marks["two"], m.marks["three"] = true, true

	m = press(m, "D")
	if m.mode != modeReviewers || m.picker.step.pr.Number != 2 || len(m.wizard.steps) != 1 {
		t.Fatalf("only the draft should be walked, mode %v", m.mode)
	}
	m = submit(t, m)
	if m.mode != modeNormal || len(m.queue) != 1 || m.queue[0].title != "ready #2 → yuki" {
		t.Fatalf("the ready PR should stay ready, mode %v queue %+v", m.mode, m.queue)
	}
}

func TestWizardMovesOnBeforeTheRequestReturns(t *testing.T) {
	m := press(draftPRs(t, &fakeGitHub{status: 201}), "D")
	m.submitPicker([]string{"yuki"})
	if m.mode != modeReviewers || m.picker.step.pr.Number != 3 {
		t.Fatalf("the next prompt should open right away, mode %v", m.mode)
	}
	if len(m.queue) != 0 {
		t.Fatalf(
			"#2 should not be marked ready before its reviewers are requested, queue %+v",
			m.queue,
		)
	}

	next, _ := m.Update(reviewersRequestedMsg{num: 2, ready: true, who: "yuki"})
	m = next.(Model)
	if m.mode != modeReviewers || m.picker.step.pr.Number != 3 ||
		len(m.queue) != 1 || m.queue[0].title != "ready #2 → yuki" {
		t.Fatalf("the reply should mark #2 ready and leave the #3 prompt open, mode %v queue %+v",
			m.mode, m.queue)
	}
}

func TestCancelKeepsSelection(t *testing.T) {
	m := press(draftPRs(t, &fakeGitHub{status: 201}), "D")
	if len(m.marks) != 2 {
		t.Fatalf("marks = %v", m.marks)
	}
	m = press(m, "esc")
	if m.mode != modeReviewers || len(m.marks) != 2 {
		t.Fatalf("skipping the first PR should keep the selection, mode %v marks %v",
			m.mode, m.marks)
	}
	m = press(m, "esc")
	if len(m.marks) != 2 {
		t.Fatalf("cancelling the wizard should keep the selection, marks %v", m.marks)
	}
}

func TestAskReviewersDoesNotChangeDraftState(t *testing.T) {
	fake := &fakeGitHub{status: 201}
	m := draftModel(t, fake)
	m.prs["two"].IsDraft = false
	m = press(m, "A")
	if m.mode != modeReviewers || m.picker.ready {
		t.Fatalf("A should open the reviewers picker without marking ready, mode %v", m.mode)
	}
	m = submit(t, m)
	if len(fake.requests) != 1 || len(m.queue) != 0 {
		t.Fatalf("A should only request reviewers, requests %q queue %+v", fake.requests, m.queue)
	}
}

func TestDraftWizardConfirmsEachAndSkipsRejected(t *testing.T) {
	fake := &fakeGitHub{status: 201}
	m := draftModel(t, fake)
	m.prs["two"].IsDraft = false
	m.prs["three"] = &github.PR{
		Number: 3, State: "OPEN", Title: "Third layer", HeadSHA: "h3",
	}
	m.marks["two"], m.marks["three"] = true, true

	m = press(m, "D")
	if m.mode != modeConfirm || !strings.Contains(m.confirm.text, "#2 (two) Second layer") {
		t.Fatalf("first confirm should name #2, branch, and title, mode %v text %q",
			m.mode, m.confirm.text)
	}
	m = press(m, "n")
	if m.mode != modeConfirm || !strings.Contains(m.confirm.text, "#3 (three) Third layer") {
		t.Fatalf("rejecting #2 should confirm #3 next, mode %v text %q", m.mode, m.confirm.text)
	}
	m = press(m, "y")
	if m.mode != modeNormal || len(m.queue) != 1 || m.queue[0].title != "draft #3" {
		t.Fatalf("only #3 should be converted, queue %+v", m.queue)
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
		"Request reviewers",
		"#2 Second layer",
		"only teams are requested: go-readability",
		"2/5",
		"· bob",
		"38 recent PRs",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen is missing %q:\n%s", want, screen)
		}
	}
	for _, l := range strings.Split(m.render(), "\n") {
		if strings.Contains(ansi.Strip(l), "· bob") && !strings.Contains(l, bgSeq(m.st.cursorBg)) {
			t.Errorf("the cursor should be on bob: %q", l)
		}
	}
}
