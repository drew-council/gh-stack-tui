package reviewtui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/drew-council/gh-stack-tui/internal/desktop"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
	"github.com/drew-council/gh-stack-tui/internal/ui"
)

// detailMinScreen is the narrowest screen that has room for the detail pane.
const detailMinScreen = 132

// Columns of a queue line, counted from the panel's inner left edge: a mark
// for an open tab, the stack's rail, the PR's bullet, then the cells.
const (
	colTab     = 0
	colRail    = 2
	colBullet  = 4
	colContent = 6
)

// Widths of the fixed cells.
const (
	needW    = 9  // re-review
	reviewsW = 5  // ✓1 ✗1
	ciW      = 2  // the header's "CI"
	diffW    = 11 // +1234 -1234
	ageW     = 5  // the header's "ASKED"
	gap      = 2
)

// staleAfter is how long a request waits before its age is called out.
const staleAfter = 3 * 24 * time.Hour

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "review queue"
	if n := len(m.waiting()); m.queue != nil {
		v.WindowTitle = fmt.Sprintf("review queue (%d)", n)
	}
	return v
}

func (m Model) render() string {
	if m.width == 0 {
		return ""
	}
	lines := m.noteLines()
	h := m.panelHeight()
	switch m.mode {
	case modeHelp:
		body := m.helpLines()
		top := m.helpTop(len(body), h)
		lines = append(lines, ui.Panel(
			m.width, m.st.Rule, m.st.Title.Render("Keys"), "",
			m.st.ScrollHint(top, len(body), h), body[top:min(len(body), top+h)], h,
		)...)
	default:
		listW, detailW := m.layout()
		edge := m.st.Rule
		if m.visual {
			edge = m.st.Selection
		}
		title, info := m.queueTitle()
		left := ui.Panel(listW, edge, title, info, m.queueFoot(), m.listLines(listW-2), h)
		if detailW == 0 {
			lines = append(lines, left...)
			break
		}
		body := m.detailLines(detailW - 2)
		top := max(0, min(m.detailScroll, len(body)-h))
		dTitle, dInfo := m.detailTitle()
		right := ui.Panel(
			detailW, m.st.Rule, dTitle, dInfo,
			m.st.ScrollHint(top, len(body), h), body[top:min(len(body), top+h)], h,
		)
		for i := range left {
			lines = append(lines, left[i]+right[i])
		}
	}
	lines = append(lines, " "+m.footerText())
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(lines, "\n")
}

// layout is the width of the queue's panel and of the detail pane, which is
// zero on a screen too narrow for both.
func (m Model) layout() (listW, detailW int) {
	if m.width < detailMinScreen {
		return m.width, 0
	}
	detailW = max(56, min(84, m.width*34/100))
	return m.width - detailW, detailW
}

// panelHeight is the number of lines inside the panels' borders.
func (m Model) panelHeight() int {
	return max(2, m.height-len(m.noteLines())-2-1)
}

// listHeight is the number of queue rows on screen, under the column header.
func (m Model) listHeight() int { return m.panelHeight() - 1 }

// listTop is the screen row of the first queue row.
func (m Model) listTop() int { return len(m.noteLines()) + 2 }

func (m *Model) clampScroll() {
	m.scroll = max(0, min(m.scroll, len(m.rows)-m.listHeight()))
}

// ensureVisible scrolls so the cursor's row is on screen, along with the
// header of its stack where that fits.
func (m *Model) ensureVisible() {
	if len(m.rows) == 0 || m.height == 0 {
		m.scroll = 0
		return
	}
	h := m.listHeight()
	// The lines under the last row it can reach, a stack's trunk, come along.
	end := m.cursor
	for end+1 < len(m.rows) && !m.navigable(end+1) {
		end++
	}
	if end == len(m.rows)-1 && end >= m.scroll+h {
		m.scroll = end - h + 1
	}
	if m.cursor >= m.scroll+h {
		m.scroll = m.cursor - h + 1
	}
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if start := m.groupStart(m.rows[m.cursor].group); start >= 0 && start < m.scroll &&
		m.cursor-start < h {
		m.scroll = start
	}
	m.clampScroll()
}

