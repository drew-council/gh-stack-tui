package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// Theme picks the colors: Catppuccin Mocha for dark, Latte for light, and for
// auto whichever suits the terminal's background. The zero value is auto.
type Theme string

const (
	ThemeAuto  Theme = "auto"
	ThemeDark  Theme = "dark"
	ThemeLight Theme = "light"
)

func (t *Theme) UnmarshalText(b []byte) error {
	switch v := Theme(b); v {
	case ThemeAuto, ThemeDark, ThemeLight:
		*t = v
		return nil
	}
	return fmt.Errorf("theme must be %q, %q, or %q, not %q", ThemeAuto, ThemeDark, ThemeLight, b)
}

// Auto reports whether t follows the terminal's background.
func (t Theme) Auto() bool { return t != ThemeDark && t != ThemeLight }

// Styles returns the styles to start with. Auto starts dark until the
// terminal reports its background.
func (t Theme) Styles() Styles { return NewStyles(t != ThemeLight) }

// Request asks the terminal for its background when t follows it, and is nil
// otherwise.
func (t Theme) Request() tea.Cmd {
	if !t.Auto() {
		return nil
	}
	return tea.RequestBackgroundColor
}

// Follow returns the styles for the terminal's background, and whether they
// differ from cur. Only auto follows it.
func (t Theme) Follow(cur Styles, bg tea.BackgroundColorMsg) (Styles, bool) {
	if !t.Auto() || bg.IsDark() == cur.Dark {
		return cur, false
	}
	return NewStyles(bg.IsDark()), true
}
