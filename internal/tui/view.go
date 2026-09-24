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

// ensureVisible scrolls so the cursor row is on screen with a little context.
func (m *Model) ensureVisible() {
	if len(m.rows) == 0 || m.height == 0 {
		m.scroll = 0
		return
	}
	h := m.bodyHeight()
	last := m.rows[len(m.rows)-1]
	total := last.y + last.height
	r := m.rows[max(0, min(m.cursor, len(m.rows)-1))]
	margin := min(2, h/4)
	if r.y-margin < m.scroll {
		m.scroll = r.y - margin
	}
	if bottom := r.y + r.height + margin; bottom > m.scroll+h {
		m.scroll = bottom - h
	}
	m.scroll = max(0, min(m.scroll, total-h))
}

// --- header ---

func (m Model) headerLines() []string {
	left := titleStyle.Render("gh stack")
	if s := m.snap; s != nil && s.Index >= 0 {
		if s.Number != 0 {
			left += " " + boldStyle.Render(fmt.Sprintf("#%d", s.Number))
		}
		if s.StackCount > 1 {
			left += dimStyle.Render(fmt.Sprintf(" [%d/%d]", s.Index+1, s.StackCount))
		}
		merged := 0
		for i := range s.Branches {
			if m.merged(i) {
				merged++
			}
		}
		info := plural(len(s.Branches), "branch", "branches")
		if merged > 0 {
			info += fmt.Sprintf(", %d merged", merged)
		}
		left += dimStyle.Render("  on ") + trunkStyle.Render(s.Trunk) + dimStyle.Render(" · "+info)
		if s.CurrentBranch != "" {
			left += dimStyle.Render(" · at ") + branchCurrentStyle.Render(s.CurrentBranch)
		} else {
			left += dimStyle.Render(" · ") + warnStyle.Render("detached HEAD")
		}
		if s.Dirty {
			left += " " + warnStyle.Render("✎ uncommitted changes")
		}
		if s.Rebasing {
			left += " " + errStyle.Render("⚠ REBASE IN PROGRESS (rc continue, ra abort)")
		}
		if m.pinned != "" {
			left += " " + dimStyle.Render("(pinned)")
		}
	}

	var right string
	switch {
	case m.loadingRemote:
		right = m.spinner.View() + dimStyle.Render(" refreshing")
	case !m.lastRemote.IsZero():
		right = dimStyle.Render("↻ " + shortAgo(m.lastRemote))
	}
	lines := []string{spread(left, right, m.width)}
	if m.remoteErr != nil {
		lines = append(lines, errStyle.Render("⚠ GitHub: "+firstLine(m.remoteErr.Error())))
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
	switch {
	case m.localErr != nil:
		return []string{errStyle.Render("error: " + m.localErr.Error())}
	case m.snap == nil:
		return []string{m.spinner.View() + " loading stack…"}
	case m.snap.Index < 0:
		return []string{
			dimStyle.Render("No stacks in this repository."),
			dimStyle.Render(
				"Create one with ",
			) + keyStyle.Render(
				"gh stack init",
			) + dimStyle.Render(
				", or : to run a gh stack command.",
			),
		}
	}
	var lines []string
	for i, r := range m.rows {
		lines = append(lines, m.renderRow(r, i == m.cursor)...)
	}
	return lines
}

func (m Model) gutter(r row, focused bool, lineIdx int) string {
	cur := " "
	if focused && lineIdx == 0 {
		cur = cursorStyle.Render(glyphCursor)
	}
	mark := " "
	if r.kind == rowBranch && r.branch >= 0 {
		name := m.snap.Branches[r.branch].Name
		switch {
		case m.marks[name]:
			mark = markStyle.Render(glyphMark)
		case m.visual && m.isSelected(r.branch, name):
			mark = visualStyle.Render(glyphVisual)
		}
	}
	return cur + mark + " "
}

func (m Model) connector(bi int, focused bool) string {
	c := "│"
	if b := m.snap.Branches[bi]; b.NeedsRebase && !m.merged(bi) {
		c = "┊"
	}
	if focused {
		return connFocusStyle.Render(c)
	}
	return connStyle.Render(c)
}

func (m Model) renderRow(r row, focused bool) []string {
	label := func(s string, style lipgloss.Style) string {
		if focused {
			return focusStyle.Render(s)
		}
		return style.Render(s)
	}
	switch r.kind {
	case rowSeparator:
		return []string{
			"   " + connStyle.Render(
				"────",
			) + dimStyle.Render(
				" "+r.label+" ",
			) + connStyle.Render(
				"─────",
			),
		}
	case rowTrunk:
		return []string{"   " + connStyle.Render("└ ") + trunkStyle.Render(r.label)}
	case rowSpacer:
		return []string{"   " + m.connector(r.branch, false)}
	case rowBranch:
		return m.renderBranch(r, focused)
	}

	b := m.snap.Branches[r.branch]
	prefix := m.gutter(r, focused, 0) + m.connector(r.branch, false) + "   "
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
			extra = "  " + diffStat(b.Additions, b.Deletions)
		case secCommits:
			text = plural(len(b.Commits), "commit", "commits")
		case secChecks:
			pr := m.prFor(r.branch)
			text = "checks"
			extra = "  " + checkSummary(pr.Checks)
			return []string{
				prefix + sectionStyle.Render(
					glyph+" ",
				) + checkGlyph(
					pr.Checks.State,
				) + " " + label(
					text,
					sectionStyle,
				) + extra,
			}
		}
		return []string{prefix + sectionStyle.Render(glyph+" ") + label(text, sectionStyle) + extra}

	case rowFile:
		f := r.file
		status := f.Status
		if status == "" {
			status = "M"
		}
		stat := diffStat(f.Additions, f.Deletions)
		if f.Binary {
			stat = dimStyle.Render("binary")
		}
		return []string{
			prefix + "  " + fileStatusStyle(
				status,
			).Render(status) +
				" " + label(
				f.Path,
				branchStyle,
			) + "  " + stat,
		}

	case rowCommit:
		c := r.commit
		return []string{
			prefix + "  " + shaStyle.Render(
				c.SHA[:min(7, len(c.SHA))],
			) + " " + label(
				c.Subject,
				branchStyle,
			) +
				dimStyle.Render(
					"  "+c.Author+", "+shortAgo(c.Time),
				),
		}

	case rowWorkflow:
		w := r.workflow
		glyph := glyphCollapsed
		if m.expanded[r.key] {
			glyph = glyphExpanded
		}
		return []string{
			prefix + "  " + sectionStyle.Render(
				glyph+" ",
			) + checkGlyph(
				w.State,
			) + " " + label(
				w.Name,
				branchStyle,
			) +
				"  " + dimStyle.Render(
				checkCounts(w.Checks),
			),
		}

	case rowCheck:
		c := r.check
		return []string{prefix + "      " + checkGlyph(c.State) + " " + label(c.Name, branchStyle)}
	}
	return nil
}

