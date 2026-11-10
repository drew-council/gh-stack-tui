package reviewtui

import (
	tea "charm.land/bubbletea/v2"
)

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeHelp:
		half := max(1, m.panelHeight()/2)
		switch key {
		case "j", "down":
			m.helpScroll++
		case "k", "up":
			m.helpScroll--
		case "ctrl+d":
			m.helpScroll += half
		case "ctrl+u":
			m.helpScroll -= half
		case "g":
			m.helpScroll = 0
		case "G":
			m.helpScroll = len(m.helpLines())
		default:
			m.mode = modeNormal
			return m, nil
		}
		m.helpScroll = m.helpTop(len(m.helpLines()), m.panelHeight())
		return m, nil
	case modeFilter:
		return m.handleFilterKey(msg)
	}

	if m.pendingKey != "" {
		prefix := m.pendingKey
		m.pendingKey = ""
		if prefix+key == "gg" {
			m.firstRow()
		}
		m.ensureVisible()
		return m, m.requestDetails()
	}

	var cmd tea.Cmd
	half := max(1, m.listHeight()/2)
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode, m.helpScroll = modeHelp, 0
	case "esc":
		switch {
		case m.visual || len(m.marks) > 0:
			m.clearSelection()
			m.rebuild()
		case m.filter != "":
			m.setFilter("")
		default:
			m.flash = ""
		}

	// navigation
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "]":
		m.jumpGroup(1)
	case "[":
		m.jumpGroup(-1)
	case "g":
		m.pendingKey = key
	case "G", "end":
		m.lastRow()
	case "home":
		m.firstRow()
	case "ctrl+d", "pgdown":
		m.moveCursor(half)
	case "ctrl+u", "pgup":
		m.moveCursor(-half)
	case "J", "shift+down":
		m.detailScroll += max(3, m.panelHeight()/4)
		m.clampDetail()
	case "K", "shift+up":
		m.detailScroll -= max(3, m.panelHeight()/4)
		m.clampDetail()
	case "l", "right":
		m.expand()
	case "h", "left":
		m.collapse()

	// review
	case "space", " ":
		m.toggleMark()
		m.rebuild()
	case "v":
		m.toggleVisual()
	case "enter", "R":
		cmd = m.startReview()
	case "o":
		cmd = m.openBrowser()
	case "y":
		cmd = m.copyURLs()
	case "Y":
		cmd = m.copyBranches()

	// queue
	case ".":
		m.showHidden = !m.showHidden
		m.rebuild()
	case "/":
		m.mode = modeFilter
		m.input.SetValue(m.filter)
		m.input.CursorEnd()
		cmd = m.input.Focus()
	case "ctrl+r":
		cmd = tea.Batch(m.requestRemote(), m.setFlash("refreshing…", false))
	}
	m.ensureVisible()
	return m, tea.Batch(cmd, m.requestDetails())
}

// handleFilterKey edits the filter, which applies as it is typed. Enter
// keeps it and esc drops it.
func (m Model) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.input.Blur()
		m.setFilter("")
		return m, m.requestDetails()
	case "enter":
		m.mode = modeNormal
		m.input.Blur()
		return m, m.requestDetails()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.setFilter(m.input.Value())
	return m, cmd
}

// setFilter shows only the groups that match every word of filter.
func (m *Model) setFilter(filter string) {
	if filter == m.filter {
		return
	}
	m.filter = filter
	m.rebuild()
}
