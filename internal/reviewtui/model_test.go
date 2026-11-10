package reviewtui

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/drew-council/gh-stack-tui/internal/demo"
	"github.com/drew-council/gh-stack-tui/internal/desktop"
	"github.com/drew-council/gh-stack-tui/internal/github"
)

// fakeTabs records what is opened and focused.
type fakeTabs struct {
	tabs    []desktop.Tab
	opened  []string // "label focus command"
	focused []string
	err     error
}

func (f *fakeTabs) Noun() string { return "test tab" }

func (f *fakeTabs) Open(_, label, command string, focus bool) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	id := "t" + label
	f.tabs = append(f.tabs, desktop.Tab{ID: id, Label: label})
	state := "behind"
	if focus {
		state = "focused"
	}
	f.opened = append(f.opened, label+" "+state+" "+command)
	return id, nil
}

func (f *fakeTabs) List() ([]desktop.Tab, error) { return f.tabs, nil }

func (f *fakeTabs) Focus(id string) error {
	f.focused = append(f.focused, id)
	return nil
}

// withTabs is the demo queue with tabs that record what happens to them.
func withTabs(t *testing.T, open ...string) (Model, *fakeTabs) {
	t.Helper()
	tabs := &fakeTabs{}
	for _, label := range open {
		tabs.tabs = append(tabs.tabs, desktop.Tab{ID: "t" + label, Label: label})
	}
	m := demoModel(t, 200, 50)
	m.opts.Tabs = tabs
	return update(m, m.loadTabs()()), tabs
}

// startReview presses enter and delivers what the review command reports.
func startReview(t *testing.T, m Model) Model {
	t.Helper()
	cmd := (&m).startReview()
	if cmd == nil {
		t.Fatal("enter started no review")
	}
	return update(m, cmd())
}

func TestStartsOnFirstPRForViewer(t *testing.T) {
	m := demoModel(t, 200, 50)
	it, ok := m.hoveredItem()
	if !ok || it.Number != 325 {
		t.Fatalf("cursor on %+v, want #325", it)
	}
}

func TestReviewOpensFocusedTab(t *testing.T) {
	m, tabs := withTabs(t)
	m = startReview(t, onPR(t, m, 322))
	want := "#322 focused tuicr pr https://github.com/acme/auth-service/pull/322"
	if len(tabs.opened) != 1 || tabs.opened[0] != want {
		t.Fatalf("opened %q, want %q", tabs.opened, want)
	}
	if _, open := m.tabs[322]; !open ||
		!strings.Contains(m.flash, "opened #322 in a new test tab") {
		t.Fatalf("tabs %v flash %q", m.tabs, m.flash)
	}
}

// A PR that has a tab is not opened twice: enter goes to the tab.
func TestReviewGoesToOpenTab(t *testing.T) {
	m, tabs := withTabs(t, "#322", "notes")
	m = startReview(t, onPR(t, m, 322))
	if len(tabs.opened) != 0 || !slices.Equal(tabs.focused, []string{"t#322"}) {
		t.Fatalf("opened %q focused %q", tabs.opened, tabs.focused)
	}
	if !strings.Contains(m.flash, "went to the test tab of #322") {
		t.Fatalf("flash %q", m.flash)
	}
}

// A selection opens behind the queue, bottom of the stack first, skipping the
// PRs that already have a tab.
func TestReviewSelectionOpensBehindInReviewOrder(t *testing.T) {
	m, tabs := withTabs(t, "#304")
	m = press(t, onPR(t, m, 305), "space", "space", "space")
	m = startReview(t, press(t, onPR(t, m, 322), "space"))
	var labels []string
	for _, o := range tabs.opened {
		label, rest, _ := strings.Cut(o, " ")
		if !strings.HasPrefix(rest, "behind ") {
			t.Fatalf("%s should open behind: %s", label, o)
		}
		labels = append(labels, label)
	}
	if !slices.Equal(labels, []string{"#303", "#305", "#322"}) {
		t.Fatalf("opened %q", labels)
	}
	if len(m.marks) != 0 || !strings.Contains(m.flash, "opened 3 PRs") ||
		!strings.Contains(m.flash, "1 already open") {
		t.Fatalf("marks %v flash %q", m.marks, m.flash)
	}
}

