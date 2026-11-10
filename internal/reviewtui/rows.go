package reviewtui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/drew-council/gh-stack-tui/internal/review"
)

type rowKind int

const (
	rowPR rowKind = iota
	// rowStack heads a stack.
	rowStack
	// rowFold stands for a run of a stack's PRs that do not wait on the
	// viewer.
	rowFold
	rowTrunk
	rowSpacer
)

// row is one line of the queue.
type row struct {
	kind rowKind
	// key identifies the row across reloads so the cursor survives a refresh.
	key string
	// group indexes Model.shown.
	group int
	// item indexes the group's Items: the PR of a rowPR, or the first PR of a
	// rowFold. count is how many PRs a rowFold stands for.
	item  int
	count int
}

// foldMin is the shortest run of PRs that folds into one line.
const foldMin = 3

// stackNumber is the number of g's stack, or 0 outside any stack.
func stackNumber(g review.Group) int {
	if g.Stack == nil {
		return 0
	}
	return g.Stack.Number
}

func prKey(g review.Group, number int) string {
	return fmt.Sprintf("pr/%d/%d", stackNumber(g), number)
}

// standsOut reports whether it gets a line of its own in a collapsed stack:
// it waits on the viewer, is marked, or is a hidden PR that is being shown.
func (m *Model) standsOut(it review.Item) bool {
	if _, marked := m.marks[it.Number]; marked {
		return true
	}
	return it.ForYou() || m.showHidden && it.PR != nil && it.PR.Requested
}

// visible reports whether g is on screen: something in it waits on the
// viewer, or hidden PRs are shown, and it passes the filter.
func (m *Model) visible(g review.Group) bool {
	if g.ForYou() == 0 && !(m.showHidden && g.Requested()) {
		return false
	}
	terms := strings.Fields(strings.ToLower(m.filter))
	if len(terms) == 0 {
		return true
	}
	var b strings.Builder
	b.WriteString(g.Author)
	if g.Stack != nil {
		fmt.Fprintf(&b, " stack #%d", g.Stack.Number)
	}
	for _, it := range g.Items {
		fmt.Fprintf(&b, " #%d", it.Number)
		if pr := it.PR; pr != nil {
			fmt.Fprintf(&b, " %s %s %s %s %s",
				pr.Title, pr.Author, pr.HeadRef,
				strings.Join(pr.Via, " "), strings.Join(pr.Labels, " "))
		}
	}
	hay := strings.ToLower(b.String())
	for _, t := range terms {
		if !strings.Contains(hay, t) {
			return false
		}
	}
	return true
}

// buildRows lays the visible groups out as rows.
func (m *Model) buildRows() {
	m.shown = m.shown[:0]
	m.rows = m.rows[:0]
	for _, g := range m.groups {
		if m.visible(g) {
			m.shown = append(m.shown, g)
		}
	}
	for gi, g := range m.shown {
		if gi > 0 {
			// Stacks stand apart, and so do the PRs of different authors.
			prev := m.shown[gi-1]
			if g.Stack != nil || prev.Stack != nil || g.Author != prev.Author {
				m.rows = append(m.rows, row{kind: rowSpacer, group: gi})
			}
		}
		pr := func(i int) {
			m.rows = append(m.rows, row{
				kind: rowPR, key: prKey(g, g.Items[i].Number), group: gi, item: i,
			})
		}
		if g.Stack == nil {
			pr(0)
			continue
		}
		n := g.Stack.Number
		m.rows = append(m.rows, row{kind: rowStack, key: "stack/" + strconv.Itoa(n), group: gi})
		_, open := m.expanded[n]
		for i := 0; i < len(g.Items); {
			if open || m.standsOut(g.Items[i]) {
				pr(i)
				i++
				continue
			}
			j := i
			for j < len(g.Items) && !m.standsOut(g.Items[j]) {
				j++
			}
			if j-i >= foldMin {
				m.rows = append(m.rows, row{
					kind:  rowFold,
					key:   fmt.Sprintf("fold/%d/%d", n, g.Items[i].Number),
					group: gi, item: i, count: j - i,
				})
			} else {
				for k := i; k < j; k++ {
					pr(k)
				}
			}
			i = j
		}
		m.rows = append(m.rows, row{kind: rowTrunk, group: gi})
	}
}

