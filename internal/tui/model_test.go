package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
				Name:      "two",
				Parent:    "main",
				Head:      "h2",
				DiffBase:  "h1",
				IsCurrent: true,
				PR:        &stack.PullRequestRef{Number: 2, URL: "https://github.com/o/r/pull/2"},
				Files: []git.FileChange{
					{Path: "a.go", Status: "M", Additions: 3},
					{Path: "b.go", Status: "A", Additions: 9},
				},
				Commits:   []git.Commit{{SHA: "c2c2c2c2c2", Subject: "two"}},
				Additions: 12,
			},
			{
				Name:     "three",
				Parent:   "two",
				Head:     "h3",
				DiffBase: "h2",
				Files:    []git.FileChange{{Path: "c.go"}},
			},
			{Name: "four", Parent: "three", Head: "h4", DiffBase: "h3"},
		},
	}
	m.prs = map[string]*github.PR{"two": {
		Number: 2, URL: "https://github.com/o/r/pull/2", State: "OPEN", Title: "Second layer",
		Approvals: 1, Unresolved: 2,
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

func hovered(t *testing.T, m Model) row {
	t.Helper()
	r, ok := m.hovered()
	if !ok {
		t.Fatal("nothing hovered")
	}
	return r
}

func onBranch(t *testing.T, m Model, name string) {
	t.Helper()
	r := hovered(t, m)
	if r.kind != rowBranch || m.snap.Branches[r.branch].Name != name {
		t.Fatalf("expected the cursor on branch %s, got %+v", name, r)
	}
}

func TestRowsTopFirstWithMergedSeparator(t *testing.T) {
	m := testModel()
	var kinds []rowKind
	for _, r := range m.rows {
		kinds = append(kinds, r.kind)
	}
	want := []rowKind{
		rowBranch, rowSpacer, // four
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
	onBranch(t, m, "two")
}

func TestJKMoveBetweenBranchesOnly(t *testing.T) {
	m := testModel()
	// Sections are not stops, and the merged branch below is skipped.
	m = press(m, "j")
	onBranch(t, m, "two")
	m = press(m, "k")
	onBranch(t, m, "three")
	m = press(m, "k", "k")
	onBranch(t, m, "four")
	m = press(m, "G")
	onBranch(t, m, "two")
	m = press(m, "g", "g")
	onBranch(t, m, "four")
	m = press(m, ".")
	onBranch(t, m, "two")
	// Open sections do not change what j and k stop on.
	m = press(m, "f", "C", "k")
	onBranch(t, m, "three")
	m = press(m, "j")
	onBranch(t, m, "two")
}

func TestDescendIntoItemsAndBack(t *testing.T) {
	m := press(testModel(), "f", "l")
	if r := hovered(t, m); r.kind != rowFile || r.file.Path != "a.go" {
		t.Fatalf("l should go into the open files, got %+v", r)
	}
	m = press(m, "j")
	if r := hovered(t, m); r.kind != rowFile || r.file.Path != "b.go" {
		t.Fatalf("j should step to b.go, got %+v", r)
	}
	// j on the last item stays; k twice climbs back onto the branch.
	m = press(m, "j")
	if r := hovered(t, m); r.file == nil || r.file.Path != "b.go" {
		t.Fatalf("j on the last item should stay, got %+v", r)
	}
	m = press(m, "k", "k")
	onBranch(t, m, "two")
	// h on a branch closes all of its sections.
	m = press(m, "h")
	if m.branchExpanded("two") {
		t.Fatal("h on a branch should close its sections")
	}
	// Checks: l on the workflow opens it, j reaches the check, h closes it.
	m = press(m, "x", "l")
	if r := hovered(t, m); r.kind != rowWorkflow {
		t.Fatalf("expected the workflow, got %+v", r)
	}
	m = press(m, "l", "j")
	if r := hovered(t, m); r.kind != rowCheck || r.check.Name != "test" {
		t.Fatalf("expected the failing check, got %+v", r)
	}
	m = press(m, "h")
	if r := hovered(t, m); r.kind != rowWorkflow || m.expanded[r.key] {
		t.Fatalf("h on a check should close the workflow and park on it, got %+v", r)
	}
	// Closing the section from inside puts the cursor back on the branch.
	m = press(m, "x")
	onBranch(t, m, "two")
	// J and K leave the items for the neighbouring branches.
	m = press(m, "f", "l", "K")
	onBranch(t, m, "three")
}

func TestCursorSurvivesReload(t *testing.T) {
	m := press(testModel(), "f", "l", "j")
	before := hovered(t, m)
	m.buildRows()
	m.restoreCursor("")
	if after := hovered(t, m); after.key != before.key {
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
	m = press(testModel(), "esc", "K", "K", "space", "J", "space")
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

func TestRenderLayout(t *testing.T) {
	m := press(testModel(), "f", "x", "l", "j")
	var lines []string
	for _, l := range strings.Split(m.render(), "\n") {
		lines = append(lines, strings.TrimRight(ansi.Strip(l), " "))
	}
	t.Log("\n" + strings.Join(lines, "\n"))
	want := []string{
		"├ four",
		"│",
		"├ three",
		"│  ▸ 1 file changed",
		"│",
		"├ ○ #2 OPEN  ✗ checks  Second layer",
		"│ two (current)  +12 -0",
		"│ approved · 2 unresolved comments",
		"│  ▾ 2 files changed",
		"│    a.go  +3 -0",
		"│  ▶ b.go  +9 -0",
		"│  ▸ 1 commit",
		"│  ▾ 1 check  1 failed",
		"│    ✗ ci  1 failed",
		"│",
		"──── merged ─────",
		"├ ✓ #1 MERGED",
		"│ one",
		"│",
		"└ main",
	}
	body := lines[headerHeight : headerHeight+len(want)]
	for i := range want {
		if body[i] != want[i] {
			t.Fatalf("line %d = %q, want %q\n%s", i, body[i], want[i], strings.Join(body, "\n"))
		}
	}
}
