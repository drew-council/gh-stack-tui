// Package github fetches pull request and CI state through `gh api graphql`,
// reusing the gh CLI's authentication.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Repo identifies a GitHub repository.
type Repo struct {
	Host  string
	Owner string
	Name  string
}

// Lookup is a branch whose pull request should be fetched. When Number is zero
// the newest open PR with the branch as its head is used instead.
type Lookup struct {
	Branch string
	Number int
}

// PR is the remote state of one pull request.
type PR struct {
	Number         int
	URL            string
	Title          string
	State          string // OPEN, CLOSED, MERGED
	IsDraft        bool
	Merged         bool
	Queued         bool
	ReviewDecision string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
	MergeState     string // mergeStateStatus: CLEAN, BLOCKED, DIRTY, BEHIND, ...
	HeadSHA        string
	Checks         Rollup
	// Approvals and ChangesRequested count reviewers whose latest review
	// says so. They fill in for ReviewDecision, which is empty when the
	// repository does not require reviews.
	Approvals        int
	ChangesRequested int
	// Unresolved is the number of review threads still open.
	Unresolved int
}

// ReviewStatus summarizes where an open PR stands with its reviewers.
type ReviewStatus int

const (
	ReviewNone  ReviewStatus = iota // draft, merged, or closed: nothing to say
	ReviewReady                     // ready for review, nobody has weighed in
	ReviewApproved
	ReviewChangesRequested
)

// Review derives the review status from the decision GitHub computes when
// reviews are required, falling back to the latest review of each reviewer.
func (pr *PR) Review() ReviewStatus {
	if pr.State != "OPEN" || pr.IsDraft || pr.Merged {
		return ReviewNone
	}
	switch {
	case pr.ReviewDecision == "CHANGES_REQUESTED" || pr.ChangesRequested > 0:
		return ReviewChangesRequested
	case pr.ReviewDecision == "APPROVED" || (pr.ReviewDecision == "" && pr.Approvals > 0):
		return ReviewApproved
	}
	return ReviewReady
}

const prFields = `number url title state isDraft merged reviewDecision mergeStateStatus
  mergeQueueEntry { state }
  latestOpinionatedReviews(first: 100) { nodes { state } }
  reviewThreads(first: 100) { nodes { isResolved } }
  commits(last: 1) { nodes { commit { oid statusCheckRollup { state contexts(first: 100) { nodes {
    __typename
    ... on CheckRun { name status conclusion detailsUrl startedAt
      checkSuite { workflowRun { url workflow { name } } app { name } } }
    ... on StatusContext { context state targetUrl }
  } } } } } }`

// FetchPRs returns the PR for each lookup, keyed by branch. Branches with no PR
// are absent from the result.
func FetchPRs(ctx context.Context, repo Repo, lookups []Lookup) (map[string]*PR, error) {
	if len(lookups) == 0 {
		return map[string]*PR{}, nil
	}

	var q strings.Builder
	args := []string{"api", "graphql", "-f", "owner=" + repo.Owner, "-f", "name=" + repo.Name}
	if repo.Host != "" && repo.Host != "github.com" {
		args = append(args, "--hostname", repo.Host)
	}
	var params []string
	for i, l := range lookups {
		if l.Number == 0 {
			v := fmt.Sprintf("h%d", i)
			params = append(params, fmt.Sprintf("$%s: String!", v))
			args = append(args, "-f", v+"="+l.Branch)
		}
	}
	fmt.Fprintf(
		&q,
		"query($owner: String!, $name: String!%s) { repository(owner: $owner, name: $name) {\n",
		prefixed(", ", params),
	)
	for i, l := range lookups {
		if l.Number != 0 {
			fmt.Fprintf(&q, "b%d: pullRequest(number: %d) { %s }\n", i, l.Number, prFields)
		} else {
			fmt.Fprintf(
				&q,
				"b%d: pullRequests(headRefName: $h%d, states: [OPEN], first: 1, orderBy: {field: CREATED_AT, direction: DESC}) { nodes { %s } }\n",
				i,
				i,
				prFields,
			)
		}
	}
	q.WriteString("} }")
	args = append(args, "-f", "query="+q.String())

	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// gh exits non-zero on partial GraphQL errors (for example one PR number
	// that no longer resolves) but still prints the data, so parse first.
	var resp struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil || resp.Data.Repository == nil {
		if runErr != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = runErr.Error()
			}
			return nil, fmt.Errorf("gh api graphql: %s", msg)
		}
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("gh api graphql: %s", resp.Errors[0].Message)
		}
		return nil, fmt.Errorf("gh api graphql: unexpected response")
	}

	out := map[string]*PR{}
	for i, l := range lookups {
		raw := resp.Data.Repository[fmt.Sprintf("b%d", i)]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var node *prNode
		if l.Number != 0 {
			if err := json.Unmarshal(raw, &node); err != nil {
				return nil, err
			}
		} else {
			var conn struct {
				Nodes []*prNode `json:"nodes"`
			}
			if err := json.Unmarshal(raw, &conn); err != nil {
				return nil, err
			}
			if len(conn.Nodes) > 0 {
				node = conn.Nodes[0]
			}
		}
		if node != nil {
			out[l.Branch] = node.toPR()
		}
	}
	return out, nil
}

