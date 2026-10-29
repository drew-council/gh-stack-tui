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
	red, teal, onFill, cursorBg        color.Color

	normal lipgloss.Style // primary ink
	dim    lipgloss.Style // secondary ink: labels, hints, timestamps
	faintS lipgloss.Style // tertiary ink: counts of collapsed sections

	// Tree
	conn       lipgloss.Style // connector lines
	connDashed lipgloss.Style // needs-rebase connector
	connActive lipgloss.Style // connector of the hovered branch
	cursor     lipgloss.Style // cursor bar in the gutter

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
	fg := func(col color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(col) }

	s.normal = fg(s.text)
	s.dim = fg(s.muted)
	s.faintS = fg(s.faint)

	s.conn = fg(s.border)
	s.connDashed = fg(s.peach)
	s.connActive = fg(s.accent)
	s.cursor = fg(s.accent)

	s.branch = fg(s.text).Bold(true)
	s.branchCurrent = fg(s.accent).Bold(true)
	s.branchMerged = fg(s.muted)
	s.trunk = fg(s.muted).Italic(true)
	s.prNum = fg(s.faint)

	s.bulletCurrent = fg(s.accent)
	s.bulletSelected = fg(s.green)
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
		Background(s.green).
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
	return s
}

// highlight paints line's background in the cursor color. Styled segments
// end in a full reset, so the background is set again after each one.
func (s styles) highlight(line string) string {
	r, g, b, _ := s.cursorBg.RGBA()
	bg := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[m")
	return bg + strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+bg) + "\x1b[m"
}

const (
	glyphBranch    = "○"
	glyphCurrent   = "●"
	glyphSelected  = "◆"
	glyphMerged    = "✓"
	glyphQueued    = "◎"
	glyphCollapsed = "▸"
	glyphExpanded  = "▾"
	glyphCursor    = "▌"
	glyphActive    = "▏"
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