func (m Model) renderBranch(r row, focused bool) []string {
	b := m.snap.Branches[r.branch]
	pr := m.prFor(r.branch)
	merged := m.merged(r.branch)

	bullet := connStyle.Render("├ ")
	if focused {
		bullet = connFocusStyle.Render("├ ")
	}

	var icon string
	switch {
	case merged:
		icon = mergedStyle.Render(glyphMerged)
	case pr != nil && pr.Queued:
		icon = queuedStyle.Render(glyphQueued)
	case b.NeedsRebase:
		icon = warnStyle.Render(glyphWarn)
	case pr != nil && pr.IsDraft:
		icon = draftStyle.Render(glyphOpen)
	case pr != nil || b.PR != nil:
		icon = openStyle.Render(glyphOpen)
	default:
		icon = dimStyle.Render("·")
	}

	nameStyle := branchStyle
	name := b.Name
	switch {
	case b.IsCurrent:
		nameStyle = branchCurrentStyle
	case merged:
		nameStyle = branchMergedStyle
	}
	styledName := nameStyle.Render(name)
	if focused {
		styledName = focusStyle.Inherit(nameStyle).Render(name)
	}
	line := m.gutter(r, focused, 0) + bullet + icon + " " + styledName
	if b.IsCurrent {
		line += " " + branchCurrentStyle.Render("(current)")
	}
	if b.Additions > 0 || b.Deletions > 0 {
		line += "  " + diffStat(b.Additions, b.Deletions)
	}
	if b.NeedsRebase && !merged {
		line += "  " + warnStyle.Render("needs rebase")
	}
	if b.Head == "" && !merged {
		line += "  " + dimStyle.Render("(no local branch)")
	}
	lines := []string{line}
	if r.height < 2 {
		return lines
	}

	second := m.gutter(r, focused, 1) + m.connector(r.branch, focused) + "   "
	num := m.prNumber(r.branch)
	second += prNumStyle.Render(fmt.Sprintf("#%d", num))
	if pr == nil {
		if m.loadingRemote || m.lastRemote.IsZero() {
			second += " " + m.spinner.View()
		}
		return append(lines, second)
	}
	second += " " + prStateLabel(pr)
	if pr.State == "OPEN" {
		if len(pr.Checks.Workflows) > 0 {
			second += "  " + checkGlyph(pr.Checks.State) + dimStyle.Render(" CI")
		}
		switch pr.ReviewDecision {
		case "APPROVED":
			second += "  " + okStyle.Render("approved")
		case "CHANGES_REQUESTED":
			second += "  " + errStyle.Render("changes requested")
		case "REVIEW_REQUIRED":
			second += "  " + dimStyle.Render("review required")
		}
		switch pr.MergeState {
		case "DIRTY":
			second += "  " + errStyle.Render("conflicts")
		case "BEHIND":
			second += "  " + warnStyle.Render("behind base")
		}
	}
	if pr.Title != "" {
		second += dimStyle.Render("  " + pr.Title)
	}
	return append(lines, second)
}

