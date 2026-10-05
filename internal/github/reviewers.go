package github

import (
	"context"
	"fmt"
	"time"
)

// Reviewer is a person who can be asked to review pull requests.
type Reviewer struct {
	Login string
	Name  string
	// RecentPRs counts the viewer's recent PRs in the repository that asked
	// them for review, and Recent weighs those PRs by how recent they are.
	RecentPRs int
	Recent    float64
}

// recentPRs is how many of the viewer's latest PRs inform Reviewer.Recent.
const recentPRs = 50

// recentDecay is the weight lost per older PR, so that among people asked
// equally often, the one asked most lately ranks first.
const recentDecay = 0.95

// assignableUsers is the people who can be requested for review, a page at
// a time.
const assignableUsers = `repository(owner: $owner, name: $name) {
    assignableUsers(first: 100, after: $after) {
      pageInfo { hasNextPage endCursor }
      nodes { login name }
    }
  }`

// firstReviewersQuery fetches the first page of users along with the
// viewer's latest PRs in the repository and who they asked for review.
var firstReviewersQuery = `query($owner: String!, $name: String!, $after: String, $search: String!) {
  ` + assignableUsers + `
  viewer { login }
  search(query: $search, type: ISSUE, first: ` + fmt.Sprint(
	recentPRs,
) + `) { nodes { ... on PullRequest {
    reviewRequests(first: 50) { nodes { requestedReviewer { __typename ... on User { login } } } }
    latestReviews(first: 50) { nodes { author { __typename login } } }
  } } }
}`

// nextReviewersQuery fetches the following pages of users.
const nextReviewersQuery = `query($owner: String!, $name: String!, $after: String) {
  ` + assignableUsers + `
}`

// FetchReviewers lists the people who can review pull requests in the
// repository, other than the viewer, along with how often the viewer's recent
// PRs went to them.
func (c *Client) FetchReviewers(ctx context.Context) ([]Reviewer, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	var viewer string
	var out []Reviewer
	index := map[string]int{}
	add := func(login, name string) int {
		if i, ok := index[login]; ok {
			return i
		}
		index[login] = len(out)
		out = append(out, Reviewer{Login: login, Name: name})
		return len(out) - 1
	}

	vars := map[string]any{
		"owner": c.Repo.Owner,
		"name":  c.Repo.Name,
		"after": nil,
		"search": fmt.Sprintf(
			"repo:%s/%s is:pr author:@me sort:created-desc",
			c.Repo.Owner,
			c.Repo.Name,
		),
	}
	// Repositories rarely have more than a few hundred assignable users;
	// stop paging well before that becomes slow.
	for page := 0; page < 10; page++ {
		var data struct {
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
			Repository struct {
				AssignableUsers struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						Login string `json:"login"`
						Name  string `json:"name"`
					} `json:"nodes"`
				} `json:"assignableUsers"`
			} `json:"repository"`
			Search struct {
				Nodes []prNode `json:"nodes"`
			} `json:"search"`
		}
		query := nextReviewersQuery
		if page == 0 {
			query = firstReviewersQuery
		}
		if err := c.graphQL(ctx, query, vars, &data); err != nil {
			return nil, err
		}
		for _, u := range data.Repository.AssignableUsers.Nodes {
			add(u.Login, u.Name)
		}
		if page == 0 {
			viewer = data.Viewer.Login
			delete(vars, "search")
			weight := 1.0
			for _, n := range data.Search.Nodes {
				pr := n.toPR()
				for _, login := range pr.Reviewers {
					if login == viewer {
						continue
					}
					r := &out[add(login, "")]
					r.RecentPRs++
					r.Recent += weight
				}
				weight *= recentDecay
			}
		}
		info := data.Repository.AssignableUsers.PageInfo
		if !info.HasNextPage {
			break
		}
		vars["after"] = info.EndCursor
	}

	reviewers := out[:0]
	for _, r := range out {
		if r.Login != viewer {
			reviewers = append(reviewers, r)
		}
	}
	return reviewers, nil
}
