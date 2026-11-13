package ui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Markdown renders text, such as a pull request's description, as lines at
// most width columns wide, with glamour, in the styles' palette. Text that
// glamour cannot render is wrapped as it is.
func (s Styles) Markdown(text string, width int) []string {
	width = max(8, width)
	var out string
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(s.glamour()),
		glamour.WithWordWrap(width),
	)
	if err == nil {
		out, err = r.Render(text)
	}
	if err != nil {
		out = s.Normal.Render(ansi.Wrap(text, width, ""))
	}
	lines := strings.Split(out, "\n")
	// glamour puts blank lines around the text, and pads lines to the width.
	blank := func(l string) bool { return strings.TrimSpace(ansi.Strip(l)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		text := strings.TrimRight(ansi.Strip(l), " ")
		lines[i] = ansi.Truncate(l, lipgloss.Width(text), "")
	}
	return lines
}

// glamour is the palette as a glamour style: headings bold, code set apart,
// links in the accent color, and everything else quiet.
func (s Styles) glamour() gansi.StyleConfig {
	hex := func(c color.Color) *string {
		r, g, b, _ := c.RGBA()
		v := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
		return &v
	}
	yes := true
	margin := func(n uint) *uint { return &n }
	quote := GlyphConn + " "
	surface := s.flavor.Surface0().Hex
	// Latte's peach is too faint on its surface, so code is set apart by its
	// background alone there, as on GitHub.
	code := s.Peach
	if !s.Dark {
		code = s.Text
	}
	return gansi.StyleConfig{
		Document: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{Color: hex(s.Text)},
			Margin:         margin(0),
		},
		BlockQuote: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{Color: hex(s.Muted)},
			Indent:         margin(1),
			IndentToken:    &quote,
		},
		List: gansi.StyleList{LevelIndent: 2},
		Heading: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{BlockSuffix: "\n", Bold: &yes},
		},
		Strikethrough: gansi.StylePrimitive{CrossedOut: &yes},
		Emph:          gansi.StylePrimitive{Italic: &yes},
		Strong:        gansi.StylePrimitive{Bold: &yes},
		HorizontalRule: gansi.StylePrimitive{
			Color: hex(s.Border), Format: "\n────────\n",
		},
		Item:        gansi.StylePrimitive{BlockPrefix: "• "},
		Enumeration: gansi.StylePrimitive{BlockPrefix: ". "},
		Task: gansi.StyleTask{
			Ticked: "[✓] ", Unticked: "[ ] ",
		},
		Link:      gansi.StylePrimitive{Color: hex(s.Faint)},
		LinkText:  gansi.StylePrimitive{Color: hex(s.Accent)},
		Image:     gansi.StylePrimitive{Color: hex(s.Faint)},
		ImageText: gansi.StylePrimitive{Color: hex(s.Muted), Format: "image: {{.text}}"},
		Code: gansi.StyleBlock{
			StylePrimitive: gansi.StylePrimitive{
				Color: hex(code), BackgroundColor: &surface,
			},
		},
		CodeBlock: gansi.StyleCodeBlock{
			StyleBlock: gansi.StyleBlock{
				StylePrimitive: gansi.StylePrimitive{Color: hex(s.Muted)},
				Margin:         margin(2),
			},
			Theme: "catppuccin-" + s.flavor.Name(),
		},
		Table: gansi.StyleTable{
			StyleBlock: gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: hex(s.Text)}},
		},
		DefinitionTerm:        gansi.StylePrimitive{Bold: &yes},
		DefinitionDescription: gansi.StylePrimitive{BlockPrefix: "\n  "},
	}
}