// clampDetail keeps the detail pane's scroll inside its text.
func (m *Model) clampDetail() {
	_, detailW := m.layout()
	if detailW == 0 || m.height == 0 {
		m.detailScroll = 0
		return
	}
	lines := len(m.detailLines(detailW - 2))
	m.detailScroll = max(0, min(m.detailScroll, lines-m.panelHeight()))
}

// noteLines are the lines above the panels: errors from GitHub. They are
// empty most of the time.
func (m Model) noteLines() []string {
	var lines []string
	if m.remoteErr != nil {
		lines = append(lines, " "+m.st.Err.Render("GitHub: "+ui.FirstLine(m.remoteErr.Error())))
	}
	return lines
}

// --- queue chrome ---

// tally counts the requested PRs: how many need a first review, how many
// another, and how many are left out, by reason.
func (m Model) tally() (first, again int, hidden map[review.Hidden]int) {
	hidden = map[review.Hidden]int{}
	seen := map[int]struct{}{}
	for _, g := range m.groups {
		for _, it := range g.Items {
			if _, dup := seen[it.Number]; dup || it.PR == nil || !it.PR.Requested {
				continue
			}
			seen[it.Number] = struct{}{}
			switch {
			case it.Hidden != review.HideNone:
				hidden[it.Hidden]++
			case it.Need == review.NeedReReview:
				again++
			default:
				first++
			}
		}
	}
	return first, again, hidden
}

// queueTitle is the queue panel's title, and what waits and the refresh
// state on its right.
func (m Model) queueTitle() (title, info string) {
	s := m.st
	title = s.Title.Render("Review queue")
	if r := m.opts.Repo; r.Name != "" {
		title += s.Dim.Render(" · " + r.Owner + "/" + r.Name)
	}
	var parts []string
	if m.queue != nil {
		first, again, _ := m.tally()
		if first > 0 {
			parts = append(parts, s.Key.Render(fmt.Sprintf("%d to review", first)))
		}
		if again > 0 {
			parts = append(parts, s.Warn.Render(fmt.Sprintf("%d to re-review", again)))
		}
	}
	now := m.now()
	switch {
	case m.loadingRemote:
		refresh := m.spinner.View() + s.Dim.Render(" refreshing")
		if !m.cachedAt.IsZero() {
			refresh += s.Dim.Render(" · cached " + ui.ShortAgo(now, m.cachedAt))
		}
		parts = append(parts, refresh)
	case !m.cachedAt.IsZero():
		parts = append(parts, s.Dim.Render("cached "+ui.ShortAgo(now, m.cachedAt)))
	case !m.lastRemote.IsZero():
		parts = append(parts, s.Dim.Render("↻ "+ui.ShortAgo(now, m.lastRemote)))
	}
	return title, strings.Join(parts, s.Dim.Render(" · "))
}

// queueFoot is the queue panel's bottom border: the filter, what is left
// out, and how far the list is scrolled.
func (m Model) queueFoot() string {
	s := m.st
	var parts []string
	if m.filter != "" && m.mode != modeFilter {
		parts = append(parts, s.Prompt.Render("/"+m.filter))
	}
	if m.queue != nil {
		first, again, hidden := m.tally()
		loaded := first + again
		for _, n := range hidden {
			loaded += n
		}
		if m.queue.Total > loaded {
			parts = append(parts,
				s.Warn.Render(fmt.Sprintf("%d more not loaded", m.queue.Total-loaded)))
		}
		var kinds []string
		total := 0
		for _, h := range []review.Hidden{
			review.HideDraft, review.HideBot, review.HideWaiting, review.HideApproved,
		} {
			if n := hidden[h]; n > 0 {
				total += n
				kind := fmt.Sprintf("%d %s", n, h)
				if n > 1 && (h == review.HideDraft || h == review.HideBot) {
					kind += "s"
				}
				kinds = append(kinds, kind)
			}
		}
		switch {
		case total == 0:
		case m.showHidden:
			parts = append(parts, s.Dim.Render("showing "+strings.Join(kinds, " · ")))
		default:
			parts = append(parts, s.Dim.Render("hidden: "+strings.Join(kinds, " · ")))
		}
	}
	if hint := s.ScrollHint(m.scroll, len(m.rows), m.listHeight()); hint != "" {
		parts = append(parts, hint)
	}
	return strings.Join(parts, s.Dim.Render(" · "))
}

