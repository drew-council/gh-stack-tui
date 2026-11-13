package stacktui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/drew-council/gh-stack-tui/internal/ui"
	"github.com/drew-council/gh-stack-tui/internal/ui/uitest"
)

// Under auto, a light background redraws the stack in Latte, and changes
// nothing but colors.
func TestLightBackgroundSwitchesToLatte(t *testing.T) {
	m := demoModel(t, 80, 50)
	dark := m.render()
	uitest.AssertFlavor(t, dark, true)

	m = update(m, uitest.LightBackground)
	light := m.render()
	uitest.AssertFlavor(t, light, false)
	if ansi.Strip(light) != ansi.Strip(dark) {
		t.Error("the theme should change only colors")
	}
}

func TestDarkThemeIgnoresBackground(t *testing.T) {
	m := demoModel(t, 80, 50)
	m.opts.Theme = ui.ThemeDark
	m = update(m, uitest.LightBackground)
	uitest.AssertFlavor(t, m.render(), true)
}