// Enter on a stack's header reviews the PRs of it that wait on the viewer.
func TestReviewStackHeader(t *testing.T) {
	m, tabs := withTabs(t)
	m = startReview(t, press(t, onPR(t, m, 338), "["))
	if len(tabs.opened) != 2 || !strings.HasPrefix(tabs.opened[0], "#337 behind") ||
		!strings.HasPrefix(tabs.opened[1], "#338 behind") {
		t.Fatalf("opened %q", tabs.opened)
	}
}

func TestReviewRefusesMergedPR(t *testing.T) {
	m, tabs := withTabs(t)
	m = onPR(t, m, 301)
	if cmd := (&m).startReview(); cmd == nil || len(tabs.opened) != 0 ||
		!strings.Contains(m.flash, "#301 is merged") || !m.flashErr {
		t.Fatalf("flash %q opened %q", m.flash, tabs.opened)
	}
}

func TestReviewReportsTabErrors(t *testing.T) {
	m, tabs := withTabs(t)
	tabs.err = errors.New("no such workspace")
	m = startReview(t, onPR(t, m, 322))
	if !m.flashErr || !strings.Contains(m.flash, "no such workspace") {
		t.Fatalf("flash %q", m.flash)
	}
}

func TestReviewCommandPlaceholders(t *testing.T) {
	m := demoModel(t, 200, 50)
	m.opts.ReviewCmd = "review {pr} {url}"
	m = onPR(t, m, 322)
	it, _ := m.hoveredItem()
	if got := m.command(it.PR); got != "review 322 https://github.com/acme/auth-service/pull/322" {
		t.Fatalf("command %q", got)
	}
}

// Only tabs named after a PR mark it, and a tab the TUI just opened stays
// marked until a poll has had time to list it.
func TestTabsMarkPRs(t *testing.T) {
	m, tabs := withTabs(t, "#337", "337", "#x", "shell")
	if len(m.tabs) != 1 || m.tabs[337] != "t#337" {
		t.Fatalf("tabs %v", m.tabs)
	}
	m = startReview(t, onPR(t, m, 322))
	// A poll that started before the tab opened does not list it.
	m = update(m, tabsLoadedMsg{tabs: map[int]string{337: "t#337"}})
	if _, open := m.tabs[322]; !open {
		t.Fatalf("a tab opened a moment ago should stay marked: %v", m.tabs)
	}
	m.now = func() time.Time { return testNow.Add(2 * openedGrace) }
	tabs.tabs = nil
	m = update(m, m.loadTabs()())
	if len(m.tabs) != 0 {
		t.Fatalf("closed tabs should clear: %v", m.tabs)
	}
}

func TestCursorAndMarksSurviveRefresh(t *testing.T) {
	m := press(t, onPR(t, demoModel(t, 200, 50), 304), "space")
	m = press(t, onPR(t, m, 322), "space")
	m = onPR(t, m, 303)

	// #322 was reviewed and left the queue, and #340 above the cursor is new.
	queue := demo.Queue(testNow)
	queue.PRs = slices.DeleteFunc(queue.PRs, func(pr *github.ReviewPR) bool {
		return pr.Number == 322
	})
	for _, pr := range queue.PRs {
		if pr.Number == 306 {
			pr.IsDraft, pr.Requested, pr.Via = false, true, []string{"go-readability"}
			pr.AskedAt = testNow.Add(-time.Minute)
		}
	}
	m = loadQueue(t, m, m.gh, queue, time.Second)

	if it, _ := m.hoveredItem(); it.Number != 303 {
		t.Fatalf("cursor moved to #%d", it.Number)
	}
	if _, ok := m.marks[304]; !ok || len(m.marks) != 1 {
		t.Fatalf("marks %v, want only #304", m.marks)
	}
	if !strings.Contains(m.flash, "new: #306") {
		t.Fatalf("flash %q should announce #306", m.flash)
	}
}

// The first queue is not news, and neither is a refresh that brings nothing.
func TestNoAnnouncementWithoutArrivals(t *testing.T) {
	m := demoModel(t, 200, 50)
	if m.flash != "" {
		t.Fatalf("first load flashed %q", m.flash)
	}
	if m = refresh(t, m, m.gh, time.Second); m.flash != "" {
		t.Fatalf("an unchanged refresh flashed %q", m.flash)
	}
}

