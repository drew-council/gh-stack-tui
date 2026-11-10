package reviewtui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/demo"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/ui/uitest"
)

// testNow is the tests' clock.
var testNow = time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)

// newDemo is a model of the demo queue on a width by height screen, before
// anything has loaded.
func newDemo(t *testing.T, width, height int) (Model, *demo.ReviewRemote) {
	t.Helper()
	remote := &demo.ReviewRemote{Now: func() time.Time { return testNow }}
	m := New(Options{
		Dir:       t.TempDir(),
		Repo:      remote.Repository(),
		ReviewCmd: "tuicr pr {url}",
		Tabs:      demo.NewTabs("#337"),
		Connect:   func() (Remote, error) { return remote, nil },
	})
	m.now = func() time.Time { return testNow }
	return update(m, tea.WindowSizeMsg{Width: width, Height: height}), remote
}

// demoModel shows the demo queue on a width by height screen, loaded the way
// the TUI loads a repository's: the queue, the open tabs, then the details.
func demoModel(t *testing.T, width, height int) Model {
	t.Helper()
	m, remote := newDemo(t, width, height)
	m = refresh(t, m, remote, 12*time.Second)
	return update(m, m.loadTabs()())
}

// refresh loads the queue and the details it asks for, as a refresh tick
// would, age before the tests' clock.
func refresh(t *testing.T, m Model, remote Remote, age time.Duration) Model {
	t.Helper()
	queue, err := remote.FetchQueue(context.Background(), github.QueueQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return loadQueue(t, m, remote, queue, age)
}

func loadQueue(t *testing.T, m Model, remote Remote, queue *github.Queue, age time.Duration) Model {
	t.Helper()
	m.loadingRemote = true
	m = update(m, remoteLoadedMsg{client: remote, queue: queue, at: testNow.Add(-age)})
	return loadDetails(t, m, remote)
}

// loadDetails answers the detail fetches in flight until none is left.
func loadDetails(t *testing.T, m Model, remote Remote) Model {
	t.Helper()
	for range 10 {
		if len(m.loadingDetails) == 0 {
			return m
		}
		versions := map[int]string{}
		var numbers []int
		for _, pr := range m.queue.PRs {
			if _, ok := m.loadingDetails[pr.Number]; ok {
				versions[pr.Number] = pr.Version()
				numbers = append(numbers, pr.Number)
			}
		}
		details, err := remote.FetchReviewDetails(context.Background(), numbers)
		if err != nil {
			t.Fatal(err)
		}
		m = update(m, detailsLoadedMsg{versions: versions, details: details, at: testNow})
	}
	t.Fatal("details never finished loading")
	return m
}

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

// press sends keys, answering the detail fetches they start.
func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "ctrl+d":
			msg = tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
		default:
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		m = update(m, msg)
		if m.gh != nil {
			m = loadDetails(t, m, m.gh)
		}
	}
	return m
}

// typed sends each rune of text as a key.
func typed(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		m = press(t, m, string(r))
	}
	return m
}

// onPR moves the cursor to PR number, which must be on screen.
func onPR(t *testing.T, m Model, number int) Model {
	t.Helper()
	for i, r := range m.rows {
		if r.kind == rowPR && m.shown[r.group].Items[r.item].Number == number {
			m.setCursor(i)
			m.ensureVisible()
			if m.gh != nil {
				(&m).requestDetails()
				return loadDetails(t, m, m.gh)
			}
			return m
		}
	}
	t.Fatalf("#%d is not on screen", number)
	return m
}

func assertScreen(t *testing.T, m Model) {
	t.Helper()
	uitest.AssertScreen(t, m.st, m.render())
}

// TestRenderQueue is a full screen, with the cursor on the first PR.
func TestRenderQueue(t *testing.T) {
	assertScreen(t, demoModel(t, 200, 50))
}

// TestRenderReReview hovers a PR in a stack that the viewer reviewed before.
func TestRenderReReview(t *testing.T) {
	assertScreen(t, onPR(t, demoModel(t, 200, 50), 303))
}

// TestRenderFailingChecks names the checks that fail.
func TestRenderFailingChecks(t *testing.T) {
	assertScreen(t, onPR(t, demoModel(t, 200, 50), 304))
}

// TestRenderOffTrunk hovers a PR outside any stack that is based on another
// PR's branch.
func TestRenderOffTrunk(t *testing.T) {
	assertScreen(t, onPR(t, demoModel(t, 200, 50), 324))
}

