package stacktui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/ui"
)

// outputPanelLines is the number of output lines shown under the stack, on a
// screen tall enough for them.
const outputPanelLines = 8

// Columns of a stack line, counted from the panel's inner left edge. The tree
// runs down colTree and content starts at colContent. Items nest indentStep
// columns per level. Values on the right end one column short of the border.
const (
	colTree    = 1
	colContent = 3
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
	edge := m.st.Rule
	switch m.mode {
	case modeHelp:
		title, body = m.st.Title.Render("Keys"), m.helpLines()
		top := m.helpTop(len(body), h)
		foot = m.scrollHint(top, len(body), h)
		body = body[top:]
	case modeReviewers:
		title, body = m.st.Title.Render("Request reviewers"), m.pickerLines(h)
	default:
		title, info = m.stackTitle()
		body = m.bodyLines()
		total := len(body)
		if m.scroll < len(body) {
			body = body[m.scroll:min(len(body), m.scroll+h)]
		} else {
			body = nil
		}
		foot = m.scrollHint(m.scroll, total, h)
		if m.visual {
			edge = m.st.Selection
		}
	}
	if len(body) > h {
		body = body[:h]
	}
	lines = append(lines, m.panel(edge, title, info, foot, body, h)...)
	if m.showOutput {
		lines = append(lines, m.outputLines()...)
	}
	lines = append(lines, m.footer()...)

	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}
	return strings.Join(lines, "\n")
}

// innerWidth is the width inside a panel's border.
func (m Model) innerWidth() int { return max(10, m.width-2) }

func (m Model) bodyHeight() int {
	h := m.height - len(m.headerLines()) - 2 - len(m.footer())
	if m.showOutput {
		h -= m.outputHeight() + 2
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

// panel frames body in a border as wide as the screen; see [ui.Panel].
func (m Model) panel(edge lipgloss.Style, title, info, foot string, body []string, h int) []string {
	return ui.Panel(m.width, edge, title, info, foot, body, h)
}

// scrollHint counts the lines above and below a view of h lines, starting at
// top, into total.
func (m Model) scrollHint(top, total, h int) string {
	return m.st.ScrollHint(top, total, h)
}

// --- header ---

// stackTitle is the stack panel's title and the refresh state on its right.
func (m Model) stackTitle() (title, info string) {
	s := m.st
	title = s.Title.Render("gh stack")
	if snap := m.snap; snap != nil && snap.Index >= 0 {
		title = ""
		if snap.Number != 0 {
			title = s.Title.Render(fmt.Sprintf("#%d", snap.Number)) + " "
		}
		title += s.Dim.Render("on ") + s.Normal.Render(snap.Trunk)
		if snap.StackCount > 1 {
			title += s.Dim.Render(fmt.Sprintf(" · %d/%d", snap.Index+1, snap.StackCount))
		}
		if m.pinned != "" {
			title += s.Dim.Render(" · pinned")
		}
	}
	now := m.now()
	switch {
	case m.loadingRemote:
		info = m.spinner.View() + s.Dim.Render(" refreshing")
		if !m.cachedAt.IsZero() {
			info += s.Dim.Render(" · cached " + ui.ShortAgo(now, m.cachedAt))
		}
	case !m.cachedAt.IsZero():
		info = s.Dim.Render("cached " + ui.ShortAgo(now, m.cachedAt))
	case !m.lastRemote.IsZero():
		info = s.Dim.Render("↻ " + ui.ShortAgo(now, m.lastRemote))
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
				s.Err.Render("rebase in progress")+s.Dim.Render("  rc continue · ra abort"),
			)
		} else if snap.CurrentBranch == "" {
			notes = append(notes, s.Warn.Render("detached HEAD"))
		}
		if snap.Dirty {
			notes = append(notes, s.Warn.Render("uncommitted changes"))
		}
		if len(notes) > 0 {
			lines = append(lines, " "+strings.Join(notes, s.Dim.Render(" · ")))
		}
	}
	if m.remoteErr != nil {
		lines = append(lines, " "+s.Err.Render("GitHub: "+ui.FirstLine(m.remoteErr.Error())))
	}
	return lines
}

// --- body ---

