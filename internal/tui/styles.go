package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	catppuccin "github.com/catppuccin/go"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// styles is the palette and the styles built from it. Colors come from
// Catppuccin Mocha, so the TUI matches other Catppuccin-themed tools. Color
// marks state that needs attention; everything else is primary or muted ink.
type styles struct {
	text, muted, faint, border, accent color.Color
	green, yellow, peach, mauve        color.Color
	red, teal, onFill                  color.Color

	// Backgrounds. The row under the cursor, or the whole hovered branch, is
	// cursorBg; the rest of a branch whose item is hovered is blockBg, a step
	// fainter so the cursor row stands out. Branches selected for review are
	// selBg, a hue apart from the cursor.
	blockBg, cursorBg, selBg color.Color

	normal lipgloss.Style // primary ink
	dim    lipgloss.Style // secondary ink: labels, hints, timestamps
	faintS lipgloss.Style // tertiary ink: counts of collapsed sections

	// Tree
	conn       lipgloss.Style // connector lines
	connDashed lipgloss.Style // needs-rebase connector
	connActive lipgloss.Style // connector of the hovered branch

	branch        lipgloss.Style
	branchCurrent lipgloss.Style
	branchMerged  lipgloss.Style
	trunk         lipgloss.Style
	prNum         lipgloss.Style

	bulletCurrent  lipgloss.Style
	bulletSelected lipgloss.Style
	bulletMerged   lipgloss.Style
	bulletQueued   lipgloss.Style

	add lipgloss.Style
	del lipgloss.Style
	sha lipgloss.Style

	ok   lipgloss.Style
	warn lipgloss.Style
	err  lipgloss.Style
	bot  lipgloss.Style // comments from bots such as Copilot
	info lipgloss.Style // merged and other settled states

	selected lipgloss.Style // pill for the selection mode
	note     lipgloss.Style // flash that is neither success nor error
	match    lipgloss.Style // fuzzy-matched characters

	// Chrome
	title    lipgloss.Style
	key      lipgloss.Style
	keyDesc  lipgloss.Style
	rule     lipgloss.Style
	prompt   lipgloss.Style
	flashOK  lipgloss.Style
	flashErr lipgloss.Style
}

func newStyles() styles {
	f := catppuccin.Mocha
	c := func(col catppuccin.Color) color.Color { return lipgloss.Color(col.Hex) }
	s := styles{
		text:     c(f.Text()),
		muted:    c(f.Subtext0()),
		faint:    c(f.Overlay1()),
		border:   c(f.Surface2()),
		accent:   c(f.Blue()),
		green:    c(f.Green()),
		yellow:   c(f.Yellow()),
		peach:    c(f.Peach()),
		mauve:    c(f.Mauve()),
		red:      c(f.Red()),
		teal:     c(f.Teal()),
		onFill:   c(f.Base()),
		cursorBg: c(f.Surface1()),
	}
	s.blockBg = blend(c(f.Base()), c(f.Surface0()), 0.5)
	s.selBg = blend(c(f.Base()), s.mauve, 0.18)
	fg := func(col color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(col) }

	s.normal = fg(s.text)
	s.dim = fg(s.muted)
	s.faintS = fg(s.faint)

	s.conn = fg(s.border)
	s.connDashed = fg(s.peach)
	s.connActive = fg(s.accent)

	s.branch = fg(s.text).Bold(true)
	s.branchCurrent = fg(s.accent).Bold(true)
	s.branchMerged = fg(s.muted)
	s.trunk = fg(s.muted).Italic(true)
	s.prNum = fg(s.faint)

	s.bulletCurrent = fg(s.accent)
	s.bulletSelected = fg(s.mauve)
	s.bulletMerged = fg(s.mauve)
	s.bulletQueued = fg(s.peach)

	s.add = fg(s.green)
	s.del = fg(s.red)
	s.sha = fg(s.yellow)

	s.ok = fg(s.green)
	s.warn = fg(s.peach)
	s.err = fg(s.red)
	s.bot = fg(s.teal)
	s.info = fg(s.mauve)

	s.selected = lipgloss.NewStyle().
		Foreground(s.onFill).
		Background(s.mauve).
		Bold(true).
		Padding(0, 1)

	s.match = fg(s.accent).Bold(true)

	s.title = fg(s.text).Bold(true)
	s.key = fg(s.accent)
	s.keyDesc = fg(s.muted)
	s.rule = fg(s.border)
	s.prompt = fg(s.accent)
	s.flashOK = fg(s.green)
	s.flashErr = fg(s.red)
	s.note = fg(s.muted)
	return s
}

// shade paints line's background. Styled segments end in a full reset, so
// the background is set again after each one. A nil bg leaves line as is.
func shade(line string, bg color.Color) string {
	if bg == nil {
		return line
	}
	seq := bgSeq(bg)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[m")
	return seq + strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+seq) + "\x1b[m"
}

// lift brightens secondary and tertiary ink in line one step each, for the
// row under the cursor.
func (s styles) lift(line string) string {
	return strings.NewReplacer(
		fgSeq(s.muted), fgSeq(s.text),
		fgSeq(s.faint), fgSeq(s.muted),
	).Replace(line)
}

// fgSeq is the escape sequence that sets the foreground to c.
func fgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// bgSeq is the escape sequence that sets the background to c.
func bgSeq(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
}

// blend mixes t of top into base.
func blend(base, top color.Color, t float64) color.Color {
	br, bg, bb, _ := base.RGBA()
	tr, tg, tb, _ := top.RGBA()
	mix := func(lo, hi uint32) uint8 { return uint8(float64(lo>>8)*(1-t) + float64(hi>>8)*t + 0.5) }
	return color.RGBA{mix(br, tr), mix(bg, tg), mix(bb, tb), 0xff}
}

const (
	glyphBranch    = "○"
	glyphCurrent   = "●"
	glyphSelected  = "◆"
	glyphMerged    = "✓"
	glyphQueued    = "◎"
	glyphCollapsed = "▸"
	glyphExpanded  = "▾"
	glyphTrunk     = "└"
	glyphConn      = "│"
	glyphDashed    = "┊"
)

func (s styles) checkGlyph(st github.CheckState) string {
	switch st {
	case github.CheckSuccess:
		return s.ok.Render("✓")
	case github.CheckFailure:
		return s.err.Render("✗")
	case github.CheckPending:
		return s.warn.Render("●")
	case github.CheckCancelled:
		return s.dim.Render("⊘")
	case github.CheckSkipped:
		return s.dim.Render("-")
	default:
		return s.dim.Render("·")
	}
}
