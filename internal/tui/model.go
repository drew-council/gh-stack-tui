// Package tui contains the Bubble Tea model for gh-stack-tui.
package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var titleStyle = lipgloss.NewStyle().Bold(true)

// Model is the root Bubble Tea model. It is a placeholder until stack
// monitoring is implemented.
type Model struct{}

func New() Model {
	return Model{}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) View() tea.View {
	v := tea.NewView(titleStyle.Render("gh-stack-tui") + "\n\nPress q to quit.\n")
	v.AltScreen = true
	return v
}
