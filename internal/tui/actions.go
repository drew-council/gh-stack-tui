package tui

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// --- background commands with streamed output ---

type opStatus int

const (
	opRunning opStatus = iota
	opSucceeded
	opFailed
)

type opState struct {
	title   string
	started time.Time
	// quiet ops hide the output panel again when they succeed.
	quiet bool
}

type (
	opLineMsg struct {
		line string
		ch   <-chan tea.Msg
	}
	opDoneMsg struct {
		title    string
		err      error
		exitCode int
	}
	execDoneMsg struct {
		what string
		err  error
	}
)

// maxOutput bounds the output panel's scrollback.
const maxOutput = 500

// runOp runs a command in the background, streaming its combined output into
// the output panel. Stdin is closed, so gh stack takes its non-interactive
// paths instead of prompting.
func (m *Model) runOp(title string, name string, args ...string) tea.Cmd {
	if m.op != nil {
		return m.setFlash("busy: "+m.op.title+" is still running", true)
	}
	m.op = &opState{title: title, started: time.Now()}
	m.output = []string{"$ " + name + " " + strings.Join(args, " ")}
	m.outputTitle = title
	m.outputState = opRunning
	m.showOutput = true

	ch := make(chan tea.Msg, 64)
	root := m.repo.Root
	go func() {
		defer close(ch)
		cmd := exec.Command(name, args...)
		cmd.Dir = root
		cmd.Env = append(
			os.Environ(),
			"GH_PROMPT_DISABLED=1",
			"NO_COLOR=1",
			"GIT_TERMINAL_PROMPT=0",
		)
		pr, pw := io.Pipe()
		cmd.Stdout = pw
		cmd.Stderr = pw
		if err := cmd.Start(); err != nil {
			ch <- opDoneMsg{title: title, err: err, exitCode: -1}
			return
		}
		go func() {
			err := cmd.Wait()
			pw.CloseWithError(err)
		}()
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			ch <- opLineMsg{line: sc.Text(), ch: ch}
		}
		err := sc.Err()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			code = -1
		}
		ch <- opDoneMsg{title: title, err: err, exitCode: code}
	}()
	return waitOp(ch)
}

func waitOp(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) appendOutput(line string) {
	// Progress output redraws with carriage returns; keep the final frame.
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	m.output = append(m.output, ansi.Strip(line))
	if len(m.output) > maxOutput {
		m.output = m.output[len(m.output)-maxOutput:]
	}
}

// exitHints explains gh stack's documented exit codes.
var exitHints = map[int]string{
	2:  "not in a stack",
	3:  "rebase conflict: resolve, git add, then rc to continue or ra to abort",
	4:  "GitHub API failure",
	6:  "branch is in several stacks, check out a non-shared branch",
	7:  "rebase already in progress: rc to continue or ra to abort",
	8:  "stack file locked by another gh stack process, retry shortly",
	9:  "stacked PRs are not enabled on this repository",
	10: "modify recovery required: run gh stack modify --abort",
}

func (m Model) finishOp(msg opDoneMsg) (tea.Model, tea.Cmd) {
	quiet := m.op != nil && m.op.quiet
	m.op = nil
	var flash tea.Cmd
	if msg.err != nil {
		m.outputState = opFailed
		text := msg.title + " failed"
		if hint, ok := exitHints[msg.exitCode]; ok {
			text += ": " + hint
		} else if last := lastLine(m.output); last != "" {
			text += ": " + last
		}
		m.appendOutput(fmt.Sprintf("[exit %d]", msg.exitCode))
		flash = m.setFlash(text, true)
	} else {
		m.outputState = opSucceeded
		flash = m.setFlash(msg.title+" done", false)
		if quiet {
			m.showOutput = false
		}
	}
	return m, tea.Batch(flash, m.requestLocal(), m.requestRemote())
}

// lastLine picks the line that best explains a failure: gh stack prefixes
// errors with ✗, and follows them with suggestions, so the last ✗ line wins
// over the last line.
func lastLine(lines []string) string {
	for i := len(lines) - 1; i > 0; i-- {
		if s := strings.TrimSpace(
			lines[i],
		); strings.HasPrefix(s, "✗") ||
			strings.HasPrefix(s, "error") {
			return s
		}
	}
	for i := len(lines) - 1; i > 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}

// runInteractive suspends the TUI to run an interactive command in the
// terminal, then resumes and refreshes.
func (m *Model) runInteractive(what string, name string, args ...string) tea.Cmd {
	if m.op != nil {
		return m.setFlash("busy: "+m.op.title+" is still running", true)
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = m.repo.Root
	return tea.ExecProcess(
		cmd,
		func(err error) tea.Msg { return execDoneMsg{what: what, err: err} },
	)
}

// --- clipboard and browser ---

func copyToClipboard(text, what string) tea.Cmd {
	return func() tea.Msg {
		if err := nativeCopy(text); err != nil {
			// No clipboard tool (for example over SSH): fall back to OSC 52,
			// which most terminals forward to the local clipboard.
			return tea.Sequence(tea.SetClipboard(text), func() tea.Msg {
				return flashMsg{text: "copied " + what + " (OSC 52)"}
			})()
		}
		return flashMsg{text: "copied " + what}
	}
}

func nativeCopy(text string) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "windows":
		candidates = [][]string{{"clip"}}
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			candidates = append(candidates, []string{"wl-copy"})
		}
		candidates = append(
			candidates,
			[]string{"xclip", "-selection", "clipboard"},
			[]string{"xsel", "--clipboard", "--input"},
		)
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return errors.New("no clipboard tool found")
}

