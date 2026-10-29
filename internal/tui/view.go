package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// outputPanelLines is the number of output lines shown under the stack.
const outputPanelLines = 8

// Columns of a stack line, counted from the panel's inner left edge. The
// gutter holds the cursor bar, the tree runs down colTree, and content starts
// at colContent. Items nest indentStep columns per level. Values on the right
// end one column short of the border.
const (
	colTree    = 2
	colContent = 4
	indentStep = 2
)

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "gh stack"
	if m.snap != nil && m.snap.Number != 0 {
		v.WindowTitle = fmt.Sprintf("gh stack #%d", m.snap.Number)
	}
	return v
}

func (m Model) render() string {
	if m.width == 0 {
		return ""
	}
	lines := m.headerLines()
	h := m.bodyHeight()

	var title, info, foot string
	var body []string
	switch m.mode {
	case modeHelp:
		title, body = m.st.title.Render("Keys"), m.helpLines()
	case modeReviewers:
		title, body = m.st.title.Render("Request reviewers"), m.pickerLines(h)
	default:
		title, info = m.stackTitle()
		body = m.bodyLines()
		total := len(body)
		if m.scroll < len(body) {
			body = body[m.scroll:min(len(body), m.scroll+h)]
		} else {
			body = nil
		}
		foot = m.scrollHint(total, h)
	}
	if len(body) > h {
		body = body[:h]
	}
	lines = append(lines, m.panel(title, info, foot, body, h)...)
	if m.showOutput {
		lines = append(lines, m.outputLines()...)
	}
	lines = append(lines, m.footer())

	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(lines, "\n")
}

// innerWidth is the width inside a panel's border.
func (m Model) innerWidth() int { return max(10, m.width-2) }

func (m Model) bodyHeight() int {
	h := m.height - len(m.headerLines()) - 2 - 1
	if m.showOutput {
		h -= outputPanelLines + 2
	}
	return max(1, h)
}

// bodyTop is the screen row of the first body line, under the panel's top
// border.
func (m Model) bodyTop() int { return len(m.headerLines()) + 1 }

func (m Model) totalLines() int {
	if len(m.rows) == 0 {
		return 0
	}
	last := m.rows[len(m.rows)-1]
	return last.y + last.height
}

func (m *Model) clampScroll() {
	m.scroll = max(0, min(m.scroll, m.totalLines()-m.bodyHeight()))
}

// ensureVisible scrolls so the hovered branch, or the hovered item, is on
// screen. A branch is shown whole where it fits, with its header line taking
// priority when it does not.
func (m *Model) ensureVisible() {
	if len(m.rows) == 0 || m.height == 0 {
		m.scroll = 0
		return
	}
	h := m.bodyHeight()
	r, ok := m.hovered()
	if !ok {
		m.clampScroll()
		return
	}
	start, end := r.y, r.y+r.height
	if r.kind == rowBranch {
		start, end = m.nodeSpan(r.branch)
	} else {
		start, end = max(0, start-1), end+1
	}
	if end > m.scroll+h {
		m.scroll = end - h
	}
	if r.y < m.scroll {
		m.scroll = r.y
	}
	if start < m.scroll && r.y-start < h {
		m.scroll = start
	}
	m.clampScroll()
}

// --- frame ---

// panel frames body, padded to h lines, in a rounded border as wide as the
// screen. title and info sit on the top border, foot on the bottom one.
func (m Model) panel(title, info, foot string, body []string, h int) []string {
	s := m.st
	w := m.innerWidth()
	edge := s.rule.Render(glyphConn)
	lines := []string{m.border(title, info, "╭", "╮")}
	for i := range h {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		lines = append(lines, edge+pad(l, w)+edge)
	}
	return append(lines, m.border("", foot, "╰", "╯"))
}