// --- queue rows ---

// columns are the widths of the cells whose width depends on the queue. A
// width of zero leaves the column out.
type columns struct {
	num, title, author, via int
	reviews                 bool
}

// columns sizes the cells for a queue w columns wide. The title takes what
// the other cells leave, and the least useful cells go first when that is
// too little.
func (m Model) columns(w int) columns {
	c := columns{num: 2, author: len("AUTHOR"), via: len("VIA"), reviews: true}
	for _, g := range m.shown {
		for _, it := range g.Items {
			c.num = max(c.num, len(fmt.Sprintf("#%d", it.Number)))
			if it.PR == nil {
				continue
			}
			if g.Stack == nil || it.PR.Author != g.Author {
				c.author = max(c.author, lipgloss.Width(it.PR.Author))
			}
			if m.standsOut(it) {
				c.via = max(c.via, lipgloss.Width(m.via(it.PR)))
			}
		}
	}
	c.author, c.via = min(c.author, 14), min(c.via, 14)
	fixed := colContent + c.num + gap + needW + gap + ciW + 1 + diffW + gap + ageW + 1
	c.title = w - fixed - gap - (c.author + gap) - (c.via + gap) - (reviewsW + gap)
	if c.title < 30 {
		c.title += c.via + gap
		c.via = 0
	}
	if c.title < 30 {
		c.title += reviewsW + gap
		c.reviews = false
	}
	if c.title < 24 {
		c.title += c.author + gap
		c.author = 0
	}
	c.title = max(8, c.title)
	return c
}

// cells lays a line's cells out in the columns of c.
func (c columns) cells(num, title, author, need, via, reviews, ci, diff, age string) string {
	var b strings.Builder
	sp := strings.Repeat(" ", gap)
	b.WriteString(ui.Pad(num, c.num) + sp)
	switch {
	case c.author == 0:
		b.WriteString(ui.Pad(title, c.title) + sp)
	case author == "":
		// A title runs on under the author's column when it is empty, as it
		// is down a stack.
		b.WriteString(ui.Pad(title, c.title+gap+c.author) + sp)
	default:
		b.WriteString(ui.Pad(title, c.title) + sp + ui.Pad(author, c.author) + sp)
	}
	b.WriteString(ui.Pad(need, needW) + sp)
	if c.via > 0 {
		b.WriteString(ui.Pad(via, c.via) + sp)
	}
	if c.reviews {
		b.WriteString(ui.Pad(reviews, reviewsW) + sp)
	}
	b.WriteString(ui.Pad(ci, ciW) + " " + ui.PadLeft(diff, diffW) + sp + ui.PadLeft(age, ageW))
	return b.String()
}

// listLines is the queue panel's body, w columns wide: the column header,
// then the rows on screen.
func (m Model) listLines(w int) []string {
	s := m.st
	switch {
	case m.queue == nil && m.remoteErr != nil:
		return []string{"", " " + s.Dim.Render("The review queue could not be loaded.")}
	case m.queue == nil:
		return []string{"", " " + m.spinner.View() + s.Dim.Render(" loading the review queue…")}
	case len(m.rows) == 0:
		return m.emptyLines()
	}
	c := m.columns(w)
	head := strings.Repeat(" ", colContent) +
		c.cells("PR", "TITLE", "AUTHOR", "YOU", "VIA", "REVS", "CI", "DIFF", "ASKED")
	lines := []string{s.Subtle.Render(head)}

	hov, hovering := m.hovered()
	for i := m.scroll; i < min(len(m.rows), m.scroll+m.listHeight()); i++ {
		r := m.rows[i]
		active := hovering && r.group == hov.group && m.shown[r.group].Stack != nil
		l := ui.Pad(m.renderRow(i, c, active, w), w)
		bg := m.rowBg(i, active && hov.kind == rowStack)
		if bg == s.CursorBg || bg == s.SelCursorBg {
			l = s.Lift(l)
		}
		lines = append(lines, ui.Shade(l, bg))
	}
	return lines
}

