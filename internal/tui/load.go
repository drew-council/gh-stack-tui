package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/drew-council/gh-stack-tui/internal/cache"
	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/stack"
)

type (
	pollTickMsg    struct{}
	remoteTickMsg  struct{}
	fingerprintMsg struct{ value string }

	localLoadedMsg struct {
		snap   *stack.Snapshot
		pinned string
		err    error
	}

	remoteLoadedMsg struct {
		client Remote
		prs    map[string]*github.PR
		at     time.Time
		err    error
	}

	// cachedLoadedMsg is what the cache knew about a stack's PRs when the
	// stack was shown.
	cachedLoadedMsg struct {
		stack       string
		prs         map[string]*github.PR
		at          time.Time
		suggested   map[int]cache.Suggestion
		reviewers   []github.Reviewer
		reviewersAt time.Time
	}

	reviewersLoadedMsg struct {
		reviewers []github.Reviewer
		at        time.Time
		err       error
	}

	suggestedLoadedMsg struct {
		// heads are the head commits the suggestions were requested for.
		heads  map[int]string
		logins map[int][]string
		at     time.Time
		err    error
	}
)

// suggestion is GitHub's reviewer suggestions for a PR at one head commit.
type suggestion struct {
	head   string
	logins []string
}

func pollTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return pollTickMsg{} })
}

func remoteTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return remoteTickMsg{} })
}

// fingerprintCmd cheaply summarizes local state so that commits, checkouts,
// and gh stack commands run in another terminal show up within a poll
// interval without re-reading every branch's history.
func (m *Model) fingerprintCmd() tea.Cmd {
	local := m.local
	return func() tea.Msg {
		fp, err := local.Fingerprint()
		if err != nil {
			return nil
		}
		return fingerprintMsg{fp}
	}
}

// requestLocal starts a local reload, or queues one if a load is in flight.
func (m *Model) requestLocal() tea.Cmd {
	if m.loadingLocal {
		m.reloadLocal = true
		return nil
	}
	m.loadingLocal = true
	return m.loadLocal()
}

func (m *Model) loadLocal() tea.Cmd {
	local := m.local
	pinned := m.pinned
	previous := ""
	if m.snap != nil {
		previous = m.snap.Key
	}
	wantNumber := 0
	if m.snap == nil {
		wantNumber = m.opts.StackNumber
	}
	return func() tea.Msg {
		if wantNumber != 0 {
			refs, _ := local.Stacks()
			for _, r := range refs {
				if r.Number == wantNumber {
					pinned = r.Key
				}
			}
		}
		snap, err := local.Load(pinned, previous)
		return localLoadedMsg{snap: snap, pinned: pinned, err: err}
	}
}

func (m Model) applyLocal(msg localLoadedMsg) (tea.Model, tea.Cmd) {
	m.loadingLocal = false
	var cmds []tea.Cmd
	if m.reloadLocal {
		m.reloadLocal = false
		cmds = append(cmds, m.requestLocal())
	}
	m.localErr = msg.err
	if msg.err != nil {
		return m, tea.Batch(cmds...)
	}

	first := m.snap == nil
	if msg.pinned != m.pinned && first {
		m.pinned = msg.pinned
		m.pinnedCurrent = msg.snap.CurrentBranch
	}
	// Checking out a branch un-pins, so the view follows you again.
	if m.pinned != "" && msg.snap.CurrentBranch != m.pinnedCurrent {
		m.pinned = ""
	}

	fallback := ""
	if bi := m.hoveredBranch(); bi >= 0 && m.snap != nil {
		fallback = m.snap.Branches[bi].Name
	}
	// When the checked out branch changes (a checkout, add, or navigation in
	// another terminal) and the cursor was on the old one, follow it.
	if m.snap != nil && msg.snap.CurrentBranch != m.snap.CurrentBranch &&
		fallback != "" && fallback == m.snap.CurrentBranch && msg.snap.HasBranch(msg.snap.CurrentBranch) {
		fallback = msg.snap.CurrentBranch
		m.cursorKey = ""
	}
	lookupsChanged := first || m.snap == nil || lookupKey(m.snap) != lookupKey(msg.snap)
	stackChanged := m.snap == nil || m.snap.Key != msg.snap.Key

	m.snap = msg.snap
	if stackChanged {
		m.marks = map[string]bool{}
		m.visual = false
		m.prs = map[string]*github.PR{}
	}
	for name := range m.marks {
		if !m.snap.HasBranch(name) {
			delete(m.marks, name)
		}
	}

	m.buildRows()
	if first || stackChanged {
		m.cursorKey = ""
		fallback = m.snap.CurrentBranch
		if fallback == "" && len(m.snap.Branches) > 0 {
			fallback = m.snap.Branches[len(m.snap.Branches)-1].Name
		}
	}
	m.restoreCursor(fallback)
	m.ensureVisible()

	if stackChanged {
		cmds = append(cmds, m.loadCached())
	}
	if lookupsChanged {
		cmds = append(cmds, m.requestRemote())
	}
	return m, tea.Batch(cmds...)
}