func prStateLabel(pr *github.PR) string {
	switch {
	case pr.Merged:
		return mergedStyle.Render("MERGED")
	case pr.Queued:
		return queuedStyle.Render("QUEUED")
	case pr.State == "CLOSED":
		return closedStyle.Render("CLOSED")
	case pr.IsDraft:
		return draftStyle.Render("DRAFT")
	default:
		return openStyle.Render("OPEN")
	}
}

func checkSummary(r github.Rollup) string {
	var all []github.Check
	for _, w := range r.Workflows {
		all = append(all, w.Checks...)
	}
	return dimStyle.Render(
		plural(len(r.Workflows), "workflow", "workflows")+", ",
	) + checkCounts(
		all,
	)
}

func checkCounts(checks []github.Check) string {
	counts := map[github.CheckState]int{}
	for _, c := range checks {
		counts[c.State]++
	}
	var parts []string
	for _, s := range []struct {
		state github.CheckState
		word  string
		style lipgloss.Style
	}{
		{github.CheckFailure, "failed", errStyle},
		{github.CheckPending, "running", warnStyle},
		{github.CheckCancelled, "cancelled", dimStyle},
		{github.CheckSuccess, "passed", okStyle},
		{github.CheckSkipped, "skipped", dimStyle},
	} {
		if n := counts[s.state]; n > 0 {
			parts = append(parts, s.style.Render(fmt.Sprintf("%d %s", n, s.word)))
		}
	}
	return strings.Join(parts, dimStyle.Render(" · "))
}

func diffStat(add, del int) string {
	return addStyle.Render(fmt.Sprintf("+%d", add)) + " " + delStyle.Render(fmt.Sprintf("-%d", del))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

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
	var status string
	switch m.outputState {
	case opRunning:
		elapsed := ""
		if m.op != nil {
			elapsed = fmt.Sprintf(" %ds", int(time.Since(m.op.started).Seconds()))
		}
		status = m.spinner.View() + warnStyle.Render(" running"+elapsed)
	case opSucceeded:
		status = okStyle.Render("✓ done")
	case opFailed:
		status = errStyle.Render("✗ failed")
	}
	title := connStyle.Render("── ") + boldStyle.Render(m.outputTitle) + " " + status + " "
	title += connStyle.Render(
		strings.Repeat("─", max(0, m.width-lipgloss.Width(title)-12)),
	) + dimStyle.Render(
		" ! hide",
	)

	out := m.output
	if len(out) > outputPanelLines {
		out = out[len(out)-outputPanelLines:]
	}
	lines := []string{title}
	for _, l := range out {
		lines = append(lines, dimStyle.Render(l))
	}
	for len(lines) < outputPanelLines+1 {
		lines = append(lines, "")
	}
	return lines
}

// --- footer ---

func (m Model) footer() string {
	switch m.mode {
	case modePrompt:
		p := "gh stack add "
		if m.promptKind == promptCommand {
			p = "gh stack "
		}
		return keyStyle.Render(p) + m.input.View()
	case modeConfirm:
		return warnStyle.Render(m.confirm.text)
	case modeHelp:
		return dimStyle.Render("press any key to close help")
	}
	switch m.pendingKey {
	case "r":
		return keyStyle.Render(
			"rebase: ",
		) + hints(
			"r",
			"whole stack",
			"u",
			"upstack",
			"d",
			"downstack",
			"c",
			"continue",
			"a",
			"abort",
		)
	case "g":
		return keyStyle.Render("g") + dimStyle.Render("…")
	}
	if m.flash != "" {
		if m.flashErr {
			return errStyle.Render(m.flash)
		}
		return okStyle.Render(m.flash)
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
		mode := "SELECT"
		if m.visual {
			mode = "VISUAL"
		}
		return markStyle.Render(
			fmt.Sprintf("-- %s -- %d selected  ", mode, n),
		) + hints(
			"R",
			"review",
			"space",
			"mark",
			"v",
			"end visual",
			"esc",
			"clear",
		)
	}
	return hints(
		"j/k",
		"move",
		"l/h",
		"expand",
		"o",
		"open",
		"y",
		"copy",
		"c",
		"checkout",
		"p",
		"push",
		"s",
		"sync",
		"P",
		"prune",
		"R",
		"review",
		"?",
		"help",
	)
}

func hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+" "+dimStyle.Render(pairs[i+1]))
	}
	return strings.Join(parts, dimStyle.Render(" · "))
}

// --- help ---

func (m Model) helpLines() []string {
	var cols []string
	for _, s := range helpSections {
		var b strings.Builder
		b.WriteString(boldStyle.Render(s.title) + "\n")
		for _, k := range s.keys {
			fmt.Fprintf(&b, "%s %s\n", keyStyle.Render(fmt.Sprintf("%-10s", k[0])), k[1])
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
