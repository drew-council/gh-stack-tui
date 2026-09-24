package tui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/stack"
)

type promptKind int

const (
	promptAdd promptKind = iota
	promptCommand
)

type confirmState struct {
	text    string
	actions map[string]func(*Model) tea.Cmd
}

// helpSection groups key bindings on the help screen.
type helpSection struct {
	title string
	keys  [][2]string
}

// Stack operations mirror the aliases of the gs wrapper (p push, s sync, P
// sync --prune, r rebase, c checkout, a add, m modify, u unstack), with
// R taking the place of `gs review`.
var helpSections = []helpSection{
	{"navigate", [][2]string{
		{"j/k ↓/↑", "move"},
		{"J/K", "next/prev branch"},
		{"gg/G", "top/bottom"},
		{"ctrl+d/u", "half page"},
		{".", "current branch"},
		{"[ ]", "prev/next stack"},
	}},
	{"expand", [][2]string{
		{"l → enter", "expand / toggle"},
		{"h ←", "collapse / parent"},
		{"f", "files"},
		{"C", "commits"},
		{"x", "CI checks"},
		{"z / Z", "toggle branch / collapse all"},
	}},
	{"hovered item", [][2]string{
		{"o", "open in browser"},
		{"y", "copy name / path / sha / url"},
		{"Y", "copy all file paths"},
		{"e", "edit file in $EDITOR"},
		{"c", "checkout branch"},
		{"M", "merge PR (and below)"},
	}},
	{"stack", [][2]string{
		{"p", "push"},
		{"s", "sync"},
		{"P", "sync --prune"},
		{"S", "submit --auto"},
		{"rr ru rd", "rebase all/upstack/downstack"},
		{"rc ra", "rebase continue/abort"},
		{"a", "add branch"},
		{"m", "modify (interactive)"},
		{"u", "unstack"},
		{":", "run gh stack …"},
	}},
	{"review", [][2]string{
		{"space", "mark branch"},
		{"v", "visual range"},
		{"R", "review selection in tuicr"},
	}},
	{"other", [][2]string{
		{"ctrl+r", "refresh now"},
		{"!", "toggle output"},
		{"esc", "clear / close"},
		{"?", "help"},
		{"q", "quit"},
	}},
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.mode {
	case modeHelp:
		m.mode = modeNormal
		return m, nil
	case modePrompt:
		return m.handlePromptKey(msg)
	case modeConfirm:
		c := m.confirm
		m.mode, m.confirm = modeNormal, nil
		if act, ok := c.actions[key]; ok {
			return m, act(&m)
		}
		return m, m.setFlash("cancelled", false)
	}

	if m.pendingKey != "" {
		prefix := m.pendingKey
		m.pendingKey = ""
		return m.handleChord(prefix, key)
	}

	var cmd tea.Cmd
	switch key {
	case "q":
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
	case "esc":
		switch {
		case m.visual || len(m.marks) > 0:
			m.visual = false
			m.marks = map[string]bool{}
		case m.showOutput:
			m.showOutput = false
		default:
			m.flash = ""
		}

	// navigation
	case "j", "down":
		m.moveCursor(1)
	case "k", "up":
		m.moveCursor(-1)
	case "J", "shift+down":
		m.jumpBranch(1)
	case "K", "shift+up":
		m.jumpBranch(-1)
	case "G", "end":
		m.setCursor(len(m.rows) - 1)
	case "home":
		m.setCursor(0)
	case "ctrl+d", "pgdown":
		m.moveCursor(max(1, m.bodyHeight()/2))
	case "ctrl+u", "pgup":
		m.moveCursor(-max(1, m.bodyHeight()/2))
	case ".":
		if m.snap != nil {
			if i := m.snap.IndexOf(m.snap.CurrentBranch); i >= 0 {
				m.setCursor(m.branchRow(i))
			}
		}
	case "[", "]":
		cmd = m.cycleStack(key)
	case "g", "r":
		m.pendingKey = key

	// expansion
	case "l", "right", "enter", "tab":
		cmd = m.expand(key == "enter" || key == "tab")
	case "h", "left":
		m.collapse()
	case "f":
		m.toggleSection(secFiles)
	case "C":
		m.toggleSection(secCommits)
	case "x":
		m.toggleSection(secChecks)
	case "z":
		m.toggleBranch()
	case "Z":
		m.expanded = map[string]bool{}
		m.rebuild()

	// hovered item
	case "o":
		cmd = m.openHovered()
	case "y":
		cmd = m.yankHovered()
	case "Y":
		cmd = m.yankFiles()
	case "e":
		cmd = m.editHovered()
	case "c":
		cmd = m.checkoutHovered()
	case "M":
		cmd = m.confirmMerge()

	// stack operations
	case "p":
		cmd = m.stackOp("push", "push")
	case "s":
		cmd = m.stackOp("sync", "sync")
	case "P":
		cmd = m.stackOp("sync --prune", "sync", "--prune")
	case "S":
		cmd = m.stackOp("submit", "submit", "--auto")
	case "a":
		if m.requireCurrentStack() {
			cmd = m.openPrompt(promptAdd, "")
		} else {
			cmd = m.notCurrentFlash()
		}
	case "m":
		if m.requireCurrentStack() {
			cmd = m.runInteractive("modify", "gh", "stack", "modify")
		} else {
			cmd = m.notCurrentFlash()
		}
	case "u":
		cmd = m.confirmUnstack()
	case ":":
		cmd = m.openPrompt(promptCommand, "")

	// review
	case "space", " ":
		m.toggleMark()
	case "v":
		m.toggleVisual()
	case "R":
		cmd = m.startReview()

	case "ctrl+r":
		cmd = tea.Batch(m.requestLocal(), m.requestRemote(), m.setFlash("refreshing…", false))
	case "!":
		m.showOutput = !m.showOutput
	}
	m.ensureVisible()
	return m, cmd
}

