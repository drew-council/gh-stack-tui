// Package ui is the look shared by the TUIs: the palette, the styles built
// from it, and the layout of panels, lines, and key hints.
package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	catppuccin "github.com/catppuccin/go"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// Styles is the palette and the styles built from it. Colors come from
// Catppuccin, Mocha on dark terminals and Latte on light ones, so the TUIs
// match other Catppuccin-themed tools. Color marks state that needs
// attention; everything else is primary or muted ink.
type Styles struct {
	// Dark is set for Mocha and unset for Latte.
	Dark   bool
	flavor catppuccin.Flavor

	Text, Muted, Faint, Border, Accent color.Color
	Green, Yellow, Peach, Mauve        color.Color
	Red, Teal, Draft, OnFill           color.Color

	// Backgrounds: the rest of the hovered block, the cursor's row, a
	// selected row, and the cursor on a selected row.
	BlockBg, CursorBg, SelBg, SelCursorBg color.Color

	Normal lipgloss.Style // primary ink
	Dim    lipgloss.Style // secondary ink: labels, hints, timestamps
	Subtle lipgloss.Style // tertiary ink: counts of collapsed sections

	// Tree
	Conn       lipgloss.Style // connector lines
	ConnDashed lipgloss.Style // needs-rebase connector
	ConnActive lipgloss.Style // connector of the hovered branch

	Branch        lipgloss.Style
	BranchCurrent lipgloss.Style
	BranchMerged  lipgloss.Style
	Trunk         lipgloss.Style
	PRNum         lipgloss.Style

	// PR state labels: OPEN, DRAFT, QUEUED, CLOSED.
	PROpen, PRDraft, PRQueued, PRClosed lipgloss.Style

	BulletCurrent lipgloss.Style
	BulletMerged  lipgloss.Style
	BulletQueued  lipgloss.Style

	Selection lipgloss.Style // marks of a row selected for review

	Add lipgloss.Style
	Del lipgloss.Style
	SHA lipgloss.Style

	OK   lipgloss.Style
	Warn lipgloss.Style
	Err  lipgloss.Style
	Bot  lipgloss.Style // comments from bots such as Copilot

	Selected lipgloss.Style // pill for the selection mode
	Note     lipgloss.Style // flash that is neither success nor error
	Match    lipgloss.Style // fuzzy-matched characters

	// Chrome
	Title    lipgloss.Style
	Key      lipgloss.Style
	KeyDesc  lipgloss.Style
	Rule     lipgloss.Style
	Prompt   lipgloss.Style
	FlashOK  lipgloss.Style
	FlashErr lipgloss.Style
}

// NewStyles returns the styles of every TUI, in Mocha when dark and in
// Latte otherwise.
func NewStyles(dark bool) Styles {
	f := catppuccin.Latte
	if dark {
		f = catppuccin.Mocha
	}
	c := func(col catppuccin.Color) color.Color { return lipgloss.Color(col.Hex) }
	s := Styles{
		Dark:     dark,
		flavor:   f,
		Text:     c(f.Text()),
		Muted:    c(f.Subtext0()),
		Faint:    c(f.Overlay1()),
		Border:   c(f.Surface2()),
		Accent:   c(f.Blue()),
		Green:    c(f.Green()),
		Yellow:   c(f.Yellow()),
		Peach:    c(f.Peach()),
		Mauve:    c(f.Mauve()),
		Red:      c(f.Red()),
		Teal:     c(f.Teal()),
		Draft:    c(f.Overlay2()),
		OnFill:   c(f.Base()),
		CursorBg: c(f.Surface1()),
	}
	s.BlockBg = Blend(c(f.Base()), c(f.Surface0()), 0.5)
	s.SelBg = Blend(c(f.Base()), s.Mauve, 0.18)
	s.SelCursorBg = Blend(c(f.Base()), s.Mauve, 0.34)
	fg := func(col color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(col) }

	s.Normal = fg(s.Text)
	s.Dim = fg(s.Muted)
	s.Subtle = fg(s.Faint)

	s.Conn = fg(s.Border)
	s.ConnDashed = fg(s.Peach)
	s.ConnActive = fg(s.Accent)

	s.Branch = fg(s.Text).Bold(true)
	s.BranchCurrent = fg(s.Accent).Bold(true)
	s.BranchMerged = fg(s.Muted)
	s.Trunk = fg(s.Muted).Italic(true)
	s.PRNum = fg(s.Faint)

	s.PROpen = s.Pill(s.Green)
	s.PRDraft = s.Pill(s.Draft)
	s.PRQueued = s.Pill(s.Peach)
	s.PRClosed = s.Pill(s.Red)

	s.BulletCurrent = fg(s.Accent)
	s.Selection = fg(s.Mauve)
	s.BulletMerged = fg(s.Green)
	s.BulletQueued = fg(s.Peach)

	s.Add = fg(s.Green)
	s.Del = fg(s.Red)
	s.SHA = fg(s.Yellow)

	s.OK = fg(s.Green)
	s.Warn = fg(s.Peach)
	s.Err = fg(s.Red)
	s.Bot = fg(s.Teal)

	s.Selected = s.Pill(s.Mauve)

	s.Match = fg(s.Accent).Bold(true)

	s.Title = fg(s.Text).Bold(true)
	s.Key = fg(s.Accent)
	s.KeyDesc = fg(s.Muted)
	s.Rule = fg(s.Border)
	s.Prompt = fg(s.Accent)
	s.FlashOK = fg(s.Green)
	s.FlashErr = fg(s.Red)
	s.Note = fg(s.Muted)
	return s
}