// border is a horizontal border line with left and right labels set into it.
func (m Model) border(left, right, lc, rc string) string {
	s := m.st
	if left != "" {
		left = " " + left + " "
	}
	if right != "" {
		right = " " + right + " "
	}
	fill := m.width - 4 - lipgloss.Width(left) - lipgloss.Width(right)
	if fill < 1 && right != "" {
		fill += lipgloss.Width(right)
		right = ""
	}
	if fill < 1 {
		left = ansi.Truncate(left, max(0, m.width-5), "…")
		fill = m.width - 4 - lipgloss.Width(left)
	}
	return s.rule.Render(lc+"─") + left + s.rule.Render(strings.Repeat("─", max(0, fill))) +
		right + s.rule.Render("─"+rc)
}

// pad cuts or pads l to exactly w columns.
func pad(l string, w int) string {
	l = ansi.Truncate(l, w, "…")
	return l + strings.Repeat(" ", max(0, w-lipgloss.Width(l)))
}

// fit lays out left and right on one line w columns wide. right ends one
// column short of the edge, and left is cut to keep two columns between
// them. On a line too narrow for both, right is dropped.
func fit(left, right string, w int) string {
	rw := lipgloss.Width(right)
	room := w - 1
	if rw > 0 {
		room -= rw + 2
		if room < 12 {
			right, rw, room = "", 0, w-1
		}
	}
	left = ansi.Truncate(left, room, "…")
	if rw == 0 {
		return pad(left, w)
	}
	return left + strings.Repeat(" ", w-1-lipgloss.Width(left)-rw) + right + " "
}

func (m Model) scrollHint(total, h int) string {
	if total <= h {
		return ""
	}
	var parts []string
	if m.scroll > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", m.scroll))
	}
	if below := total - m.scroll - h; below > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", below))
	}
	return m.st.dim.Render(strings.Join(parts, " "))
}

// --- header ---

// stackTitle is the stack panel's title and the refresh state on its right.
func (m Model) stackTitle() (title, info string) {
	s := m.st
	title = s.title.Render("gh stack")
	if snap := m.snap; snap != nil && snap.Index >= 0 {
		title = ""
		if snap.Number != 0 {
			title = s.title.Render(fmt.Sprintf("#%d", snap.Number)) + " "
		}
		title += s.dim.Render("on ") + s.normal.Render(snap.Trunk)
		if snap.StackCount > 1 {
			title += s.dim.Render(fmt.Sprintf(" · %d/%d", snap.Index+1, snap.StackCount))
		}
		if m.pinned != "" {
			title += s.dim.Render(" · pinned")
		}
	}
	now := m.now()
	switch {
	case m.loadingRemote:
		info = m.spinner.View() + s.dim.Render(" refreshing")
		if !m.cachedAt.IsZero() {
			info += s.dim.Render(" · cached " + shortAgo(now, m.cachedAt))
		}
	case !m.cachedAt.IsZero():
		info = s.dim.Render("cached " + shortAgo(now, m.cachedAt))
	case !m.lastRemote.IsZero():
		info = s.dim.Render("↻ " + shortAgo(now, m.lastRemote))
	}
	return title, info
}

// headerLines are the notes above the stack panel: working tree state that
// needs attention, and GitHub errors. They are empty most of the time.
func (m Model) headerLines() []string {
	s := m.st
	var lines []string
	if snap := m.snap; snap != nil && snap.Index >= 0 {
		var notes []string
		if snap.Rebasing {
			notes = append(
				notes,
				s.err.Render("rebase in progress")+s.dim.Render("  rc continue · ra abort"),
			)
		} else if snap.CurrentBranch == "" {
			notes = append(notes, s.warn.Render("detached HEAD"))
		}
		if snap.Dirty {
			notes = append(notes, s.warn.Render("uncommitted changes"))
		}
		if len(notes) > 0 {
			lines = append(lines, " "+strings.Join(notes, s.dim.Render(" · ")))
		}
	}
	if m.remoteErr != nil {
		lines = append(lines, " "+s.err.Render("GitHub: "+firstLine(m.remoteErr.Error())))
	}
	return lines
}

