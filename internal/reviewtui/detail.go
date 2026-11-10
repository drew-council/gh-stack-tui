package reviewtui

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
	"github.com/drew-council/gh-stack-tui/internal/ui"
)

// maxFailing is how many failing or running checks the detail pane names.
const maxFailing = 8

// detailTitle is the detail pane's title, and on its right whether a tab is
// reviewing the PR.
func (m Model) detailTitle() (title, info string) {
	s := m.st
	r, ok := m.hovered()
	if !ok {
		return "", ""
	}
	g := m.shown[r.group]
	switch r.kind {
	case rowStack:
		return s.Title.Render(fmt.Sprintf("Stack #%d", g.Stack.Number)), ""
	case rowFold:
		return s.Title.Render(fmt.Sprintf("Stack #%d", g.Stack.Number)) +
			s.Dim.Render(" · "+ui.Plural(r.count, "folded PR", "folded PRs")), ""
	}
	it := g.Items[r.item]
	title = s.Title.Render(fmt.Sprintf("#%d", it.Number))
	if _, open := m.tabs[it.Number]; open && m.opts.Tabs != nil {
		info = s.Bot.Render("▎open in a " + m.opts.Tabs.Noun())
	}
	return title, info
}

// detailLines is everything the detail pane says about the hovered row, w
// columns wide. The pane scrolls over it.
func (m Model) detailLines(w int) []string {
	r, ok := m.hovered()
	if !ok || w < 20 {
		return nil
	}
	g := m.shown[r.group]
	var lines []string
	switch r.kind {
	case rowStack:
		lines = m.stackDetail(g, g.Items, w)
	case rowFold:
		lines = m.itemList(g.Items[r.item:r.item+r.count], w)
	default:
		lines = m.prDetail(g, r.item, w)
	}
	out := []string{""}
	for _, l := range lines {
		out = append(out, " "+l)
	}
	return out
}

// rule heads a section of the detail pane.
func (m Model) rule(label string, w int) string {
	s := m.st
	head := s.Rule.Render("── ") + s.Dim.Render(label) + " "
	return head + s.Rule.Render(strings.Repeat("─", max(0, w-lipgloss.Width(head))))
}

// wrap breaks text into lines of at most w columns.
func wrap(text string, w int) []string {
	return strings.Split(ansi.Wrap(text, max(8, w), ""), "\n")
}

