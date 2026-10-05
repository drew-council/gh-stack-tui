package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// draftModel is testModel with two's PR a draft that only a team is asked to
// review, and the repository's reviewers loaded.
func draftModel() Model {
	m := testModel()
	m.ghRepo = github.Repo{Host: "github.com", Owner: "o", Name: "r"}
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
	m := press(draftModel(), "D")
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

	// Pick two, then enter requests both and marks the PR ready.
	m = press(m, "D")
	m = keys(m, keyDown, keyTab, keyTab)
	m = press(m, "enter")
	if m.mode != modeNormal || len(m.queue) != 1 {
		t.Fatalf("enter should queue the op, mode %v queue %+v", m.mode, m.queue)
	}
	op := m.queue[0]
	if op.title != "ready #2 → mbhatg, zachsheer" {
		t.Fatalf("title = %q", op.title)
	}
	wantCmd := `gh api --silent -X POST 'repos/o/r/pulls/2/requested_reviewers'` +
		` -f 'reviewers[]=mbhatg' -f 'reviewers[]=zachsheer'` +
		` && echo 'requested review from mbhatg, zachsheer on #2' && gh pr ready 2`
	if op.name != "sh" || len(op.args) != 2 || op.args[1] != wantCmd {
		t.Fatalf("command = %s %q\nwant %s", op.name, op.args, wantCmd)
	}
}

func TestReviewerPickerEnterTakesHovered(t *testing.T) {
	m := press(draftModel(), "D")
	m = press(m, "a", "m", "y", "enter")
	if len(m.queue) != 1 || m.queue[0].title != "ready #2 → amy" {
		t.Fatalf("enter with nobody picked should take the hovered person, queue %+v", m.queue)
	}

	m = press(draftModel(), "D")
	m = keys(m, keyCtrlS)
	if len(m.queue) != 1 || m.queue[0].title != "ready #2" ||
		m.queue[0].name+" "+strings.Join(m.queue[0].args, " ") != "gh pr ready 2" {
		t.Fatalf("ctrl+s should mark ready without reviewers, queue %+v", m.queue)
	}
}

func TestReadySkipsPickerWhenSomeoneIsRequested(t *testing.T) {
	m := draftModel()
	m.prs["two"].Reviewers = []string{"mbhatg"}
	m = press(m, "D")
	if m.mode != modeNormal || len(m.queue) != 1 || m.queue[0].title != "ready #2" {
		t.Fatalf("a draft with a reviewer should just be marked ready, queue %+v", m.queue)
	}

	// Converting back to a draft never asks.
	m = draftModel()
	m.prs["two"].IsDraft = false
	m = press(m, "D")
	if m.mode != modeNormal || len(m.queue) != 1 || m.queue[0].title != "draft #2" {
		t.Fatalf("queue %+v", m.queue)
	}

	// Nor does a repository with nobody to ask.
	m = draftModel()
	m.reviewers = nil
	m = press(m, "D")
	if m.mode != modeNormal || len(m.queue) != 1 {
		t.Fatalf("queue %+v", m.queue)
	}
}

func TestReviewerPickerWaitsForReviewers(t *testing.T) {
	m := draftModel()
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

func TestReadyForReviewOrdersEachPR(t *testing.T) {
	repo := github.Repo{Host: "ghe.example.com", Owner: "o", Name: "r"}
	title, name, args := readyForReview(
		repo, []string{"2", "3"}, map[string]bool{"3": true}, []string{"amy"},
	)
	want := `gh pr ready 2 && gh api --hostname 'ghe.example.com' --silent -X POST` +
		` 'repos/o/r/pulls/3/requested_reviewers' -f 'reviewers[]=amy'` +
		` && echo 'requested review from amy on #3' && gh pr ready 3`
	if title != "ready 2 PRs → amy" || name != "sh" || args[1] != want {
		t.Fatalf("got %q %s %q\nwant %s", title, name, args, want)
	}
}