func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- body ---

func (m Model) bodyLines() []string {
	s := m.st
	switch {
	case m.localErr != nil:
		return []string{"  " + s.err.Render("error: "+m.localErr.Error())}
	case m.snap == nil:
		return []string{"  " + m.spinner.View() + s.dim.Render(" loading stack…")}
	case m.snap.Index < 0:
		return []string{
			"  " + s.dim.Render("No stacks in this repository."),
			"  " + s.dim.Render("Create one with ") + s.normal.Render("gh stack init") +
				s.dim.Render(", or press : to run a gh stack command."),
		}
	}
	hb := m.hoveredBranch()
	var lines []string
	for i, r := range m.rows {
		cursor := i == m.cursor && m.navigable(i)
		active := r.branch >= 0 && r.branch == hb
		for _, l := range m.renderRow(r, active, cursor) {
			if cursor {
				l = s.highlight(l)
			}
			lines = append(lines, l)
		}
	}
	return lines
}

// parts is a line's left-aligned content and its right-aligned value.
type parts struct{ left, right string }

// gutter marks the active branch with a thin bar down every line, and the
// row under the cursor with a thick one.
func (m Model) gutter(active, cursor bool) string {
	switch {
	case cursor:
		return m.st.cursor.Render(glyphCursor) + " "
	case active:
		return m.st.cursor.Render(glyphActive) + " "
	}
	return "  "
}

// connector returns the tree line for branch bi and its style: accent for the
// active branch, peach and dashed when the branch needs a rebase.
func (m Model) connector(bi int, active bool) (string, lipgloss.Style) {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	glyph, style := glyphConn, s.conn
	if b.NeedsRebase && !m.merged(bi) && (pr == nil || !pr.Queued) {
		glyph, style = glyphDashed, s.connDashed
	}
	if active {
		style = s.connActive
	}
	return glyph, style
}

// bullet marks branch bi on the tree: selected, checked out, merged, queued,
// or plain.
func (m Model) bullet(bi int, active bool) string {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	switch {
	case m.marks[b.Name] || (m.visual && m.isSelected(bi, b.Name)):
		return s.bulletSelected.Render(glyphSelected)
	case b.IsCurrent:
		return s.bulletCurrent.Render(glyphCurrent)
	case m.merged(bi):
		return s.bulletMerged.Render(glyphMerged)
	case pr != nil && pr.Queued:
		return s.bulletQueued.Render(glyphQueued)
	case active:
		return s.connActive.Render(glyphBranch)
	}
	return s.dim.Render(glyphBranch)
}