func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url)
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", url)
		default:
			cmd = exec.Command("xdg-open", url)
		}
		if err := cmd.Start(); err != nil {
			return flashMsg{text: "opening browser: " + err.Error(), err: true}
		}
		go func() { _ = cmd.Wait() }()
		return flashMsg{text: "opened " + url}
	}
}

// fileAnchor is GitHub's anchor for a file in a PR's "Files changed" tab.
func fileAnchor(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "#diff-" + hex.EncodeToString(sum[:])
}

// --- review ---

// reviewSelection returns the branch indexes (bottom first) to review: the
// visual range, else the marked branches, else the hovered branch.
func (m *Model) reviewSelection() []int {
	var sel []int
	for i, b := range m.snap.Branches {
		if m.isSelected(i, b.Name) {
			sel = append(sel, i)
		}
	}
	if len(sel) == 0 {
		if r, ok := m.hovered(); ok && r.branch >= 0 {
			sel = []int{r.branch}
		}
	}
	return sel
}

// isSelected reports whether branch i is marked or inside the visual range.
func (m *Model) isSelected(i int, name string) bool {
	if m.marks[name] {
		return true
	}
	if !m.visual {
		return false
	}
	r, ok := m.hovered()
	if !ok || r.branch < 0 {
		return false
	}
	a := m.snap.IndexOf(m.anchor)
	if a < 0 {
		return false
	}
	lo, hi := min(a, r.branch), max(a, r.branch)
	return i >= lo && i <= hi
}

// reviewRange turns a selection into a single base..head range. The stack is
// linear, so a non-contiguous selection also covers the layers in between.
func (m *Model) reviewRange(sel []int) (rng string, gap bool, err error) {
	bottom, top := m.snap.Branches[sel[0]], m.snap.Branches[sel[len(sel)-1]]
	if bottom.DiffBase == "" || top.Head == "" {
		return "", false, fmt.Errorf("%s is not available locally", bottom.Name)
	}
	gap = sel[len(sel)-1]-sel[0]+1 != len(sel)
	return bottom.DiffBase + ".." + top.Head, gap, nil
}

func (m *Model) startReview() tea.Cmd {
	if m.snap == nil || len(m.snap.Branches) == 0 {
		return nil
	}
	sel := m.reviewSelection()
	if len(sel) == 0 {
		return m.setFlash("nothing selected to review", true)
	}
	rng, gap, err := m.reviewRange(sel)
	if err != nil {
		return m.setFlash("review: "+err.Error(), true)
	}
	var names []string
	for _, i := range sel {
		names = append(names, m.snap.Branches[i].Name)
	}
	label := "review " + shortBranch(names[len(names)-1])
	if len(names) > 1 {
		label = fmt.Sprintf("review %s +%d", shortBranch(names[len(names)-1]), len(names)-1)
	}
	command := strings.ReplaceAll(m.opts.ReviewCmd, "{range}", rng)

	m.marks = map[string]bool{}
	m.visual = false

	note := ""
	if gap {
		note = " (selection not contiguous, layers in between included)"
	}
	root := m.repo.Root
	switch where := reviewTarget(m.opts.ReviewIn); where {
	case "herdr":
		return func() tea.Msg {
			if err := openHerdrTab(root, label, command); err != nil {
				return flashMsg{text: "review: " + err.Error(), err: true}
			}
			return flashMsg{text: "opened " + label + " in a new herdr tab" + note}
		}
	case "tmux":
		return func() tea.Msg {
			cmd := exec.Command("tmux", "new-window", "-c", root, "-n", label, command)
			if out, err := cmd.CombinedOutput(); err != nil {
				return flashMsg{text: "review: tmux: " + strings.TrimSpace(string(out)), err: true}
			}
			return flashMsg{text: "opened " + label + " in a new tmux window" + note}
		}
	default:
		return m.runInteractive("", "sh", "-c", command)
	}
}

func reviewTarget(pref string) string {
	if pref != "" && pref != "auto" {
		return pref
	}
	if os.Getenv("HERDR_ENV") == "1" {
		if _, err := exec.LookPath("herdr"); err == nil {
			return "herdr"
		}
	}
	if os.Getenv("TMUX") != "" {
		return "tmux"
	}
	return "inline"
}

// openHerdrTab creates a focused tab in the current herdr workspace and runs
// command in its shell.
func openHerdrTab(cwd, label, command string) error {
	args := []string{"tab", "create", "--cwd", cwd, "--label", label, "--focus"}
	if ws := os.Getenv("HERDR_WORKSPACE_ID"); ws != "" {
		args = append(args, "--workspace", ws)
	}
	out, err := exec.Command("herdr", args...).Output()
	if err != nil {
		return fmt.Errorf("herdr tab create: %w", herdrErr(err))
	}
	var resp struct {
		Result struct {
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || resp.Result.RootPane.PaneID == "" {
		return fmt.Errorf(
			"herdr tab create: unexpected response %q",
			strings.TrimSpace(string(out)),
		)
	}
	if _, err := exec.Command("herdr", "pane", "run", resp.Result.RootPane.PaneID, command).
		Output(); err != nil {
		return fmt.Errorf("herdr pane run: %w", herdrErr(err))
	}
	return nil
}

func herdrErr(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return errors.New(strings.TrimSpace(string(exitErr.Stderr)))
	}
	return err
}

// shortBranch drops a leading "user/" style prefix for compact labels.
func shortBranch(name string) string {
	if _, rest, ok := strings.Cut(name, "/"); ok && rest != "" {
		return rest
	}
	return name
}
