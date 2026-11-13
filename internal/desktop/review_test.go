package desktop

import "testing"

func TestProgram(t *testing.T) {
	cases := map[string]string{
		"tuicr pr {url}":           "tuicr",
		"  tuicr -r a..b":          "tuicr",
		"TERM=xterm tuicr pr 3":    "tuicr",
		"/opt/bin/tuicr pr 3":      "/opt/bin/tuicr",
		"(cd sub && tuicr)":        "",
		"$EDITOR file":             "",
		"FOO=1":                    "",
		"":                         "",
		"tuicr;echo done":          "",
		"exec tuicr -r a..b":       "tuicr",
		"cd sub && tuicr":          "",
		"~/bin/tuicr pr 3":         "",
		"if true; then tuicr; fi":  "",
		"nvim -c 'DiffviewOpen' x": "nvim",
	}
	for command, want := range cases {
		if got := Program(command); got != want {
			t.Errorf("Program(%q) = %q, want %q", command, got, want)
		}
	}
}

type noTabs struct{ Tabs }

func TestWhere(t *testing.T) {
	has := func(installed bool) func(string) bool {
		return func(string) bool { return installed }
	}
	cases := []struct {
		name        string
		r           Reviews
		command     string
		place       Place
		wantMissing string
	}{
		{"tabs", Reviews{Tabs: noTabs{}, Installed: has(true)}, "tuicr pr 1", InTab, ""},
		{"no tabs", Reviews{Installed: has(true)}, "tuicr pr 1", Inline, ""},
		{
			"missing in tabs",
			Reviews{Tabs: noTabs{}, Installed: has(false)},
			"tuicr pr 1",
			InBrowser,
			"tuicr",
		},
		{"missing inline", Reviews{Installed: has(false)}, "tuicr pr 1", InBrowser, "tuicr"},
		{"browser", Reviews{Browser: true, Installed: has(true)}, "tuicr pr 1", InBrowser, ""},
		{"unknown program", Reviews{Installed: has(false)}, "$REVIEW 1", Inline, ""},
	}
	for _, c := range cases {
		place, missing := c.r.Where(c.command)
		if place != c.place || missing != c.wantMissing {
			t.Errorf(
				"%s: Where = %v, %q, want %v, %q",
				c.name,
				place,
				missing,
				c.place,
				c.wantMissing,
			)
		}
	}
}

func TestNewReviews(t *testing.T) {
	if r, err := NewReviews("browser"); err != nil || !r.Browser {
		t.Errorf("browser: %+v, %v", r, err)
	}
	if r, err := NewReviews("inline"); err != nil || r.Tabs != nil || r.Browser {
		t.Errorf("inline: %+v, %v", r, err)
	}
	if _, err := NewReviews("kitty"); err == nil {
		t.Error("kitty: no error")
	}
}
