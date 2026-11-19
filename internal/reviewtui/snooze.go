package reviewtui

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/exp/collection/set"

	"github.com/drew-council/gh-stack-tui/internal/review"
	"github.com/drew-council/gh-stack-tui/internal/ui"
)

// snoozeDialog asks how long to snooze its targets.
type snoozeDialog struct {
	targets []review.Target
	// subject names what is snoozed, and about says more of it: a PR's
	// title, or a stack's author and size.
	subject, about string
	// asleep is when the targets wake already, zero when none sleeps.
	asleep time.Time
	input  textinput.Model
	// preset is the hovered preset, which enter takes while nothing is
	// typed.
	preset int
	err    string
}

// snoozePreset is a choice in the dialog: text is what [review.ParseSnooze]
// reads.
type snoozePreset struct{ label, text string }

var snoozePresets = []snoozePreset{
	{"1 hour", "1h"},
	{"4 hours", "4h"},
	{"tomorrow morning", "tomorrow"},
	{"3 days", "3d"},
	{"monday morning", "monday"},
	{"2 weeks", "2w"},
}

// snoozeTargets are what z snoozes: the selected PRs, else the hovered PR,
// else the hovered stack as a whole, so its later PRs sleep too.
func (m *Model) snoozeTargets() (targets []review.Target, subject, about string) {
	sel := m.selection()
	r, ok := m.hovered()
	switch {
	case len(sel) > 0:
		numbers := slices.Sorted(maps.Keys(sel))
		for _, n := range numbers {
			targets = append(targets, review.Target{Number: n})
		}
		if len(numbers) == 1 {
			it, _ := m.find(numbers[0])
			return targets, fmt.Sprintf("#%d", numbers[0]), titleOf(it)
		}
		var labels []string
		for _, n := range numbers {
			labels = append(labels, fmt.Sprintf("#%d", n))
		}
		return targets, ui.Plural(len(numbers), "PR", "PRs"), strings.Join(labels, " ")
	case !ok:
		return nil, "", ""
	case r.kind == rowPR:
		it := m.shown[r.group].Items[r.item]
		return []review.Target{{Number: it.Number}}, fmt.Sprintf("#%d", it.Number), titleOf(it)
	}
	g := m.shown[r.group]
	t := review.Target{Stack: true, Number: g.Stack.Number}
	return []review.Target{t}, t.String(),
		g.Author + " · " + ui.Plural(len(g.Items), "PR", "PRs")
}

func titleOf(it review.Item) string {
	if it.PR == nil {
		return ""
	}
	return it.PR.Title
}

// openSnooze opens the snooze dialog on the targets.
func (m *Model) openSnooze() tea.Cmd {
	if m.opts.Snoozes == nil {
		why := "snoozes cannot be saved"
		if err := m.opts.SnoozesErr; err != nil {
			why += ": " + ui.FirstLine(err.Error())
		}
		return m.setFlash(why, true)
	}
	targets, subject, about := m.snoozeTargets()
	if len(targets) == 0 {
		return m.setFlash("nothing here to snooze", true)
	}
	now := m.now()
	d := &snoozeDialog{targets: targets, subject: subject, about: about}
	for _, t := range targets {
		if w := m.snoozes[t]; w.After(now) && w.After(d.asleep) {
			d.asleep = w
		}
	}
	d.input = textinput.New()
	d.input.Prompt = ""
	d.input.CharLimit = 24
	d.input.Placeholder = "or type 90m, 3d, fri…"
	d.input.SetWidth(24)
	d.input.SetStyles(m.input.Styles())
	m.snooze, m.mode = d, modeSnooze
	return d.input.Focus()
}

// handleSnoozeKey picks a preset or edits the typed duration. Enter snoozes
// for what is typed, else for the hovered preset.
func (m Model) handleSnoozeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.snooze
	move := func(delta int) {
		n := len(snoozePresets)
		d.preset = (d.preset + delta + n) % n
		d.input.SetValue("")
		d.err = ""
	}
	switch msg.String() {
	case "esc":
		m.closeSnooze()
		return m, nil
	case "up", "ctrl+p", "shift+tab":
		move(-1)
		return m, nil
	case "down", "ctrl+n", "tab":
		move(1)
		return m, nil
	case "enter":
		text := strings.TrimSpace(d.input.Value())
		if text == "" {
			text = snoozePresets[d.preset].text
		}
		wakes, err := review.ParseSnooze(text, m.now())
		if err != nil {
			d.err = err.Error()
			return m, nil
		}
		m.closeSnooze()
		cmd := m.applySnooze(d.targets, wakes)
		return m, tea.Batch(cmd, m.requestDetails())
	}
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	d.err = ""
	return m, cmd
}

func (m *Model) closeSnooze() {
	m.snooze.input.Blur()
	m.snooze, m.mode = nil, modeNormal
}

// applySnooze snoozes targets until wakes. It writes at once, rather than
// in the background, so the next read of the snoozes finds it.
func (m *Model) applySnooze(targets []review.Target, wakes time.Time) tea.Cmd {
	now := m.now()
	if err := m.opts.Snoozes.Snooze(m.opts.Repo, targets, wakes, now); err != nil {
		return m.setFlash("snooze: "+ui.FirstLine(err.Error()), true)
	}
	if m.snoozes == nil {
		m.snoozes = review.Snoozes{}
	}
	for _, t := range targets {
		m.snoozes[t] = wakes
	}
	m.snoozeSeq++
	m.clearSelection()
	m.setQueue(m.queue)

	what := targets[0].String()
	if len(targets) > 1 {
		what = ui.Plural(len(targets), "PR", "PRs")
	}
	return m.setFlash(
		fmt.Sprintf("snoozed %s until %s · Z wakes it", what, wakeLabel(now, wakes)),
		false,
	)
}