// emptyLines says why the queue shows nothing.
func (m Model) emptyLines() []string {
	s := m.st
	_, _, hidden := m.tally()
	n := 0
	for _, c := range hidden {
		n += c
	}
	switch {
	case m.filter != "":
		return []string{"", " " + s.Dim.Render("Nothing matches ") + s.Normal.Render(m.filter) +
			s.Dim.Render(". Press esc to clear the filter.")}
	case n > 0 && !m.showHidden:
		return []string{
			"",
			" " + s.OK.Render("✓") + s.Normal.Render(" Nothing waits on your review."),
			" " + s.Dim.Render("  "+ui.Plural(n, "request is", "requests are")+
				" left out as drafts, bots, or already reviewed. Press . to show them."),
		}
	}
	return []string{"", " " + s.OK.Render("✓") + s.Normal.Render(" Nothing waits on your review.")}
}

// A row's colors follow from what the PR needs from the viewer, whether it
// is selected, and where the cursor is:
//
//   - Background: the cursor's row is gray, and brighter mauve when the PR is
//     selected. Other selected PRs are mauve. When the cursor is on a stack's
//     header, the rest of the stack is a faint gray.
//   - Bullet: ● blue waits on a first review, ● peach on another one. ○ is a
//     PR that is in a stack only for context, and ✓ a merged one. Selected
//     PRs become mauve diamonds, ◆ when they wait on the viewer and ◇
//     otherwise.
//   - Ink: PRs that wait on the viewer are in primary ink, the rest muted.
//   - Tab mark: a teal bar in the first column while a tab reviews the PR.
//   - Rail: mauve next to a selected PR, blue in the hovered stack, else gray.
//
// Mauve means selected and nothing else, and peach that something is due.

// rowBg is the background of row i. block is set when the cursor is on the
// header of the row's stack.
func (m Model) rowBg(i int, block bool) color.Color {
	s := m.st
	if !m.navigable(i) && m.rows[i].kind != rowTrunk {
		return nil
	}
	cursor, selected := i == m.cursor, m.isSelected(i)
	switch {
	case cursor && selected:
		return s.SelCursorBg
	case cursor:
		return s.CursorBg
	case selected:
		return s.SelBg
	case block:
		return s.BlockBg
	}
	return nil
}

// renderRow renders row i for a queue w columns wide. active is set for the
// rows of the stack the cursor is in.
func (m Model) renderRow(i int, c columns, active bool, w int) string {
	s := m.st
	r := m.rows[i]
	if r.kind == rowSpacer {
		return ""
	}
	g := m.shown[r.group]
	rail := s.Conn
	switch {
	case m.isSelected(i):
		rail = s.Selection
	case active:
		rail = s.ConnActive
	}
	lead := strings.Repeat(" ", colRail)

	switch r.kind {
	case rowStack:
		left := lead + rail.Render("╭") + " " + s.Branch.Render(g.Author) +
			s.Dim.Render(fmt.Sprintf(" · stack #%d · %s on %s",
				g.Stack.Number, ui.Plural(len(g.Items), "PR", "PRs"), g.Stack.Base))
		right := ""
		if n := g.ForYou(); n > 0 {
			right = s.Key.Render(fmt.Sprintf("%d for you", n))
			if n > 1 {
				right += s.Dim.Render(fmt.Sprintf(" · start at #%d", g.Start()))
			}
		}
		return ui.Fit(left, right, w)

	case rowTrunk:
		return lead + rail.Render("╰") + " " + s.Trunk.Render(g.Stack.Base)

	case rowFold:
		return lead + rail.Render(ui.GlyphConn) + " " + s.Subtle.Render(ui.GlyphDashed) + " " +
			s.Dim.Render(ui.Plural(r.count, "PR", "PRs")+"  ") +
			m.foldTally(g.Items[r.item:r.item+r.count])
	}

	it := g.Items[r.item]
	tab := " "
	if _, open := m.tabs[it.Number]; open {
		tab = s.Bot.Render("▎")
	}
	line := tab + " "
	if g.Stack != nil {
		line += rail.Render(ui.GlyphConn)
	} else {
		line += " "
	}
	line += " " + m.bullet(it, m.isSelected(i)) + " "

	num := s.PRNum.Render(fmt.Sprintf("#%d", it.Number))
	pr := it.PR
	if pr == nil {
		return line + c.cells(num, s.Subtle.Render("not available"), "", "", "", "", "", "", "")
	}
	ink := s.Dim
	if it.ForYou() {
		ink = s.Normal
	}
	author := ""
	if g.Stack == nil || pr.Author != g.Author {
		author = s.Dim.Render(pr.Author)
	}
	via, age := "", ""
	if m.standsOut(it) && pr.Requested {
		via = m.viaCell(pr)
		asked := pr.AskedAt
		if asked.IsZero() {
			asked = pr.UpdatedAt
		}
		age = s.Dim.Render(ui.Age(m.now(), asked))
		if it.ForYou() && m.now().Sub(asked) >= staleAfter {
			age = s.Warn.Render(ui.Age(m.now(), asked))
		}
	}
	ci := ""
	if checks := m.checkState(pr); pr.State == "OPEN" && checks != github.CheckNone {
		ci = s.CheckGlyph(checks)
	}
	title := ink.Render(pr.Title)
	if g.Stack == nil && m.offTrunk(it) {
		// Outside a stack, nothing else says the PR builds on another one.
		onto := ui.TruncateLeft(pr.BaseRef, 20)
		if it.Base != 0 {
			onto = fmt.Sprintf("#%d", it.Base)
		}
		title = s.Subtle.Render("on "+onto+" ") + title
	}
	return line + c.cells(
		num, title, author, m.needLabel(it), via,
		m.reviewsCell(pr), ci, m.diffCell(pr), age,
	)
}

