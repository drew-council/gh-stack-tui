// Package tui contains the Bubble Tea model for gh-stack-tui.
package tui

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/git"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/stack"
)

// Options configures the TUI.
type Options struct {
	// RemoteInterval is how often PR and CI state is refetched from GitHub.
	RemoteInterval time.Duration
	// PollInterval is how often local refs are checked for changes.
	PollInterval time.Duration
	// StackNumber, when set, is the stack shown at startup.
	StackNumber int
	// ReviewCmd is the review command; {range} is replaced by base..head.
	ReviewCmd string
	// ReviewIn picks where the review opens: auto, herdr, tmux, or inline.
	ReviewIn string
}

type mode int

const (
	modeNormal mode = iota
	modePrompt
	modeConfirm
	modeHelp
)

// Model is the root Bubble Tea model.
type Model struct {
	opts Options
	repo *git.Repo

	snap *stack.Snapshot
	prs  map[string]*github.PR
	// pinned is the stack key chosen with [ and ], which overrides following
	// the checked out branch until the checked out branch changes.
	pinned        string
	pinnedCurrent string

	localErr      error
	remoteErr     error
	lastRemote    time.Time
	loadingLocal  bool
	reloadLocal   bool
	loadingRemote bool
	reloadRemote  bool
	fingerprint   string
	ghRepo        github.Repo

	st styles

	rows      []row
	cursor    int
	cursorKey string
	scroll    int
	expanded  map[string]bool
	marks     map[string]bool
	visual    bool
	anchor    string // branch name the visual selection started on

	width, height int
	mode          mode
	pendingKey    string

	input      textinput.Model
	promptKind promptKind
	confirm    *confirmState

	op          *opState
	output      []string
	outputTitle string
	outputState opStatus
	showOutput  bool

	flash    string
	flashErr bool
	flashSeq int

	spinner spinner.Model
}

// New creates the model for the repository at repo.
func New(repo *git.Repo, opts Options) Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 256
	st := newStyles()
	return Model{
		opts: opts,
		repo: repo,
		st:   st,
		// Init starts the first local load.
		loadingLocal: true,
		prs:          map[string]*github.PR{},
		expanded:     map[string]bool{},
		marks:        map[string]bool{},
		input:        in,
		spinner: spinner.New(
			spinner.WithSpinner(spinner.MiniDot),
			spinner.WithStyle(st.dim),
		),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadLocal(),
		m.spinner.Tick,
		pollTick(m.opts.PollInterval),
		remoteTick(m.opts.RemoteInterval),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, msg.Width-20))
		m.ensureVisible()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		switch msg.Mouse().Button {
		case tea.MouseWheelUp:
			m.scroll -= 3
		case tea.MouseWheelDown:
			m.scroll += 3
		}
		m.clampScroll()
		return m, nil

	case tea.MouseClickMsg:
		if mo := msg.Mouse(); mo.Button == tea.MouseLeft && m.mode == modeNormal {
			return m, m.click(mo.X, mo.Y)
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case pollTickMsg:
		return m, tea.Batch(m.fingerprintCmd(), pollTick(m.opts.PollInterval))

	case fingerprintMsg:
		if msg.value != m.fingerprint {
			m.fingerprint = msg.value
			return m, m.requestLocal()
		}
		return m, nil

	case remoteTickMsg:
		return m, tea.Batch(m.requestRemote(), m.requestLocal(), remoteTick(m.opts.RemoteInterval))

	case localLoadedMsg:
		return m.applyLocal(msg)

	case remoteLoadedMsg:
		return m.applyRemote(msg)

	case opLineMsg:
		m.appendOutput(msg.line)
		return m, waitOp(msg.ch)

	case opDoneMsg:
		return m.finishOp(msg)

	case execDoneMsg:
		if msg.err != nil {
			m.setFlash(msg.what+": "+msg.err.Error(), true)
		} else if msg.what != "" {
			m.setFlash(msg.what+" finished", false)
		}
		return m, tea.Batch(m.requestLocal(), m.requestRemote())

	case flashMsg:
		m.setFlash(msg.text, msg.err)
		if msg.refresh {
			return m, tea.Batch(m.requestLocal(), m.requestRemote())
		}
		return m, nil

	case clearFlashMsg:
		if msg.seq == m.flashSeq {
			m.flash = ""
		}
		return m, nil
	}

	if m.mode == modePrompt {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

type flashMsg struct {
	text    string
	err     bool
	refresh bool
}

type clearFlashMsg struct{ seq int }

func (m *Model) setFlash(text string, isErr bool) tea.Cmd {
	m.flash = text
	m.flashErr = isErr
	m.flashSeq++
	seq := m.flashSeq
	d := 4 * time.Second
	if isErr {
		d = 10 * time.Second
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return clearFlashMsg{seq} })
}

// prFor returns the best known PR for branch index i: live remote data when
// available, otherwise nil (the header then falls back to the stack file).
func (m *Model) prFor(i int) *github.PR {
	if m.snap == nil || i < 0 || i >= len(m.snap.Branches) {
		return nil
	}
	return m.prs[m.snap.Branches[i].Name]
}

// prURL returns the PR URL for branch i from remote data or the stack file.
func (m *Model) prURL(i int) string {
	if pr := m.prFor(i); pr != nil {
		return pr.URL
	}
	if b := m.snap.Branches[i]; b.PR != nil {
		return b.PR.URL
	}
	return ""
}

// prNumber returns the PR number for branch i, or 0.
func (m *Model) prNumber(i int) int {
	if pr := m.prFor(i); pr != nil {
		return pr.Number
	}
	if b := m.snap.Branches[i]; b.PR != nil {
		return b.PR.Number
	}
	return 0
}

// merged reports whether branch i has landed, preferring live remote state
// over the stack file, which only updates on the next sync.
func (m *Model) merged(i int) bool {
	if pr := m.prFor(i); pr != nil && pr.Merged {
		return true
	}
	return m.snap.Branches[i].Merged
}

// click selects the branch or item under the mouse, toggles a section or a
// workflow, and opens a PR when its number is clicked.
func (m *Model) click(x, y int) tea.Cmd {
	line := y - m.bodyTop() + m.scroll
	if y < m.bodyTop() || line < 0 {
		return nil
	}
	for i, r := range m.rows {
		if line < r.y || line >= r.y+r.height {
			continue
		}
		switch {
		case r.kind == rowSection:
			if m.navigable(m.branchRow(r.branch)) {
				m.setCursor(m.branchRow(r.branch))
			}
			m.expanded[r.key] = !m.expanded[r.key]
			m.rebuild()
		case r.kind == rowWorkflow:
			m.setCursor(i)
			m.expanded[r.key] = !m.expanded[r.key]
			m.rebuild()
		case m.navigable(i):
			m.setCursor(i)
			// The PR number sits right after the bullet and status icon.
			if r.kind == rowBranch && r.height >= 2 && line == r.y && x >= 2 && x < 12 {
				if url := m.prURL(r.branch); url != "" {
					return openBrowser(url)
				}
			}
		}
		m.ensureVisible()
		return nil
	}
	return nil
}
