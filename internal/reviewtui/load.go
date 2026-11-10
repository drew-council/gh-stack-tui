package reviewtui

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/cache"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
)

type (
	pollTickMsg   struct{}
	remoteTickMsg struct{}

	// cachedLoadedMsg is what the cache knew about the queue at startup.
	cachedLoadedMsg struct {
		queue   *github.Queue
		at      time.Time
		details map[int]cache.Detail
	}

	remoteLoadedMsg struct {
		client Remote
		queue  *github.Queue
		at     time.Time
		err    error
	}

	detailsLoadedMsg struct {
		// versions are the PR versions the details were requested for.
		versions map[int]string
		details  map[int]*github.ReviewDetail
		at       time.Time
		err      error
	}

	tabsLoadedMsg struct {
		tabs map[int]string
		err  error
	}
)

// openedTab is a tab the TUI opened itself.
type openedTab struct {
	id string
	at time.Time
}

// openedGrace is how long a tab the TUI opened counts as open before a poll
// has to list it.
const openedGrace = 5 * time.Second

// detailBatch is how many PRs' details are fetched at once.
const detailBatch = 8

func pollTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return pollTickMsg{} })
}

func remoteTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return remoteTickMsg{} })
}

// loadCached reads the last known queue, to show while GitHub is asked.
// Cache errors are ignored: GitHub is asked regardless.
func (m *Model) loadCached() tea.Cmd {
	if m.opts.Cache == nil {
		return nil
	}
	c, repo, query, local := m.opts.Cache, m.opts.Repo, m.opts.Query, m.opts.Local
	return func() tea.Msg {
		queue, at, err := c.Queue(repo, query)
		if err != nil || queue == nil {
			return nil
		}
		var numbers []int
		for _, pr := range queue.PRs {
			numbers = append(numbers, pr.Number)
		}
		details, _ := c.Details(repo, numbers)
		// .gitattributes may have changed since the details were cached.
		for _, d := range details {
			markGenerated(local, d.Detail)
		}
		return cachedLoadedMsg{queue: queue, at: at, details: details}
	}
}

// applyCached shows the cached queue until GitHub answers.
func (m Model) applyCached(msg cachedLoadedMsg) (tea.Model, tea.Cmd) {
	for n, d := range msg.details {
		if _, ok := m.details[n]; !ok {
			m.details[n] = d
		}
	}
	// GitHub answering first leaves nothing for the cache to fill in.
	if m.queue != nil {
		return m, nil
	}
	m.cachedAt = msg.at
	m.setQueue(msg.queue)
	return m, nil
}

// requestRemote starts a refresh, or queues one if a load is in flight.
func (m *Model) requestRemote() tea.Cmd {
	if m.loadingRemote {
		m.reloadRemote = true
		return nil
	}
	m.loadingRemote = true
	return m.loadRemote()
}

func (m *Model) loadRemote() tea.Cmd {
	client, connect, query := m.gh, m.opts.Connect, m.opts.Query
	return func() tea.Msg {
		if client == nil {
			var err error
			if client, err = connect(); err != nil {
				return remoteLoadedMsg{err: err}
			}
		}
		at := time.Now()
		queue, err := client.FetchQueue(context.Background(), query)
		return remoteLoadedMsg{client: client, queue: queue, at: at, err: err}
	}
}

func (m Model) applyRemote(msg remoteLoadedMsg) (tea.Model, tea.Cmd) {
	m.loadingRemote = false
	var cmds []tea.Cmd
	if m.reloadRemote {
		m.reloadRemote = false
		cmds = append(cmds, m.requestRemote())
	}
	if msg.client != nil {
		m.gh = msg.client
	}
	m.remoteErr = msg.err
	if msg.err != nil {
		return m, tea.Batch(cmds...)
	}

	// Arrivals are worth a word only once a live queue was on screen.
	var before map[int]struct{}
	if !m.lastRemote.IsZero() {
		before = m.waiting()
	}
	m.lastRemote = msg.at
	m.cachedAt = time.Time{}
	m.failedDetails = map[int]string{}
	m.setQueue(msg.queue)
	if before != nil {
		cmds = append(cmds, m.announce(before))
	}

	queue, at := msg.queue, msg.at
	cmds = append(cmds, m.requestDetails(), m.save(func(c *cache.Cache) error {
		return c.SaveQueue(m.opts.Repo, m.opts.Query, queue, at)
	}))
	return m, tea.Batch(cmds...)
}

// waiting are the numbers of the PRs that wait on the viewer.
func (m *Model) waiting() map[int]struct{} {
	out := map[int]struct{}{}
	for _, g := range m.groups {
		for _, it := range g.Items {
			if it.ForYou() {
				out[it.Number] = struct{}{}
			}
		}
	}
	return out
}

// announce flashes the PRs that wait on the viewer now and did not before.
func (m *Model) announce(before map[int]struct{}) tea.Cmd {
	var arrived []review.Item
	seen := map[int]struct{}{}
	for _, g := range m.groups {
		for _, it := range g.Items {
			_, was := before[it.Number]
			_, dup := seen[it.Number]
			if it.ForYou() && !was && !dup {
				seen[it.Number] = struct{}{}
				arrived = append(arrived, it)
			}
		}
	}
	switch len(arrived) {
	case 0:
		return nil
	case 1:
		it := arrived[0]
		return m.setFlash(fmt.Sprintf("new: #%d %s", it.Number, it.PR.Title), false)
	}
	return m.setFlash(fmt.Sprintf("%d new review requests", len(arrived)), false)
}