func TestCachedQueueYieldsToGitHub(t *testing.T) {
	m, remote := newDemo(t, 200, 50)
	cached := demo.Queue(testNow)
	cached.PRs[0].Title = "stale title"
	m = update(m, cachedLoadedMsg{queue: cached, at: testNow.Add(-time.Hour)})
	if m.cachedAt.IsZero() || !strings.Contains(ansi.Strip(m.render()), "cached 1h ago") {
		t.Fatal("the cached queue should show, with its age")
	}
	m = refresh(t, m, remote, time.Second)
	if !m.cachedAt.IsZero() || strings.Contains(ansi.Strip(m.render()), "stale title") {
		t.Fatal("GitHub's queue should replace the cached one")
	}
	// A cache that answers after GitHub changes nothing.
	m = update(m, cachedLoadedMsg{queue: cached, at: testNow.Add(-time.Hour)})
	if !m.cachedAt.IsZero() {
		t.Fatal("a late cache should not replace GitHub's queue")
	}
}

// Details are fetched for the PRs that wait on the viewer, and for a PR of
// another kind only when the cursor reaches it.
func TestDetailsLoadOnDemand(t *testing.T) {
	m := demoModel(t, 200, 50)
	for _, n := range []int{303, 304, 305, 318, 322, 324, 325, 337, 338} {
		if _, ok := m.details[n]; !ok {
			t.Fatalf("details of #%d should be loaded", n)
		}
	}
	if _, ok := m.details[302]; ok {
		t.Fatal("details of #302 should wait for the cursor")
	}
	if m = onPR(t, m, 302); m.details[302].Detail == nil {
		t.Fatal("hovering #302 should load its details")
	}
}

func TestFailedDetailsAreNotRetriedUntilRefresh(t *testing.T) {
	m, remote := newDemo(t, 200, 50)
	queue := demo.Queue(testNow)
	m.loadingRemote = true
	m = update(m, remoteLoadedMsg{client: remote, queue: queue, at: testNow})
	versions := map[int]string{}
	for _, pr := range queue.PRs {
		if _, ok := m.loadingDetails[pr.Number]; ok {
			versions[pr.Number] = pr.Version()
		}
	}
	m = update(m, detailsLoadedMsg{versions: versions, err: errors.New("HTTP 502")})
	if _, again := m.loadingDetails[325]; again || len(m.failedDetails) != len(versions) {
		t.Fatalf("failed %v loading %v", m.failedDetails, m.loadingDetails)
	}
	if !strings.Contains(ansi.Strip(m.render()), "could not load: HTTP 502") {
		t.Fatal("the detail pane should say the details failed to load")
	}
}

func TestFoldsOpenAndClose(t *testing.T) {
	m := press(t, onPR(t, demoModel(t, 200, 50), 337), "j")
	if r, _ := m.hovered(); r.kind != rowFold || r.count != 5 {
		t.Fatalf("hovered %+v, want the fold of 5", r)
	}
	m = press(t, m, "enter")
	if it, _ := m.hoveredItem(); it.Number != 336 {
		t.Fatalf("unfolding should land on the first folded PR, got #%d", it.Number)
	}
	m = press(t, m, "j", "h")
	if r, _ := m.hovered(); r.kind != rowFold {
		t.Fatalf("folding should land on the fold that holds the PR, got %+v", r)
	}
}

// A marked PR is not folded away, where enter would open it unseen.
func TestMarkedPRStaysOutOfFold(t *testing.T) {
	m := press(t, onPR(t, demoModel(t, 200, 50), 337), "j", "l", "space", "h")
	found := false
	for _, r := range m.rows {
		if r.kind == rowPR && m.shown[r.group].Items[r.item].Number == 336 {
			found = true
		}
	}
	if _, marked := m.marks[336]; !marked || !found {
		t.Fatalf("#336 should stay on screen while marked: marks %v", m.marks)
	}
}

func TestSpaceOnStackMarksPRsForViewer(t *testing.T) {
	m := press(t, onPR(t, demoModel(t, 200, 50), 305), "[", "space")
	if len(m.marks) != 3 {
		t.Fatalf("marks %v, want #303, #304, #305", m.marks)
	}
	if m = press(t, m, "space"); len(m.marks) != 0 {
		t.Fatalf("space again should unmark them: %v", m.marks)
	}
}

