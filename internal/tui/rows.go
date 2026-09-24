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

// row is one entry of the flattened stack tree. Rows are the unit of layout
// (each knows its line offset and height) and, for branches and the items
// inside them, the unit of navigation.
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

// item reports whether the row is a file, commit, workflow, or check: the
// rows inside a branch that can be focused after descending into it.
func (r row) item() bool {
	switch r.kind {
	case rowFile, rowCommit, rowWorkflow, rowCheck:
		return true
	}
	return false
}

func sectionKey(branch, section string) string { return section + "\x00" + branch }

func workflowKey(branch, workflow string) string { return "wf\x00" + branch + "\x00" + workflow }

func branchKey(branch string) string { return "branch\x00" + branch }

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

		// Like gh stack view, a branch with a PR takes two lines: the PR
		// line on top, then the branch line.
		height := 1
		if m.prNumber(i) != 0 {
			height = 2
		}
		add(row{kind: rowBranch, key: branchKey(b.Name), branch: i, height: height})

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

// navigable reports whether the cursor may rest on row i. Branches are the
// unit of navigation; merged branches are skipped like gh stack view does.
// Items inside a branch are reachable only by descending into it.
func (m *Model) navigable(i int) bool {
	if i < 0 || i >= len(m.rows) {
		return false
	}
	r := m.rows[i]
	switch {
	case r.kind == rowBranch:
		return !m.merged(r.branch)
	case r.item():
		return true
	}
	return false
}

// restoreCursor puts the cursor back on the row it was on before a rebuild,
// falling back to that row's branch, then to the nearest navigable row.
func (m *Model) restoreCursor(fallbackBranch string) {
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	for i, r := range m.rows {
		if r.key != "" && r.key == m.cursorKey && m.navigable(i) {
			m.cursor = i
			return
		}
	}
	if i := m.branchRow(m.snap.IndexOf(fallbackBranch)); i >= 0 {
		m.setCursor(i)
		return
	}
	m.setCursor(min(m.cursor, len(m.rows)-1))
}

// setCursor moves to idx, sliding to the nearest navigable row.
func (m *Model) setCursor(idx int) {
	if len(m.rows) == 0 {
		return
	}
	idx = max(0, min(idx, len(m.rows)-1))
	for d := 0; d < len(m.rows); d++ {
		for _, i := range []int{idx + d, idx - d} {
			if m.navigable(i) {
				m.cursor = i
				m.cursorKey = m.rows[i].key
				return
			}
		}
	}
}

// hovered returns the row under the cursor.
func (m *Model) hovered() (row, bool) {
	if !m.navigable(m.cursor) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

// hoveredBranch returns the index of the branch the cursor is on or inside.
func (m *Model) hoveredBranch() int {
	if r, ok := m.hovered(); ok {
		return r.branch
	}
	return -1
}

// branchRow returns the index of the header row of branch bi, or -1.
func (m *Model) branchRow(bi int) int {
	if bi < 0 {
		return -1
	}
	for i, r := range m.rows {
		if r.kind == rowBranch && r.branch == bi {
			return i
		}
	}
	return -1
}

// moveCursor is j/k. On a branch it steps to the next or previous branch.
// Inside a branch it steps through that branch's items; k on the first item
// climbs back onto the branch, and j on the last one stays put.
func (m *Model) moveCursor(delta int) {
	r, ok := m.hovered()
	if !ok || r.kind == rowBranch {
		m.jumpBranch(delta)
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for ; delta != 0; delta -= step {
		next := m.cursor + step
		for next >= 0 && next < len(m.rows) && m.rows[next].branch == r.branch &&
			!m.rows[next].item() && m.rows[next].kind != rowBranch {
			next += step
		}
		if next < 0 || next >= len(m.rows) || m.rows[next].branch != r.branch {
			return
		}
		if m.rows[next].kind == rowBranch {
			m.setCursor(next)
			return
		}
		m.setCursor(next)
	}
}

// jumpBranch is J/K: the next (delta > 0) or previous unmerged branch,
// counted from the branch the cursor is on or inside.
func (m *Model) jumpBranch(delta int) {
	step := 1
	if delta < 0 {
		step = -1
	}
	from := m.branchRow(m.hoveredBranch())
	if from < 0 {
		from = m.cursor
	}
	last := from
	for ; delta != 0; delta -= step {
		i := last + step
		for i >= 0 && i < len(m.rows) && !(m.rows[i].kind == rowBranch && m.navigable(i)) {
			i += step
		}
		if i < 0 || i >= len(m.rows) {
			break
		}
		last = i
	}
	if last != from || m.rows[from].kind == rowBranch {
		m.cursor = last
		m.cursorKey = m.rows[last].key
	}
}

// firstBranch and lastBranch move to the top and bottom unmerged branches.
func (m *Model) firstBranch() {
	for i := range m.rows {
		if m.rows[i].kind == rowBranch && m.navigable(i) {
			m.cursor, m.cursorKey = i, m.rows[i].key
			return
		}
	}
}

func (m *Model) lastBranch() {
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].kind == rowBranch && m.navigable(i) {
			m.cursor, m.cursorKey = i, m.rows[i].key
			return
		}
	}
}

// pageBranch moves about half a screen of lines in the given direction,
// landing on the branch nearest to that point.
func (m *Model) pageBranch(lines int) {
	if len(m.rows) == 0 {
		return
	}
	target := m.rows[max(0, min(m.cursor, len(m.rows)-1))].y + lines
	best, bestDist := -1, -1
	for i, r := range m.rows {
		if r.kind != rowBranch || !m.navigable(i) {
			continue
		}
		if (lines > 0 && i <= m.cursor) || (lines < 0 && i >= m.cursor) {
			continue
		}
		d := max(r.y, target) - min(r.y, target)
		if best < 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	if best >= 0 {
		m.cursor, m.cursorKey = best, m.rows[best].key
	} else if lines > 0 {
		m.lastBranch()
	} else {
		m.firstBranch()
	}
}

// firstItem returns the index of the first item row inside branch bi, or -1.
func (m *Model) firstItem(bi int) int {
	for i := m.branchRow(bi) + 1; i > 0 && i < len(m.rows) && m.rows[i].branch == bi; i++ {
		if m.rows[i].item() {
			return i
		}
	}
	return -1
}

// nodeSpan returns the first line and the line after the last of branch bi's
// rows, spacer included.
func (m *Model) nodeSpan(bi int) (start, end int) {
	i := m.branchRow(bi)
	if i < 0 {
		return 0, 0
	}
	start = m.rows[i].y
	end = start
	for ; i < len(m.rows) && m.rows[i].branch == bi; i++ {
		end = m.rows[i].y + m.rows[i].height
	}
	return start, end
}
