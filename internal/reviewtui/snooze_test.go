package reviewtui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/drew-council/gh-stack-tui/internal/demo"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
)

func snoozes(m Model) *demo.Snoozes { return m.opts.Snoozes.(*demo.Snoozes) }

func waits(m Model, number int) bool {
	_, ok := m.waiting()[number]
	return ok
}

// Enter on the dialog snoozes the hovered PR for the hovered preset, and
// the PR leaves the queue.
func TestSnoozeHidesPR(t *testing.T) {
	m := press(t, demoModel(t, 200, 50), "z")
	if m.mode != modeSnooze {
		t.Fatalf("z should open the dialog, mode %v", m.mode)
	}
	m = press(t, m, "down", "enter")
	pr := review.Target{Number: 325}
	if got := snoozes(m).All(); len(got) != 1 || !got[pr].Equal(testNow.Add(4*time.Hour)) {
		t.Fatalf("saved %v, want #325 for 4 hours", got)
	}
	if m.mode != modeNormal || waits(m, 325) {
		t.Fatalf("#325 should leave the queue: mode %v", m.mode)
	}
	if it, ok := m.hoveredItem(); !ok || !it.ForYou() {
		t.Fatalf("cursor should move on to a PR for the viewer, on %+v", it)
	}
	if !strings.Contains(m.flash, "snoozed #325 until 7:00 pm") {
		t.Fatalf("flash %q", m.flash)
	}
}

// A typed duration wins over the presets; one that does not parse keeps
// the dialog open.
func TestSnoozeTyped(t *testing.T) {
	m := typed(t, press(t, demoModel(t, 200, 50), "z"), "soon")
	m = press(t, m, "enter")
	if m.mode != modeSnooze || m.snooze.err == "" {
		t.Fatalf("a bad duration should keep the dialog with an error, mode %v", m.mode)
	}
	m = press(t, m, "esc")
	m = typed(t, press(t, m, "z"), "3d")
	m = press(t, m, "enter")
	if got := snoozes(m).All()[review.Target{Number: 325}]; !got.Equal(
		testNow.Add(72 * time.Hour),
	) {
		t.Fatalf("snoozed until %v", got)
	}
}

func TestSnoozeEscCancels(t *testing.T) {
	m := press(t, demoModel(t, 200, 50), "z", "esc")
	if m.mode != modeNormal || len(snoozes(m).All()) != 0 || !waits(m, 325) {
		t.Fatalf("esc should snooze nothing, mode %v", m.mode)
	}
}

// On a stack's header, z snoozes the stack, and each of its PRs for the
// viewer with it.
func TestSnoozeStack(t *testing.T) {
	m := press(t, onPR(t, demoModel(t, 200, 50), 305), "[", "z", "enter")
	got := snoozes(m).All()
	if _, ok := got[review.Target{Stack: true, Number: 307}]; !ok || len(got) != 1 {
		t.Fatalf("saved %v, want stack #307", got)
	}
	for _, n := range []int{303, 304, 305} {
		if waits(m, n) {
			t.Errorf("#%d still waits", n)
		}
	}
}

// Marked PRs are snoozed each on its own, and the marks go.
func TestSnoozeSelection(t *testing.T) {
	m := press(t, onPR(t, demoModel(t, 200, 50), 318), "space", "space", "z", "enter")
	got := snoozes(m).All()
	if len(got) != 2 || len(m.marks) != 0 {
		t.Fatalf("saved %v, marks %v", got, m.marks)
	}
	for _, n := range []int{318, 322} {
		if _, ok := got[review.Target{Number: n}]; !ok {
			t.Errorf("#%d not snoozed", n)
		}
	}
}

// Z on a snoozed PR wakes it, and its stack with it.
func TestWake(t *testing.T) {
	m := press(t, onPR(t, demoModel(t, 200, 50), 305), "[", "z", "enter")
	m = press(t, onPR(t, press(t, m, "."), 304), "Z")
	if got := snoozes(m).All(); len(got) != 0 {
		t.Fatalf("left %v", got)
	}
	if !waits(m, 304) || m.flash != "woke stack #307" {
		t.Fatalf("#304 should be back: flash %q", m.flash)
	}
	if m = press(t, m, "Z"); m.flash != "nothing here is snoozed" {
		t.Fatalf("flash %q", m.flash)
	}
}

// A snooze wakes on time, without waiting on GitHub.
func TestSnoozeWakesOnTime(t *testing.T) {
	m := press(t, demoModel(t, 200, 50), "z", "enter")
	if waits(m, 325) {
		t.Fatal("#325 should sleep")
	}
	m.now = func() time.Time { return testNow.Add(time.Hour + time.Second) }
	m = update(m, pollTickMsg{})
	if !waits(m, 325) || !strings.HasPrefix(m.flash, "awake: #325") {
		t.Fatalf("#325 should be back: flash %q", m.flash)
	}
}

// A refresh takes in what another TUI snoozed, but not a read that started
// before a snooze made here.
func TestSnoozesFromRefresh(t *testing.T) {
	m := demoModel(t, 200, 50)
	queue := m.queue
	other := review.Snoozes{{Number: 322}: testNow.Add(time.Hour)}
	m.loadingRemote = true
	m = update(m, remoteLoadedMsg{client: m.gh, queue: queue, at: testNow, snoozes: other})
	if waits(m, 322) {
		t.Fatal("a snooze from another TUI should apply")
	}

	stale := m.snoozeSeq
	m = press(t, m, "z", "enter")
	m.loadingRemote = true
	m = update(m, remoteLoadedMsg{
		client: m.gh, queue: queue, at: testNow, snoozes: other, snoozeSeq: stale,
	})
	if waits(m, 325) {
		t.Fatal("a stale read should not wake #325")
	}
}

// failingSnoozes cannot save a snooze.
type failingSnoozes struct{ SnoozeStore }

func (failingSnoozes) Snooze(github.Repo, []review.Target, time.Time, time.Time) error {
	return errors.New("disk full")
}

// A snooze that cannot be saved is not made.
func TestSnoozeSaveFails(t *testing.T) {
	m := demoModel(t, 200, 50)
	m.opts.Snoozes = failingSnoozes{m.opts.Snoozes}
	m = press(t, m, "z", "enter")
	if !waits(m, 325) || !m.flashErr || !strings.Contains(m.flash, "disk full") {
		t.Fatalf("flash %q", m.flash)
	}
}

func TestSnoozeWithoutStore(t *testing.T) {
	m := demoModel(t, 200, 50)
	m.opts.Snoozes, m.opts.SnoozesErr = nil, errors.New("read-only file system")
	m = press(t, m, "z")
	if m.mode != modeNormal || !strings.Contains(m.flash, "read-only file system") {
		t.Fatalf("mode %v flash %q", m.mode, m.flash)
	}
}

func TestWakeLabel(t *testing.T) {
	for _, tt := range []struct {
		at   time.Time
		want string
	}{
		{testNow.Add(time.Hour), "4:00 pm"},
		{time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC), "tomorrow 9:00 am"},
		{time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC), "Mon 9:00 am"},
		{time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC), "Nov 12 3:00 pm"},
	} {
		if got := wakeLabel(testNow, tt.at); got != tt.want {
			t.Errorf("wakeLabel(%v) = %q, want %q", tt.at, got, tt.want)
		}
	}
}