// prDetail describes one PR, in the order a reviewer asks: what it is, what
// it wants from them, who else has looked, whether it is ready, where it
// sits in its stack, and then the change itself.
func (m Model) prDetail(g review.Group, idx, w int) []string {
	s := m.st
	it := g.Items[idx]
	pr := it.PR
	w -= 2 // the pane's left and right margins
	if pr == nil {
		return []string{s.Dim.Render("GitHub did not return this pull request.")}
	}
	now := m.now()
	var out []string
	for _, l := range wrap(pr.Title, w) {
		out = append(out, s.Title.Render(l))
	}

	pill := s.PROpen.Render("OPEN")
	switch {
	case pr.Merged:
		pill = s.Pill(s.Muted).Render("MERGED")
	case pr.State == "CLOSED":
		pill = s.PRClosed.Render("CLOSED")
	case pr.Queued:
		pill = s.PRQueued.Render("QUEUED")
	case pr.IsDraft:
		pill = s.PRDraft.Render("DRAFT")
	}
	meta := []string{"opened " + ui.ShortAgo(now, pr.CreatedAt)}
	if !pr.PushedAt.IsZero() {
		meta = append(meta, "pushed "+ui.ShortAgo(now, pr.PushedAt))
	}
	out = append(out,
		pill+" "+s.Normal.Render(pr.Author)+s.Dim.Render(" · "+strings.Join(meta, " · ")))
	if lipgloss.Width(pr.HeadRef)+lipgloss.Width(pr.BaseRef)+3 <= w {
		out = append(out, s.Dim.Render(pr.HeadRef+" → ")+s.Normal.Render(pr.BaseRef))
	} else {
		out = append(out,
			s.Dim.Render(ui.TruncateLeft(pr.HeadRef, w)),
			s.Dim.Render("→ ")+s.Normal.Render(ui.TruncateLeft(pr.BaseRef, w-2)))
	}
	size := m.size(pr)
	stats := []string{
		ui.FilesLabel(size.files, size.generated), ui.Plural(pr.Commits, "commit", "commits"),
	}
	line := s.Dim.Render(strings.Join(stats, " · "))
	if d := s.DiffStat(size.add, size.del); d != "" {
		line = d + s.Dim.Render(" · ") + line
	}
	if pr.Conflicts {
		line += s.Dim.Render(" · ") + s.Err.Render("conflicts")
	}
	out = append(out, line)
	if len(pr.Labels) > 0 {
		out = append(out, s.Subtle.Render(strings.Join(pr.Labels, " · ")))
	}

	out = append(out, "", m.rule("you", w))
	out = append(out, m.youLines(it, w)...)

	if lines := m.reviewerLines(pr); len(lines) > 0 {
		out = append(out, "", m.rule("reviewers", w))
		out = append(out, lines...)
	}

	d, loaded := m.detailOf(pr)
	if pr.State == "OPEN" {
		if lines := m.checkLines(pr, d, w); len(lines) > 0 {
			out = append(out, "", m.rule("checks", w))
			out = append(out, lines...)
		}
	}

	if g.Stack != nil {
		out = append(out, "", m.rule(
			fmt.Sprintf("stack #%d · %d of %d", g.Stack.Number, len(g.Items)-idx, len(g.Items)), w,
		))
		if idx > 0 {
			out = append(out, m.neighbor("↑", g.Items[idx-1], w))
		}
		if idx+1 < len(g.Items) {
			out = append(out, m.neighbor("↓", g.Items[idx+1], w))
		} else {
			out = append(out, s.Dim.Render("↓ ")+s.Trunk.Render(g.Stack.Base))
		}
	}

	if g.Stack == nil && m.offTrunk(it) {
		out = append(out, "", m.rule("based on", w))
		if base, ok := m.find(it.Base); ok {
			out = append(out, m.neighbor("↓", base, w))
		} else {
			out = append(out, s.Dim.Render("↓ ")+s.Normal.Render(pr.BaseRef)+
				s.Dim.Render(", not "+m.queue.Trunk))
		}
	}

	out = append(out, "", m.rule("description", w))
	switch {
	case !loaded:
		out = append(out, m.pending(pr))
	case d.Body == "":
		out = append(out, s.Subtle.Render("No description."))
	default:
		out = append(out, m.markdown(d.Body, w)...)
	}

	if loaded && len(d.Files) == 0 {
		return out
	}
	out = append(out, "", m.rule(ui.FilesLabel(size.files, size.generated), w))
	if !loaded {
		return append(out, m.pending(pr))
	}
	// Generated files are counted, not listed: nobody reviews them.
	gen, genAdd, genDel := 0, 0, 0
	for _, f := range d.Files {
		if f.Generated {
			gen++
			genAdd += f.Additions
			genDel += f.Deletions
			continue
		}
		stat := s.DiffStat(f.Additions, f.Deletions)
		room := w - 2 - lipgloss.Width(stat) - 3
		out = append(out, ui.Fit(
			m.fileStatus(f.Status)+" "+s.Normal.Render(ui.TruncateLeft(f.Path, room)), stat, w+1,
		))
	}
	if gen > 0 {
		stat := strings.TrimSpace(fmt.Sprintf("+%d -%d", genAdd, genDel))
		out = append(out, ui.Fit(
			s.Subtle.Render("  "+ui.Plural(gen, "generated file", "generated files")),
			s.Subtle.Render(stat), w+1,
		))
	}
	if more := pr.ChangedFiles - len(d.Files); more > 0 {
		out = append(out, s.Subtle.Render(fmt.Sprintf("… %d more", more)))
	}
	return out
}

