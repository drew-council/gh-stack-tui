package review

import (
	"testing"
	"time"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

func TestParseSnooze(t *testing.T) {
	// A Thursday afternoon.
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Time
	}{
		{"90m", now.Add(90 * time.Minute)},
		{"4h", now.Add(4 * time.Hour)},
		{"4", now.Add(4 * time.Hour)},
		{" 1d12h ", now.Add(36 * time.Hour)},
		{"2w", now.Add(14 * 24 * time.Hour)},
		{"tomorrow", time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)},
		{"mon", time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)},
		{"Friday", time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)},
		// A weekday is never today.
		{"thu", time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		got, err := ParseSnooze(tt.in, now)
		if err != nil || !got.Equal(tt.want) {
			t.Errorf("ParseSnooze(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{"", "0m", "soon", "3x", "mo", "h"} {
		if got, err := ParseSnooze(in, now); err == nil {
			t.Errorf("ParseSnooze(%q) = %v, want an error", in, got)
		}
	}
}

func TestSnoozesApply(t *testing.T) {
	now := time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)
	pr := func(n int, draft bool) *github.ReviewPR {
		return &github.ReviewPR{Number: n, State: "OPEN", Requested: true, IsDraft: draft}
	}
	q := &github.Queue{
		Trunk: "main",
		PRs: []*github.ReviewPR{
			pr(1, false),
			pr(2, false),
			pr(3, true),
			pr(4, false),
			pr(5, false),
		},
		Stacks: []github.Stack{{Number: 9, PRs: []int{1, 2, 3}}},
	}
	groups := Build(q, Options{})
	Snoozes{
		{Stack: true, Number: 9}: now.Add(time.Hour),
		{Number: 2}:              now.Add(2 * time.Hour),
		{Number: 4}:              now.Add(-time.Minute), // woke already
		{Number: 5}:              now.Add(3 * time.Hour),
	}.Apply(groups, now)

	byNumber := map[int]Item{}
	for _, g := range groups {
		if g.Stack != nil && !g.Wakes.Equal(now.Add(time.Hour)) {
			t.Errorf("stack wakes %v", g.Wakes)
		}
		for _, it := range g.Items {
			byNumber[it.Number] = it
		}
	}
	for n, want := range map[int]struct {
		hidden Hidden
		wakes  time.Time
	}{
		1: {HideSnoozed, now.Add(time.Hour)},
		// The PR's own snooze outlasts its stack's.
		2: {HideSnoozed, now.Add(2 * time.Hour)},
		// A draft stays a draft.
		3: {HideDraft, time.Time{}},
		4: {HideNone, time.Time{}},
		5: {HideSnoozed, now.Add(3 * time.Hour)},
	} {
		if it := byNumber[n]; it.Hidden != want.hidden || !it.Wakes.Equal(want.wakes) {
			t.Errorf(
				"#%d hidden %v wakes %v, want %v %v",
				n,
				it.Hidden,
				it.Wakes,
				want.hidden,
				want.wakes,
			)
		}
	}
}
