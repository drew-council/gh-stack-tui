// Package reviewtui is the Bubble Tea model of ghrv, the TUI for the pull
// requests that wait on your review.
package reviewtui

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/cache"
	"github.com/drew-council/gh-stack-tui/internal/desktop"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
	"github.com/drew-council/gh-stack-tui/internal/ui"
)

// Options configures the TUI.
type Options struct {
	// Dir is where review commands run: the top of the repository's working
	// tree. Repo is its repository on GitHub.
	Dir  string
	Repo github.Repo
	// RemoteInterval is how often the queue is refetched from GitHub.
	RemoteInterval time.Duration
	// PollInterval is how often the open tabs are checked.
	PollInterval time.Duration
	// ReviewCmd reviews one pull request; {pr} is replaced by its number and
	// {url} by its URL.
	ReviewCmd string
	// Tabs is where reviews open. Nil runs them in the TUI's own terminal.
	Tabs desktop.Tabs
	// Query selects the queue, and Show the kinds of PRs it keeps.
	Query github.QueueQuery
	Show  review.Options
	// Connect returns the Remote of Repo.
	Connect func() (Remote, error)
	// Cache, when set, shows the last known queue at startup. CacheErr is
	// why it is not set.
	Cache    *cache.Cache
	CacheErr error
}

type mode int

const (
	modeNormal mode = iota
	modeFilter
	modeHelp
)

// Model is the root Bubble Tea model.
type Model struct {
	opts Options
	// gh talks to the repository on GitHub, once it is connected.
	gh Remote

	queue *github.Queue
	// groups are the queue's stacks and lone PRs, hidden ones included.
	groups []review.Group
	// teams are short names of the viewer's teams.
	teams map[string]string
	// details are the details of the PRs looked at so far, by number, and
	// loadingDetails the ones being fetched.
	details        map[int]cache.Detail
	loadingDetails map[int]struct{}
	// failedDetails are the versions whose fetch failed, by number. They are
	// not asked for again until the queue refreshes.
	failedDetails map[int]string
	detailErr     error

	remoteErr  error
	lastRemote time.Time
	// cachedAt is when the queue was fetched while it comes from the cache,
	// and zero once GitHub has answered.
	cachedAt      time.Time
	loadingRemote bool
	reloadRemote  bool

	// tabs are the IDs of the open review tabs, by PR number. opened are the
	// tabs this TUI opened a moment ago, which a poll may not list yet.
	tabs   map[int]string
	opened map[int]openedTab

	st ui.Styles

	// shown are the groups on screen, and rows their lines.
	shown     []review.Group
	rows      []row
	cursor    int
	cursorKey string
	scroll    int
	// detailScroll is how far the detail pane is scrolled, and helpScroll
	// the help screen.
	detailScroll int
	helpScroll   int
	// expanded are the stacks, by number, that show every PR.
	expanded map[int]struct{}
	// marks are the PRs selected for review, by number.
	marks  map[int]struct{}
	visual bool
	anchor string // key of the row the visual selection started on
	// showHidden shows the requested PRs the queue leaves out.
	showHidden bool
	filter     string

	width, height int
	mode          mode
	pendingKey    string
	input         textinput.Model

	flash     string
	flashErr  bool
	flashNote bool // flash is neither success nor an error
	flashSeq  int

	spinner spinner.Model

	// now is the clock for relative times, fixed in tests.
	now func() time.Time
}

// New creates the model of a review queue.
func New(opts Options) Model {
	st := ui.NewStyles()
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 128
	in.Placeholder = "author, title, number, branch, or team"
	is := in.Styles()
	is.Focused.Placeholder = st.Dim
	is.Blurred.Placeholder = st.Dim
	in.SetStyles(is)
	return Model{
		opts: opts,
		st:   st,
		// Init starts the first load.
		loadingRemote:  true,
		details:        map[int]cache.Detail{},
		loadingDetails: map[int]struct{}{},
		failedDetails:  map[int]string{},
		tabs:           map[int]string{},
		opened:         map[int]openedTab{},
		expanded:       map[int]struct{}{},
		marks:          map[int]struct{}{},
		input:          in,
		now:            time.Now,
		spinner: spinner.New(
			spinner.WithSpinner(spinner.MiniDot),
			spinner.WithStyle(st.Dim),
		),
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.loadCached(),
		m.loadRemote(),
		m.loadTabs(),
		m.spinner.Tick,
		pollTick(m.opts.PollInterval),
		remoteTick(m.opts.RemoteInterval),
	}
	if err := m.opts.CacheErr; err != nil {
		cmds = append(cmds, func() tea.Msg {
			return ui.FlashMsg{Text: "cache: " + ui.FirstLine(err.Error()), Err: true}
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(10, msg.Width-20))
		m.ensureVisible()
		m.clampDetail()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		if m.mode != modeNormal {
			return m, nil
		}
		delta := 3
		if msg.Mouse().Button == tea.MouseWheelUp {
			delta = -3
		}
		if listW, _ := m.layout(); msg.Mouse().X >= listW {
			m.detailScroll += delta
			m.clampDetail()
		} else {
			m.scroll += delta
			m.clampScroll()
		}
		return m, nil

	case tea.MouseClickMsg:
		if mo := msg.Mouse(); mo.Button == tea.MouseLeft && m.mode == modeNormal {
			m.click(mo.X, mo.Y)
			return m, m.requestDetails()
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case pollTickMsg:
		return m, tea.Batch(m.loadTabs(), pollTick(m.opts.PollInterval))

	case remoteTickMsg:
		return m, tea.Batch(m.requestRemote(), remoteTick(m.opts.RemoteInterval))

	case cachedLoadedMsg:
		return m.applyCached(msg)

	case remoteLoadedMsg:
		return m.applyRemote(msg)

	case detailsLoadedMsg:
		return m.applyDetails(msg)

	case tabsLoadedMsg:
		m.applyTabs(msg)
		return m, nil

	case reviewOpenedMsg:
		return m.applyOpened(msg)

	case execDoneMsg:
		if msg.err != nil {
			return m, tea.Batch(m.setFlash("review: "+msg.err.Error(), true), m.requestRemote())
		}
		return m, m.requestRemote()

	case ui.FlashMsg:
		flash := m.setFlash(msg.Text, msg.Err)
		if msg.Refresh {
			return m, tea.Batch(flash, m.requestRemote())
		}
		return m, flash

	case clearFlashMsg:
		if msg.seq == m.flashSeq {
			m.flash = ""
		}
		return m, nil
	}

	if m.mode == modeFilter {
		// Pastes and cursor blinks; a paste changes the filter.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.setFilter(m.input.Value())
		return m, cmd
	}
	return m, nil
}

type clearFlashMsg struct{ seq int }

// setNote flashes text that reports neither success nor an error.
func (m *Model) setNote(text string) tea.Cmd {
	cmd := m.setFlash(text, false)
	m.flashNote = true
	return cmd
}

func (m *Model) setFlash(text string, isErr bool) tea.Cmd {
	m.flash = text
	m.flashErr, m.flashNote = isErr, false
	m.flashSeq++
	seq := m.flashSeq
	d := 4 * time.Second
	if isErr {
		d = 10 * time.Second
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return clearFlashMsg{seq} })
}

// click puts the cursor on the row under the mouse.
func (m *Model) click(x, y int) {
	listW, _ := m.layout()
	i := y - m.listTop() + m.scroll
	if x >= listW || y < m.listTop() || y >= m.listTop()+m.listHeight() || i < 0 ||
		i >= len(m.rows) {
		return
	}
	if m.navigable(i) {
		m.setCursor(i)
		m.ensureVisible()
	}
}