// renderRow renders one row as lines exactly the panel's inner width. active
// is true for every row of the branch the cursor is on or inside, cursor for
// the row under the cursor.
func (m Model) renderRow(r row, active, cursor bool) []string {
	s := m.st
	w := m.innerWidth()
	switch r.kind {
	case rowSeparator:
		lead := "  " + s.rule.Render("── ") + s.dim.Render(r.label) + " "
		return []string{
			pad(lead+s.rule.Render(strings.Repeat("─", max(0, w-1-lipgloss.Width(lead)))), w),
		}
	case rowTrunk:
		return []string{pad("  "+s.conn.Render(glyphTrunk)+" "+s.trunk.Render(r.label), w)}
	case rowBranch:
		return m.renderBranch(r, active, cursor)
	}

	conn, cs := m.connector(r.branch, active)
	lead := m.gutter(active, cursor) + cs.Render(conn) + " "
	if r.kind == rowSpacer {
		return []string{pad(m.gutter(active, cursor)+cs.Render(conn), w)}
	}
	indent := func(level int) string { return lead + strings.Repeat(" ", level*indentStep) }

	switch r.kind {
	case rowSections:
		var segs []string
		for _, seg := range m.sectionSegments(r.branch) {
			segs = append(segs, seg.text)
		}
		return []string{pad(lead+strings.Join(segs, "   "), w)}

	case rowFile:
		f := r.file
		level := 1
		if f.Generated {
			level = 2
		}
		stat := m.diffStat(f.Additions, f.Deletions)
		if f.Binary {
			stat = s.dim.Render("binary")
		}
		room := w - colContent - level*indentStep - 2 - lipgloss.Width(stat) - 3
		return []string{fit(indent(level)+m.fileStatus(f.Status)+" "+
			s.normal.Render(truncateLeft(f.Path, room)), stat, w)}

	case rowGenerated:
		glyph := glyphCollapsed
		if m.expanded[r.key] {
			glyph = glyphExpanded
		}
		text := plural(r.count, "generated file", "generated files")
		return []string{fit(indent(1)+s.dim.Render(glyph+" "+text), "", w)}

	case rowCommit:
		c := r.commit
		return []string{fit(
			indent(1)+s.sha.Render(c.SHA[:min(7, len(c.SHA))])+" "+s.normal.Render(c.Subject),
			s.dim.Render(shortAgo(m.now(), c.Time)), w,
		)}

	case rowWorkflow:
		wf := r.workflow
		glyph := glyphCollapsed
		if m.expanded[r.key] {
			glyph = glyphExpanded
		}
		return []string{
			fit(
				indent(
					1,
				)+s.faintS.Render(
					glyph,
				)+" "+s.checkGlyph(
					wf.State,
				)+" "+s.normal.Render(
					wf.Name,
				),
				m.checkCounts(wf.Checks),
				w,
			),
		}

	case rowCheck:
		c := r.check
		return []string{
			fit(indent(2)+"  "+s.checkGlyph(c.State)+" "+s.normal.Render(c.Name), "", w),
		}
	}
	return nil
}

func (m Model) fileStatus(status string) string {
	s := m.st
	switch status {
	case "A":
		return s.ok.Render("A")
	case "D":
		return s.err.Render("D")
	case "R", "C":
		return s.prompt.Render(status)
	case "":
		return s.dim.Render(" ")
	}
	return s.dim.Render(status[:1])
}

func (m Model) renderBranch(r row, active, cursor bool) []string {
	w := m.innerWidth()
	conn, cs := m.connector(r.branch, active)
	var lines []string
	for i, p := range m.branchParts(r.branch) {
		mark := cs.Render(conn)
		if i == 0 {
			mark = m.bullet(r.branch, active)
		}
		lines = append(lines, fit(m.gutter(active, cursor)+mark+" "+p.left, p.right, w))
	}
	return lines
}

// branchParts lays out the lines of branch bi's header. The left column
// reads like a sentence: the branch, then its PR and title, then anything
// that needs attention. The right column holds the state: the diff, where
// the PR stands with review, and CI. A merged branch takes one line.
func (m Model) branchParts(bi int) []parts {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	num := m.prNumber(bi)

	if m.merged(bi) {
		left := s.branchMerged.Render(b.Name)
		if num != 0 {
			left += "  " + s.prNum.Render(fmt.Sprintf("#%d", num))
			if pr != nil && pr.Title != "" {
				left += " " + s.dim.Render(pr.Title)
			}
		}
		return []parts{{left, s.info.Render("merged")}}
	}

	name := s.branch.Render(b.Name)
	if b.IsCurrent {
		name = s.branchCurrent.Render(b.Name)
	}
	var diff string
	switch {
	case b.Head == "":
		diff = s.dim.Render("no local branch")
	case b.Additions > 0 || b.Deletions > 0:
		diff = m.diffStat(b.Additions, b.Deletions)
	}
	out := []parts{{name, diff}}

	if num != 0 {
		left := s.prNum.Render(fmt.Sprintf("#%d", num))
		if pr != nil && pr.Title != "" {
			left += " " + s.normal.Render(pr.Title)
		}
		out = append(out, parts{left, m.prState(bi)})
	}
	if notes, ci := m.attention(bi), m.ciState(bi); notes != "" || ci != "" {
		out = append(out, parts{notes, ci})
	}
	return out
}