func (m Model) bodyLines() []string {
	s := m.st
	switch {
	case m.localErr != nil:
		return []string{" " + s.Err.Render("error: "+m.localErr.Error())}
	case m.snap == nil:
		return []string{" " + m.spinner.View() + s.Dim.Render(" loading stack…")}
	case m.snap.Index < 0:
		return []string{
			" " + s.Dim.Render("No stacks in this repository."),
			" " + s.Dim.Render("Create one with ") + s.Normal.Render("gh stack init") +
				s.Dim.Render(", or press : to run a gh stack command."),
		}
	}
	hb := m.hoveredBranch()
	var lines []string
	for i, r := range m.rows {
		active := r.branch >= 0 && r.branch == hb
		bg := m.rowBg(i, active)
		for _, l := range m.renderRow(r, active) {
			if bg == s.CursorBg || bg == s.SelCursorBg {
				l = s.Lift(l)
			}
			lines = append(lines, ui.Shade(l, bg))
		}
	}
	return lines
}

// A branch's colors follow from three facts about it: whether it is selected
// for review, where the cursor is, and its own state. Each mark answers one
// question, so the facts never compete:
//
//   - Background, where is the cursor and what is selected: the cursor's row,
//     or the whole branch when the cursor is on the branch, is gray, and
//     brighter mauve when the branch is selected. The rest of the hovered
//     branch is a faint gray, and other selected branches are mauve.
//   - Bullet shape, what the branch is: ✓ merged, ◎ queued, ● checked out,
//     ○ other. Selected branches become diamonds, ◆ checked out, ◇ other.
//   - Bullet color: mauve when selected. Otherwise blue when checked out,
//     peach when queued, green when merged, and gray.
//   - Rail: peach and dashed when the branch needs a rebase, which outranks
//     everything. Otherwise mauve when selected, blue when hovered, and gray.
//   - Name: blue when checked out.
//
// Mauve means selected and nothing else.

// rowBg is the background of row i.
func (m Model) rowBg(i int, active bool) color.Color {
	s := m.st
	r := m.rows[i]
	if r.branch < 0 {
		return nil
	}
	selected := m.isSelected(r.branch, m.snap.Branches[r.branch].Name)
	cursor := i == m.cursor && m.navigable(i) ||
		active && m.rows[m.cursor].kind == rowBranch
	switch {
	case cursor && selected:
		return s.SelCursorBg
	case cursor:
		return s.CursorBg
	case selected:
		return s.SelBg
	case active:
		return s.BlockBg
	}
	return nil
}

// parts is a line's left-aligned content and its right-aligned value.
type parts struct{ left, right string }

// connector returns branch bi's rail and its style.
func (m Model) connector(bi int, active bool) (string, lipgloss.Style) {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	switch {
	case b.NeedsRebase && !m.merged(bi) && (pr == nil || !pr.Queued):
		return ui.GlyphDashed, s.ConnDashed
	case m.isSelected(bi, b.Name):
		return ui.GlyphConn, s.Selection
	case active:
		return ui.GlyphConn, s.ConnActive
	}
	return ui.GlyphConn, s.Conn
}

// bullet is branch bi's mark on the tree.
func (m Model) bullet(bi int) string {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	switch {
	case m.merged(bi):
		return s.BulletMerged.Render(ui.GlyphMerged)
	case m.isSelected(bi, b.Name) && b.IsCurrent:
		return s.Selection.Render(ui.GlyphSelectedCurrent)
	case m.isSelected(bi, b.Name):
		return s.Selection.Render(ui.GlyphSelected)
	case b.IsCurrent:
		return s.BulletCurrent.Render(ui.GlyphCurrent)
	case pr != nil && pr.Queued:
		return s.BulletQueued.Render(ui.GlyphQueued)
	}
	return s.Dim.Render(ui.GlyphBranch)
}

