package tui

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// wizardKind says what each wizard step does to its PR.
type wizardKind int

const (
	// wizardReady asks for reviewers when a draft needs them, then marks the
	// draft ready for review.
	wizardReady wizardKind = iota
	// wizardReviewers asks for reviewers and leaves the PR as it is.
	wizardReviewers
	// wizardDraft confirms, then converts a ready PR back to a draft.
	wizardDraft
)

// wizardStep is one PR the wizard acts on.
type wizardStep struct {
	pr     *github.PR
	branch string
	kind   wizardKind
}

// wizardState walks the selected PRs one at a time, so each gets its own
// prompt with its own branch, number, and title for context.
type wizardState struct {
	steps []wizardStep
	i     int
}

// pickerState is the reviewer picker. It opens when PRs need reviewers, and
// asks for the chosen people before the wizard moves on.
type pickerState struct {
	// step is the PR this prompt is for; ready also marks it ready for review
	// after the reviewers are requested.
	step  wizardStep
	ready bool
	need  []int
	// subject and context describe the PR in the header.
	subject, context string
	// suggested are GitHub's suggestions for the PRs in need, best first.
	suggested []string

	all    []candidate // ranked: suggested, then recent reviewers, then A-Z
	shown  []candidate // all, filtered by query and ordered by match
	query  string
	cursor int
	scroll int
	picked []string // logins in the order they were picked
}

type candidate struct {
	github.Reviewer
	suggested bool
	// pos are the rune positions the query matched in haystack.
	pos []int
}

// haystack is what the query matches against: the login and the name.
func (c candidate) haystack() string { return c.Login + " " + c.Name }

// pickerHeaderLines is the number of lines above the candidate list.
const pickerHeaderLines = 6

// needsReviewers reports whether draft pr should prompt for reviewers when it
// is marked ready.
func needsReviewers(pr *github.PR) bool {
	return pr.IsDraft && pr.NeedsReviewers()
}

// openPickerStep asks who should review the current wizard step's PR.
func (m *Model) openPickerStep(step wizardStep, ready bool) tea.Cmd {
	pr := step.pr
	p := &pickerState{step: step, ready: ready, need: []int{pr.Number}}
	p.suggested = m.suggestionsFor(p.need)
	p.subject = fmt.Sprintf("#%d", pr.Number)
	if pr.Title != "" {
		p.subject += " " + pr.Title
	}
	p.context = step.branch
	if w := m.wizard; w != nil && len(w.steps) > 1 {
		p.context += fmt.Sprintf(" · step %d/%d", w.i+1, len(w.steps))
	}
	switch {
	case len(pr.Reviewers) > 0:
		p.context += " · requested: " + strings.Join(pr.Reviewers, ", ")
	case len(pr.RequestedTeams) > 0:
		p.context += " · only teams are requested: " + strings.Join(pr.RequestedTeams, ", ")
	default:
		p.context += " · nobody is requested yet"
	}
	if len(pr.Reviewers) > 0 && len(pr.RequestedTeams) > 0 {
		p.context += " · teams: " + strings.Join(pr.RequestedTeams, ", ")
	}
	p.setCandidates(m.reviewers)

	m.picker = p
	m.mode = modeReviewers
	m.input.SetValue("")
	m.input.Placeholder = "type to filter"
	// Suggestions still missing (a failed load, or a PR that just appeared)
	// rerank the picker when they arrive.
	return tea.Batch(m.input.Focus(), m.requestSuggested())
}

// combine runs cmds together, ignoring the ones that are nil.
func combine(cmds ...tea.Cmd) tea.Cmd {
	var out []tea.Cmd
	for _, cmd := range cmds {
		if cmd != nil {
			out = append(out, cmd)
		}
	}
	switch len(out) {
	case 0:
		return nil
	case 1:
		return out[0]
	}
	return tea.Batch(out...)
}

// startWizard walks steps one at a time. The selection stays until a step
// actually does something, so cancelling does not lose it.
func (m *Model) startWizard(steps []wizardStep) tea.Cmd {
	m.wizard = &wizardState{steps: steps}
	return m.nextWizardStep()
}

// advanceWizard moves to the next step, or finishes when there is none.
func (m *Model) advanceWizard() tea.Cmd {
	if m.wizard == nil {
		return nil
	}
	m.wizard.i++
	return m.nextWizardStep()
}