// wakeTargets are the snoozes that keep the hovered or selected PRs
// asleep, their stacks' included: waking a PR wakes its stack. On a stack,
// they are the stack's and those of each of its PRs.
func (m *Model) wakeTargets() []review.Target {
	now := m.now()
	found := set.Of[review.Target]()
	add := func(t review.Target) {
		if t.Number != 0 && m.snoozes[t].After(now) {
			found.Insert(t)
		}
	}
	addPR := func(g review.Group, it review.Item) {
		add(review.Target{Number: it.Number})
		add(review.Target{Stack: true, Number: stackNumber(g)})
	}
	sel := m.selection()
	r, ok := m.hovered()
	switch {
	case len(sel) > 0:
		for _, g := range m.shown {
			for _, it := range g.Items {
				if _, picked := sel[it.Number]; picked {
					addPR(g, it)
				}
			}
		}
	case !ok:
	case r.kind == rowPR:
		g := m.shown[r.group]
		addPR(g, g.Items[r.item])
	default:
		g := m.shown[r.group]
		for _, it := range g.Items {
			addPR(g, it)
		}
	}
	return slices.SortedFunc(found.All(), func(a, b review.Target) int {
		if a.Stack != b.Stack {
			if a.Stack {
				return 1
			}
			return -1
		}
		return a.Number - b.Number
	})
}

// wake ends the snoozes that keep the hovered or selected PRs asleep.
func (m *Model) wake() tea.Cmd {
	if m.opts.Snoozes == nil {
		return m.setFlash("snoozes cannot be saved", true)
	}
	targets := m.wakeTargets()
	if len(targets) == 0 {
		return m.setNote("nothing here is snoozed")
	}
	if err := m.opts.Snoozes.Wake(m.opts.Repo, targets); err != nil {
		return m.setFlash("wake: "+ui.FirstLine(err.Error()), true)
	}
	for _, t := range targets {
		delete(m.snoozes, t)
	}
	m.snoozeSeq++
	m.setQueue(m.queue)
	var names []string
	for _, t := range targets {
		names = append(names, t.String())
	}
	return m.setFlash("woke "+strings.Join(names, ", "), false)
}

// wakeLabel says when t comes, as seen at now and in now's time zone: a
// time today, a day this week, else a date.
func wakeLabel(now, t time.Time) string {
	t = t.In(now.Location())
	clock := t.Format("3:04 pm")
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch days := int(t.Sub(today).Hours() / 24); {
	case days == 0:
		return clock
	case days == 1:
		return "tomorrow " + clock
	case days < 7:
		return t.Format("Mon ") + clock
	}
	return t.Format("Jan 2 ") + clock
}

// snoozeLines is the dialog, w columns wide, border included.
func (m Model) snoozeLines(w int) []string {
	s := m.st
	d := m.snooze
	now := m.now()
	inner := w - 2
	pad := func(l string) string { return " " + ui.Pad(l, inner-2) + " " }

	body := []string{""}
	head := s.PRNum.Render(d.subject)
	if d.about != "" {
		head += " " + s.Normal.Render(d.about)
	}
	body = append(body, pad(head))
	note := "leaves the queue until it wakes"
	if len(d.targets) > 1 || d.targets[0].Stack {
		note = "leave the queue until they wake"
	}
	body = append(body, pad(s.Dim.Render(note)))
	if !d.asleep.IsZero() {
		body = append(body, pad(s.Warn.Render("snoozed until "+wakeLabel(now, d.asleep))+
			s.Dim.Render(" · a new time replaces it")))
	}
	body = append(body, "")

	typed := strings.TrimSpace(d.input.Value())
	labelW := 0
	for _, p := range snoozePresets {
		labelW = max(labelW, lipgloss.Width(p.label))
	}
	for i, p := range snoozePresets {
		wakes, _ := review.ParseSnooze(p.text, now)
		hovered := i == d.preset && typed == ""
		mark, label := "  ", s.Normal.Render(ui.Pad(p.label, labelW))
		if hovered {
			mark = s.Prompt.Render("▸ ")
		}
		l := pad(mark + label + "  " + s.Dim.Render("until "+wakeLabel(now, wakes)))
		if hovered {
			l = ui.Shade(s.Lift(l), s.CursorBg)
		}
		body = append(body, l)
	}
	body = append(body, "")

	field := s.Prompt.Render("> ") + d.input.View()
	right := ""
	if typed != "" {
		if wakes, err := review.ParseSnooze(typed, now); err == nil {
			right = s.Dim.Render("until " + wakeLabel(now, wakes))
		}
	}
	body = append(body, pad(ui.Fit(field, right, inner-2)))
	if d.err != "" {
		body = append(body, pad(s.Err.Render(d.err)))
	}
	body = append(body, "")
	return ui.Panel(w, s.Prompt, s.Title.Render("Snooze"), "", "", body, len(body))
}

// overlay draws box over lines, its top left corner at column x of line
// y, and fades the lines it covers so the box stands out.
func (m Model) overlay(lines, box []string, x, y int) []string {
	fade := m.st.Rule
	out := make([]string, len(lines))
	for i, l := range lines {
		plain := ansi.Strip(l)
		k := i - y
		if k < 0 || k >= len(box) {
			out[i] = fade.Render(plain)
			continue
		}
		left := ui.Pad(ansi.Truncate(plain, x, ""), x)
		right := ansi.TruncateLeft(plain, x+lipgloss.Width(box[k]), "")
		out[i] = fade.Render(left) + box[k] + fade.Render(right)
	}
	return out
}