// lookups lists the PRs a snapshot needs.
func lookups(s *stack.Snapshot) []github.Lookup {
	var out []github.Lookup
	for _, b := range s.Branches {
		l := github.Lookup{Branch: b.Name}
		if b.PR != nil {
			l.Number = b.PR.Number
		}
		out = append(out, l)
	}
	return out
}

// loadCached reads what the cache knows about the shown stack, to show while
// GitHub is asked. Cache errors are ignored: GitHub is asked regardless.
func (m *Model) loadCached() tea.Cmd {
	if m.opts.Cache == nil || m.snap == nil || len(m.snap.Branches) == 0 {
		return nil
	}
	c := m.opts.Cache
	key := m.snap.Key
	repository := m.snap.Repository
	ls := lookups(m.snap)
	var known *github.Repo
	if m.gh != nil {
		repo := m.gh.Repository()
		known = &repo
	}
	return func() tea.Msg {
		var repo github.Repo
		if known != nil {
			repo = *known
		} else {
			var err error
			if repo, err = resolveRepo(repository); err != nil {
				return nil
			}
		}
		msg := cachedLoadedMsg{stack: key}
		msg.prs, msg.at, _ = c.PRs(repo, ls)
		var numbers []int
		for _, pr := range msg.prs {
			numbers = append(numbers, pr.Number)
		}
		msg.suggested, _ = c.Suggested(repo, numbers)
		msg.reviewers, msg.reviewersAt, _ = c.Reviewers(repo)
		return msg
	}
}

// applyCached shows cached PRs until GitHub answers, and takes cached
// suggestions and reviewers that are newer than what the TUI has.
func (m Model) applyCached(msg cachedLoadedMsg) Model {
	if m.snap == nil || m.snap.Key != msg.stack {
		return m
	}
	for n, s := range msg.suggested {
		if _, ok := m.suggested[n]; !ok {
			if m.suggested == nil {
				m.suggested = map[int]suggestion{}
			}
			m.suggested[n] = suggestion{head: s.Head, logins: s.Logins}
		}
	}
	if msg.reviewers != nil && msg.reviewersAt.After(m.reviewersAt) {
		m.reviewers = msg.reviewers
		m.reviewersAt = msg.reviewersAt
		m.reviewersLoaded = true
	}
	if m.picker != nil {
		m.picker.suggested = m.suggestionsFor(m.picker.need)
		m.picker.setCandidates(m.reviewers)
		m.ensurePickerVisible()
	}
	// GitHub answering first leaves nothing for the cache to fill in.
	if len(msg.prs) == 0 || len(m.prs) > 0 {
		return m
	}
	m.prs = msg.prs
	m.cachedAt = msg.at
	fallback := ""
	if bi := m.hoveredBranch(); bi >= 0 {
		fallback = m.snap.Branches[bi].Name
	}
	m.buildRows()
	m.restoreCursor(fallback)
	m.ensureVisible()
	return m
}