// nextWizardStep opens the prompt for the current step, and does the steps
// that need no prompt. Steps that need no prompt still advance one at a time.
func (m *Model) nextWizardStep() tea.Cmd {
	w := m.wizard
	if w == nil {
		return nil
	}
	if w.i >= len(w.steps) {
		m.wizard = nil
		return nil
	}
	step := w.steps[w.i]
	n := []string{fmt.Sprint(step.pr.Number)}
	switch step.kind {
	case wizardDraft:
		text := fmt.Sprintf("convert #%d (%s)", step.pr.Number, step.branch)
		if step.pr.Title != "" {
			text += " " + step.pr.Title
		}
		text += " to draft?"
		if len(w.steps) > 1 {
			text += fmt.Sprintf("  step %d/%d", w.i+1, len(w.steps))
		}
		m.mode = modeConfirm
		m.confirm = &confirmState{
			text: text,
			keys: []string{"y", "yes", "n", "no"},
			actions: map[string]func(*Model) tea.Cmd{
				"y": func(m *Model) tea.Cmd {
					title, name, args := draftToggle(n, true)
					m.clearSelection()
					return combine(m.runQuietOp(title, name, args...), m.advanceWizard())
				},
			},
			cancel: func(m *Model) tea.Cmd { return m.advanceWizard() },
		}
		return nil
	case wizardReady:
		if needsReviewers(step.pr) && !(m.reviewersLoaded && len(m.reviewers) == 0) {
			return m.openPickerStep(step, true)
		}
		title, name, args := draftToggle(n, false)
		m.clearSelection()
		return combine(m.runQuietOp(title, name, args...), m.advanceWizard())
	default:
		return m.openPickerStep(step, false)
	}
}

func (m *Model) closePicker() {
	m.mode = modeNormal
	m.picker = nil
	m.input.Blur()
	m.input.SetValue("")
}

// setCandidates ranks reviewers the way GitHub's reviewer menu does: its
// suggestions for the PR first, then the people the viewer's recent PRs went
// to, most often first and most lately among equals, then everyone else by
// login. The hovered candidate stays hovered.
func (p *pickerState) setCandidates(reviewers []github.Reviewer) {
	hovered, wasHovered := p.hovered()
	p.all = p.all[:0]
	for _, r := range reviewers {
		p.all = append(
			p.all,
			candidate{Reviewer: r, suggested: slices.Contains(p.suggested, r.Login)},
		)
	}
	for _, login := range p.suggested {
		if !slices.ContainsFunc(p.all, func(c candidate) bool { return c.Login == login }) {
			p.all = append(
				p.all,
				candidate{Reviewer: github.Reviewer{Login: login}, suggested: true},
			)
		}
	}
	rank := func(c candidate) int {
		if i := slices.Index(p.suggested, c.Login); i >= 0 {
			return i
		}
		return len(p.suggested)
	}
	sort.SliceStable(p.all, func(i, j int) bool {
		a, b := p.all[i], p.all[j]
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		if a.RecentPRs != b.RecentPRs {
			return a.RecentPRs > b.RecentPRs
		}
		if a.Recent != b.Recent {
			return a.Recent > b.Recent
		}
		return strings.ToLower(a.Login) < strings.ToLower(b.Login)
	})
	p.filter(p.query)
	if wasHovered {
		if i := slices.IndexFunc(
			p.shown,
			func(c candidate) bool { return c.Login == hovered.Login },
		); i >= 0 {
			p.cursor = i
		}
	}
}