func (m Model) handleChord(prefix, key string) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch prefix + key {
	case "gg":
		m.setCursor(0)
	case "rr":
		cmd = m.stackOp("rebase", "rebase")
	case "ru":
		cmd = m.stackOp("rebase --upstack", "rebase", "--upstack")
	case "rd":
		cmd = m.stackOp("rebase --downstack", "rebase", "--downstack")
	case "rc":
		cmd = m.stackOp("rebase --continue", "rebase", "--continue")
	case "ra":
		cmd = m.stackOp("rebase --abort", "rebase", "--abort")
	}
	m.ensureVisible()
	return m, cmd
}

// requireCurrentStack reports whether the shown stack is the checked out one.
// gh stack operates on the stack of the checked out branch, so running an
// operation while viewing another stack would act on the wrong one.
func (m *Model) requireCurrentStack() bool {
	return m.snap != nil && m.snap.HasBranch(m.snap.CurrentBranch)
}

func (m *Model) notCurrentFlash() tea.Cmd {
	return m.setFlash("this stack is not checked out: press c on one of its branches first", true)
}

func (m *Model) stackOp(title string, args ...string) tea.Cmd {
	// Continue and abort must work mid-rebase, while HEAD is detached.
	resuming := len(args) > 1 && (args[1] == "--continue" || args[1] == "--abort")
	if !resuming && !m.requireCurrentStack() {
		return m.notCurrentFlash()
	}
	return m.runOp(title, "gh", append([]string{"stack"}, args...)...)
}

func (m *Model) cycleStack(key string) tea.Cmd {
	if m.snap == nil || m.snap.StackCount < 2 {
		return m.setFlash("only one stack in this repository", false)
	}
	delta := 1
	if key == "[" {
		delta = -1
	}
	next := (m.snap.Index + delta + m.snap.StackCount) % m.snap.StackCount
	f, err := stack.ReadFile(m.repo.GitDir)
	if err != nil || next >= len(f.Stacks) {
		return m.setFlash("reading stack file failed", true)
	}
	m.pinned = f.Stacks[next].Key()
	m.pinnedCurrent = m.snap.CurrentBranch
	return m.requestLocal()
}

// --- expansion ---

func (m *Model) rebuild() {
	fallback := ""
	if r, ok := m.hovered(); ok && r.branch >= 0 {
		fallback = m.snap.Branches[r.branch].Name
	}
	m.buildRows()
	m.restoreCursor(fallback)
}

func (m *Model) expand(toggle bool) tea.Cmd {
	r, ok := m.hovered()
	if !ok {
		return nil
	}
	switch r.kind {
	case rowBranch:
		name := m.snap.Branches[r.branch].Name
		if toggle && m.branchExpanded(name) {
			m.setBranchExpanded(name, false)
		} else {
			m.setBranchExpanded(name, true)
		}
	case rowSection, rowWorkflow:
		if toggle {
			m.expanded[r.key] = !m.expanded[r.key]
		} else if !m.expanded[r.key] {
			m.expanded[r.key] = true
		} else {
			m.moveCursor(1)
			return nil
		}
	default:
		if toggle {
			return m.openHovered()
		}
		return nil
	}
	m.rebuild()
	return nil
}

