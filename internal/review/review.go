// Package review turns the pull requests GitHub says wait on the viewer into
// a queue: what each one needs from the viewer, and how they stack.
package review

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// Need is what a pull request needs from the viewer.
type Need int

const (
	// NeedNone: nothing. The PR is in the queue as part of a stack.
	NeedNone Need = iota
	// NeedReview: review is requested and the viewer has not reviewed.
	NeedReview
	// NeedReReview: the viewer reviewed, and was asked again or the PR
	// changed since.
	NeedReReview
	// NeedWaiting: the viewer reviewed and the author has not answered.
	NeedWaiting
	// NeedApproved: the viewer approved.
	NeedApproved
)

// Hidden is why a requested pull request is left out of the queue.
type Hidden int

const (
	HideNone Hidden = iota
	HideDraft
	HideBot
	HideWaiting
	HideApproved
)

// String names the reason, as the queue counts it.
func (h Hidden) String() string {
	switch h {
	case HideDraft:
		return "draft"
	case HideBot:
		return "bot"
	case HideWaiting:
		return "waiting"
	case HideApproved:
		return "approved"
	}
	return ""
}

// Options are the kinds of requested pull requests the queue keeps.
type Options struct {
	// Drafts keeps draft PRs, and Bots PRs opened by bots such as Dependabot.
	Drafts bool
	Bots   bool
}

// Item is one pull request of the queue.
type Item struct {
	Number int
	// PR is nil when GitHub lists the number in a stack but did not return
	// the pull request.
	PR   *github.ReviewPR
	Need Need
	// Why says what brings a PR back for another review.
	Why    string
	Hidden Hidden
	// Base is the number of the open pull request this one is based on, when
	// its base is the branch of another PR of the queue rather than trunk.
	Base int
}

// ForYou reports whether the PR waits on the viewer now.
func (it Item) ForYou() bool {
	return it.PR != nil && it.PR.Requested && it.Hidden == HideNone
}

// Group is a stack, or one pull request outside any stack.
type Group struct {
	// Stack is nil for a pull request outside any stack.
	Stack *github.Stack
	// Items are top of the stack first.
	Items []Item
	// Author opened the group's requested pull requests.
	Author string
}

// ForYou counts the PRs of the group that wait on the viewer.
func (g Group) ForYou() int {
	n := 0
	for _, it := range g.Items {
		if it.ForYou() {
			n++
		}
	}
	return n
}

// Requested reports whether review is requested on any PR of the group,
// including the hidden ones.
func (g Group) Requested() bool {
	return slices.ContainsFunc(g.Items, func(it Item) bool {
		return it.PR != nil && it.PR.Requested
	})
}

// Start is the PR to review first: the lowest one that waits on the viewer.
// It returns 0 when none does.
func (g Group) Start() int {
	for _, it := range slices.Backward(g.Items) {
		if it.ForYou() {
			return it.Number
		}
	}
	return 0
}

// Classify says what pr needs from the viewer, and for another review, why.
func Classify(pr *github.ReviewPR) (Need, string) {
	m := pr.Mine
	if m == nil || m.State == "PENDING" {
		if pr.Requested {
			return NeedReview, ""
		}
		return NeedNone, ""
	}
	if pr.Requested {
		switch {
		// GitHub drops a request by name once the person reviews, so a
		// standing one was made after the review. A team's request stays.
		case pr.Direct || pr.AskedAt.After(m.At):
			return NeedReReview, "asked again"
		case m.State == "DISMISSED":
			return NeedReReview, "review dismissed"
		case m.State != "APPROVED" && m.Commit != "" && m.Commit != pr.HeadSHA:
			return NeedReReview, "new commits"
		}
	}
	if m.State == "APPROVED" {
		return NeedApproved, ""
	}
	if pr.State != "OPEN" {
		return NeedNone, ""
	}
	return NeedWaiting, ""
}