// Pill is a filled label: dark bold text on bg.
func (s Styles) Pill(bg color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(s.OnFill).Background(bg).Bold(true).Padding(0, 1)
}

// Shade paints line's background. Styled segments end in a full reset, so
// the background is set again after each one. A nil bg leaves line as is.
func Shade(line string, bg color.Color) string {
	if bg == nil {
		return line
	}
	seq := BgSeq(bg)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[m")
	return seq + strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+seq) + "\x1b[m"
}

// Lift brightens secondary and tertiary ink in line one step each, for the
// row under the cursor.
func (s Styles) Lift(line string) string {
	return strings.NewReplacer(
		FgSeq(s.Muted), FgSeq(s.Text),
		FgSeq(s.Faint), FgSeq(s.Muted),
	).Replace(line)
}

// FgSeq is the escape sequence that sets the foreground to c.
func FgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// BgSeq is the escape sequence that sets the background to c.
func BgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// Blend mixes t of top into base.
func Blend(base, top color.Color, t float64) color.Color {
	br, bg, bb, _ := base.RGBA()
	tr, tg, tb, _ := top.RGBA()
	mix := func(lo, hi uint32) uint8 { return uint8(float64(lo>>8)*(1-t) + float64(hi>>8)*t + 0.5) }
	return color.RGBA{mix(br, tr), mix(bg, tg), mix(bb, tb), 0xff}
}

const (
	GlyphBranch          = "○"
	GlyphCurrent         = "●"
	GlyphSelectedCurrent = "◆"
	GlyphSelected        = "◇"
	GlyphMerged          = "✓"
	GlyphQueued          = "◎"
	GlyphCollapsed       = "▸"
	GlyphExpanded        = "▾"
	GlyphTrunk           = "└"
	GlyphConn            = "│"
	GlyphDashed          = "┊"
)

// CheckGlyph is the mark of a check, workflow, or rollup in state st.
func (s Styles) CheckGlyph(st github.CheckState) string {
	switch st {
	case github.CheckSuccess:
		return s.OK.Render("✓")
	case github.CheckFailure:
		return s.Err.Render("✗")
	case github.CheckPending:
		return s.Warn.Render("●")
	case github.CheckCancelled:
		return s.Dim.Render("⊘")
	case github.CheckSkipped:
		return s.Dim.Render("-")
	default:
		return s.Dim.Render("·")
	}
}

// CheckCounts tallies checks by state, the states that need attention first.
func (s Styles) CheckCounts(checks []github.Check) string {
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
		{github.CheckFailure, "failed", s.Err},
		{github.CheckPending, "running", s.Warn},
		{github.CheckCancelled, "cancelled", s.Dim},
		{github.CheckSuccess, "passed", s.OK},
		{github.CheckSkipped, "skipped", s.Dim},
	} {
		if n := counts[st.state]; n > 0 {
			parts = append(parts, st.style.Render(fmt.Sprintf("%d %s", n, st.word)))
		}
	}
	return strings.Join(parts, s.Dim.Render(" · "))
}

// DiffStat is the lines added and deleted, leaving out a count of zero.
func (s Styles) DiffStat(add, del int) string {
	var out []string
	if add > 0 {
		out = append(out, s.Add.Render(fmt.Sprintf("+%d", add)))
	}
	if del > 0 {
		out = append(out, s.Del.Render(fmt.Sprintf("-%d", del)))
	}
	return strings.Join(out, " ")
}