// save writes to the cache in the background, once the repository is known.
// The cache is best effort, so failures are dropped.
func (m *Model) save(write func(c *cache.Cache, repo github.Repo) error) tea.Cmd {
	if m.opts.Cache == nil || m.gh == nil {
		return nil
	}
	c, repo := m.opts.Cache, m.gh.Repository()
	return func() tea.Msg {
		_ = write(c, repo)
		return nil
	}
}

// lookupKey summarizes which PRs a snapshot needs, to refetch when it changes.
func lookupKey(s *stack.Snapshot) string {
	var b strings.Builder
	b.WriteString(s.Key)
	for _, br := range s.Branches {
		b.WriteString("|" + br.Name + "|" + br.Head)
		if br.PR != nil {
			fmt.Fprintf(&b, "#%d", br.PR.Number)
		}
	}
	return b.String()
}

func (m *Model) requestRemote() tea.Cmd {
	if m.snap == nil || len(m.snap.Branches) == 0 {
		return nil
	}
	if m.loadingRemote {
		m.reloadRemote = true
		return nil
	}
	m.loadingRemote = true
	client := m.gh
	connect := m.opts.Connect
	repository := m.snap.Repository
	ls := lookups(m.snap)
	return func() tea.Msg {
		if client == nil {
			var err error
			if client, err = connect(repository); err != nil {
				return remoteLoadedMsg{err: err}
			}
		}
		at := time.Now()
		prs, err := client.FetchPRs(context.Background(), ls)
		return remoteLoadedMsg{client: client, prs: prs, at: at, err: err}
	}
}

// resolveRepo reads the repository from the stack file's "host:owner/name",
// falling back to the git remotes of the working directory, as gh does.
func resolveRepo(repository string) (github.Repo, error) {
	if host, rest, ok := strings.Cut(repository, ":"); ok {
		if owner, name, ok := strings.Cut(rest, "/"); ok && owner != "" && name != "" {
			return github.Repo{Host: host, Owner: owner, Name: name}, nil
		}
	}
	repo, err := github.CurrentRepo()
	if err != nil {
		return github.Repo{}, fmt.Errorf("resolving GitHub repository: %w", err)
	}
	return repo, nil
}

func (m Model) applyRemote(msg remoteLoadedMsg) (tea.Model, tea.Cmd) {
	m.loadingRemote = false
	var cmd tea.Cmd
	if m.reloadRemote {
		m.reloadRemote = false
		cmd = m.requestRemote()
	}
	if msg.client != nil {
		m.gh = msg.client
	}
	m.remoteErr = msg.err
	if msg.err != nil {
		return m, cmd
	}
	m.prs = msg.prs
	m.lastRemote = msg.at
	m.cachedAt = time.Time{}
	fallback := ""
	if bi := m.hoveredBranch(); bi >= 0 {
		fallback = m.snap.Branches[bi].Name
	}
	m.buildRows()
	m.restoreCursor(fallback)
	m.ensureVisible()
	prs, at := msg.prs, msg.at
	return m, tea.Batch(
		cmd,
		m.requestReviewers(),
		m.requestSuggested(),
		m.save(func(c *cache.Cache, repo github.Repo) error { return c.SavePRs(repo, prs, at) }),
	)
}

// reviewersMaxAge is how long a reviewer list is used before it is fetched
// again. People rarely join or leave a repository.
const reviewersMaxAge = 24 * time.Hour