// item classifies pr for the queue.
func item(pr *github.ReviewPR, opts Options) Item {
	it := Item{Number: pr.Number, PR: pr}
	it.Need, it.Why = Classify(pr)
	if !pr.Requested {
		return it
	}
	switch {
	case pr.IsDraft && !opts.Drafts:
		it.Hidden = HideDraft
	case pr.AuthorBot && !opts.Bots:
		it.Hidden = HideBot
	case it.Need == NeedWaiting:
		it.Hidden = HideWaiting
	case it.Need == NeedApproved:
		it.Hidden = HideApproved
	}
	return it
}

// Build groups the queue's pull requests by stack. Groups are ordered by
// author, then by their oldest requested pull request, so they stay put as
// the queue refreshes.
func Build(q *github.Queue, opts Options) []Group {
	if q == nil {
		return nil
	}
	byNumber := map[int]*github.ReviewPR{}
	byHead := map[string]int{}
	for _, pr := range q.PRs {
		byNumber[pr.Number] = pr
		if pr.State == "OPEN" {
			byHead[pr.HeadRef] = pr.Number
		}
	}
	item := func(pr *github.ReviewPR) Item {
		it := item(pr, opts)
		if pr.BaseRef != q.Trunk {
			it.Base = byHead[pr.BaseRef]
		}
		return it
	}
	stacked := map[int]struct{}{}
	var groups []Group
	for i := range q.Stacks {
		s := &q.Stacks[i]
		g := Group{Stack: s}
		for _, n := range slices.Backward(s.PRs) {
			stacked[n] = struct{}{}
			if pr := byNumber[n]; pr != nil {
				g.Items = append(g.Items, item(pr))
			} else {
				g.Items = append(g.Items, Item{Number: n})
			}
		}
		if g.Requested() {
			groups = append(groups, g)
		}
	}
	for _, pr := range q.PRs {
		if _, ok := stacked[pr.Number]; !ok && pr.Requested {
			groups = append(groups, Group{Items: []Item{item(pr)}})
		}
	}
	for i := range groups {
		groups[i].Author = author(groups[i])
	}
	slices.SortStableFunc(groups, func(a, b Group) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Author), strings.ToLower(b.Author)),
			cmp.Compare(oldest(a), oldest(b)),
		)
	})
	return groups
}

// author is who opened most of the group's requested PRs.
func author(g Group) string {
	counts := map[string]int{}
	best := ""
	for _, it := range g.Items {
		if it.PR == nil || !it.PR.Requested {
			continue
		}
		counts[it.PR.Author]++
		if best == "" || counts[it.PR.Author] > counts[best] {
			best = it.PR.Author
		}
	}
	return best
}

// oldest is the lowest number among the group's requested PRs.
func oldest(g Group) int {
	n := 0
	for _, it := range g.Items {
		if it.PR != nil && it.PR.Requested && (n == 0 || it.Number < n) {
			n = it.Number
		}
	}
	return n
}

// ShortTeams maps each team slug to a short name for narrow columns, by
// dropping an affix that several of the teams share: go-readability and
// api-readability become go and api.
func ShortTeams(teams []string) map[string]string {
	out := map[string]string{}
	for _, t := range teams {
		out[t] = t
	}
	teams = slices.Sorted(maps.Keys(out))
	suffix := sharedAffix(teams, func(s string, i int) string { return s[i:] })
	prefix := sharedAffix(teams, func(s string, i int) string { return s[:i+1] })
	for _, t := range teams {
		short := t
		if suffix != "" && strings.HasSuffix(short, suffix) {
			short = strings.TrimSuffix(short, suffix)
		}
		if prefix != "" && strings.HasPrefix(short, prefix) {
			short = strings.TrimPrefix(short, prefix)
		}
		if short != "" {
			out[t] = short
		}
	}
	return out
}

// sharedAffix is the longest affix that at least two of names share, where
// cut returns the affix of a name on one side of the dash at i.
func sharedAffix(names []string, cut func(s string, i int) string) string {
	counts := map[string]int{}
	for _, n := range names {
		for i := range len(n) {
			if n[i] == '-' {
				counts[cut(n, i)]++
			}
		}
	}
	best := ""
	for affix, n := range counts {
		// A dash and at least three letters.
		if n < 2 || len(affix) < 4 {
			continue
		}
		if len(affix) > len(best) || len(affix) == len(best) && affix < best {
			best = affix
		}
	}
	return best
}