func (m *Model) collapse() {
	r, ok := m.hovered()
	if !ok {
		return
	}
	switch {
	case (r.kind == rowSection || r.kind == rowWorkflow) && m.expanded[r.key]:
		m.expanded[r.key] = false
	case r.kind == rowBranch:
		m.setBranchExpanded(m.snap.Branches[r.branch].Name, false)
	default:
		// Move to the parent row, closing it like a file tree does.
		parent := m.parentKey(r)
		if r.kind == rowFile || r.kind == rowCommit || r.kind == rowWorkflow || r.kind == rowCheck {
			m.expanded[parent] = false
		}
		m.cursorKey = parent
	}
	m.rebuild()
}

func (m *Model) parentKey(r row) string {
	name := m.snap.Branches[r.branch].Name
	switch r.kind {
	case rowFile:
		return sectionKey(name, secFiles)
	case rowCommit:
		return sectionKey(name, secCommits)
	case rowWorkflow:
		return sectionKey(name, secChecks)
	case rowCheck:
		return workflowKey(name, r.workflow.Name)
	default:
		return "branch\x00" + name
	}
}

func (m *Model) toggleSection(section string) {
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return
	}
	k := sectionKey(m.snap.Branches[r.branch].Name, section)
	m.expanded[k] = !m.expanded[k]
	// Collapsing the section the cursor is inside parks it on the section.
	if !m.expanded[k] && sectionOf(r) == section {
		m.cursorKey = k
	}
	m.rebuild()
}

func sectionOf(r row) string {
	switch r.kind {
	case rowFile:
		return secFiles
	case rowCommit:
		return secCommits
	case rowWorkflow, rowCheck:
		return secChecks
	}
	return ""
}

func (m *Model) toggleBranch() {
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return
	}
	name := m.snap.Branches[r.branch].Name
	open := !m.branchExpanded(name)
	m.setBranchExpanded(name, open)
	if !open {
		m.cursorKey = "branch\x00" + name
	}
	m.rebuild()
}

func (m *Model) branchExpanded(name string) bool {
	for _, s := range []string{secFiles, secCommits, secChecks} {
		if m.expanded[sectionKey(name, s)] {
			return true
		}
	}
	return false
}

func (m *Model) setBranchExpanded(name string, open bool) {
	for _, s := range []string{secFiles, secCommits, secChecks} {
		m.expanded[sectionKey(name, s)] = open
	}
}

// --- selection for review ---

func (m *Model) toggleMark() {
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return
	}
	name := m.snap.Branches[r.branch].Name
	if m.marks[name] {
		delete(m.marks, name)
	} else {
		m.marks[name] = true
	}
	m.jumpBranch(1)
}

func (m *Model) toggleVisual() {
	if m.visual {
		// Leaving visual mode keeps the range as marks, like vim's gv-able
		// selection, so it can be extended with space.
		for i, b := range m.snap.Branches {
			if m.isSelected(i, b.Name) {
				m.marks[b.Name] = true
			}
		}
		m.visual = false
		return
	}
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return
	}
	m.visual = true
	m.anchor = m.snap.Branches[r.branch].Name
}

// --- hovered item actions ---

func (m *Model) openHovered() tea.Cmd {
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return nil
	}
	pr := m.prURL(r.branch)
	var url string
	switch r.kind {
	case rowBranch:
		url = pr
	case rowSection:
		if pr != "" {
			url = pr + "/" + map[string]string{secFiles: "files", secCommits: "commits", secChecks: "checks"}[r.section]
		}
	case rowFile:
		if pr != "" {
			url = pr + "/files" + fileAnchor(r.file.Path)
		}
	case rowCommit:
		if pr != "" {
			url = pr + "/commits/" + r.commit.SHA
		} else if m.ghRepo.Owner != "" {
			url = fmt.Sprintf(
				"https://%s/%s/%s/commit/%s",
				m.ghRepo.Host,
				m.ghRepo.Owner,
				m.ghRepo.Name,
				r.commit.SHA,
			)
		}
	case rowWorkflow:
		url = r.workflow.URL
	case rowCheck:
		url = r.check.URL
	}
	if url == "" {
		return m.setFlash("no pull request for "+m.snap.Branches[r.branch].Name, true)
	}
	return openBrowser(url)
}