// renderRow renders one row as lines exactly the panel's inner width. active
// is true for the rows of the branch the cursor is on or inside.
func (m Model) renderRow(r row, active bool) []string {
	s := m.st
	w := m.innerWidth()
	lead := strings.Repeat(" ", colTree)
	switch r.kind {
	case rowSeparator:
		head := lead + s.Rule.Render("── ") + s.Dim.Render(r.label) + " "
		return []string{
			ui.Pad(head+s.Rule.Render(strings.Repeat("─", max(0, w-1-lipgloss.Width(head)))), w),
		}
	case rowTrunk:
		return []string{ui.Pad(lead+s.Conn.Render(ui.GlyphTrunk)+" "+s.Trunk.Render(r.label), w)}
	case rowBranch:
		return m.renderBranch(r, active)
	}

	conn, cs := m.connector(r.branch, active)
	lead += cs.Render(conn)
	if r.kind == rowSpacer {
		return []string{ui.Pad(lead, w)}
	}
	indent := func(level int) string { return lead + " " + strings.Repeat(" ", level*indentStep) }
	toggle := func(key string) string {
		if m.expanded[key] {
			return ui.GlyphExpanded
		}
		return ui.GlyphCollapsed
	}

	switch r.kind {
	case rowSection:
		style := s.Subtle
		if m.expanded[r.key] {
			style = s.Normal
		}
		return []string{ui.Fit(indent(0)+style.Render(toggle(r.key)+" "+r.label), "", w)}

	case rowFile:
		f := r.file
		level := 1
		if f.Generated {
			level = 2
		}
		stat := m.st.DiffStat(f.Additions, f.Deletions)
		if f.Binary {
			stat = s.Dim.Render("binary")
		}
		room := w - colContent - level*indentStep - 2 - lipgloss.Width(stat) - 3
		return []string{ui.Fit(indent(level)+m.fileStatus(f.Status)+" "+
			s.Normal.Render(ui.TruncateLeft(f.Path, room)), stat, w)}

	case rowGenerated:
		text := ui.Plural(r.count, "generated file", "generated files")
		return []string{ui.Fit(indent(1)+s.Dim.Render(toggle(r.key)+" "+text), "", w)}

	case rowCommit:
		c := r.commit
		return []string{ui.Fit(
			indent(1)+s.SHA.Render(c.SHA[:min(7, len(c.SHA))])+" "+s.Normal.Render(c.Subject),
			s.Dim.Render(ui.ShortAgo(m.now(), c.Time)), w,
		)}

	case rowWorkflow:
		wf := r.workflow
		return []string{ui.Fit(
			indent(1)+s.Subtle.Render(toggle(r.key))+" "+s.CheckGlyph(wf.State)+" "+
				s.Normal.Render(wf.Name),
			m.st.CheckCounts(wf.Checks), w,
		)}

	case rowCheck:
		c := r.check
		return []string{
			ui.Fit(indent(2)+"  "+s.CheckGlyph(c.State)+" "+s.Normal.Render(c.Name), "", w),
		}
	}
	return nil
}

func (m Model) fileStatus(status string) string {
	s := m.st
	switch status {
	case "A":
		return s.OK.Render("A")
	case "D":
		return s.Err.Render("D")
	case "R", "C":
		return s.Prompt.Render(status)
	case "":
		return s.Dim.Render(" ")
	}
	return s.Dim.Render(status[:1])
}

func (m Model) renderBranch(r row, active bool) []string {
	w := m.innerWidth()
	conn, cs := m.connector(r.branch, active)
	lead := strings.Repeat(" ", colTree)
	var lines []string
	for i, p := range m.branchParts(r.branch) {
		mark := cs.Render(conn)
		if i == 0 {
			mark = m.bullet(r.branch)
		}
		lines = append(lines, ui.Fit(lead+mark+" "+p.left, p.right, w))
	}
	return lines
}

// branchParts lays out the lines of branch bi's header. The left column
// reads like a sentence: the branch, then its PR with its state and title,
// then the branch's own state and where review stands. The right
// column holds CI, then the diff. A merged branch takes one line and any other
// branch three, however long its text, so headers never wrap.
func (m Model) branchParts(bi int) []parts {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	num := m.prNumber(bi)

	if m.merged(bi) {
		left := s.BranchMerged.Render(b.Name)
		if num != 0 {
			left += "  " + s.PRNum.Render(fmt.Sprintf("#%d", num))
			if pr != nil && pr.Title != "" {
				left += " " + s.Dim.Render(pr.Title)
			}
		}
		return []parts{{left, s.Dim.Render("merged")}}
	}

	name := s.Branch.Render(b.Name)
	if b.IsCurrent {
		name = s.BranchCurrent.Render(b.Name)
	}
	var diff string
	switch {
	case b.Head == "":
		diff = s.Dim.Render("no local branch")
	case b.Additions > 0 || b.Deletions > 0:
		diff = m.st.DiffStat(b.Additions, b.Deletions)
	}

	var title string
	if num != 0 {
		title = s.PRNum.Render(fmt.Sprintf("#%d", num)) + " "
	}
	switch label := m.prLabel(bi); {
	case label != "":
		title += label
		if pr != nil && pr.Title != "" {
			title += " " + s.Normal.Render(pr.Title)
		}
	case num == 0:
		title = s.Subtle.Render("no pull request")
	}

	return []parts{
		{name, m.ciState(bi)},
		{title, diff},
		{m.attention(bi), ""},
	}
}

