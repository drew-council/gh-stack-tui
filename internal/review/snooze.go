package review

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Target is what a snooze puts off: a whole stack, or one pull request.
type Target struct {
	Stack  bool
	Number int
}

// String names t as the queue does: "stack #12" or "#325".
func (t Target) String() string {
	if t.Stack {
		return fmt.Sprintf("stack #%d", t.Number)
	}
	return fmt.Sprintf("#%d", t.Number)
}

// Snoozes are when each snoozed target wakes.
// A snooze that has woken MAY stay in the map; it counts for nothing.
type Snoozes map[Target]time.Time

// Wakes is when the PR number of stack wakes at now,
// by its own snooze or its stack's, whichever lasts longer.
// It is zero when neither sleeps.
// stack is 0 outside any stack.
func (s Snoozes) Wakes(stack, number int, now time.Time) time.Time {
	var at time.Time
	for _, t := range []Target{{Number: number}, {Stack: true, Number: stack}} {
		if t.Number == 0 {
			continue
		}
		if w := s[t]; w.After(now) && w.After(at) {
			at = w
		}
	}
	return at
}

// Next is the soonest wake after now, or zero when nothing sleeps.
func (s Snoozes) Next(now time.Time) time.Time {
	var next time.Time
	for _, w := range s {
		if w.After(now) && (next.IsZero() || w.Before(next)) {
			next = w
		}
	}
	return next
}

// Apply hides the PRs of groups that sleep at now.
// Only PRs that would wait on the viewer are hidden as snoozed:
// a draft or an approved PR stays hidden for that reason.
func (s Snoozes) Apply(groups []Group, now time.Time) {
	for gi := range groups {
		g := &groups[gi]
		stack := 0
		if g.Stack != nil {
			stack = g.Stack.Number
			if w := s[Target{Stack: true, Number: stack}]; w.After(now) {
				g.Wakes = w
			}
		}
		for i := range g.Items {
			it := &g.Items[i]
			if !it.ForYou() {
				continue
			}
			if w := s.Wakes(stack, it.Number, now); !w.IsZero() {
				it.Hidden, it.Wakes = HideSnoozed, w
			}
		}
	}
}

// morning is when "tomorrow" and weekday snoozes wake.
const morning = 9

// ErrSnooze explains what [ParseSnooze] takes.
var ErrSnooze = errors.New("try 90m, 4h, 3d, 1w, tomorrow, or a weekday")

// ParseSnooze reads how long to snooze, as typed, into when the snooze
// wakes. It takes a duration of minutes, hours, days, and weeks, such as
// 90m, 4h, 1d12h, or 2w; "tomorrow"; or a weekday, such as fri. Tomorrow
// and weekdays wake at 9 in the morning, local time; a weekday is never
// today.
func ParseSnooze(text string, now time.Time) (time.Time, error) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return time.Time{}, ErrSnooze
	}
	if text == "tomorrow" || text == "tmrw" {
		return dayAt(now, 1), nil
	}
	for day := time.Sunday; day <= time.Saturday; day++ {
		name := strings.ToLower(day.String())
		if len(text) >= 3 && strings.HasPrefix(name, text) {
			ahead := (int(day) - int(now.Weekday()) + 7) % 7
			if ahead == 0 {
				ahead = 7
			}
			return dayAt(now, ahead), nil
		}
	}
	d, err := parseDuration(text)
	if err != nil || d <= 0 {
		return time.Time{}, ErrSnooze
	}
	return now.Add(d), nil
}

// dayAt is the morning days after now's day.
func dayAt(now time.Time, days int) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+days, morning, 0, 0, 0, now.Location())
}

// parseDuration reads a number and unit, repeated, such as 1d12h. Units
// are m, h, d, and w. A number without a unit is hours.
func parseDuration(text string) (time.Duration, error) {
	units := map[byte]time.Duration{
		'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour,
	}
	var total time.Duration
	for text != "" {
		i := 0
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			i++
		}
		n, err := strconv.Atoi(text[:i])
		if err != nil {
			return 0, ErrSnooze
		}
		text = strings.TrimSpace(text[i:])
		unit := time.Hour
		if text != "" {
			var ok bool
			if unit, ok = units[text[0]]; !ok {
				return 0, ErrSnooze
			}
			text = strings.TrimSpace(text[1:])
		}
		total += time.Duration(n) * unit
	}
	return total, nil
}