func (m *Model) yankHovered() tea.Cmd {
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return nil
	}
	b := m.snap.Branches[r.branch]
	switch r.kind {
	case rowBranch:
		return copyToClipboard(b.Name, b.Name)
	case rowSection:
		switch r.section {
		case secFiles:
			return m.yankFiles()
		case secCommits:
			var shas []string
			for _, c := range b.Commits {
				shas = append(shas, c.SHA)
			}
			return copyToClipboard(
				strings.Join(shas, "\n"),
				fmt.Sprintf("%d commit SHAs", len(shas)),
			)
		default:
			if url := m.prURL(r.branch); url != "" {
				return copyToClipboard(url+"/checks", "checks URL")
			}
		}
	case rowFile:
		return copyToClipboard(r.file.Path, r.file.Path)
	case rowCommit:
		return copyToClipboard(r.commit.SHA, r.commit.SHA[:min(7, len(r.commit.SHA))])
	case rowWorkflow:
		return copyToClipboard(r.workflow.URL, r.workflow.Name+" URL")
	case rowCheck:
		return copyToClipboard(r.check.URL, r.check.Name+" URL")
	}
	return nil
}

func (m *Model) yankFiles() tea.Cmd {
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return nil
	}
	b := m.snap.Branches[r.branch]
	if len(b.Files) == 0 {
		return m.setFlash("no files changed on "+b.Name, true)
	}
	var paths []string
	for _, f := range b.Files {
		paths = append(paths, f.Path)
	}
	return copyToClipboard(strings.Join(paths, "\n"), fmt.Sprintf("%d file paths", len(paths)))
}

func (m *Model) editHovered() tea.Cmd {
	r, ok := m.hovered()
	if !ok || r.kind != rowFile {
		return m.setFlash("hover a file to edit it", true)
	}
	if r.file.Status == "D" {
		return m.setFlash(r.file.Path+" was deleted", true)
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	return m.runInteractive("", "sh", "-c", editor+` "$1"`, "sh", r.file.Path)
}

func (m *Model) checkoutHovered() tea.Cmd {
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return nil
	}
	b := m.snap.Branches[r.branch]
	if b.IsCurrent {
		return m.setFlash(b.Name+" is already checked out", false)
	}
	cmd := m.runOp("checkout "+b.Name, "git", "checkout", b.Name)
	if m.op != nil {
		m.op.quiet = true
	}
	return cmd
}

func (m *Model) confirmMerge() tea.Cmd {
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return nil
	}
	n := m.prNumber(r.branch)
	if n == 0 {
		return m.setFlash("no pull request to merge", true)
	}
	if m.merged(r.branch) {
		return m.setFlash(fmt.Sprintf("#%d is already merged", n), true)
	}
	below := 0
	for i := 0; i < r.branch; i++ {
		if !m.merged(i) {
			below++
		}
	}
	text := fmt.Sprintf("merge #%d", n)
	if below > 0 {
		text += fmt.Sprintf(" and %d PR(s) below it", below)
	}
	title := fmt.Sprintf("merge #%d", n)
	merge := func(flags ...string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd {
			args := append([]string{"stack", "merge", fmt.Sprint(n), "--yes"}, flags...)
			return m.runOp(title, "gh", args...)
		}
	}
	m.mode = modeConfirm
	m.confirm = &confirmState{
		text: text + "?  [y] last-used method  [s]quash  [m]erge commit  [r]ebase  [n]o",
		actions: map[string]func(*Model) tea.Cmd{
			"y": merge(),
			"s": merge("--squash"),
			"m": merge("--merge"),
			"r": merge("--rebase"),
		},
	}
	return nil
}

func (m *Model) confirmUnstack() tea.Cmd {
	if !m.requireCurrentStack() {
		return m.notCurrentFlash()
	}
	m.mode = modeConfirm
	m.confirm = &confirmState{
		text: "unstack?  [y] locally and on GitHub  [l]ocal only  [n]o",
		actions: map[string]func(*Model) tea.Cmd{
			"y": func(m *Model) tea.Cmd { return m.stackOp("unstack", "unstack") },
			"l": func(m *Model) tea.Cmd { return m.stackOp("unstack --local", "unstack", "--local") },
		},
	}
	return nil
}

// --- prompt ---

func (m *Model) openPrompt(kind promptKind, value string) tea.Cmd {
	m.mode = modePrompt
	m.promptKind = kind
	m.input.SetValue(value)
	m.input.CursorEnd()
	switch kind {
	case promptAdd:
		m.input.Placeholder = "new branch name"
	case promptCommand:
		m.input.Placeholder = "subcommand and flags, e.g. rebase --upstack"
	}
	return m.input.Focus()
}

func (m Model) handlePromptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.input.Blur()
		return m, nil
	case "enter":
		m.mode = modeNormal
		m.input.Blur()
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			return m, nil
		}
		switch m.promptKind {
		case promptAdd:
			return m, m.stackOp("add "+value, "add", value)
		case promptCommand:
			args := strings.Fields(strings.TrimPrefix(value, "gh stack "))
			return m, m.runOp(
				"gh stack "+strings.Join(args, " "),
				"gh",
				append([]string{"stack"}, args...)...)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