// prState is where branch bi's PR stands: queued, closed, draft, or for an
// open PR, its review decision. A PR still loading shows the spinner.
func (m Model) prState(bi int) string {
	s := m.st
	pr := m.prFor(bi)
	if pr == nil {
		if m.loadingRemote || (m.lastRemote.IsZero() && m.remoteErr == nil) {
			return m.spinner.View()
		}
		return ""
	}
	switch {
	case pr.Merged:
		return s.info.Render("merged")
	case pr.Queued:
		return s.warn.Render("queued")
	case pr.State == "CLOSED":
		return s.err.Render("closed")
	case pr.IsDraft:
		return s.dim.Render("draft")
	}
	switch pr.Review() {
	case github.ReviewApproved:
		return s.ok.Render("approved")
	case github.ReviewChangesRequested:
		return s.err.Render("changes requested")
	}
	return s.dim.Render("in review")
}

// attention lists what needs doing on branch bi: a rebase, merge conflicts,
// and open review threads.
func (m Model) attention(bi int) string {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	var notes []string
	if b.NeedsRebase && (pr == nil || !pr.Queued) {
		notes = append(notes, s.warn.Render("needs rebase"))
	}
	if pr != nil && pr.State == "OPEN" {
		if !pr.Queued {
			switch pr.MergeState {
			case "DIRTY":
				notes = append(notes, s.err.Render("conflicts"))
			case "BEHIND":
				notes = append(notes, s.warn.Render("behind base"))
			}
		}
		if pr.Unresolved > 0 {
			notes = append(notes, s.warn.Render(fmt.Sprintf("%d unresolved", pr.Unresolved)))
		}
		if pr.UnresolvedBots > 0 {
			notes = append(notes, s.bot.Render(fmt.Sprintf("%d bot", pr.UnresolvedBots)))
		}
	}
	return strings.Join(notes, s.dim.Render(" · "))
}

// ciState summarizes CI on branch bi's open PR in a word or two.
func (m Model) ciState(bi int) string {
	s := m.st
	pr := m.prFor(bi)
	if pr == nil || pr.State != "OPEN" || len(pr.Checks.Workflows) == 0 {
		return ""
	}
	counts := pr.Checks.Counts()
	var out []string
	if n := counts[github.CheckFailure]; n > 0 {
		out = append(out, s.err.Render(fmt.Sprintf("✗ %d failing", n)))
	}
	if n := counts[github.CheckPending]; n > 0 {
		out = append(out, s.warn.Render(fmt.Sprintf("● %d running", n)))
	}
	if len(out) > 0 {
		return strings.Join(out, " ")
	}
	switch pr.Checks.State {
	case github.CheckCancelled:
		return s.dim.Render("⊘ cancelled")
	case github.CheckSkipped:
		return s.dim.Render("- skipped")
	}
	return s.ok.Render("✓ passing")
}

// segment is one section on a branch's sections line.
type segment struct {
	section string
	text    string
}

// sectionSegments are the sections branch bi has, open ones in primary ink.
func (m Model) sectionSegments(bi int) []segment {
	s := m.st
	b := m.snap.Branches[bi]
	var segs []segment
	add := func(section, text string) {
		glyph, style := glyphCollapsed, s.faintS
		if m.expanded[sectionKey(b.Name, section)] {
			glyph, style = glyphExpanded, s.normal
		}
		segs = append(segs, segment{section, style.Render(glyph + " " + text)})
	}
	if len(b.Files) > 0 {
		n := 0
		for _, f := range b.Files {
			if !f.Generated {
				n++
			}
		}
		if n == 0 {
			add(secFiles, plural(len(b.Files), "generated file", "generated files"))
		} else {
			add(secFiles, plural(n, "file", "files"))
		}
	}
	if len(b.Commits) > 0 {
		add(secCommits, plural(len(b.Commits), "commit", "commits"))
	}
	if pr := m.prFor(bi); pr != nil && len(pr.Checks.Workflows) > 0 {
		n := 0
		for _, w := range pr.Checks.Workflows {
			n += len(w.Checks)
		}
		add(secChecks, plural(n, "check", "checks"))
	}
	return segs
}

