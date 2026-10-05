package tui

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// pickerState is the reviewer picker. It opens when drafts are marked ready
// that nobody has been asked to review in person, only teams such as
// CODEOWNERS teams, and asks the chosen people before marking them ready.
type pickerState struct {
	// nums are the drafts to mark ready, and need the ones among them that
	// get the reviewers.
	nums []string
	need map[string]bool
	// subject and context describe the PRs in the header.
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

// openPicker asks who should review the drafts in need before the drafts are
// marked ready.
func (m *Model) openPicker(drafts []*github.PR, need []*github.PR) tea.Cmd {
	p := &pickerState{need: map[string]bool{}}
	for _, pr := range drafts {
		p.nums = append(p.nums, fmt.Sprint(pr.Number))
	}
	var teams, numbers []string
	for _, pr := range need {
		p.need[fmt.Sprint(pr.Number)] = true
		numbers = append(numbers, fmt.Sprintf("#%d", pr.Number))
		for _, t := range pr.RequestedTeams {
			if !slices.Contains(teams, t) {
				teams = append(teams, t)
			}
		}
		for _, login := range pr.Suggested {
			if !slices.Contains(p.suggested, login) {
				p.suggested = append(p.suggested, login)
			}
		}
	}
	p.subject = strings.Join(numbers, ", ")
	if len(need) == 1 && need[0].Title != "" {
		p.subject += " " + need[0].Title
	}
	p.context = "nobody is requested yet"
	if len(teams) > 0 {
		p.context = "only teams are requested: " + strings.Join(teams, ", ")
	}
	if extra := len(drafts) - len(need); extra > 0 {
		p.context += fmt.Sprintf(" · %s with reviewers also marked ready",
			plural(extra, "other draft", "other drafts"))
	}
	p.setCandidates(m.reviewers)

	m.picker = p
	m.mode = modeReviewers
	m.input.SetValue("")
	m.input.Placeholder = "type to filter"
	return m.input.Focus()
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
// login.
func (p *pickerState) setCandidates(reviewers []github.Reviewer) {
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
		return m, m.setFlash("cancelled", false)
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

// submitPicker asks logins to review the drafts in need and marks every draft
// ready. With no logins the drafts are only marked ready.
func (m *Model) submitPicker(logins []string) tea.Cmd {
	p := m.picker
	m.closePicker()
	m.marks = map[string]bool{}
	m.visual = false
	title, name, args := readyForReview(m.ghRepo, p.nums, p.need, logins)
	return m.runQuietOp(title, name, args...)
}

// readyForReview returns the title and command that mark the drafts nums
// ready for review, first asking logins to review the ones in need. Each PR
// gets its reviewers before it leaves draft, so a failure leaves it a draft.
func readyForReview(
	repo github.Repo,
	nums []string,
	need map[string]bool,
	logins []string,
) (string, string, []string) {
	title, name, args := draftToggle(nums, false)
	if len(logins) == 0 {
		return title, name, args
	}
	title += " → " + strings.Join(logins, ", ")
	api := "gh api"
	if repo.Host != "" && repo.Host != "github.com" {
		api += " --hostname " + shellQuote(repo.Host)
	}
	var steps []string
	for _, n := range nums {
		if need[n] {
			req := fmt.Sprintf(
				"%s --silent -X POST %s",
				api,
				shellQuote(
					fmt.Sprintf(
						"repos/%s/%s/pulls/%s/requested_reviewers",
						repo.Owner,
						repo.Name,
						n,
					),
				),
			)
			for _, l := range logins {
				req += " -f " + shellQuote("reviewers[]="+l)
			}
			steps = append(steps, req, "echo "+shellQuote(
				fmt.Sprintf("requested review from %s on #%s", strings.Join(logins, ", "), n),
			))
		}
		steps = append(steps, "gh pr ready "+n)
	}
	return title, "sh", []string{"-c", strings.Join(steps, " && ")}
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
		s.title.Render("Request reviewers") + s.dim.Render("  "+p.subject),
		s.dim.Render(p.context),
		"",
		spread(s.prompt.Render("> ")+m.input.View(), count, m.width),
		selected,
		"",
	}

	// GitHub's suggestions for the PR arrive with it, so they show while
	// everyone else is still loading.
	var status []string
	switch {
	case m.reviewersErr != nil && !m.reviewersLoaded:
		status = []string{
			s.err.Render("loading reviewers failed: " + firstLine(m.reviewersErr.Error())),
			s.dim.Render("retrying on the next refresh; ctrl+s marks ready without reviewers"),
		}
	case !m.reviewersLoaded:
		status = []string{m.spinner.View() + s.dim.Render(" loading reviewers…")}
	case len(p.all) == 0:
		status = []string{s.dim.Render("nobody can be requested in this repository")}
	case len(p.shown) == 0:
		status = []string{s.dim.Render("no match")}
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
	lead := "  "
	if cursor {
		lead = s.prompt.Render(glyphFocus) + " "
	}
	mark := s.dim.Render("·")
	if slices.Contains(m.picker.picked, c.Login) {
		mark = s.ok.Render("✓")
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
	return spread(line, strings.Join(tags, s.dim.Render(" · ")), m.width)
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
