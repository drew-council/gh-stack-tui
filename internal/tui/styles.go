package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	catppuccin "github.com/catppuccin/go"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// styles is the palette and the styles built from it. Colors come from
// Catppuccin Mocha, so the TUI matches other Catppuccin-themed tools; the
// layout and the role of each color follow gh stack view.
type styles struct {
	text, muted, border, accent color.Color
	green, gray, yellow, peach  color.Color
	mauve, red, onFill          color.Color

	normal lipgloss.Style // primary ink
	dim    lipgloss.Style // secondary ink: labels, hints, timestamps

	// Tree
	conn        lipgloss.Style // connector lines and bullets
	connDashed  lipgloss.Style // needs-rebase connector
	connFocus   lipgloss.Style // connector of the hovered branch
	connCurrent lipgloss.Style // hovered branch that is checked out
	connMerged  lipgloss.Style
	connQueued  lipgloss.Style

	branch        lipgloss.Style
	branchCurrent lipgloss.Style
	branchMerged  lipgloss.Style
	trunk         lipgloss.Style

	// PR state
	prLink   lipgloss.Style
	prOpen   lipgloss.Style
	prMerged lipgloss.Style
	prClosed lipgloss.Style
	prDraft  lipgloss.Style
	prQueued lipgloss.Style

	iconOpen   lipgloss.Style
	iconMerged lipgloss.Style
	iconQueued lipgloss.Style
	iconWarn   lipgloss.Style

	add lipgloss.Style
	del lipgloss.Style
	sha lipgloss.Style

	ok   lipgloss.Style
	warn lipgloss.Style
	err  lipgloss.Style

	selected lipgloss.Style // review-selection pill

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
		text:   c(f.Text()),
		muted:  c(f.Subtext0()),
		border: c(f.Surface2()),
		accent: c(f.Blue()),
		green:  c(f.Green()),
		gray:   c(f.Overlay1()),
		yellow: c(f.Yellow()),
		peach:  c(f.Peach()),
		mauve:  c(f.Mauve()),
		red:    c(f.Red()),
		onFill: c(f.Base()),
	}
	fg := func(col color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(col) }

	s.normal = fg(s.text)
	s.dim = fg(s.muted)

	s.conn = fg(s.border)
	s.connDashed = fg(s.peach)
	s.connFocus = fg(s.text)
	s.connCurrent = fg(s.accent)
	s.connMerged = fg(s.mauve)
	s.connQueued = fg(s.peach)

	s.branch = fg(s.text)
	s.branchCurrent = fg(s.accent).Bold(true)
	s.branchMerged = fg(s.muted)
	s.trunk = fg(s.muted).Italic(true)

	s.prLink = fg(s.text).Underline(true)
	s.prOpen = fg(s.green)
	s.prMerged = fg(s.mauve)
	s.prClosed = fg(s.red)
	s.prDraft = fg(s.gray)
	s.prQueued = fg(s.peach)

	s.iconOpen = fg(s.green)
	s.iconMerged = fg(s.mauve)
	s.iconQueued = fg(s.peach)
	s.iconWarn = fg(s.peach)

	s.add = fg(s.green)
	s.del = fg(s.red)
	s.sha = fg(s.yellow)

	s.ok = fg(s.green)
	s.warn = fg(s.peach)
	s.err = fg(s.red)

	s.selected = lipgloss.NewStyle().
		Foreground(s.onFill).
		Background(s.green).
		Bold(true).
		Padding(0, 1)

	s.title = fg(s.text).Bold(true)
	s.key = fg(s.text)
	s.keyDesc = fg(s.muted)
	s.rule = fg(s.border)
	s.prompt = fg(s.accent)
	s.flashOK = fg(s.green)
	s.flashErr = fg(s.red)
	return s
}

// Glyphs follow gh stack view.
const (
	glyphOpen      = "○"
	glyphMerged    = "✓"
	glyphQueued    = "◎"
	glyphWarn      = "⚠"
	glyphCollapsed = "▸"
	glyphExpanded  = "▾"
	glyphFocus     = "▶"
	glyphBullet    = "├"
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
