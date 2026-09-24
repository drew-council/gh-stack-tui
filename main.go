package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/tui"
)

func main() {
	if _, err := tea.NewProgram(tui.New()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