// setQueue shows queue, keeping the cursor, the marks, and what is expanded.
func (m *Model) setQueue(queue *github.Queue) {
	m.queue = queue
	m.groups = review.Build(queue, m.opts.Show)

	teams := append([]string(nil), queue.Teams...)
	present := map[int]struct{}{}
	for _, pr := range queue.PRs {
		present[pr.Number] = struct{}{}
		teams = append(teams, pr.Via...)
	}
	m.teams = review.ShortTeams(teams)
	for n := range m.marks {
		if _, ok := present[n]; !ok {
			delete(m.marks, n)
		}
	}
	m.rebuild()
}

// save writes to the cache in the background. The cache is best effort, so
// failures are dropped.
func (m *Model) save(write func(c *cache.Cache) error) tea.Cmd {
	if m.opts.Cache == nil {
		return nil
	}
	c := m.opts.Cache
	return func() tea.Msg {
		_ = write(c)
		return nil
	}
}

// requestDetails fetches the details the screen is missing: the hovered PR's
// first, then those of the PRs that wait on the viewer. One fetch runs at a
// time, and starts the next when it lands.
func (m *Model) requestDetails() tea.Cmd {
	if m.gh == nil || len(m.loadingDetails) > 0 {
		return nil
	}
	versions := map[int]string{}
	var numbers []int
	want := func(pr *github.ReviewPR) {
		if pr == nil || len(numbers) == detailBatch {
			return
		}
		v := pr.Version()
		if _, ok := versions[pr.Number]; ok || m.details[pr.Number].Version == v ||
			m.failedDetails[pr.Number] == v {
			return
		}
		versions[pr.Number] = v
		numbers = append(numbers, pr.Number)
	}
	if it, ok := m.hoveredItem(); ok {
		want(it.PR)
	}
	for _, g := range m.shown {
		for _, it := range g.Items {
			if it.ForYou() {
				want(it.PR)
			}
		}
	}
	if len(numbers) == 0 {
		return nil
	}
	for _, n := range numbers {
		m.loadingDetails[n] = struct{}{}
	}
	return m.fetchDetails(versions)
}

// fetchDetails fetches the details of the PRs in versions, and flags their
// generated files.
func (m *Model) fetchDetails(versions map[int]string) tea.Cmd {
	numbers := slices.Sorted(maps.Keys(versions))
	client, local := m.gh, m.opts.Local
	return func() tea.Msg {
		at := time.Now()
		details, err := client.FetchReviewDetails(context.Background(), numbers)
		for _, d := range details {
			markGenerated(local, d)
		}
		return detailsLoadedMsg{versions: versions, details: details, at: at, err: err}
	}
}

// markGenerated flags the files of d that local says are generated.
func markGenerated(local Local, d *github.ReviewDetail) {
	if local == nil || d == nil {
		return
	}
	paths := make([]string, len(d.Files))
	for i, f := range d.Files {
		paths[i] = f.Path
	}
	gen := local.Generated(paths)
	for i := range d.Files {
		_, d.Files[i].Generated = gen[d.Files[i].Path]
	}
}

func (m Model) applyDetails(msg detailsLoadedMsg) (tea.Model, tea.Cmd) {
	m.detailErr = msg.err
	saved := map[int]cache.Detail{}
	for n, v := range msg.versions {
		delete(m.loadingDetails, n)
		if msg.err != nil {
			m.failedDetails[n] = v
			continue
		}
		d := msg.details[n]
		if d == nil {
			// The PR no longer resolves; do not ask again.
			d = &github.ReviewDetail{Number: n}
		}
		m.details[n] = cache.Detail{Version: v, Detail: d}
		saved[n] = m.details[n]
	}
	m.clampDetail()
	at := msg.at
	return m, tea.Batch(m.requestDetails(), m.save(func(c *cache.Cache) error {
		return c.SaveDetails(m.opts.Repo, saved, at)
	}))
}

// tabLabel names the tab that reviews PR number.
func tabLabel(number int) string { return "#" + strconv.Itoa(number) }

// loadTabs lists the open tabs, to mark the PRs under review.
func (m *Model) loadTabs() tea.Cmd {
	tabs := m.opts.Tabs
	if tabs == nil {
		return nil
	}
	return func() tea.Msg {
		list, err := tabs.List()
		if err != nil {
			return tabsLoadedMsg{err: err}
		}
		open := map[int]string{}
		for _, t := range list {
			if n, err := strconv.Atoi(strings.TrimPrefix(t.Label, "#")); err == nil &&
				strings.HasPrefix(t.Label, "#") {
				open[n] = t.ID
			}
		}
		return tabsLoadedMsg{tabs: open}
	}
}

// applyTabs takes the polled tabs. A failed poll keeps the last known ones.
func (m *Model) applyTabs(msg tabsLoadedMsg) {
	if msg.err != nil {
		return
	}
	m.tabs = msg.tabs
	now := m.now()
	for n, t := range m.opened {
		switch _, listed := m.tabs[n]; {
		case listed || now.Sub(t.at) > openedGrace:
			delete(m.opened, n)
		default:
			m.tabs[n] = t.id
		}
	}
}
