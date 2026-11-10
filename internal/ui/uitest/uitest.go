// Package uitest compares a TUI's rendered screen to a golden file.
package uitest

import (
	"image/color"
	"strings"
	"testing"

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
