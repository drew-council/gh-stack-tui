package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/demo"
)

// demoModel shows the demo stacks on a width by height screen, loaded the way
// the TUI loads a repository.
func demoModel(t *testing.T, width, height int) Model {
	t.Helper()
	src := demo.NewSource(func() time.Time { return testNow }, t.TempDir())
	remote := &demo.Remote{}
	m := New(src, Options{
		ReviewCmd:   "tuicr -r {range}",
		ReviewPRCmd: "tuicr pr {pr}",
		Connect:     func(string) (Remote, error) { return remote, nil },
	})
	m.now = func() time.Time { return testNow }
	m = update(m, tea.WindowSizeMsg{Width: width, Height: height})
	m = loadDemo(t, m)
	reviewers, err := remote.FetchReviewers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return update(m, reviewersLoadedMsg{reviewers: reviewers, at: testNow})
}

// loadDemo loads the stack m.pinned picks and its PRs, as Init and the
// refresh ticks would.
func loadDemo(t *testing.T, m Model) Model {
	t.Helper()
	snap, err := m.local.Load(m.pinned, "")
	if err != nil {
		t.Fatal(err)
	}
	m = update(m, localLoadedMsg{snap: snap, pinned: m.pinned})
	remote, _ := m.opts.Connect("")
	prs, err := remote.FetchPRs(context.Background(), lookups(snap))
	if err != nil {
		t.Fatal(err)
	}
	return update(m, remoteLoadedMsg{client: remote, prs: prs, at: testNow.Add(-12 * time.Second)})
}

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

// TestRenderShowcase opens a little of everything on the current branch:
// files, checks with one workflow expanded, and the commits of the branch
// below.
func TestRenderShowcase(t *testing.T) {
	m := press(demoModel(t, 80, 64), "x", "l", "j", "j", "l", "l", "J", "C", "K", "f")
	onBranch(t, m, demo.CurrentBranch)
	assertScreen(t, m)
}

func TestRenderCollapsed(t *testing.T) {
	assertScreen(t, demoModel(t, 80, 40))
}

// TestRenderNarrow is a quarter of a 4K screen at a large font.
func TestRenderNarrow(t *testing.T) {
	assertScreen(t, press(demoModel(t, 56, 40), "f"))
}

func TestRenderWide(t *testing.T) {
	assertScreen(t, press(demoModel(t, 120, 40), "f", "C"))
}

// TestRenderScrolled is too short for the stack, so the panel says how much
// is above and below.
func TestRenderScrolled(t *testing.T) {
	assertScreen(t, press(demoModel(t, 80, 20), "f", "l", "j", "j", "j"))
}

func TestRenderItemCursor(t *testing.T) {
	assertScreen(t, press(demoModel(t, 80, 40), "f", "l", "j", "j"))
}

func TestRenderSelection(t *testing.T) {
	assertScreen(t, press(demoModel(t, 80, 40), "K", "v", "J", "J"))
}

// The checked out branch is marked, and the cursor moves on to an unmarked one.
func TestRenderMarks(t *testing.T) {
	assertScreen(t, press(demoModel(t, 80, 40), "K", "space", "space"))
}

func TestRenderOtherStack(t *testing.T) {
	m := loadDemo(t, press(demoModel(t, 80, 30), "]"))
	if r := hovered(t, m); r.kind != rowBranch {
		t.Fatalf("a new stack should start on a branch, got %+v", r)
	}
	assertScreen(t, m)
}

func TestRenderLoading(t *testing.T) {
	m := demoModel(t, 80, 30)
	m.prs, m.loadingRemote = nil, true
	m.buildRows()
	assertScreen(t, m)
}

func TestRenderHeaderNotes(t *testing.T) {
	m := demoModel(t, 80, 30)
	m.snap.Rebasing, m.snap.Dirty = true, true
	m.remoteErr = errors.New("HTTP 502: Bad Gateway (https://api.github.com/graphql)")
	assertScreen(t, m)
}

func TestRenderOutputPanel(t *testing.T) {
	m := demoModel(t, 80, 40)
	m.output = []string{
		"$ gh stack push",
		"Pushing 5 branches to origin…",
		"  auth/token-refresh   up to date",
		"  auth/middleware      up to date",
		"  auth/rate-limit      ! [rejected] (non-fast-forward)",
		"hint: Updates were rejected because the tip of your current branch is behind its remote counterpart.",
		"error: failed to push some refs",
		"[exit 1]",
	}
	m.outputTitle, m.outputState, m.showOutput = "push", opFailed, true
	m.flash, m.flashErr = "push failed: error: failed to push some refs", true
	assertScreen(t, m)
}

func TestRenderHelp(t *testing.T) {
	assertScreen(t, press(demoModel(t, 80, 64), "?"))
}

func TestRenderHelpScrolled(t *testing.T) {
	assertScreen(t, press(demoModel(t, 80, 30), "?", "ctrl+d", "j", "j"))
}

func TestRenderConfirm(t *testing.T) {
	m := press(demoModel(t, 56, 30), "J", "M")
	if m.mode != modeConfirm {
		t.Fatalf("M should ask before merging, mode %v", m.mode)
	}
	assertScreen(t, m)
}

func TestRenderReviewerPicker(t *testing.T) {
	m := press(demoModel(t, 80, 30), "K", "D")
	if m.mode != modeReviewers {
		t.Fatalf("D on a draft without reviewers should open the picker, mode %v", m.mode)
	}
	assertScreen(t, press(m, "tab"))
}