// TestRenderStack hovers a stack's header: the stack is shaded, and the
// detail pane lists it.
func TestRenderStack(t *testing.T) {
	assertScreen(t, press(t, onPR(t, demoModel(t, 200, 50), 338), "["))
}

func TestRenderFold(t *testing.T) {
	assertScreen(t, press(t, onPR(t, demoModel(t, 200, 50), 337), "j"))
}

func TestRenderUnfolded(t *testing.T) {
	assertScreen(t, press(t, onPR(t, demoModel(t, 200, 50), 337), "j", "l"))
}

// TestRenderContext hovers a PR that is in a stack but not asked of the
// viewer, whose details load when the cursor reaches it.
func TestRenderContext(t *testing.T) {
	assertScreen(t, onPR(t, demoModel(t, 200, 50), 302))
}

func TestRenderMarks(t *testing.T) {
	assertScreen(t, press(t, onPR(t, demoModel(t, 200, 50), 305), "space", "space", "j"))
}

// TestRenderVisual sweeps over a stack: only the PRs for the viewer are taken.
func TestRenderVisual(t *testing.T) {
	assertScreen(t, press(t, onPR(t, demoModel(t, 200, 50), 306), "v", "j", "j", "j", "j"))
}

func TestRenderHidden(t *testing.T) {
	assertScreen(t, press(t, demoModel(t, 200, 50), ".", "G"))
}

func TestRenderFilter(t *testing.T) {
	m := typed(t, press(t, demoModel(t, 200, 50), "/"), "priya")
	assertScreen(t, m)
	if m = press(t, m, "enter"); m.mode != modeNormal || m.filter != "priya" {
		t.Fatalf("enter should keep the filter: mode %v filter %q", m.mode, m.filter)
	}
}

func TestRenderFilterNoMatch(t *testing.T) {
	assertScreen(t, press(t, typed(t, press(t, demoModel(t, 200, 50), "/"), "zzz"), "enter"))
}

// TestRenderNoDetail is too narrow for the detail pane.
func TestRenderNoDetail(t *testing.T) {
	assertScreen(t, demoModel(t, 120, 40))
}

// TestRenderNarrow drops columns to keep room for titles.
func TestRenderNarrow(t *testing.T) {
	assertScreen(t, demoModel(t, 84, 40))
}

// TestRenderScrolled is too short for the queue and for the details.
func TestRenderScrolled(t *testing.T) {
	assertScreen(t, press(t, onPR(t, demoModel(t, 160, 16), 303), "G", "k", "k", "J"))
}

func TestRenderHelp(t *testing.T) {
	assertScreen(t, press(t, demoModel(t, 200, 50), "?"))
}

func TestRenderLoading(t *testing.T) {
	m, _ := newDemo(t, 160, 20)
	assertScreen(t, m)
}

// TestRenderCached shows the cached queue while GitHub is asked.
func TestRenderCached(t *testing.T) {
	m, _ := newDemo(t, 160, 30)
	m = update(m, cachedLoadedMsg{queue: demo.Queue(testNow), at: testNow.Add(-5 * time.Minute)})
	assertScreen(t, m)
}

func TestRenderError(t *testing.T) {
	m, _ := newDemo(t, 160, 20)
	m = update(m, remoteLoadedMsg{err: errors.New("HTTP 502: Bad Gateway\nsecond line")})
	assertScreen(t, m)
}

// TestRenderErrorKeepsQueue keeps showing the last queue when a refresh fails.
func TestRenderErrorKeepsQueue(t *testing.T) {
	m := demoModel(t, 160, 30)
	m.loadingRemote = true
	m = update(m, remoteLoadedMsg{err: errors.New("HTTP 502: Bad Gateway")})
	assertScreen(t, m)
}

func TestRenderEmpty(t *testing.T) {
	m, remote := newDemo(t, 160, 20)
	queue := demo.Queue(testNow)
	var hidden []*github.ReviewPR
	for _, pr := range queue.PRs {
		if pr.IsDraft || pr.AuthorBot {
			hidden = append(hidden, pr)
		}
	}
	queue.PRs, queue.Stacks, queue.Total = hidden, nil, len(hidden)
	assertScreen(t, loadQueue(t, m, remote, queue, time.Second))
}

func TestRenderNothing(t *testing.T) {
	m, remote := newDemo(t, 160, 20)
	assertScreen(
		t,
		loadQueue(t, m, remote, &github.Queue{Viewer: demo.Viewer, Trunk: "main"}, time.Second),
	)
}
