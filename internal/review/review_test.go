package review

import (
	"fmt"
	"testing"
	"time"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

var now = time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC)

func ago(d time.Duration) time.Time { return now.Add(-d) }

func TestClassify(t *testing.T) {
	mine := func(state string, at time.Duration, commit string) *github.Review {
		return &github.Review{State: state, At: ago(at), Commit: commit}
	}
	tests := []struct {
		name string
		pr   github.ReviewPR
		need Need
		why  string
	}{
		{"asked, never reviewed", github.ReviewPR{Requested: true}, NeedReview, ""},
		{"not asked", github.ReviewPR{}, NeedNone, ""},
		{
			"an unsubmitted review is not a review",
			github.ReviewPR{Requested: true, Mine: &github.Review{State: "PENDING"}},
			NeedReview, "",
		},
		{
			"asked again after requesting changes",
			github.ReviewPR{
				Requested: true, HeadSHA: "b", AskedAt: ago(time.Hour),
				Mine: mine("CHANGES_REQUESTED", 2*time.Hour, "a"),
			},
			NeedReReview, "asked again",
		},
		{
			"asked again after approving",
			github.ReviewPR{
				Requested: true, HeadSHA: "a", AskedAt: ago(time.Hour),
				Mine: mine("APPROVED", 2*time.Hour, "a"),
			},
			NeedReReview, "asked again",
		},
		{
			// GitHub drops a request by name on review, so it was made since.
			"asked by name with a review, the events not saying when",
			github.ReviewPR{
				Requested: true,
				Direct:    true,
				HeadSHA:   "a",
				Mine:      mine("COMMENTED", time.Hour, "a"),
			},
			NeedReReview,
			"asked again",
		},
		{
			"a team's request outlives the viewer's approval",
			github.ReviewPR{
				Requested: true, HeadSHA: "b", AskedAt: ago(3 * time.Hour),
				Mine: mine("APPROVED", time.Hour, "a"),
			},
			NeedApproved, "",
		},
		{
			"changes pushed since the viewer asked for them",
			github.ReviewPR{
				Requested: true, HeadSHA: "b", AskedAt: ago(3 * time.Hour),
				Mine: mine("CHANGES_REQUESTED", time.Hour, "a"),
			},
			NeedReReview, "new commits",
		},
		{
			"nothing pushed since the viewer commented",
			github.ReviewPR{
				Requested: true, State: "OPEN", HeadSHA: "a", AskedAt: ago(3 * time.Hour),
				Mine: mine("COMMENTED", time.Hour, "a"),
			},
			NeedWaiting, "",
		},
		{
			"the viewer's approval was dismissed",
			github.ReviewPR{
				Requested: true, HeadSHA: "b", AskedAt: ago(3 * time.Hour),
				Mine: mine("DISMISSED", time.Hour, "a"),
			},
			NeedReReview, "review dismissed",
		},
		{
			"approved, and no longer asked",
			github.ReviewPR{State: "OPEN", Mine: mine("APPROVED", time.Hour, "a")},
			NeedApproved, "",
		},
		{
			"commented on a PR that then merged",
			github.ReviewPR{State: "MERGED", Mine: mine("COMMENTED", time.Hour, "a")},
			NeedNone, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if need, why := Classify(&tt.pr); need != tt.need || why != tt.why {
				t.Fatalf("got %v %q, want %v %q", need, why, tt.need, tt.why)
			}
		})
	}
}

// summary is the groups as text: stacks as s<number>[...] top first, and a
// PR as its number with ! when it waits on the viewer, or its hidden reason.
func summary(groups []Group) string {
	out := ""
	for _, g := range groups {
		if out != "" {
			out += " "
		}
		out += g.Author + ":"
		if g.Stack != nil {
			out += fmt.Sprintf("s%d[", g.Stack.Number)
		}
		for i, it := range g.Items {
			if i > 0 {
				out += ","
			}
			out += fmt.Sprint(it.Number)
			switch {
			case it.PR == nil:
				out += "?"
			case it.ForYou():
				out += "!"
			case it.Hidden != HideNone:
				out += "(" + it.Hidden.String() + ")"
			}
			if it.Base != 0 {
				out += fmt.Sprintf("^%d", it.Base)
			}
		}
		if g.Stack != nil {
			out += "]"
		}
	}
	return out
}

