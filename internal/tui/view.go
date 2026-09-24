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

// headerHeight is the header line plus the blank line under it.
const headerHeight = 2

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
	var lines []string
	lines = append(lines, m.headerLines()...)

	body := m.bodyLines()
	h := m.bodyHeight()
	if m.mode == modeHelp {
		body = m.helpLines()
		if len(body) > h {
			body = body[:h]
		}
	} else if m.scroll < len(body) {
		body = body[m.scroll:min(len(body), m.scroll+h)]
	} else {
		body = nil
	}
	lines = append(lines, body...)
	for i := len(body); i < h; i++ {
		lines = append(lines, "")
	}
	if m.showOutput {
		lines = append(lines, m.outputLines()...)
	}
	lines = append(lines, m.footer())

	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(lines, "\n")
}

func (m Model) bodyHeight() int {
	h := m.height - len(m.headerLines()) - 1
	if m.showOutput {
		h -= outputPanelLines + 1
	}
	return max(1, h)
}

// bodyTop is the screen row of the first body line.
func (m Model) bodyTop() int { return len(m.headerLines()) }

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

// --- header ---

func (m Model) headerLines() []string {
	s := m.st
	left := s.title.Render("gh stack")
	if snap := m.snap; snap != nil && snap.Index >= 0 {
		if snap.Number != 0 {
			left += " " + s.normal.Render(fmt.Sprintf("#%d", snap.Number))
		}
		if snap.StackCount > 1 {
			left += s.dim.Render(fmt.Sprintf(" %d/%d", snap.Index+1, snap.StackCount))
		}
		merged := 0
		for i := range snap.Branches {
			if m.merged(i) {
				merged++
			}
		}
		info := plural(len(snap.Branches), "branch", "branches")
		if merged > 0 {
			info += fmt.Sprintf(" (%d merged)", merged)
		}
		left += s.dim.Render("  on ") + s.normal.Render(snap.Trunk) + s.dim.Render(" · "+info)
		switch {
		case snap.Rebasing:
			left += s.dim.Render(
				" · ",
			) + s.err.Render(
				"rebase in progress",
			) + s.dim.Render(
				"  rc continue · ra abort",
			)
		case snap.CurrentBranch != "":
			left += s.dim.Render(" · at ") + s.branchCurrent.Render(snap.CurrentBranch)
		default:
			left += s.dim.Render(" · ") + s.warn.Render("detached HEAD")
		}
		if snap.Dirty {
			left += s.dim.Render(" · ") + s.warn.Render("uncommitted changes")
		}
		if m.pinned != "" {
			left += s.dim.Render(" · pinned")
		}
	}

	var right string
	switch {
	case m.loadingRemote:
		right = m.spinner.View() + s.dim.Render(" refreshing")
	case !m.lastRemote.IsZero():
		right = s.dim.Render("↻ " + shortAgo(m.lastRemote))
	}
	lines := []string{spread(left, right, m.width)}
	if m.remoteErr != nil {
		lines = append(lines, s.err.Render("GitHub: "+firstLine(m.remoteErr.Error())))
	}
	return append(lines, "")
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
		return []string{s.err.Render("error: " + m.localErr.Error())}
	case m.snap == nil:
		return []string{m.spinner.View() + s.dim.Render(" loading stack…")}
	case m.snap.Index < 0:
		return []string{
			s.dim.Render("No stacks in this repository."),
			s.dim.Render("Create one with ") + s.normal.Render("gh stack init") +
				s.dim.Render(", or press : to run a gh stack command."),
		}
	}
	hb := m.hoveredBranch()
	var lines []string
	for i, r := range m.rows {
		lines = append(
			lines,
			m.renderRow(r, i == m.cursor && m.navigable(i), r.branch >= 0 && r.branch == hb)...)
	}
	return lines
}

// connector returns the tree line for branch bi and the style to draw it and
// its bullet in. A focused branch draws its whole node in one color: accent
// when it is checked out, purple when merged, yellow when queued, otherwise
// the primary ink. Elsewhere the line is border-colored, or yellow and
// dashed when the branch needs a rebase.
func (m Model) connector(bi int, focused bool) (string, lipgloss.Style) {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	merged := m.merged(bi)
	queued := pr != nil && pr.Queued
	glyph, style := glyphConn, s.conn
	if b.NeedsRebase && !merged && !queued {
		glyph, style = glyphDashed, s.connDashed
	}
	if focused {
		switch {
		case b.IsCurrent:
			style = s.connCurrent
		case merged:
			style = s.connMerged
		case queued:
			style = s.connQueued
		default:
			style = s.connFocus
		}
	}
	return glyph, style
}