func prefixed(sep string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	return sep + strings.Join(items, sep)
}

type prNode struct {
	Number           int    `json:"number"`
	URL              string `json:"url"`
	Title            string `json:"title"`
	State            string `json:"state"`
	IsDraft          bool   `json:"isDraft"`
	Merged           bool   `json:"merged"`
	ReviewDecision   string `json:"reviewDecision"`
	MergeStateStatus string `json:"mergeStateStatus"`
	MergeQueueEntry  *struct {
		State string `json:"state"`
	} `json:"mergeQueueEntry"`
	LatestOpinionatedReviews struct {
		Nodes []struct {
			State string `json:"state"`
		} `json:"nodes"`
	} `json:"latestOpinionatedReviews"`
	ReviewThreads struct {
		Nodes []struct {
			IsResolved bool `json:"isResolved"`
		} `json:"nodes"`
	} `json:"reviewThreads"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				OID               string `json:"oid"`
				StatusCheckRollup *struct {
					State    string `json:"state"`
					Contexts struct {
						Nodes []contextNode `json:"nodes"`
					} `json:"contexts"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

type contextNode struct {
	Typename string `json:"__typename"`
	// CheckRun
	Name       string      `json:"name"`
	Status     string      `json:"status"`
	Conclusion string      `json:"conclusion"`
	DetailsURL string      `json:"detailsUrl"`
	StartedAt  time.Time   `json:"startedAt"`
	CheckSuite *checkSuite `json:"checkSuite"`
	// StatusContext
	Context   string `json:"context"`
	State     string `json:"state"`
	TargetURL string `json:"targetUrl"`
}

type checkSuite struct {
	WorkflowRun *workflowRun `json:"workflowRun"`
	App         *struct {
		Name string `json:"name"`
	} `json:"app"`
}

type workflowRun struct {
	URL      string `json:"url"`
	Workflow struct {
		Name string `json:"name"`
	} `json:"workflow"`
}

func (n *prNode) toPR() *PR {
	pr := &PR{
		Number:         n.Number,
		URL:            n.URL,
		Title:          n.Title,
		State:          n.State,
		IsDraft:        n.IsDraft,
		Merged:         n.Merged,
		Queued:         n.MergeQueueEntry != nil,
		ReviewDecision: n.ReviewDecision,
		MergeState:     n.MergeStateStatus,
	}
	for _, r := range n.LatestOpinionatedReviews.Nodes {
		switch r.State {
		case "APPROVED":
			pr.Approvals++
		case "CHANGES_REQUESTED":
			pr.ChangesRequested++
		}
	}
	for _, t := range n.ReviewThreads.Nodes {
		if !t.IsResolved {
			pr.Unresolved++
		}
	}
	if len(n.Commits.Nodes) > 0 {
		c := n.Commits.Nodes[0].Commit
		pr.HeadSHA = c.OID
		if c.StatusCheckRollup != nil {
			pr.Checks = buildRollup(c.StatusCheckRollup.Contexts.Nodes)
		}
	}
	return pr
}