// prLabel is the state of branch bi's PR as a colored word: OPEN, DRAFT,
// QUEUED, or CLOSED. A PR still loading shows the spinner.
func (m Model) prLabel(bi int) string {
	s := m.st
	pr := m.prFor(bi)
	if pr == nil {
		if m.loadingRemote || (m.lastRemote.IsZero() && m.remoteErr == nil) {
			return m.spinner.View()
		}
		return ""
	}
	switch {
	case pr.Queued:
		return s.PRQueued.Render("QUEUED")
	case pr.State == "CLOSED":
		return s.PRClosed.Render("CLOSED")
	case pr.IsDraft:
		return s.PRDraft.Render("DRAFT")
	}
	return s.PROpen.Render("OPEN")
}

// review is the review decision on branch bi's PR, when it is open and
// ready for review.
func (m Model) review(bi int) string {
	s := m.st
	pr := m.prFor(bi)
	if pr == nil || pr.State != "OPEN" || pr.IsDraft {
		return ""
	}
	switch pr.Review() {
	case github.ReviewApproved:
		return s.OK.Render("approved")
	case github.ReviewChangesRequested:
		return s.Err.Render("changes requested")
	}
	return s.Dim.Render("in review")
}

// attention is the state of branch bi, then where its review stands: a
// rebase or merge conflicts first, then the review decision and open review
// threads.
func (m Model) attention(bi int) string {
	s := m.st
	b := m.snap.Branches[bi]
	pr := m.prFor(bi)
	var notes []string
	if b.NeedsRebase && (pr == nil || !pr.Queued) {
		notes = append(notes, s.Warn.Render("needs rebase"))
	}
	if pr != nil && pr.State == "OPEN" {
		if !pr.Queued {
			switch pr.MergeState {
			case "DIRTY":
				notes = append(notes, s.Err.Render("conflicts"))
			case "BEHIND":
				notes = append(notes, s.Warn.Render("behind base"))
			}
		}
		if review := m.review(bi); review != "" {
			notes = append(notes, review)
		}
		if pr.Unresolved > 0 {
			notes = append(notes, s.Warn.Render(fmt.Sprintf("%d unresolved", pr.Unresolved)))
		}
		if pr.UnresolvedBots > 0 {
			notes = append(notes, s.Bot.Render(fmt.Sprintf("%d bot", pr.UnresolvedBots)))
		}
	}
	return strings.Join(notes, s.Dim.Render(" · "))
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
		out = append(out, s.Err.Render(fmt.Sprintf("✗ %d failing", n)))
	}
	if n := counts[github.CheckPending]; n > 0 {
		out = append(out, s.Warn.Render(fmt.Sprintf("● %d running", n)))
	}
	if len(out) > 0 {
		return strings.Join(out, " ")
	}
	switch pr.Checks.State {
	case github.CheckCancelled:
		return s.Dim.Render("⊘ cancelled")
	case github.CheckSkipped:
		return s.Dim.Render("- skipped")
	}
	return s.OK.Render("✓ passing")
}

// section is one of a branch's files, commits, or checks, with its label.
type section struct{ name, label string }

// sections are the sections branch bi has, in the order they show.
func (m Model) sections(bi int) []section {
	b := m.snap.Branches[bi]
	if m.merged(bi) {
		return nil
	}
	var out []section
	if len(b.Files) > 0 {
		n := 0
		for _, f := range b.Files {
			if !f.Generated {
				n++
			}
		}
		label := ui.Plural(n, "file", "files")
		switch gen := len(b.Files) - n; {
		case n == 0:
			label = ui.Plural(gen, "generated file", "generated files")
		case gen > 0:
			label += fmt.Sprintf(" + %d generated", gen)
		}
		out = append(out, section{secFiles, label})
	}
	if len(b.Commits) > 0 {
		out = append(out, section{secCommits, ui.Plural(len(b.Commits), "commit", "commits")})
	}
	if pr := m.prFor(bi); pr != nil && len(pr.Checks.Workflows) > 0 {
		n := 0
		for _, w := range pr.Checks.Workflows {
			n += len(w.Checks)
		}
		out = append(out, section{secChecks, ui.Plural(n, "check", "checks")})
	}
	return out
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
		status = m.spinner.View() + s.Warn.Render(" running"+elapsed)
		if n := len(m.queue); n > 0 {
			status += s.Dim.Render(fmt.Sprintf(" · %d queued", n))
		}
	case opSucceeded:
		status = s.OK.Render("✓ done")
	case opFailed:
		status = s.Err.Render("✗ failed")
	case opCancelled:
		status = s.Warn.Render("✗ cancelled")
	}
	title := s.Title.Render(m.outputTitle) + " " + status
	hide := s.Key.Render("!") + s.KeyDesc.Render(" hide")

	var body []string
	for _, l := range m.outputText() {
		body = append(body, " "+s.Dim.Render(l))
	}
	return m.panel(s.Rule, title, hide, "", body, m.outputHeight())
}

