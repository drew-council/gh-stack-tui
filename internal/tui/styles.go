package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// Colors use the terminal's 16-color palette so the TUI follows your theme.
var (
	colGreen   = lipgloss.Color("2")
	colRed     = lipgloss.Color("1")
	colYellow  = lipgloss.Color("3")
	colBlue    = lipgloss.Color("4")
	colMagenta = lipgloss.Color("5")
	colCyan    = lipgloss.Color("6")
	colGray    = lipgloss.Color("8")
)

var (
	boldStyle      = lipgloss.NewStyle().Bold(true)
	dimStyle       = lipgloss.NewStyle().Foreground(colGray)
	connStyle      = lipgloss.NewStyle().Foreground(colGray)
	connFocusStyle = lipgloss.NewStyle().Foreground(colCyan)
	cursorStyle    = lipgloss.NewStyle().Foreground(colCyan).Bold(true)
	markStyle      = lipgloss.NewStyle().Foreground(colMagenta).Bold(true)
	visualStyle    = lipgloss.NewStyle().Foreground(colMagenta)
	focusStyle     = lipgloss.NewStyle().Reverse(true)

	branchStyle        = lipgloss.NewStyle()
	branchCurrentStyle = lipgloss.NewStyle().Foreground(colCyan).Bold(true)
	branchMergedStyle  = lipgloss.NewStyle().Foreground(colGray)
	trunkStyle         = lipgloss.NewStyle().Foreground(colBlue).Bold(true)

	addStyle     = lipgloss.NewStyle().Foreground(colGreen)
	delStyle     = lipgloss.NewStyle().Foreground(colRed)
	shaStyle     = lipgloss.NewStyle().Foreground(colYellow)
	prNumStyle   = lipgloss.NewStyle().Foreground(colBlue).Bold(true)
	openStyle    = lipgloss.NewStyle().Foreground(colGreen)
	draftStyle   = lipgloss.NewStyle().Foreground(colGray)
	mergedStyle  = lipgloss.NewStyle().Foreground(colMagenta)
	closedStyle  = lipgloss.NewStyle().Foreground(colRed)
	queuedStyle  = lipgloss.NewStyle().Foreground(colYellow)
	warnStyle    = lipgloss.NewStyle().Foreground(colYellow)
	errStyle     = lipgloss.NewStyle().Foreground(colRed)
	okStyle      = lipgloss.NewStyle().Foreground(colGreen)
	keyStyle     = lipgloss.NewStyle().Foreground(colCyan)
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(colMagenta)
	sectionStyle = lipgloss.NewStyle().Foreground(colGray)
)

// Glyphs follow gh stack view where it has one.
const (
	glyphOpen      = "○"
	glyphMerged    = "✓"
	glyphQueued    = "◎"
	glyphWarn      = "⚠"
	glyphCollapsed = "▸"
	glyphExpanded  = "▾"
	glyphCursor    = "❯"
	glyphMark      = "●"
	glyphVisual    = "┃"
)

func checkGlyph(s github.CheckState) string {
	switch s {
	case github.CheckSuccess:
		return okStyle.Render("✓")
	case github.CheckFailure:
		return errStyle.Render("✗")
	case github.CheckPending:
		return warnStyle.Render("●")
	case github.CheckCancelled:
		return dimStyle.Render("⊘")
	case github.CheckSkipped:
		return dimStyle.Render("-")
	default:
		return dimStyle.Render("·")
	}
}

func fileStatusStyle(status string) lipgloss.Style {
	switch status {
	case "A":
		return addStyle
	case "D":
		return delStyle
	default:
		return warnStyle
	}
}
