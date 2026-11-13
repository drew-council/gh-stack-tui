package ui

import (
	"encoding/json"
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestThemeFollow(t *testing.T) {
	light := tea.BackgroundColorMsg{Color: color.White}
	dark := tea.BackgroundColorMsg{Color: color.Black}
	for _, tc := range []struct {
		theme  Theme
		bg     tea.BackgroundColorMsg
		start  bool // dark before the reply
		want   bool // dark after it
		follow bool // whether it asks the terminal
	}{
		{"", light, true, false, true},
		{ThemeAuto, light, true, false, true},
		{ThemeAuto, dark, true, true, true},
		{ThemeDark, light, true, true, false},
		{ThemeLight, dark, false, false, false},
	} {
		st := tc.theme.Styles()
		if st.Dark != tc.start {
			t.Errorf("%q starts dark=%v, want %v", tc.theme, st.Dark, tc.start)
		}
		if got := tc.theme.Request() != nil; got != tc.follow {
			t.Errorf("%q asks for the background: %v, want %v", tc.theme, got, tc.follow)
		}
		next, changed := tc.theme.Follow(st, tc.bg)
		if next.Dark != tc.want || changed != (tc.want != tc.start) {
			t.Errorf(
				"%q on %v: dark=%v changed=%v, want dark=%v",
				tc.theme,
				tc.bg,
				next.Dark,
				changed,
				tc.want,
			)
		}
	}
}

func TestThemeUnmarshal(t *testing.T) {
	var th Theme
	if err := json.Unmarshal([]byte(`"light"`), &th); err != nil || th != ThemeLight {
		t.Errorf("got %q, %v", th, err)
	}
	if err := json.Unmarshal([]byte(`"solarized"`), &th); err == nil {
		t.Error("an unknown theme should be an error")
	}
}

// Each flavor tells the four row backgrounds apart, as the goldens' marks do.
func TestBackgroundsDiffer(t *testing.T) {
	for _, dark := range []bool{true, false} {
		s := NewStyles(dark)
		seen := map[string]string{}
		for name, c := range map[string]color.Color{
			"block": s.BlockBg, "cursor": s.CursorBg, "selected": s.SelBg, "selected cursor": s.SelCursorBg,
		} {
			if other, ok := seen[BgSeq(c)]; ok {
				t.Errorf("dark=%v: %s and %s share a background", dark, name, other)
			}
			seen[BgSeq(c)] = name
		}
	}
}