// requestReviewers loads the people who can review PRs in the background,
// once the repository is known, unless a list fetched within
// reviewersMaxAge is loaded. A failure is retried after the next remote load.
func (m *Model) requestReviewers() tea.Cmd {
	if m.loadingReviewers || m.gh == nil ||
		(m.reviewersLoaded && m.now().Sub(m.reviewersAt) < reviewersMaxAge) {
		return nil
	}
	m.loadingReviewers = true
	client := m.gh
	return func() tea.Msg {
		at := time.Now()
		reviewers, err := client.FetchReviewers(context.Background())
		return reviewersLoadedMsg{reviewers: reviewers, at: at, err: err}
	}
}

func (m Model) applyReviewers(msg reviewersLoadedMsg) (Model, tea.Cmd) {
	m.loadingReviewers = false
	m.reviewersErr = msg.err
	if msg.err != nil {
		return m, nil
	}
	m.reviewers = msg.reviewers
	m.reviewersAt = msg.at
	m.reviewersLoaded = true
	if m.picker != nil {
		m.picker.setCandidates(m.reviewers)
		m.ensurePickerVisible()
	}
	reviewers, at := msg.reviewers, msg.at
	return m, m.save(func(c *cache.Cache, repo github.Repo) error {
		return c.SaveReviewers(repo, reviewers, at)
	})
}

// requestSuggested loads GitHub's reviewer suggestions in the background for
// the drafts that would open the reviewer picker, and for the PR the picker
// is asking about. They are slow for GitHub to compute, so they are
// fetched once per head commit rather than every poll.
func (m *Model) requestSuggested() tea.Cmd {
	if m.loadingSuggested || m.gh == nil {
		return nil
	}
	heads := map[int]string{}
	var numbers []int
	seen := map[int]bool{}
	add := func(pr *github.PR) {
		if seen[pr.Number] {
			return
		}
		seen[pr.Number] = true
		if s, ok := m.suggested[pr.Number]; ok && s.head == pr.HeadSHA {
			return
		}
		heads[pr.Number] = pr.HeadSHA
		numbers = append(numbers, pr.Number)
	}
	for _, pr := range m.prs {
		if pr.State == "OPEN" && needsReviewers(pr) {
			add(pr)
		}
	}
	if m.picker != nil && m.picker.step.pr != nil {
		add(m.picker.step.pr)
	}
	if len(numbers) == 0 {
		return nil
	}
	m.loadingSuggested = true
	client := m.gh
	return func() tea.Msg {
		at := time.Now()
		logins, err := client.FetchSuggested(context.Background(), numbers)
		return suggestedLoadedMsg{heads: heads, logins: logins, at: at, err: err}
	}
}

// applySuggested records suggestions, and reranks the active picker. A failed
// load is retried after the next remote load.
func (m Model) applySuggested(msg suggestedLoadedMsg) (Model, tea.Cmd) {
	m.loadingSuggested = false
	if msg.err != nil {
		return m, nil
	}
	if m.suggested == nil {
		m.suggested = map[int]suggestion{}
	}
	saved := map[int]cache.Suggestion{}
	for n, head := range msg.heads {
		m.suggested[n] = suggestion{head: head, logins: msg.logins[n]}
		saved[n] = cache.Suggestion{Head: head, Logins: msg.logins[n]}
	}
	var cmd tea.Cmd
	if m.picker != nil {
		m.picker.suggested = m.suggestionsFor(m.picker.need)
		m.picker.setCandidates(m.reviewers)
		m.ensurePickerVisible()
		// The picker may have opened while another load was running, so
		// fetch its suggestions if they were not part of that load.
		cmd = m.requestSuggested()
	}
	at := msg.at
	return m, combine(cmd, m.save(func(c *cache.Cache, repo github.Repo) error {
		return c.SaveSuggested(repo, saved, at)
	}))
}

// suggestionsFor merges the suggestions for PR numbers, in order.
func (m *Model) suggestionsFor(numbers []int) []string {
	var logins []string
	for _, n := range numbers {
		for _, login := range m.suggested[n].logins {
			if !slices.Contains(logins, login) {
				logins = append(logins, login)
			}
		}
	}
	return logins
}