// rebuild lays the rows out again and puts the cursor back where it was.
func (m *Model) rebuild() {
	at, ok := m.hovered()
	var stack, number int
	if ok && at.group < len(m.shown) {
		g := m.shown[at.group]
		stack = stackNumber(g)
		if at.kind != rowStack && at.item < len(g.Items) {
			number = g.Items[at.item].Number
		}
	}
	m.buildRows()
	m.restoreCursor(stack, number)
	m.ensureVisible()
	m.clampDetail()
}

// restoreCursor puts the cursor back on the row it was on. Failing that, it
// goes to the row that now holds PR number of stack, then to the stack's
// header, then to the nearest row. A new queue starts on the first PR that
// waits on the viewer.
func (m *Model) restoreCursor(stack, number int) {
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	if m.cursorKey == "" {
		m.cursor = 0
	}
	header := -1
	for i, r := range m.rows {
		if r.key != "" && r.key == m.cursorKey {
			m.cursor = i
			return
		}
		g := m.shown[r.group]
		if stack == 0 || stackNumber(g) != stack {
			continue
		}
		switch r.kind {
		case rowStack:
			header = i
		case rowPR, rowFold:
			for k := r.item; k < r.item+max(1, r.count); k++ {
				if number != 0 && g.Items[k].Number == number {
					m.setCursor(i)
					return
				}
			}
		}
	}
	if header >= 0 {
		m.setCursor(header)
		return
	}
	// The row is gone: take the next PR that waits on the viewer, so enter
	// never lands on a whole stack by surprise.
	at := min(m.cursor, len(m.rows)-1)
	for _, i := range append(seq(at, len(m.rows), 1), seq(at-1, -1, -1)...) {
		if r := m.rows[i]; r.kind == rowPR && m.shown[r.group].Items[r.item].ForYou() {
			m.setCursor(i)
			return
		}
	}
	m.setCursor(at)
}

// seq counts from lo up or down to hi, which is left out.
func seq(lo, hi, step int) []int {
	var out []int
	for i := lo; i != hi; i += step {
		out = append(out, i)
	}
	return out
}

func (m *Model) navigable(i int) bool {
	if i < 0 || i >= len(m.rows) {
		return false
	}
	switch m.rows[i].kind {
	case rowPR, rowStack, rowFold:
		return true
	}
	return false
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
				if m.rows[i].key != m.cursorKey {
					m.detailScroll = 0
				}
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

// hoveredItem returns the PR under the cursor, when the cursor is on one.
func (m *Model) hoveredItem() (review.Item, bool) {
	r, ok := m.hovered()
	if !ok || r.kind != rowPR {
		return review.Item{}, false
	}
	return m.shown[r.group].Items[r.item], true
}

// moveCursor steps delta navigable rows.
func (m *Model) moveCursor(delta int) {
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	i := m.cursor
	for delta > 0 {
		next := i + step
		for next >= 0 && next < len(m.rows) && !m.navigable(next) {
			next += step
		}
		if next < 0 || next >= len(m.rows) {
			break
		}
		i = next
		delta--
	}
	m.setCursor(i)
}

// groupStart is the first navigable row of group gi, or -1.
func (m *Model) groupStart(gi int) int {
	for i, r := range m.rows {
		if r.group == gi && m.navigable(i) {
			return i
		}
	}
	return -1
}

// jumpGroup moves to the start of the next group, or back to the start of
// this one and then of the one before.
func (m *Model) jumpGroup(delta int) {
	r, ok := m.hovered()
	if !ok {
		return
	}
	gi := r.group
	if delta > 0 {
		gi++
	} else if m.groupStart(gi) == m.cursor {
		gi--
	}
	if i := m.groupStart(gi); i >= 0 {
		m.setCursor(i)
	}
}

func (m *Model) firstRow() { m.setCursor(0) }

func (m *Model) lastRow() {
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.navigable(i) {
			m.setCursor(i)
			return
		}
	}
}

// --- selection ---