// detailOf returns the details of pr, and whether they are loaded. Details
// of an older version of the PR are shown until the new ones land.
func (m Model) detailOf(pr *github.ReviewPR) (*github.ReviewDetail, bool) {
	d, ok := m.details[pr.Number]
	return d.Detail, ok && d.Detail != nil
}

// pending stands in for details that are loading, or failed to.
func (m Model) pending(pr *github.ReviewPR) string {
	if _, loading := m.loadingDetails[pr.Number]; !loading && m.detailErr != nil {
		return m.st.Err.Render("could not load: " + ui.FirstLine(m.detailErr.Error()))
	}
	return m.spinner.View() + m.st.Dim.Render(" loading…")
}

// find returns the queue's item for PR number.
func (m Model) find(number int) (review.Item, bool) {
	for _, g := range m.groups {
		for _, it := range g.Items {
			if it.Number == number && number != 0 {
				return it, true
			}
		}
	}
	return review.Item{}, false
}

var (
	mdComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	mdImage   = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)|<img[^>]*>`)
	mdLink    = regexp.MustCompile(`\[([^\]]+)\]\((?:https?://|#|/)[^)]*\)`)
	mdTag     = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	mdHeading = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*$`)
	mdItem    = regexp.MustCompile(`^(\s*(?:[-*+]|\d+[.)])\s+(?:\[[ xX]\]\s+)?)`)
)

// markdown lays a PR description out for reading, w columns wide. It does
// not render Markdown: it drops what only a browser shows, sets headings
// apart, and wraps the rest, hanging list items under their text.
func (m Model) markdown(body string, w int) []string {
	s := m.st
	body = mdComment.ReplaceAllString(body, "")
	body = mdImage.ReplaceAllString(body, "[image]")
	body = mdLink.ReplaceAllString(body, "$1")
	body = mdTag.ReplaceAllString(body, "")
	var out []string
	fenced := false
	for _, l := range strings.Split(squeeze(strings.TrimSpace(body)), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			out = append(out, s.Dim.Render(ui.Pad("  "+strings.ReplaceAll(l, "\t", "  "), w)))
			continue
		}
		if h := mdHeading.FindStringSubmatch(l); h != nil {
			// Headings stand apart from the text above them.
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, s.Title.Render(ansi.Truncate(h[1], w, "…")))
			continue
		}
		if l == "" {
			out = append(out, "")
			continue
		}
		l = strings.ReplaceAll(l, "**", "")
		hang := lipgloss.Width(mdItem.FindString(l))
		for i, part := range wrap(l, w-hang) {
			if i > 0 || hang == 0 {
				part = strings.Repeat(" ", hang) + strings.TrimLeft(part, " ")
			}
			out = append(out, s.Normal.Render(part))
		}
	}
	return out
}

// squeeze drops blank lines that follow a blank line.
func squeeze(text string) string {
	var out []string
	blank := false
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		l = strings.TrimRight(l, " \t")
		if l == "" && blank {
			continue
		}
		blank = l == ""
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// youLines say what the PR wants from the viewer: the need, who asked whom
// and when, and what the viewer said last.
func (m Model) youLines(it review.Item, w int) []string {
	s := m.st
	pr := it.PR
	now := m.now()
	var out []string

	head := m.needLabel(it)
	switch {
	case !pr.Requested && it.Need == review.NeedNone:
		head = s.Dim.Render("not asked of you")
	case it.Why != "":
		head += s.Dim.Render(" · " + it.Why)
	case it.Hidden == review.HideDraft || it.Hidden == review.HideBot:
		head += s.Dim.Render(" · left out of the queue")
	case it.Hidden == review.HideWaiting:
		head += s.Dim.Render(" on the author · left out of the queue")
	}
	out = append(out, head)

	if pr.Requested {
		whom := strings.Join(pr.Via, ", ")
		if pr.Direct {
			whom = "you"
			if len(pr.Via) > 0 {
				whom += " and " + strings.Join(pr.Via, ", ")
			}
		}
		ask := "review asked of " + whom
		if pr.AskedBy != "" {
			ask = pr.AskedBy + " asked " + whom
		}
		if !pr.AskedAt.IsZero() {
			ask += " · " + ui.ShortAgo(now, pr.AskedAt)
		}
		for _, l := range wrap(ask, w) {
			out = append(out, s.Normal.Render(l))
		}
	}

	if mine := pr.Mine; mine != nil {
		said := map[string]string{
			"APPROVED":          "you approved",
			"CHANGES_REQUESTED": "you requested changes",
			"COMMENTED":         "you commented",
			"DISMISSED":         "your review was dismissed",
		}[mine.State]
		switch {
		case mine.State == "PENDING":
			out = append(out, s.Warn.Render("you have a review in progress"))
		case said != "":
			line := s.Normal.Render(said) + s.Dim.Render(" "+ui.ShortAgo(now, mine.At))
			if since := m.sinceReview(pr); since != "" {
				line += s.Dim.Render(" · ") + s.Warn.Render(since)
			}
			out = append(out, line)
		}
	}
	return out
}

// sinceReview says how the PR moved since the viewer reviewed it, or "".
func (m Model) sinceReview(pr *github.ReviewPR) string {
	if pr.Mine.Commit == "" || pr.Mine.Commit == pr.HeadSHA || pr.State != "OPEN" {
		return ""
	}
	if d, ok := m.detailOf(pr); ok {
		i := slices.IndexFunc(d.Commits, func(c github.PRCommit) bool {
			return c.SHA == pr.Mine.Commit
		})
		if i > 0 {
			return ui.Plural(i, "commit", "commits") + " since"
		}
	}
	// The reviewed commit is gone from the branch, or too far back.
	return "changed since"
}

// reviewerLines list the other reviewers: those who gave a verdict, then
// those still asked.
func (m Model) reviewerLines(pr *github.ReviewPR) []string {
	s := m.st
	now := m.now()
	var out []string
	for _, r := range pr.Reviews {
		switch r.State {
		case "APPROVED":
			out = append(out, s.OK.Render("✓ ")+s.Normal.Render(r.Login)+
				s.Dim.Render(" approved "+ui.ShortAgo(now, r.At)))
		case "CHANGES_REQUESTED":
			out = append(out, s.Err.Render("✗ ")+s.Normal.Render(r.Login)+
				s.Dim.Render(" requested changes "+ui.ShortAgo(now, r.At)))
		}
	}
	for _, u := range pr.RequestedUsers {
		out = append(out, s.Subtle.Render("○ ")+s.Normal.Render(u)+s.Dim.Render(" asked"))
	}
	for _, t := range pr.RequestedTeams {
		if !slices.Contains(pr.Via, t) {
			out = append(out, s.Subtle.Render("○ ")+s.Normal.Render(t)+s.Dim.Render(" team asked"))
		}
	}
	return out
}

// checkLines summarize CI, name the checks that are failing or running, and
// count the open review threads.
func (m Model) checkLines(pr *github.ReviewPR, d *github.ReviewDetail, w int) []string {
	s := m.st
	var out []string
	if d == nil || len(d.Checks.Workflows) == 0 {
		word := map[github.CheckState]string{
			github.CheckSuccess: "passing", github.CheckFailure: "failing",
			github.CheckPending: "running",
		}[pr.Checks]
		if word != "" {
			out = append(out, s.CheckGlyph(pr.Checks)+" "+s.Normal.Render(word))
		}
	} else {
		var all []github.Check
		for _, wf := range d.Checks.Workflows {
			all = append(all, wf.Checks...)
		}
		out = append(out, s.CheckCounts(all))
		named := 0
		for _, wf := range d.Checks.Workflows {
			for _, c := range wf.Checks {
				if c.State != github.CheckFailure && c.State != github.CheckPending {
					continue
				}
				if named++; named > maxFailing {
					continue
				}
				name := c.Name
				if wf.Name != "" && wf.Name != c.Name {
					name = wf.Name + " / " + c.Name
				}
				out = append(out, ui.Pad(s.CheckGlyph(c.State)+" "+s.Normal.Render(name), w))
			}
		}
		if named > maxFailing {
			out = append(out, s.Subtle.Render(fmt.Sprintf("… %d more", named-maxFailing)))
		}
	}
	if d != nil && d.Unresolved+d.UnresolvedBots > 0 {
		var notes []string
		if d.Unresolved > 0 {
			notes = append(notes, s.Warn.Render(
				ui.Plural(d.Unresolved, "unresolved thread", "unresolved threads")))
		}
		if d.UnresolvedBots > 0 {
			notes = append(notes, s.Bot.Render(
				ui.Plural(d.UnresolvedBots, "bot thread", "bot threads")))
		}
		out = append(out, strings.Join(notes, s.Dim.Render(" · ")))
	}
	return out
}

// neighbor is the PR above or below in the stack, with where it stands.
func (m Model) neighbor(arrow string, it review.Item, w int) string {
	s := m.st
	left := s.Dim.Render(arrow+" ") + s.PRNum.Render(fmt.Sprintf("#%d", it.Number))
	if it.PR == nil {
		return left
	}
	right := s.Dim.Render(state(it))
	if it.ForYou() {
		right = m.needLabel(it)
	}
	return ui.Fit(left+" "+s.Normal.Render(it.PR.Title), right, w+1)
}

// stackDetail describes a stack: whose it is, how much of it waits on the
// viewer, and every PR in it.
func (m Model) stackDetail(g review.Group, items []review.Item, w int) []string {
	s := m.st
	w -= 2
	out := []string{
		s.Title.Render(g.Author) + s.Dim.Render(fmt.Sprintf(" · %s on %s",
			ui.Plural(len(items), "PR", "PRs"), g.Stack.Base)),
	}
	n, add, del, files := 0, 0, 0, 0
	for _, it := range items {
		if it.ForYou() {
			size := m.size(it.PR)
			n++
			add += size.add
			del += size.del
			files += size.files
		}
	}
	if n > 0 {
		line := s.Key.Render(ui.Plural(n, "PR waits", "PRs wait")+" on you") + s.Dim.Render(" · ")
		if d := s.DiffStat(add, del); d != "" {
			line += d + s.Dim.Render(" in ")
		}
		out = append(out, line+s.Dim.Render(ui.Plural(files, "file", "files")))
		if n > 1 {
			out = append(
				out,
				s.Dim.Render(fmt.Sprintf("review from the bottom: start at #%d", g.Start())),
			)
		}
	}
	out = append(out, "", m.rule("pull requests", w))
	return append(out, m.itemList(items, w+2)...)
}

// itemList lists PRs one per line, top of the stack first.
func (m Model) itemList(items []review.Item, w int) []string {
	s := m.st
	w -= 2
	var out []string
	for _, it := range items {
		_, marked := m.marks[it.Number]
		left := m.bullet(it, marked) + " " + s.PRNum.Render(fmt.Sprintf("#%d", it.Number))
		if it.PR == nil {
			out = append(out, left+" "+s.Subtle.Render("not available"))
			continue
		}
		ink, right := s.Dim, s.Dim.Render(state(it))
		if it.ForYou() {
			ink, right = s.Normal, m.needLabel(it)
		}
		out = append(out, ui.Fit(left+" "+ink.Render(it.PR.Title), right, w+1))
	}
	return out
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
	}
	return s.Dim.Render("M")
}