func TestEscClearsSelectionThenFilter(t *testing.T) {
	m := press(t, typed(t, press(t, demoModel(t, 200, 50), "/"), "tobias"), "enter", "space")
	if it, _ := m.hoveredItem(); !it.ForYou() {
		t.Fatalf("a filter should leave the cursor on a PR for the viewer, got %+v", it)
	}
	if len(m.marks) != 1 || m.filter != "tobias" {
		t.Fatalf("marks %v filter %q", m.marks, m.filter)
	}
	if m = press(t, m, "esc"); len(m.marks) != 0 || m.filter != "tobias" {
		t.Fatalf("first esc: marks %v filter %q", m.marks, m.filter)
	}
	if m = press(t, m, "esc"); m.filter != "" {
		t.Fatalf("second esc: filter %q", m.filter)
	}
}

func TestGroupJumps(t *testing.T) {
	m := demoModel(t, 200, 50)
	var at []string
	for range 4 {
		m = press(t, m, "]")
		r, _ := m.hovered()
		at = append(at, r.key)
	}
	want := []string{"stack/307", "pr/0/318", "pr/0/322", "pr/0/324"}
	if !slices.Equal(at, want) {
		t.Fatalf("] visited %q, want %q", at, want)
	}
	if m = press(t, m, "G", "["); m.cursorKey != "stack/342" {
		t.Fatalf("[ from inside a stack should go to its header, got %q", m.cursorKey)
	}
}

func TestWindowTitleCountsQueue(t *testing.T) {
	if got := demoModel(t, 200, 50).View().WindowTitle; got != "review queue (9)" {
		t.Fatalf("title %q", got)
	}
}

func TestQuit(t *testing.T) {
	m := demoModel(t, 200, 50)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q should quit")
	}
}

// A description is rendered once per width, not on every frame.
func TestDescriptionIsRenderedOnce(t *testing.T) {
	m := onPR(t, demoModel(t, 200, 50), 324)
	m.render()
	first := len(m.rendered)
	if first == 0 {
		t.Fatal("the hovered PR's description should be rendered")
	}
	m.render()
	if m = update(m, tea.WindowSizeMsg{Width: 200, Height: 40}); len(m.rendered) != first {
		t.Fatalf("rendered %d descriptions, want %d", len(m.rendered), first)
	}
	if !strings.Contains(ansi.Strip(m.render()), "• Evict the user's entry") {
		t.Fatal("the description's list should be rendered as a list")
	}
}

// prOf returns PR number of the queue.
func prOf(t *testing.T, m Model, number int) *github.ReviewPR {
	t.Helper()
	it, ok := m.find(number)
	if !ok || it.PR == nil {
		t.Fatalf("#%d is not in the queue", number)
	}
	return it.PR
}

func TestSizeLeavesOutGeneratedFiles(t *testing.T) {
	m := demoModel(t, 200, 50)
	if got, want := m.size(
		prOf(t, m, 304),
	), (size{add: 322, del: 1, files: 4, generated: 1}); got != want {
		t.Fatalf("#304 is %+v, want %+v", got, want)
	}
	// #325 changes more files than were fetched, so nothing can be left out.
	if got, want := m.size(prOf(t, m, 325)), (size{add: 1840, del: 220, files: 24}); got != want {
		t.Fatalf("#325 is %+v, want %+v", got, want)
	}
	// Until a PR's files are loaded, GitHub's totals stand in.
	delete(m.details, 304)
	if got, want := m.size(prOf(t, m, 304)), (size{add: 536, del: 31, files: 5}); got != want {
		t.Fatalf("#304 without details is %+v, want %+v", got, want)
	}
}

// Without a checkout to ask, every file counts.
func TestSizeCountsEveryFileWithoutCheckout(t *testing.T) {
	m, remote := newDemo(t, 200, 50)
	m.opts.Local = nil
	m = refresh(t, m, remote, time.Second)
	if got, want := m.size(prOf(t, m, 304)), (size{add: 536, del: 31, files: 5}); got != want {
		t.Fatalf("#304 is %+v, want %+v", got, want)
	}
}