// offTrunk reports whether it is based on a branch other than trunk.
func (m Model) offTrunk(it review.Item) bool {
	return it.PR != nil && m.queue != nil && m.queue.Trunk != "" &&
		it.PR.BaseRef != "" && it.PR.BaseRef != m.queue.Trunk
}

// checkState is the state of CI on pr. The queue only knows whether CI
// passes, so the PR's details refine it once they are loaded: cancelled and
// skipped checks are not failures.
func (m Model) checkState(pr *github.ReviewPR) github.CheckState {
	if d, ok := m.details[pr.Number]; ok && d.Version == pr.Version() &&
		len(d.Detail.Checks.Workflows) > 0 {
		return d.Detail.Checks.State
	}
	return pr.Checks
}

// bullet is the mark of a PR in the queue.
func (m Model) bullet(it review.Item, selected bool) string {
	s := m.st
	switch {
	case selected && it.ForYou():
		return s.Selection.Render(ui.GlyphSelectedCurrent)
	case selected:
		return s.Selection.Render(ui.GlyphSelected)
	case it.PR != nil && it.PR.Merged:
		return s.BulletMerged.Render(ui.GlyphMerged)
	case it.ForYou() && it.Need == review.NeedReReview:
		return s.Warn.Render(ui.GlyphCurrent)
	case it.ForYou():
		return s.BulletCurrent.Render(ui.GlyphCurrent)
	}
	return s.Subtle.Render(ui.GlyphBranch)
}

// needLabel is what the PR needs from the viewer, or failing that the state
// that keeps it off the viewer's plate.
func (m Model) needLabel(it review.Item) string {
	s := m.st
	pr := it.PR
	switch {
	case pr.Merged:
		return s.Dim.Render("merged")
	case pr.State == "CLOSED":
		return s.Dim.Render("closed")
	case it.Hidden == review.HideApproved || it.Need == review.NeedApproved:
		return s.OK.Render("approved")
	case it.Hidden != review.HideNone:
		return s.Dim.Render(it.Hidden.String())
	case pr.Requested && it.Need == review.NeedReReview:
		return s.Warn.Render("re-review")
	case pr.Requested:
		return s.Key.Render("review")
	case pr.IsDraft:
		return s.Dim.Render("draft")
	case it.Need == review.NeedWaiting:
		return s.Dim.Render("waiting")
	}
	return s.Subtle.Render("·")
}

// via is who of the viewer was asked: "you" by name, else the teams.
func (m Model) via(pr *github.ReviewPR) string {
	var names []string
	if pr.Direct {
		names = append(names, "you")
	}
	for _, t := range pr.Via {
		short := m.teams[t]
		if short == "" {
			short = t
		}
		names = append(names, short)
	}
	return strings.Join(names, " ")
}

