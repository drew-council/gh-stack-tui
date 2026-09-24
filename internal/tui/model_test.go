package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/git"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/stack"
)

func testModel() Model {
	m := New(&git.Repo{}, Options{ReviewCmd: "tuicr -r {range}"})
	m.width, m.height = 100, 40
	m.loadingLocal = false
	m.snap = &stack.Snapshot{
		Key:           "n:1",
		Index:         0,
		StackCount:    1,
		Trunk:         "main",
		CurrentBranch: "two",
		Branches: []stack.Branch{
			{
				Name:     "one",
				Parent:   "main",
				Head:     "h1",
				DiffBase: "b1",
				Merged:   true,
				PR:       &stack.PullRequestRef{Number: 1},
			},
			{
				Name: "two", Parent: "main", Head: "h2", DiffBase: "h1", IsCurrent: true,
				PR:      &stack.PullRequestRef{Number: 2, URL: "https://github.com/o/r/pull/2"},
				Files:   []git.FileChange{{Path: "a.go", Status: "M"}, {Path: "b.go", Status: "A"}},
				Commits: []git.Commit{{SHA: "c2", Subject: "two"}},
			},
			{
				Name:     "three",
				Parent:   "two",
				Head:     "h3",
				DiffBase: "h2",
				Files:    []git.FileChange{{Path: "c.go"}},
			},
		},
	}
	m.prs = map[string]*github.PR{"two": {
		Number: 2, URL: "https://github.com/o/r/pull/2", State: "OPEN",
		Checks: github.Rollup{State: github.CheckFailure, Workflows: []github.Workflow{
			{
				Name:   "ci",
				State:  github.CheckFailure,
				Checks: []github.Check{{Name: "test", State: github.CheckFailure}},
			},
		}},
	}}
	m.buildRows()
	m.restoreCursor("two")
	return m
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func hoveredKind(t *testing.T, m Model) row {
	t.Helper()
	r, ok := m.hovered()
	if !ok {
		t.Fatal("nothing hovered")
	}
	return r
}

func TestRowsTopFirstWithMergedSeparator(t *testing.T) {
	m := testModel()
	var kinds []rowKind
	for _, r := range m.rows {
		kinds = append(kinds, r.kind)
	}
	want := []rowKind{
		rowBranch, rowSection, rowSpacer, // three: files
		rowBranch, rowSection, rowSection, rowSection, rowSpacer, // two: files, commits, checks
		rowSeparator, rowBranch, rowSpacer, // one (merged)
		rowTrunk,
	}
	if len(kinds) != len(want) {
		t.Fatalf("rows = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("rows = %v, want %v", kinds, want)
		}
	}
	if r := hoveredKind(t, m); r.kind != rowBranch || m.snap.Branches[r.branch].Name != "two" {
		t.Fatalf("cursor should start on the current branch")
	}
}

func TestExpandCollapseKeepsCursorInTree(t *testing.T) {
	m := press(testModel(), "j", "l", "j", "j")
	r := hoveredKind(t, m)
	if r.kind != rowFile || r.file.Path != "b.go" {
		t.Fatalf("expected to be on b.go, got %+v", r)
	}
	// h on a file closes its section and parks on it.
	m = press(m, "h")
	if r := hoveredKind(t, m); r.kind != rowSection || r.section != secFiles || m.expanded[r.key] {
		t.Fatalf("h should collapse to the files section, got %+v", r)
	}
	// x opens checks from anywhere in the branch; drill into the workflow.
	m = press(m, "x", "j", "j", "j", "l", "j")
	if r := hoveredKind(t, m); r.kind != rowCheck || r.check.Name != "test" {
		t.Fatalf("expected the failing check, got %+v", r)
	}
	// Collapsing checks from inside moves the cursor onto the section.
	m = press(m, "x")
	if r := hoveredKind(t, m); r.kind != rowSection || r.section != secChecks {
		t.Fatalf("expected checks section, got %+v", r)
	}
}

func TestCursorSurvivesReload(t *testing.T) {
	m := press(testModel(), "f", "j", "j")
	before := hoveredKind(t, m)
	m.buildRows()
	m.restoreCursor("")
	if after := hoveredKind(t, m); after.key != before.key {
		t.Fatalf("cursor moved from %q to %q", before.key, after.key)
	}
}

func TestReviewRangeSpansSelection(t *testing.T) {
	m := press(testModel(), "K", "v", "J")
	sel := m.reviewSelection()
	if len(sel) != 2 || sel[0] != 1 || sel[1] != 2 {
		t.Fatalf("selection = %v, want [1 2]", sel)
	}
	rng, gap, err := m.reviewRange(sel)
	if err != nil || gap || rng != "h1..h3" {
		t.Fatalf("range = %q gap=%v err=%v, want h1..h3", rng, gap, err)
	}

	// Marks on the top and bottom branches are not contiguous.
	m = press(testModel(), "esc", "K", "space", "J", "J", "space")
	sel = m.reviewSelection()
	if _, gap, _ := m.reviewRange(sel); len(sel) != 2 || !gap {
		t.Fatalf("selection = %v gap=%v, want two non-contiguous branches", sel, gap)
	}
}

func TestOpsRequireCheckedOutStack(t *testing.T) {
	m := testModel()
	m.snap.CurrentBranch = "main"
	m = press(m, "p")
	if m.op != nil || !m.flashErr {
		t.Fatal("push should refuse while the stack is not checked out")
	}
}
