package reviewtui

import (
	"fmt"
	"maps"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
	"github.com/drew-council/gh-stack-tui/internal/ui"
)

type (
	// reviewOpenedMsg reports the tabs a review opened or went to.
	reviewOpenedMsg struct {
		// opened are the IDs of the new tabs, by PR number.
		opened map[int]string
		// focused is the PR whose tab was already open and is now focused.
		focused int
		// skipped counts the PRs that already had a tab.
		skipped int
		several bool
		err     error
	}

	execDoneMsg struct{ err error }
)

// command is the review command for pr.
func (m *Model) command(pr *github.ReviewPR) string {
	return strings.NewReplacer("{pr}", strconv.Itoa(pr.Number), "{url}", pr.URL).
		Replace(m.opts.ReviewCmd)
}

// startReview opens the PRs under review. The hovered PR opens in a tab that
// takes the focus, or its tab is focused when it has one. A selection, or a
// stack's PRs, open in tabs behind the TUI, in the order to review them, so
// the queue stays in front. Without tabs, reviews run here one after another.
func (m *Model) startReview() tea.Cmd {
	if r, ok := m.hovered(); ok && r.kind == rowFold && len(m.selection()) == 0 {
		m.expand()
		return nil
	}
	items, several := m.targets()
	var open []review.Item
	for _, it := range items {
		if it.PR.State == "OPEN" {
			open = append(open, it)
		}
	}
	switch {
	case len(items) == 0:
		return m.setFlash("nothing here waits on your review", true)
	case len(open) == 0 && len(items) == 1:
		return m.setFlash(
			fmt.Sprintf("#%d is %s", items[0].Number, strings.ToLower(items[0].PR.State)), true)
	case len(open) == 0:
		return m.setFlash("no open PR in the selection", true)
	}
	m.clearSelection()
	m.rebuild()

	tabs, dir := m.opts.Tabs, m.opts.Dir
	if tabs == nil {
		var commands []string
		for _, it := range open {
			commands = append(commands, m.command(it.PR))
		}
		cmd := exec.Command("sh", "-c", strings.Join(commands, "; "))
		cmd.Dir = dir
		return tea.ExecProcess(cmd, func(err error) tea.Msg { return execDoneMsg{err: err} })
	}

	known := maps.Clone(m.tabs)
	type job struct {
		number  int
		command string
	}
	var jobs []job
	for _, it := range open {
		jobs = append(jobs, job{it.Number, m.command(it.PR)})
	}
	return func() tea.Msg {
		msg := reviewOpenedMsg{opened: map[int]string{}, several: several}
		for _, j := range jobs {
			if id, ok := known[j.number]; ok {
				if several {
					msg.skipped++
					continue
				}
				msg.focused, msg.err = j.number, tabs.Focus(id)
				return msg
			}
			id, err := tabs.Open(dir, tabLabel(j.number), j.command, !several)
			if err != nil {
				msg.err = err
				return msg
			}
			msg.opened[j.number] = id
		}
		return msg
	}
}

// applyOpened marks the new tabs and says what happened.
func (m Model) applyOpened(msg reviewOpenedMsg) (tea.Model, tea.Cmd) {
	now := m.now()
	var numbers []string
	for n, id := range msg.opened {
		m.tabs[n] = id
		m.opened[n] = openedTab{id: id, at: now}
		numbers = append(numbers, fmt.Sprintf("#%d", n))
	}
	noun := "tab"
	if m.opts.Tabs != nil {
		noun = m.opts.Tabs.Noun()
	}
	var text string
	switch n := len(msg.opened); {
	case msg.err != nil && n > 0:
		return m, m.setFlash(
			fmt.Sprintf(
				"review: %v (after opening %s)",
				msg.err,
				ui.Plural(n, "tab", "tabs"),
			),
			true,
		)
	case msg.err != nil:
		return m, m.setFlash("review: "+msg.err.Error(), true)
	case msg.focused != 0:
		text = fmt.Sprintf("went to the %s of #%d", noun, msg.focused)
	case n == 0:
		return m, m.setNote(
			ui.Plural(msg.skipped, "PR already has", "PRs already have") + " a " + noun,
		)
	case !msg.several:
		text = fmt.Sprintf("opened %s in a new %s", numbers[0], noun)
	default:
		text = fmt.Sprintf("opened %s in %ss behind", ui.Plural(n, "PR", "PRs"), noun)
		if msg.skipped > 0 {
			text += fmt.Sprintf(" · %d already open", msg.skipped)
		}
	}
	return m, m.setFlash(text, false)
}

// openBrowser opens the targets' PRs on GitHub.
func (m *Model) openBrowser() tea.Cmd {
	items, _ := m.targets()
	var urls, labels []string
	for _, it := range items {
		urls = append(urls, it.PR.URL)
		labels = append(labels, fmt.Sprintf("#%d", it.Number))
	}
	if len(urls) == 0 {
		return m.setFlash("no pull request here", true)
	}
	return ui.OpenURLs(urls, "opened "+strings.Join(labels, ", "))
}

// copyURLs copies the targets' URLs, one per line.
func (m *Model) copyURLs() tea.Cmd {
	items, _ := m.targets()
	var urls []string
	for _, it := range items {
		urls = append(urls, it.PR.URL)
	}
	switch len(urls) {
	case 0:
		return m.setFlash("no pull request here", true)
	case 1:
		return ui.Copy(urls[0], urls[0])
	}
	return ui.Copy(strings.Join(urls, "\n"), fmt.Sprintf("%d URLs", len(urls)))
}

// copyBranches copies the targets' branch names, one per line.
func (m *Model) copyBranches() tea.Cmd {
	items, _ := m.targets()
	var names []string
	for _, it := range items {
		names = append(names, it.PR.HeadRef)
	}
	switch len(names) {
	case 0:
		return m.setFlash("no pull request here", true)
	case 1:
		return ui.Copy(names[0], names[0])
	}
	return ui.Copy(strings.Join(names, "\n"), fmt.Sprintf("%d branch names", len(names)))
}