func (m Model) viaCell(pr *github.ReviewPR) string {
	text := m.via(pr)
	if rest, direct := strings.CutPrefix(text, "you"); direct {
		return m.st.BranchCurrent.Render("you") + m.st.Dim.Render(rest)
	}
	return m.st.Dim.Render(text)
}

// reviewsCell counts the other reviewers' approvals and change requests.
func (m Model) reviewsCell(pr *github.ReviewPR) string {
	s := m.st
	approved, changes := 0, 0
	for _, r := range pr.Reviews {
		switch r.State {
		case "APPROVED":
			approved++
		case "CHANGES_REQUESTED":
			changes++
		}
	}
	var parts []string
	if approved > 0 {
		parts = append(parts, s.OK.Render(fmt.Sprintf("✓%d", approved)))
	}
	if changes > 0 {
		parts = append(parts, s.Err.Render(fmt.Sprintf("✗%d", changes)))
	}
	return strings.Join(parts, " ")
}

// size is how much of a PR there is to read: the lines and files it changes,
// with the generated files counted apart.
type size struct {
	add, del int
	// files are the files a person wrote, and generated the rest.
	files, generated int
}

// size measures pr without its generated files, as ghst measures a branch.
// GitHub's totals, which count everything, stand in until the PR's files are
// loaded, and for a PR with more files than are fetched.
func (m Model) size(pr *github.ReviewPR) size {
	d, ok := m.detailOf(pr)
	if !ok || len(d.Files) < pr.ChangedFiles {
		return size{add: pr.Additions, del: pr.Deletions, files: pr.ChangedFiles}
	}
	var s size
	for _, f := range d.Files {
		if f.Generated {
			s.generated++
			continue
		}
		s.add += f.Additions
		s.del += f.Deletions
		s.files++
	}
	return s
}

// diffCell is the lines added and deleted, each in its own half of the cell
// so they line up down the queue.
func (m Model) diffCell(pr *github.ReviewPR) string {
	s := m.st
	add, del := "", ""
	size := m.size(pr)
	if size.add > 0 {
		add = s.Add.Render(fmt.Sprintf("+%d", size.add))
	}
	if size.del > 0 {
		del = s.Del.Render(fmt.Sprintf("-%d", size.del))
	}
	half := diffW / 2
	return ui.PadLeft(add, half) + " " + ui.PadLeft(del, half)
}

// state is a word for where a PR stands, for PRs that do not wait on the
// viewer.
func state(it review.Item) string {
	pr := it.PR
	switch {
	case pr == nil:
		return "not available"
	case pr.Merged:
		return "merged"
	case pr.State == "CLOSED":
		return "closed"
	case pr.IsDraft:
		return "draft"
	case pr.ReviewDecision == "APPROVED" || it.Need == review.NeedApproved:
		return "approved"
	}
	return "in review"
}

// foldTally counts folded PRs by where they stand.
func (m Model) foldTally(items []review.Item) string {
	s := m.st
	counts := map[string]int{}
	for _, it := range items {
		counts[state(it)]++
	}
	var parts []string
	for _, word := range []string{"in review", "approved", "draft", "merged", "closed", "not available"} {
		if n := counts[word]; n > 0 {
			style := s.Dim
			if word == "merged" || word == "approved" {
				style = s.OK
			}
			parts = append(parts, style.Render(fmt.Sprintf("%d %s", n, word)))
		}
	}
	return strings.Join(parts, s.Dim.Render(" · "))
}

// --- footer ---

