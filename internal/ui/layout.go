package ui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Panel frames body, padded to h lines, in a rounded border width columns
// wide and drawn in edge. title and info sit on the top border, foot on the
// bottom one.
func Panel(
	width int,
	edge lipgloss.Style,
	title, info, foot string,
	body []string,
	h int,
) []string {
	w := max(10, width-2)
	side := edge.Render(GlyphConn)
	lines := []string{BorderLine(width, edge, title, info, "╭", "╮")}
	for i := range h {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		lines = append(lines, side+Pad(l, w)+side)
	}
	return append(lines, BorderLine(width, edge, "", foot, "╰", "╯"))
}

// BorderLine is a horizontal border width columns wide, between the corners
// lc and rc, with left and right labels set into it.
func BorderLine(width int, edge lipgloss.Style, left, right, lc, rc string) string {
	if left != "" {
		left = " " + left + " "
	}
	if right != "" {
		right = " " + right + " "
	}
	fill := width - 4 - lipgloss.Width(left) - lipgloss.Width(right)
	if fill < 1 && right != "" {
		fill += lipgloss.Width(right)
		right = ""
	}
	if fill < 1 {
		left = ansi.Truncate(left, max(0, width-5), "…")
		fill = width - 4 - lipgloss.Width(left)
	}
	return edge.Render(lc+"─") + left + edge.Render(strings.Repeat("─", max(0, fill))) +
		right + edge.Render("─"+rc)
}

// Pad cuts or pads l to exactly w columns.
func Pad(l string, w int) string {
	l = ansi.Truncate(l, w, "…")
	return l + strings.Repeat(" ", max(0, w-lipgloss.Width(l)))
}

// PadLeft cuts l to w columns, or pads it on the left to w columns.
func PadLeft(l string, w int) string {
	l = ansi.Truncate(l, w, "…")
	return strings.Repeat(" ", max(0, w-lipgloss.Width(l))) + l
}

// Fit lays out left and right on one line w columns wide. right ends one
// column short of the edge, and left is cut to keep two columns between
// them. On a line too narrow for both, right is dropped.
func Fit(left, right string, w int) string {
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
		return Pad(left, w)
	}
	return left + strings.Repeat(" ", w-1-lipgloss.Width(left)-rw) + right + " "
}

// Spread puts left and right at the two ends of a line width columns wide.
// right is dropped when both do not fit.
func Spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// ScrollHint counts the lines above and below a view of h lines, starting at
// top, into total.
func (s Styles) ScrollHint(top, total, h int) string {
	if total <= h {
		return ""
	}
	var parts []string
	if top > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", top))
	}
	if below := total - top - h; below > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", below))
	}
	return s.Dim.Render(strings.Join(parts, " "))
}

// Hints renders the key and label pairs that fit in width, in order. The last
// pair, usually help, is always kept.
func (s Styles) Hints(width int, pairs ...string) string {
	render := func(i int) string {
		return s.Key.Render(pairs[i]) + s.KeyDesc.Render(" "+pairs[i+1])
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

// HelpSection groups key bindings on a help screen.
type HelpSection struct {
	Title string
	Keys  [][2]string
}

// HelpLines lays sections out in as many columns as fit in width.
func (s Styles) HelpLines(sections []HelpSection, width int) []string {
	var cols []string
	for _, sec := range sections {
		var b strings.Builder
		b.WriteString(s.Title.Render(sec.Title) + "\n")
		for _, k := range sec.Keys {
			fmt.Fprintf(
				&b,
				"%s %s\n",
				s.Key.Render(fmt.Sprintf("%-10s", k[0])),
				s.KeyDesc.Render(k[1]),
			)
		}
		cols = append(cols, lipgloss.NewStyle().MarginRight(3).Render(b.String()))
	}
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
		out = append(out, " "+l)
	}
	return out
}

// Plural is n and the noun that agrees with it.
func Plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// FilesLabel counts changed files, the generated ones apart: "5 files + 1
// generated".
func FilesLabel(written, generated int) string {
	switch {
	case written == 0 && generated > 0:
		return Plural(generated, "generated file", "generated files")
	case generated > 0:
		return Plural(written, "file", "files") + fmt.Sprintf(" + %d generated", generated)
	}
	return Plural(written, "file", "files")
}

// TruncateLeft keeps the tail of a path, which is the distinctive part.
func TruncateLeft(s string, maxLen int) string {
	maxLen = max(20, maxLen)
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return "…" + string(r[len(r)-maxLen+1:])
}

// Age is how long before now t was, in its largest unit: 40s, 12m, 3h, 2d, 5mo.
func Age(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo", max(1, int(d.Hours()/24/30)))
	}
}

// ShortAgo is [Age] followed by "ago".
func ShortAgo(now, t time.Time) string { return Age(now, t) + " ago" }

// FirstLine is s up to its first line break.
func FirstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