func (m Model) statusIcon(bi int) string {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	switch {
	case m.merged(bi):
		return s.iconMerged.Render(glyphMerged)
	case pr != nil && pr.Queued:
		return s.iconQueued.Render(glyphQueued)
	case b.NeedsRebase:
		return s.iconWarn.Render(glyphWarn)
	case m.prNumber(bi) != 0:
		return s.iconOpen.Render(glyphOpen)
	}
	return ""
}

// renderRow renders one row. cursor is true for the row under the cursor;
// active is true for every row of the branch the cursor is on or inside.
func (m Model) renderRow(r row, cursor, active bool) []string {
	s := m.st
	switch r.kind {
	case rowSeparator:
		return []string{
			s.conn.Render("────") + s.dim.Render(" "+r.label+" ") + s.conn.Render("─────"),
		}
	case rowTrunk:
		return []string{s.conn.Render(glyphTrunk+" ") + s.trunk.Render(r.label)}
	case rowBranch:
		return m.renderBranch(r, cursor, active)
	}

	conn, cs := m.connector(r.branch, active)
	line := cs.Render(conn)
	if r.kind == rowSpacer {
		return []string{line}
	}

	b := m.snap.Branches[r.branch]
	// Items indent four columns past the connector; the cursor takes the
	// last two of them, so focused and unfocused text line up.
	indent := func(n int) string {
		if cursor {
			return strings.Repeat(" ", n-2) + cs.Render(glyphFocus) + " "
		}
		return strings.Repeat(" ", n)
	}

	switch r.kind {
	case rowSection:
		glyph := glyphCollapsed
		if m.expanded[r.key] {
			glyph = glyphExpanded
		}
		var text, extra string
		switch r.section {
		case secFiles:
			text = plural(len(b.Files), "file changed", "files changed")
		case secCommits:
			text = plural(len(b.Commits), "commit", "commits")
		case secChecks:
			pr := m.prFor(r.branch)
			n := 0
			for _, w := range pr.Checks.Workflows {
				n += len(w.Checks)
			}
			text = plural(n, "check", "checks")
			extra = "  " + m.checkSummary(pr.Checks)
		}
		return []string{line + "  " + s.dim.Render(glyph+" "+text) + extra}

	case rowFile:
		f := r.file
		path := truncateLeft(f.Path, m.width-30)
		stat := m.diffStat(f.Additions, f.Deletions)
		if f.Binary {
			stat = s.dim.Render("binary")
		}
		return []string{line + indent(4) + s.normal.Render(path) + "  " + stat}

	case rowCommit:
		c := r.commit
		subject := truncateRight(c.Subject, m.width-35)
		return []string{
			line + indent(4) + s.sha.Render(c.SHA[:min(7, len(c.SHA))]) + " " +
				s.normal.Render(subject) + "  " + s.dim.Render(timeAgo(c.Time)),
		}

	case rowWorkflow:
		w := r.workflow
		return []string{
			line + indent(4) + s.checkGlyph(w.State) + " " + s.normal.Render(w.Name) +
				"  " + m.checkCounts(w.Checks),
		}

	case rowCheck:
		c := r.check
		return []string{line + indent(6) + s.checkGlyph(c.State) + " " + s.normal.Render(c.Name)}
	}
	return nil
}

func (m Model) renderBranch(r row, cursor, active bool) []string {
	s := m.st
	b := m.snap.Branches[r.branch]
	pr := m.prFor(r.branch)
	merged := m.merged(r.branch)
	conn, cs := m.connector(r.branch, active)

	bullet := glyphBullet
	if cursor {
		bullet = glyphFocus
	}
	head := cs.Render(bullet + " ")
	if icon := m.statusIcon(r.branch); icon != "" {
		head += icon + " "
	}

	name := b.Name
	var styledName string
	switch {
	case b.IsCurrent:
		styledName = s.branchCurrent.Render(name + " (current)")
	case merged:
		styledName = s.branchMerged.Render(name)
	default:
		styledName = s.branch.Render(name)
	}
	branchLine := styledName
	if b.Additions > 0 || b.Deletions > 0 {
		branchLine += "  " + m.diffStat(b.Additions, b.Deletions)
	}
	if b.Head == "" && !merged {
		branchLine += "  " + s.dim.Render("no local branch")
	}
	if m.marks[name] || (m.visual && m.isSelected(r.branch, name)) {
		branchLine += "  " + s.selected.Render("selected")
	}

	if r.height < 2 {
		return []string{head + branchLine}
	}

	// PR line on top, branch line under it, like gh stack view.
	num := m.prNumber(r.branch)
	prLine := head + s.prLink.Render(fmt.Sprintf("#%d", num))
	switch {
	case pr == nil && merged:
		prLine += " " + s.prMerged.Render("MERGED")
	case pr == nil:
		if m.loadingRemote || (m.lastRemote.IsZero() && m.remoteErr == nil) {
			prLine += " " + m.spinner.View()
		}
	default:
		prLine += " " + m.prStateLabel(pr)
		if pr.State == "OPEN" && !pr.Queued {
			if len(pr.Checks.Workflows) > 0 {
				prLine += "  " + s.checkGlyph(pr.Checks.State) + s.dim.Render(" checks")
			}
			switch pr.ReviewDecision {
			case "APPROVED":
				prLine += "  " + s.ok.Render("approved")
			case "CHANGES_REQUESTED":
				prLine += "  " + s.err.Render("changes requested")
			}
			switch pr.MergeState {
			case "DIRTY":
				prLine += "  " + s.err.Render("conflicts")
			case "BEHIND":
				prLine += "  " + s.warn.Render("behind base")
			}
		}
		if pr.Title != "" {
			room := m.width - lipgloss.Width(prLine) - 2
			if room >= 12 {
				prLine += "  " + s.dim.Render(truncateRight(pr.Title, room))
			}
		}
	}
	return []string{prLine, cs.Render(conn) + " " + branchLine}
}