func (m Model) footerText() string {
	s := m.st
	w := m.width - 2
	switch m.mode {
	case modeFilter:
		return s.Prompt.Render("/") + m.input.View()
	case modeHelp:
		return s.Hints(
			w,
			"j/k",
			"scroll",
			"ctrl+d/u",
			"half page",
			"g/G",
			"top/end",
			"esc",
			"close",
		)
	}
	if m.pendingKey == "g" {
		return s.Prompt.Render("g") + s.Dim.Render("…")
	}
	if m.flash != "" {
		switch {
		case m.flashErr:
			return s.FlashErr.Render(m.flash)
		case m.flashNote:
			return s.Note.Render(m.flash)
		}
		return s.FlashOK.Render(m.flash)
	}
	if sel := m.selection(); m.visual || len(sel) > 0 {
		mode := "select"
		if m.visual {
			mode = "visual"
		}
		lead := s.Selected.Render(mode) + s.Dim.Render(fmt.Sprintf(" %d  ", len(sel)))
		return lead + s.Hints(w-lipgloss.Width(lead),
			"enter", "review in "+m.where(true), "o", "browser", "y", "copy URLs",
			"space", "mark", "v", "visual", "esc", "clear",
		)
	}
	r, ok := m.hovered()
	if !ok {
		return s.Hints(
			w,
			".",
			"hidden",
			"/",
			"filter",
			"ctrl+r",
			"refresh",
			"q",
			"quit",
			"?",
			"help",
		)
	}
	switch r.kind {
	case rowStack:
		return s.Hints(w,
			"j/k", "move", "enter", "review the stack's PRs for you", "space", "mark them",
			"l/h", "unfold/fold", "[ ]", "groups", ".", "hidden", "/", "filter", "?", "help",
		)
	case rowFold:
		return s.Hints(w, "j/k", "move", "l", "unfold", "[ ]", "groups", "?", "help")
	}
	review := "review in " + m.where(false)
	if it, ok := m.hoveredItem(); ok {
		if _, open := m.tabs[it.Number]; open {
			review = "go to its tab"
		}
	}
	return s.Hints(w,
		"j/k", "move", "enter", review, "space", "mark", "o", "browser", "y", "copy URL",
		"J/K", "scroll details", "[ ]", "groups", ".", "hidden", "/", "filter", "?", "help",
	)
}

// where names the place reviews open, for one PR or several.
func (m Model) where(several bool) string {
	switch {
	case m.place == desktop.InBrowser:
		return "browser"
	case m.place == desktop.Inline:
		return "this terminal"
	case several:
		return m.opts.Reviews.Tabs.Noun() + "s"
	}
	return "a " + m.opts.Reviews.Tabs.Noun()
}

// --- help ---

var helpSections = []ui.HelpSection{
	{Title: "navigate", Keys: [][2]string{
		{"j/k ↓/↑", "next/previous row"},
		{"[ ]", "previous/next stack or author"},
		{"gg/G", "top/bottom"},
		{"ctrl+d/u", "half page"},
		{"J/K", "scroll the details"},
		{"l →", "unfold the stack"},
		{"h ←", "fold the stack"},
	}},
	{Title: "review", Keys: [][2]string{
		{"enter R", "open review tabs behind the queue"},
		{"", "on a PR with a tab: go to the tab"},
		{"space", "mark a PR, or a stack's PRs for you"},
		{"v", "visual range"},
		{"o", "open in the browser"},
		{"y", "copy URL(s)"},
		{"Y", "copy branch name(s)"},
	}},
	{Title: "queue", Keys: [][2]string{
		{".", "show drafts, bots, and reviewed PRs"},
		{"/", "filter by author, title, team, …"},
		{"ctrl+r", "refresh now"},
		{"esc", "clear selection, then filter"},
		{"?", "help"},
		{"q", "quit"},
	}},
	{Title: "symbols", Keys: [][2]string{
		{"●", "waits on your review (blue)"},
		{"●", "waits on another review (peach)"},
		{"○ ✓", "in a stack for context / merged"},
		{"◆ ◇", "selected: for you / context"},
		{"▎", "a tab is reviewing it"},
		{"┊", "folded PRs that are not for you"},
		{"on #12", "based on that PR's branch, not trunk"},
		{"✓1 ✗1", "others approved / requested changes"},
		{"✓ ✗ ●", "checks passed / failed / running"},
	}},
	{Title: "you", Keys: [][2]string{
		{"review", "asked, not reviewed yet"},
		{"re-review", "asked again, or changed since"},
		{"waiting", "you reviewed, author's turn"},
		{"approved", "you approved"},
		{"via you", "asked by name, else the team"},
	}},
}

// helpTop is the first help line shown, kept so the last line can reach the
// bottom of the panel.
func (m Model) helpTop(total, h int) int {
	return max(0, min(m.helpScroll, total-h))
}

func (m Model) helpLines() []string {
	return m.st.HelpLines(helpSections, m.width-4)
}
