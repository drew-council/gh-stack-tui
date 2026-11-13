package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/desktop"
)

// FlashMsg asks a TUI to show Text on its status line for a few seconds.
type FlashMsg struct {
	Text string
	Err  bool
	// Refresh also asks for a reload of what the TUI shows.
	Refresh bool
}

// Copy puts text on the clipboard and flashes what it copied.
func Copy(text, what string) tea.Cmd {
	return func() tea.Msg {
		if err := desktop.Copy(text); err != nil {
			// No clipboard tool (for example over SSH): fall back to OSC 52,
			// which most terminals forward to the local clipboard.
			return tea.Sequence(tea.SetClipboard(text), func() tea.Msg {
				return FlashMsg{Text: "copied " + what + " (OSC 52)"}
			})()
		}
		return FlashMsg{Text: "copied " + what}
	}
}

// OpenURLs opens every URL in the browser and flashes what.
func OpenURLs(urls []string, what string) tea.Cmd {
	return func() tea.Msg {
		for _, url := range urls {
			if err := desktop.OpenURL(url); err != nil {
				return FlashMsg{Text: "opening browser: " + err.Error(), Err: true}
			}
		}
		return FlashMsg{Text: what}
	}
}

// BrowseReviews opens reviews on GitHub, for when [desktop.Reviews] puts them
// in the browser. urls show the changes and labels name them. missing is the
// program that could not run, if that is why.
func BrowseReviews(missing string, urls, labels []string) tea.Cmd {
	what := "opened " + strings.Join(labels, ", ") + " on GitHub"
	if missing != "" {
		what = missing + " not found: " + what
	}
	return OpenURLs(urls, what)
}

// OpenURL opens url in the browser and flashes it.
func OpenURL(url string) tea.Cmd {
	return OpenURLs([]string{url}, "opened "+url)
}