// filter shows the candidates matching query, best match first and in rank
// order among equal matches.
func (p *pickerState) filter(query string) {
	if query != p.query {
		p.cursor, p.scroll = 0, 0
	}
	p.query = query
	p.shown = p.shown[:0]
	type match struct {
		c     candidate
		score int
	}
	var matches []match
	for _, c := range p.all {
		score, pos, ok := fuzzyMatch(query, c.haystack())
		if ok {
			c.pos = pos
			matches = append(matches, match{c, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	for _, mt := range matches {
		p.shown = append(p.shown, mt.c)
	}
	p.cursor = max(0, min(p.cursor, len(p.shown)-1))
}

func (p *pickerState) hovered() (candidate, bool) {
	if p.cursor < 0 || p.cursor >= len(p.shown) {
		return candidate{}, false
	}
	return p.shown[p.cursor], true
}

func (p *pickerState) move(delta int) {
	p.cursor = max(0, min(p.cursor+delta, len(p.shown)-1))
}

// toggle picks the hovered candidate, or unpicks them.
func (p *pickerState) toggle() {
	c, ok := p.hovered()
	if !ok {
		return
	}
	if i := slices.Index(p.picked, c.Login); i >= 0 {
		p.picked = slices.Delete(p.picked, i, i+1)
	} else {
		p.picked = append(p.picked, c.Login)
	}
}

// filterPicker refilters the candidates when the query changed.
func (m *Model) filterPicker() {
	if m.picker != nil && m.input.Value() != m.picker.query {
		m.picker.filter(m.input.Value())
	}
}

func (m *Model) pickerListHeight() int {
	h := m.bodyHeight() - pickerHeaderLines
	if !m.reviewersLoaded {
		h -= 2 // room for the loading or error status
	}
	return max(1, h)
}

// ensurePickerVisible scrolls the candidate list to the cursor.
func (m *Model) ensurePickerVisible() {
	p, h := m.picker, m.pickerListHeight()
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	}
	if p.cursor >= p.scroll+h {
		p.scroll = p.cursor - h + 1
	}
	p.scroll = max(0, min(p.scroll, len(p.shown)-h))
}

// handlePickerKey follows fzf: type to filter, tab picks and moves on, and
// enter takes the picked people, or the hovered one when nobody is picked.
func (m Model) handlePickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closePicker()
		if m.wizard != nil {
			last := m.wizard.i+1 >= len(m.wizard.steps)
			cmd := m.advanceWizard()
			if last {
				return m, combine(cmd, m.setNote("cancelled"))
			}
			return m, cmd
		}
		return m, m.setNote("cancelled")
	case "up", "ctrl+p", "ctrl+k":
		p.move(-1)
	case "down", "ctrl+n", "ctrl+j":
		p.move(1)
	case "pgup":
		p.move(-m.pickerListHeight())
	case "pgdown":
		p.move(m.pickerListHeight())
	case "tab":
		p.toggle()
		p.move(1)
	case "shift+tab":
		p.toggle()
		p.move(-1)
	case "enter":
		logins := p.picked
		if c, ok := p.hovered(); ok && len(logins) == 0 {
			logins = []string{c.Login}
		}
		if len(logins) == 0 {
			return m, nil
		}
		return m, m.submitPicker(logins)
	case "ctrl+s":
		return m, m.submitPicker(nil)
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.filterPicker()
		m.ensurePickerVisible()
		return m, cmd
	}
	m.ensurePickerVisible()
	return m, nil
}

// reviewersRequestedMsg reports that reviewers were requested for a PR, and
// whether that PR should also be marked ready.
type reviewersRequestedMsg struct {
	num   int
	ready bool
	who   string
	err   error
}

// submitPicker asks logins to review the current step's PR, and moves on to
// the next step without waiting, so keys typed for the next prompt never land
// in normal mode. A draft is marked ready only once its reviewers are
// requested, so if that fails it stays a draft. With no logins the step is
// skipped, and a draft is only marked ready.
func (m *Model) submitPicker(logins []string) tea.Cmd {
	p := m.picker
	m.closePicker()
	num, ready := p.step.pr.Number, p.ready
	if len(logins) == 0 {
		var cmd tea.Cmd
		if ready {
			title, name, args := draftToggle([]string{fmt.Sprint(num)}, false)
			m.clearSelection()
			cmd = m.runQuietOp(title, name, args...)
		}
		return combine(cmd, m.advanceWizard())
	}
	if m.gh == nil {
		m.wizard = nil
		return m.setFlash("not connected to GitHub yet", true)
	}
	m.clearSelection()
	who := strings.Join(logins, ", ")
	client := m.gh
	return combine(
		m.setFlash(fmt.Sprintf("requesting review on #%d from %s…", num, who), false),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := client.RequestReviewers(ctx, num, logins); err != nil {
				return reviewersRequestedMsg{
					num: num, ready: ready,
					err: fmt.Errorf("requesting review on #%d: %w", num, err),
				}
			}
			return reviewersRequestedMsg{num: num, ready: ready, who: who}
		},
		m.advanceWizard(),
	)
}

// fuzzyMatch reports whether the runes of query appear in s in order,
// ignoring case and spaces in the query, and scores the match: runes at the
// start of a word and runs of consecutive runes score higher, and gaps cost a
// little. It returns the matched rune positions in s.
func fuzzyMatch(query, s string) (int, []int, bool) {
	q := []rune(strings.ToLower(strings.ReplaceAll(query, " ", "")))
	if len(q) == 0 {
		return 0, nil, true
	}
	h := []rune(strings.ToLower(s))
	best, bestPos := 0, []int(nil)
	// Try each place the first rune matches, greedily matching the rest,
	// and keep the best scoring one.
	for start := range h {
		if h[start] != q[0] {
			continue
		}
		pos := []int{start}
		for i := start + 1; i < len(h) && len(pos) < len(q); i++ {
			if h[i] == q[len(pos)] {
				pos = append(pos, i)
			}
		}
		if len(pos) < len(q) {
			// Later starts have even less left to match in.
			break
		}
		if score := matchScore(h, pos); bestPos == nil || score > best {
			best, bestPos = score, pos
		}
	}
	return best, bestPos, bestPos != nil
}