func queue() *github.Queue {
	pr := func(n int, author string, requested bool) *github.ReviewPR {
		return &github.ReviewPR{
			Number: n, Author: author, State: "OPEN", Requested: requested,
			HeadRef: fmt.Sprintf("b%d", n), BaseRef: "main",
		}
	}
	draft := pr(12, "zoe", true)
	draft.IsDraft = true
	bot := pr(13, "dependabot", true)
	bot.AuthorBot = true
	approved := pr(21, "Amy", true)
	approved.Mine = &github.Review{State: "APPROVED", At: now}
	onTwenty := pr(30, "amy", true)
	onTwenty.BaseRef = "b20"
	onGone := pr(31, "amy", true)
	onGone.BaseRef = "gone"
	return &github.Queue{
		Trunk: "main",
		PRs: []*github.ReviewPR{
			pr(40, "zoe", true), draft, bot, pr(20, "Amy", true), approved, onTwenty, onGone,
			pr(19, "Amy", false), pr(50, "bob", false),
		},
		Stacks: []github.Stack{
			// 18 is listed by the stack, but GitHub did not return it.
			{Number: 22, Base: "main", PRs: []int{18, 19, 20, 21}},
		},
	}
}

func TestBuildGroupsByStackAndAuthor(t *testing.T) {
	got := summary(Build(queue(), Options{}))
	// Authors sort without regard to case, a stack lists its top first, and
	// a PR based on another's branch names it.
	want := "Amy:s22[21(approved),20!,19,18?] amy:30!^20 amy:31! " +
		"dependabot:13(bot) zoe:12(draft) zoe:40!"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestBuildKeepsDraftsAndBotsWhenAsked(t *testing.T) {
	got := summary(Build(queue(), Options{Drafts: true, Bots: true}))
	want := "Amy:s22[21(approved),20!,19,18?] amy:30!^20 amy:31! dependabot:13! zoe:12! zoe:40!"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestGroupCountsAndStart(t *testing.T) {
	groups := Build(queue(), Options{})
	stack := groups[0]
	if stack.ForYou() != 1 || stack.Start() != 20 || !stack.Requested() {
		t.Fatalf("for you %d start %d", stack.ForYou(), stack.Start())
	}
	hidden := groups[3]
	if hidden.ForYou() != 0 || hidden.Start() != 0 || !hidden.Requested() {
		t.Fatalf("a hidden PR waits on nobody: %+v", hidden)
	}
	if Build(nil, Options{}) != nil {
		t.Fatal("no queue has no groups")
	}
}

func TestShortTeams(t *testing.T) {
	tests := []struct {
		teams []string
		want  string
	}{
		{
			[]string{"go-readability", "api-readability", "sql-readability", "engineering"},
			"map[api-readability:api engineering:engineering go-readability:go sql-readability:sql]",
		},
		{[]string{"team-go", "team-api", "ops"}, "map[ops:ops team-api:api team-go:go]"},
		// Nothing shared, an affix too short to matter, and a team that is
		// nothing but the affix.
		{[]string{"platform", "security"}, "map[platform:platform security:security]"},
		{[]string{"a-go", "b-go"}, "map[a-go:a-go b-go:b-go]"},
		{
			[]string{"x-readability", "-readability"},
			"map[-readability:-readability x-readability:x]",
		},
		// One team named twice shares nothing with itself.
		{[]string{"go-readability", "go-readability"}, "map[go-readability:go-readability]"},
		{nil, "map[]"},
	}
	for _, tt := range tests {
		if got := fmt.Sprint(ShortTeams(tt.teams)); got != tt.want {
			t.Errorf("ShortTeams(%v) = %s, want %s", tt.teams, got, tt.want)
		}
	}
}