// sectionAt returns the section whose label covers column col of branch bi's
// sections line, or "".
func (m Model) sectionAt(bi, col int) string {
	x := colContent
	for _, seg := range m.sectionSegments(bi) {
		w := lipgloss.Width(seg.text)
		if col >= x && col < x+w {
			return seg.section
		}
		x += w + 3
	}
	return ""
}

func (m Model) checkCounts(checks []github.Check) string {
	s := m.st
	counts := map[github.CheckState]int{}
	for _, c := range checks {
		counts[c.State]++
	}
	var parts []string
	for _, st := range []struct {
		state github.CheckState
		word  string
		style lipgloss.Style
	}{
		{github.CheckFailure, "failed", s.err},
		{github.CheckPending, "running", s.warn},
		{github.CheckCancelled, "cancelled", s.dim},
		{github.CheckSuccess, "passed", s.ok},
		{github.CheckSkipped, "skipped", s.dim},
	} {
		if n := counts[st.state]; n > 0 {
			parts = append(parts, st.style.Render(fmt.Sprintf("%d %s", n, st.word)))
		}
	}
	return strings.Join(parts, s.dim.Render(" · "))
}

func (m Model) diffStat(add, del int) string {
	return m.st.add.Render(fmt.Sprintf("+%d", add)) + " " + m.st.del.Render(fmt.Sprintf("-%d", del))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// truncateLeft keeps the tail of a path, which is the distinctive part.
func truncateLeft(s string, maxLen int) string {
	maxLen = max(20, maxLen)
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return "…" + string(r[len(r)-maxLen+1:])
}

// shortAgo is how long before now t was, in its largest unit.
func shortAgo(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo ago", max(1, int(d.Hours()/24/30)))
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// --- output panel ---

func (m Model) outputLines() []string {
	s := m.st
	var status string
	switch m.outputState {
	case opRunning:
		elapsed := ""
		if m.op != nil {
			elapsed = fmt.Sprintf(" %ds", int(m.now().Sub(m.op.started).Seconds()))
		}
		status = m.spinner.View() + s.warn.Render(" running"+elapsed)
		if n := len(m.queue); n > 0 {
			status += s.dim.Render(fmt.Sprintf(" · %d queued", n))
		}
	case opSucceeded:
		status = s.ok.Render("✓ done")
	case opFailed:
		status = s.err.Render("✗ failed")
	case opCancelled:
		status = s.warn.Render("✗ cancelled")
	}
	title := s.title.Render(m.outputTitle) + " " + status
	hide := s.key.Render("!") + s.keyDesc.Render(" hide")

	out := m.output
	if len(out) > outputPanelLines {
		out = out[len(out)-outputPanelLines:]
	}
	var body []string
	for _, l := range out {
		body = append(body, " "+s.dim.Render(l))
	}
	return m.panel(title, hide, "", body, outputPanelLines)
}

// --- footer ---

func (m Model) footer() string {
	return " " + m.footerText()
}

func (m Model) footerText() string {
	s := m.st
	switch m.mode {
	case modePrompt:
		p := "gh stack add "
		if m.promptKind == promptCommand {
			p = "gh stack "
		}
		return s.prompt.Render(p) + m.input.View()
	case modeConfirm:
		return s.warn.Render(m.confirm.text)
	case modeHelp:
		return s.dim.Render("press any key to close help")
	case modeReviewers:
		if m.picker != nil && !m.picker.ready {
			return m.hints(
				"↑/↓", "move", "tab", "pick", "enter", "request review",
				"ctrl+s", "skip", "esc", "skip",
			)
		}
		return m.hints(
			"↑/↓", "move", "tab", "pick", "enter", "request review + mark ready",
			"ctrl+s", "mark ready without", "esc", "skip",
		)
	}
	switch m.pendingKey {
	case "r":
		return s.prompt.Render("rebase ") + m.hints(
			"r", "whole stack", "u", "upstack", "d", "downstack", "c", "continue", "a", "abort",
		)
	case "g":
		return s.prompt.Render("g") + s.dim.Render("…")
	}
	if m.flash != "" {
		if m.flashErr {
			return s.flashErr.Render(m.flash)
		}
		return s.flashOK.Render(m.flash)
	}
	if m.visual || len(m.marks) > 0 {
		n := 0
		if m.snap != nil {
			for i, b := range m.snap.Branches {
				if m.isSelected(i, b.Name) {
					n++
				}
			}
		}
		mode := "select"
		if m.visual {
			mode = "visual"
		}
		lead := s.selected.Render(mode) + s.dim.Render(fmt.Sprintf(" %d  ", n))
		return lead + m.hintsIn(m.width-2-lipgloss.Width(lead),
			"R", "review", "D", "draft/ready", "A", "reviewers",
			"space", "mark", "v", "visual", "esc", "clear",
		)
	}
	if r, ok := m.hovered(); ok && r.item() {
		return m.hints(
			"j/k", "items", "h", "back", "o", "open", "y", "copy", "e", "edit", "?", "help",
		)
	}
	return m.hints(
		"j/k", "move", "l", "open", "f/C/x", "files/commits/checks", "o", "browser",
		"c", "checkout", "p", "push", "s", "sync", "R", "review", "?", "help",
	)
}

func (m Model) hints(pairs ...string) string {
	return m.hintsIn(m.width-2, pairs...)
}

// hintsIn renders key hints that fit in width, in order. The last pair,
// usually help, is always kept.
func (m Model) hintsIn(width int, pairs ...string) string {
	render := func(i int) string {
		return m.st.key.Render(pairs[i]) + m.st.keyDesc.Render(" "+pairs[i+1])
	}
	n := len(pairs) / 2
	if n == 0 {
		return ""
	}
	last := render(2 * (n - 1))
	room := width - lipgloss.Width(last)
	var parts []string
	for i := 0; i < n-1; i++ {
		h := render(2 * i)
		if room-lipgloss.Width(h)-2 < 0 {
			break
		}
		room -= lipgloss.Width(h) + 2
		parts = append(parts, h)
	}
	return strings.Join(append(parts, last), "  ")
}

// --- help ---

func (m Model) helpLines() []string {
	s := m.st
	var cols []string
	for _, sec := range helpSections {
		var b strings.Builder
		b.WriteString(s.title.Render(sec.title) + "\n")
		for _, k := range sec.keys {
			fmt.Fprintf(
				&b,
				"%s %s\n",
				s.key.Render(fmt.Sprintf("%-10s", k[0])),
				s.keyDesc.Render(k[1]),
			)
		}
		cols = append(cols, lipgloss.NewStyle().MarginRight(3).Render(b.String()))
	}
	// Lay the sections out in as many columns as fit.
	width := m.innerWidth() - 2
	var rows []string
	var line []string
	used := 0
	for _, c := range cols {
		w := lipgloss.Width(c)
		if used+w > width && len(line) > 0 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, line...))
			line, used = nil, 0
		}
		line = append(line, c)
		used += w
	}
	if len(line) > 0 {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, line...))
	}
	out := []string{""}
	for _, l := range strings.Split(strings.Join(rows, "\n"), "\n") {
		out = append(out, "  "+l)
	}
	return out
}