// inVisual reports whether row i is inside the visual range. The range takes
// the PRs that stand out, not the ones that are in a stack only for context.
func (m *Model) inVisual(i int) bool {
	if !m.visual || m.rows[i].kind != rowPR {
		return false
	}
	a := -1
	for k, r := range m.rows {
		if r.key == m.anchor {
			a = k
			break
		}
	}
	if a < 0 || i < min(a, m.cursor) || i > max(a, m.cursor) {
		return false
	}
	r := m.rows[i]
	return m.standsOut(m.shown[r.group].Items[r.item])
}

// isSelected reports whether the PR of row i is marked or in the visual range.
func (m *Model) isSelected(i int) bool {
	r := m.rows[i]
	if r.kind != rowPR {
		return false
	}
	if _, ok := m.marks[m.shown[r.group].Items[r.item].Number]; ok {
		return true
	}
	return m.inVisual(i)
}

// selection is the set of selected PR numbers.
func (m *Model) selection() map[int]struct{} {
	sel := map[int]struct{}{}
	for n := range m.marks {
		sel[n] = struct{}{}
	}
	for i, r := range m.rows {
		if m.inVisual(i) {
			sel[m.shown[r.group].Items[r.item].Number] = struct{}{}
		}
	}
	return sel
}

// targets are the PRs an action applies to, in the order to review them:
// groups as listed, and each stack from the bottom up. They are the
// selection, else the hovered PR, else the PRs of the hovered stack that
// wait on the viewer. several reports whether they came from a selection or
// a stack, rather than from the cursor alone.
func (m *Model) targets() (items []review.Item, several bool) {
	sel := m.selection()
	r, hovering := m.hovered()
	if len(sel) == 0 && hovering && r.kind == rowPR {
		if it := m.shown[r.group].Items[r.item]; it.PR != nil {
			return []review.Item{it}, false
		}
		return nil, false
	}
	seen := map[int]struct{}{}
	for gi, g := range m.shown {
		for k := len(g.Items) - 1; k >= 0; k-- {
			it := g.Items[k]
			_, picked := sel[it.Number]
			if len(sel) == 0 {
				picked = hovering && r.kind == rowStack && r.group == gi && it.ForYou()
			}
			if _, dup := seen[it.Number]; picked && !dup && it.PR != nil {
				seen[it.Number] = struct{}{}
				items = append(items, it)
			}
		}
	}
	return items, true
}

func (m *Model) clearSelection() {
	m.marks = map[int]struct{}{}
	m.visual = false
}

// toggleMark marks or unmarks the hovered PR and moves on. On a stack's
// header it marks every PR of the stack that waits on the viewer, or unmarks
// them when all are marked.
func (m *Model) toggleMark() {
	r, ok := m.hovered()
	if !ok {
		return
	}
	g := m.shown[r.group]
	switch r.kind {
	case rowPR:
		n := g.Items[r.item].Number
		if _, marked := m.marks[n]; marked {
			delete(m.marks, n)
		} else if g.Items[r.item].PR != nil {
			m.marks[n] = struct{}{}
		}
		m.moveCursor(1)
	case rowStack:
		all := true
		for _, it := range g.Items {
			if _, marked := m.marks[it.Number]; it.ForYou() && !marked {
				all = false
			}
		}
		for _, it := range g.Items {
			switch {
			case !it.ForYou():
			case all:
				delete(m.marks, it.Number)
			default:
				m.marks[it.Number] = struct{}{}
			}
		}
	}
}

func (m *Model) toggleVisual() {
	if m.visual {
		// Leaving visual mode keeps the range as marks, so it can be
		// extended with space.
		m.marks = m.selection()
		m.visual = false
		return
	}
	if r, ok := m.hovered(); ok {
		m.visual = true
		m.anchor = r.key
	}
}

// --- folds ---

// expand opens the hovered stack, so every PR of it shows.
func (m *Model) expand() {
	r, ok := m.hovered()
	if !ok || m.shown[r.group].Stack == nil {
		return
	}
	g := m.shown[r.group]
	m.expanded[g.Stack.Number] = struct{}{}
	if r.kind == rowFold {
		// Land on the first PR that was folded.
		m.cursorKey = prKey(g, g.Items[r.item].Number)
	}
	m.rebuild()
}

// collapse folds the hovered stack again.
func (m *Model) collapse() {
	r, ok := m.hovered()
	if !ok || m.shown[r.group].Stack == nil {
		return
	}
	delete(m.expanded, m.shown[r.group].Stack.Number)
	m.rebuild()
}
