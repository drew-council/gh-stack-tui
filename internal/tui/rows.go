package tui

import (
	"github.com/drew-council/gh-stack-tui/internal/git"
	"github.com/drew-council/gh-stack-tui/internal/github"
)

type rowKind int

const (
	rowBranch rowKind = iota
	rowSection
	rowFile
	rowCommit
	rowWorkflow
	rowCheck
	rowSpacer
	rowSeparator
	rowTrunk
)

const (
	secFiles   = "files"
	secCommits = "commits"
	secChecks  = "checks"
)

// row is one navigable (or decorative) entry of the flattened stack tree.
type row struct {
	kind rowKind
	// key identifies the row across reloads so the cursor and expansion state
	// survive a refresh.
	key string
	// branch indexes snapshot.Branches, or -1 for rows outside any branch.
	branch   int
	section  string
	file     *git.FileChange
	commit   *git.Commit
	workflow *github.Workflow
	check    *github.Check
	label    string
	height   int
	y        int // first line of the row within the body
}

func (r row) selectable() bool {
	return r.kind != rowSpacer && r.kind != rowSeparator && r.kind != rowTrunk
}

func sectionKey(branch, section string) string { return section + "\x00" + branch }

func workflowKey(branch, workflow string) string { return "wf\x00" + branch + "\x00" + workflow }

// buildRows flattens the snapshot into display rows, top of the stack first.
func (m *Model) buildRows() {
	m.rows = m.rows[:0]
	if m.snap == nil {
		return
	}
	add := func(r row) {
		if r.height == 0 {
			r.height = 1
		}
		if n := len(m.rows); n > 0 {
			r.y = m.rows[n-1].y + m.rows[n-1].height
		}
		m.rows = append(m.rows, r)
	}

	prevMerged := false
	branches := m.snap.Branches
	for i := len(branches) - 1; i >= 0; i-- {
		b := &branches[i]
		merged := m.merged(i)
		if merged && !prevMerged && i < len(branches)-1 {
			add(row{kind: rowSeparator, branch: -1, label: "merged"})
		}
		prevMerged = merged

		height := 1
		if m.prNumber(i) != 0 {
			height = 2
		}
		add(row{kind: rowBranch, key: "branch\x00" + b.Name, branch: i, height: height})

		if len(b.Files) > 0 {
			add(
				row{
					kind:    rowSection,
					key:     sectionKey(b.Name, secFiles),
					branch:  i,
					section: secFiles,
				},
			)
			if m.expanded[sectionKey(b.Name, secFiles)] {
				for fi := range b.Files {
					f := &b.Files[fi]
					add(
						row{
							kind:   rowFile,
							key:    "file\x00" + b.Name + "\x00" + f.Path,
							branch: i,
							file:   f,
						},
					)
				}
			}
		}
		if len(b.Commits) > 0 {
			add(
				row{
					kind:    rowSection,
					key:     sectionKey(b.Name, secCommits),
					branch:  i,
					section: secCommits,
				},
			)
			if m.expanded[sectionKey(b.Name, secCommits)] {
				for ci := range b.Commits {
					c := &b.Commits[ci]
					add(
						row{
							kind:   rowCommit,
							key:    "commit\x00" + b.Name + "\x00" + c.SHA,
							branch: i,
							commit: c,
						},
					)
				}
			}
		}
		if pr := m.prFor(i); pr != nil && len(pr.Checks.Workflows) > 0 {
			add(
				row{
					kind:    rowSection,
					key:     sectionKey(b.Name, secChecks),
					branch:  i,
					section: secChecks,
				},
			)
			if m.expanded[sectionKey(b.Name, secChecks)] {
				for wi := range pr.Checks.Workflows {
					w := &pr.Checks.Workflows[wi]
					wk := workflowKey(b.Name, w.Name)
					add(row{kind: rowWorkflow, key: wk, branch: i, workflow: w})
					if m.expanded[wk] {
						for ci := range w.Checks {
							c := &w.Checks[ci]
							add(
								row{
									kind:     rowCheck,
									key:      wk + "\x00" + c.Name,
									branch:   i,
									workflow: w,
									check:    c,
								},
							)
						}
					}
				}
			}
		}
		add(row{kind: rowSpacer, branch: i})
	}
	add(row{kind: rowTrunk, branch: -1, label: m.snap.Trunk})
}

// restoreCursor puts the cursor back on the row it was on before a rebuild,
// falling back to that row's branch, then to the nearest selectable row.
func (m *Model) restoreCursor(fallbackBranch string) {
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	for i, r := range m.rows {
		if r.key != "" && r.key == m.cursorKey {
			m.cursor = i
			return
		}
	}
	for i, r := range m.rows {
		if r.kind == rowBranch && m.snap.Branches[r.branch].Name == fallbackBranch {
			m.setCursor(i)
			return
		}
	}
	m.setCursor(min(m.cursor, len(m.rows)-1))
}

// setCursor moves to idx, sliding to the nearest selectable row.
func (m *Model) setCursor(idx int) {
	if len(m.rows) == 0 {
		return
	}
	idx = max(0, min(idx, len(m.rows)-1))
	for d := 0; d < len(m.rows); d++ {
		for _, i := range []int{idx + d, idx - d} {
			if i >= 0 && i < len(m.rows) && m.rows[i].selectable() {
				m.cursor = i
				m.cursorKey = m.rows[i].key
				return
			}
		}
	}
}

// moveCursor steps delta selectable rows.
func (m *Model) moveCursor(delta int) {
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	i := m.cursor
	for ; delta > 0; delta-- {
		next := i + step
		for next >= 0 && next < len(m.rows) && !m.rows[next].selectable() {
			next += step
		}
		if next < 0 || next >= len(m.rows) {
			break
		}
		i = next
	}
	m.cursor = i
	if i < len(m.rows) {
		m.cursorKey = m.rows[i].key
	}
}

// jumpBranch moves to the next (delta > 0) or previous branch header.
func (m *Model) jumpBranch(delta int) {
	for i := m.cursor + delta; i >= 0 && i < len(m.rows); i += delta {
		if m.rows[i].kind == rowBranch {
			m.setCursor(i)
			return
		}
	}
}

// hovered returns the row under the cursor.
func (m *Model) hovered() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) || !m.rows[m.cursor].selectable() {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

// branchRow returns the index of the header row of branch bi.
func (m *Model) branchRow(bi int) int {
	for i, r := range m.rows {
		if r.kind == rowBranch && r.branch == bi {
			return i
		}
	}
	return -1
}