func matchScore(h []rune, pos []int) int {
	score := 0
	for k, p := range pos {
		score++
		if p == 0 || !unicode.IsLetter(h[p-1]) && !unicode.IsDigit(h[p-1]) {
			score += 8
		}
		if k > 0 {
			if gap := p - pos[k-1] - 1; gap == 0 {
				score += 6
			} else {
				score -= min(gap, 4)
			}
		}
	}
	return score
}

// --- rendering ---

func (m Model) pickerLines(h int) []string {
	s := m.st
	p := m.picker
	w := m.innerWidth()
	count := s.dim.Render(fmt.Sprintf("%d/%d", len(p.shown), len(p.all)))
	selected := s.dim.Render("nobody picked yet")
	if len(p.picked) > 0 {
		var pills []string
		for _, login := range p.picked {
			pills = append(pills, s.selected.Render(login))
		}
		selected = s.dim.Render("picked ") + strings.Join(pills, " ")
	}
	lines := []string{
		" " + s.title.Render(p.subject),
		" " + s.dim.Render(p.context),
		"",
		fit(" "+s.prompt.Render("> ")+m.input.View(), count, w),
		" " + selected,
		"",
	}

	// GitHub's suggestions for the PR arrive with it, so they show while
	// everyone else is still loading.
	var status []string
	switch {
	case m.reviewersErr != nil && !m.reviewersLoaded:
		status = []string{
			" " + s.err.Render("loading reviewers failed: "+firstLine(m.reviewersErr.Error())),
			" " + s.dim.Render(
				"retrying on the next refresh; ctrl+s marks ready without reviewers",
			),
		}
	case !m.reviewersLoaded:
		status = []string{" " + m.spinner.View() + s.dim.Render(" loading reviewers…")}
	case len(p.all) == 0:
		status = []string{" " + s.dim.Render("nobody can be requested in this repository")}
	case len(p.shown) == 0:
		status = []string{" " + s.dim.Render("no match")}
	}

	loginW := 0
	for _, c := range p.shown {
		loginW = max(loginW, lipgloss.Width(c.Login))
	}
	end := min(len(p.shown), p.scroll+max(1, h-len(lines)-len(status)))
	for i := p.scroll; i < end; i++ {
		lines = append(lines, m.pickerItem(p.shown[i], i == p.cursor, loginW))
	}
	return append(lines, status...)
}

func (m Model) pickerItem(c candidate, cursor bool, loginW int) string {
	s := m.st
	lead := " "
	// Picked people follow the stack's selection colors; see rowBg.
	picked := slices.Contains(m.picker.picked, c.Login)
	mark := s.dim.Render("·")
	if picked {
		mark = s.selection.Render("◆")
	}
	loginStyle := s.normal
	if cursor {
		loginStyle = loginStyle.Bold(true)
	}
	login := highlight(c.Login, c.pos, 0, loginStyle, s.match)
	line := lead + mark + " " + login + strings.Repeat(" ", loginW-lipgloss.Width(c.Login))
	if c.Name != "" {
		line += "  " + highlight(c.Name, c.pos, len([]rune(c.Login))+1, s.dim, s.match)
	}

	var tags []string
	if c.suggested {
		tags = append(tags, s.prompt.Render("suggested"))
	}
	if c.RecentPRs > 0 {
		tags = append(tags, s.dim.Render(plural(c.RecentPRs, "recent PR", "recent PRs")))
	}
	line = fit(line, strings.Join(tags, s.dim.Render(" · ")), m.innerWidth())
	switch {
	case cursor && picked:
		return shade(s.lift(line), s.selCursorBg)
	case cursor:
		return shade(s.lift(line), s.cursorBg)
	case picked:
		return shade(line, s.selBg)
	}
	return line
}

// highlight renders text with the runes at pos, shifted by offset, in hi and
// the rest in base.
func highlight(text string, pos []int, offset int, base, hi lipgloss.Style) string {
	var b, run strings.Builder
	runHi := false
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if runHi {
			b.WriteString(hi.Render(run.String()))
		} else {
			b.WriteString(base.Render(run.String()))
		}
		run.Reset()
	}
	for i, r := range []rune(text) {
		isHi := slices.Contains(pos, i+offset)
		if isHi != runHi {
			flush()
			runHi = isHi
		}
		run.WriteRune(r)
	}
	flush()
	return b.String()
}