func (m Model) prStateLabel(pr *github.PR) string {
	s := m.st
	switch {
	case pr.Merged:
		return s.prMerged.Render("MERGED")
	case pr.Queued:
		return s.prQueued.Render("QUEUED")
	case pr.State == "CLOSED":
		return s.prClosed.Render("CLOSED")
	case pr.IsDraft:
		return s.prDraft.Render("DRAFT")
	default:
		return s.prOpen.Render("OPEN")
	}
}

func (m Model) checkSummary(r github.Rollup) string {
	var all []github.Check
	for _, w := range r.Workflows {
		all = append(all, w.Checks...)
	}
	return m.checkCounts(all)
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

func truncateRight(s string, maxLen int) string {
	maxLen = max(12, maxLen)
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-1]) + "…"
}

// timeAgo matches gh stack view's wording for commit times.
func timeAgo(t time.Time) string {
	d := time.Since(t)
	n, unit := 0, ""
	switch {
	case d < time.Minute:
		n, unit = int(d.Seconds()), "second"
	case d < time.Hour:
		n, unit = int(d.Minutes()), "minute"
	case d < 24*time.Hour:
		n, unit = int(d.Hours()), "hour"
	case d < 30*24*time.Hour:
		n, unit = int(d.Hours()/24), "day"
	default:
		n, unit = max(1, int(d.Hours()/24/30)), "month"
	}
	if n == 1 {
		return "1 " + unit + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

// shortAgo is the compact form used for the refresh indicator.
func shortAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
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
			elapsed = fmt.Sprintf(" %ds", int(time.Since(m.op.started).Seconds()))
		}
		status = m.spinner.View() + s.warn.Render(" running"+elapsed)
	case opSucceeded:
		status = s.ok.Render("✓ done")
	case opFailed:
		status = s.err.Render("✗ failed")
	}
	title := s.rule.Render("── ") + s.title.Render(m.outputTitle) + " " + status + " "
	hide := s.key.Render("!") + s.keyDesc.Render(" hide")
	title += s.rule.Render(
		strings.Repeat("─", max(0, m.width-lipgloss.Width(title)-lipgloss.Width(hide)-2)),
	)
	title += " " + hide

	out := m.output
	if len(out) > outputPanelLines {
		out = out[len(out)-outputPanelLines:]
	}
	lines := []string{title}
	for _, l := range out {
		lines = append(lines, s.dim.Render(l))
	}
	for len(lines) < outputPanelLines+1 {
		lines = append(lines, "")
	}
	return lines
}

// --- footer ---

func (m Model) footer() string {
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
		return s.selected.Render(mode) + s.dim.Render(fmt.Sprintf(" %d selected  ", n)) +
			m.hints("R", "review", "space", "mark", "v", "end visual", "esc", "clear")
	}
	if r, ok := m.hovered(); ok && r.item() {
		return m.hints(
			"j/k", "items", "h", "back", "o", "open", "y", "copy", "e", "edit", "?", "help",
		)
	}
	return m.hints(
		"j/k", "navigate", "l/h", "expand", "o", "open", "y", "copy", "c", "checkout",
		"p", "push", "s", "sync", "R", "review", "?", "help",
	)
}

func (m Model) hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, m.st.key.Render(pairs[i])+m.st.keyDesc.Render(" "+pairs[i+1]))
	}
	return strings.Join(parts, "  ")
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
	var rows []string
	var line []string
	width := 0
	for _, c := range cols {
		w := lipgloss.Width(c)
		if width+w > m.width && len(line) > 0 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, line...))
			line, width = nil, 0
		}
		line = append(line, c)
		width += w
	}
	if len(line) > 0 {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, line...))
	}
	return strings.Split(strings.Join(rows, "\n"), "\n")
}