// outputHeight is the output panel's height inside its border. It stays the
// same while output streams in, and shrinks on short screens so the stack
// keeps most of the room.
func (m Model) outputHeight() int {
	return max(2, min(outputPanelLines, m.height/4))
}

// outputText is the end of the output, wrapped to fit the panel.
func (m Model) outputText() []string {
	// Long lines wrap, since errors are what the panel is for.
	var out []string
	for _, l := range m.output {
		first, rest, _ := strings.Cut(ansi.Wrap(l, m.innerWidth()-2, " /"), "\n")
		out = append(out, first)
		// Continuations hang under the line's text.
		if rest != "" {
			rest = ansi.Wrap(strings.ReplaceAll(rest, "\n", " "), m.innerWidth()-4, " /")
			for _, r := range strings.Split(rest, "\n") {
				out = append(out, "  "+r)
			}
		}
	}
	if h := m.outputHeight(); len(out) > h {
		out = out[len(out)-h:]
	}
	return out
}

// --- footer ---

// footer is the line under the panels: key hints, a prompt, or a flash. A
// question, such as a confirm, takes two lines or more: the question, then
// every answer.
func (m Model) footer() []string {
	question, keys := "", []string(nil)
	switch {
	case m.mode == modeConfirm:
		question, keys = m.confirm.text, m.confirm.keys
	case m.mode == modeNormal && m.pendingKey == "r":
		question = "rebase:"
		keys = []string{
			"r", "whole stack", "u", "upstack", "d", "downstack", "c", "continue", "a", "abort",
		}
	}
	if question != "" {
		s := m.st
		w := m.width - 2
		var lines []string
		for _, l := range strings.Split(ansi.Wrap(question, w, " "), "\n") {
			lines = append(lines, " "+s.Warn.Render(l))
		}
		var line string
		for i := 0; i+1 < len(keys); i += 2 {
			h := s.Key.Render(keys[i]) + s.KeyDesc.Render(" "+keys[i+1])
			if line != "" && lipgloss.Width(line)+2+lipgloss.Width(h) > w {
				lines = append(lines, " "+line)
				line = ""
			}
			if line != "" {
				line += "  "
			}
			line += h
		}
		return append(lines, " "+line)
	}
	return []string{" " + m.footerText()}
}

func (m Model) footerText() string {
	s := m.st
	switch m.mode {
	case modePrompt:
		p := "gh stack add "
		if m.promptKind == promptCommand {
			p = "gh stack "
		}
		return s.Prompt.Render(p) + m.input.View()
	case modeHelp:
		return m.hints("j/k", "scroll", "ctrl+d/u", "half page", "g/G", "top/end", "esc", "close")
	case modeReviewers:
		if m.picker != nil && !m.picker.ready {
			return m.hints("↑/↓", "move", "tab", "pick", "enter", "request", "esc", "skip")
		}
		return m.hints(
			"tab", "pick", "enter", "request + ready", "ctrl+s", "ready only", "esc", "keep draft",
		)
	}
	switch m.pendingKey {
	case "g":
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
		lead := s.Selected.Render(mode) + s.Dim.Render(fmt.Sprintf(" %d  ", n))
		return lead + m.hintsIn(m.width-2-lipgloss.Width(lead),
			"R", "review", "D", "draft/ready", "A", "reviewers",
			"space", "mark", "v", "visual", "esc", "clear",
		)
	}
	if r, ok := m.hovered(); ok && r.item() {
		// l only goes in from a section or group.
		in := []string{"l", "in"}
		if r.kind != rowSection && r.kind != rowWorkflow && r.kind != rowGenerated {
			in = nil
		}
		pairs := append([]string{"j/k", "items"}, in...)
		return m.hints(append(pairs,
			"h", "out", "o", "open", "y", "copy", "e", "edit", "?", "help")...)
	}
	return m.hints(
		"j/k", "move", "l", "in", "f/C/x", "files/commits/checks", "o", "browser",
		"c", "checkout", "p", "push", "s", "sync", "R", "review", "?", "help",
	)
}

func (m Model) hints(pairs ...string) string {
	return m.hintsIn(m.width-2, pairs...)
}

func (m Model) hintsIn(width int, pairs ...string) string {
	return m.st.Hints(width, pairs...)
}

// --- help ---

// helpTop is the first help line shown, kept so the last line can reach the
// bottom of the panel.
func (m Model) helpTop(total, h int) int {
	return max(0, min(m.helpScroll, total-h))
}

func (m Model) helpLines() []string {
	return m.st.HelpLines(helpSections, m.innerWidth()-2)
}
