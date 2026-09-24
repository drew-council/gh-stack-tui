package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

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
		repo github.Repo
		prs  map[string]*github.PR
		err  error
	}
)

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
	repo := m.repo
	return func() tea.Msg {
		var extra []string
		for _, p := range stack.StateFiles(repo.GitDir) {
			if fi, err := os.Stat(p); err == nil {
				extra = append(
					extra,
					fmt.Sprintf("%s %d %d", p, fi.ModTime().UnixNano(), fi.Size()),
				)
			}
		}
		fp, err := repo.Fingerprint(extra...)
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
	repo := m.repo
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
			if f, err := stack.ReadFile(repo.GitDir); err == nil {
				if i := f.FindNumber(wantNumber); i >= 0 {
					pinned = f.Stacks[i].Key()
				}
			}
		}
		snap, err := stack.Load(repo, pinned, previous)
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

	if lookupsChanged {
		cmds = append(cmds, m.requestRemote())
	}
	return m, tea.Batch(cmds...)
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
	repo := m.ghRepo
	repository := m.snap.Repository
	root := m.repo.Root
	var lookups []github.Lookup
	for _, b := range m.snap.Branches {
		l := github.Lookup{Branch: b.Name}
		if b.PR != nil {
			l.Number = b.PR.Number
		}
		lookups = append(lookups, l)
	}
	return func() tea.Msg {
		if repo.Owner == "" {
			var err error
			if repo, err = resolveRepo(root, repository); err != nil {
				return remoteLoadedMsg{err: err}
			}
		}
		prs, err := github.FetchPRs(context.Background(), repo, lookups)
		return remoteLoadedMsg{repo: repo, prs: prs, err: err}
	}
}

// resolveRepo reads the repository from the stack file's "host:owner/name",
// falling back to asking gh about the current directory.
func resolveRepo(root, repository string) (github.Repo, error) {
	if host, rest, ok := strings.Cut(repository, ":"); ok {
		if owner, name, ok := strings.Cut(rest, "/"); ok && owner != "" && name != "" {
			return github.Repo{Host: host, Owner: owner, Name: name}, nil
		}
	}
	cmd := exec.Command("gh", "repo", "view", "--json", "owner,name,url")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return github.Repo{}, fmt.Errorf("resolving GitHub repository: %w", err)
	}
	var v struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return github.Repo{}, err
	}
	host := "github.com"
	if u, err := url.Parse(v.URL); err == nil && u.Host != "" {
		host = u.Host
	}
	return github.Repo{Host: host, Owner: v.Owner.Login, Name: v.Name}, nil
}

func (m Model) applyRemote(msg remoteLoadedMsg) (tea.Model, tea.Cmd) {
	m.loadingRemote = false
	var cmd tea.Cmd
	if m.reloadRemote {
		m.reloadRemote = false
		cmd = m.requestRemote()
	}
	m.remoteErr = msg.err
	if msg.err != nil {
		return m, cmd
	}
	m.ghRepo = msg.repo
	m.prs = msg.prs
	m.lastRemote = time.Now()
	fallback := ""
	if bi := m.hoveredBranch(); bi >= 0 {
		fallback = m.snap.Branches[bi].Name
	}
	m.buildRows()
	m.restoreCursor(fallback)
	m.ensureVisible()
	return m, cmd
}
