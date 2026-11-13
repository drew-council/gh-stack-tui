// Package uitest compares a TUI's rendered screen to a golden file.
package uitest

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sebdah/goldie/v2"

	"github.com/drew-council/gh-stack-tui/internal/ui"
)

// Plain is screen without color or trailing spaces. Each line starts with a
// mark for its background, which color would show: █ the cursor, ░ the rest
// of the hovered block, ▓ the cursor on a selected row, ▒ a selected row, and
// a space for none.
func Plain(st ui.Styles, screen string) string {
	marks := []struct {
		bg   color.Color
		mark string
	}{
		{st.SelCursorBg, "▓"},
		{st.SelBg, "▒"},
		{st.CursorBg, "█"},
		{st.BlockBg, "░"},
	}
	var lines []string
	for _, l := range strings.Split(screen, "\n") {
		mark := " "
		for _, mk := range marks {
			if strings.Contains(l, ui.BgSeq(mk.bg)) {
				mark = mk.mark
				break
			}
		}
		lines = append(lines, strings.TrimRight(mark+ansi.Strip(l), " "))
	}
	return strings.Join(lines, "\n") + "\n"
}

// AssertScreen compares [Plain] of screen to testdata/<test name>.golden. Run
// go test with -update to rewrite it.
func AssertScreen(t *testing.T, st ui.Styles, screen string) {
	t.Helper()
	goldie.New(t).Assert(t, t.Name(), []byte(Plain(st, screen)))
}

// LightBackground is a terminal's answer that its background is light.
var LightBackground = tea.BackgroundColorMsg{Color: color.RGBA{0xef, 0xf1, 0xf5, 0xff}}

// AssertFlavor fails t unless screen is drawn in the Catppuccin flavor of
// dark: it uses that flavor's text color and not the other's.
func AssertFlavor(t *testing.T, screen string, dark bool) {
	t.Helper()
	text := func(dark bool) string {
		r, g, b, _ := ui.NewStyles(dark).Text.RGBA()
		return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
	}
	if !strings.Contains(screen, text(dark)) {
		t.Errorf("screen lacks the text color of dark=%v", dark)
	}
	if strings.Contains(screen, text(!dark)) {
		t.Errorf("screen has the text color of dark=%v", !dark)
	}
}
