package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

// QueueQuery selects the pull requests that wait on the viewer's review.
type QueueQuery struct {
	// Teams limits team requests to these team slugs. Empty takes requests
	// to every team of the viewer. Requests to the viewer by name always
	// count.
	Teams []string
}

// Key identifies the query, to keep the results of different queries apart.
func (q QueueQuery) Key() string {
	teams := strings.Fields(strings.ToLower(strings.Join(q.Teams, " ")))
	slices.Sort(teams)
	return strings.Join(teams, ",")
}

// Queue is the viewer's review queue in one repository.
type Queue struct {
	// Viewer is the login of the gh CLI's user.
	Viewer string
	// Trunk is the repository's default branch.
	Trunk string
	// Teams are the viewer's teams in the repository's organization, or nil
	// when they could not be read.
	Teams []string
	// PRs are the requested pull requests, followed by the other pull
	// requests of their stacks.
	PRs []*ReviewPR
	// Stacks are the open stacks that hold a requested pull request.
	Stacks []Stack
	// Total is how many pull requests matched the query. It is more than the
	// requested PRs when GitHub was not asked for all of them.
	Total int
}

// Stack is a stack of pull requests as GitHub records it.
type Stack struct {
	Number int
	// Base is the branch the stack is based on.
	Base string
	// PRs are the pull request numbers, bottom first.
	PRs []int
}

// ReviewPR is a pull request as its reviewer sees it.
type ReviewPR struct {
	Number  int
	URL     string
	Title   string
	State   string // OPEN, CLOSED, MERGED
	IsDraft bool
	Merged  bool
	Queued  bool

	Author    string
	AuthorBot bool
	BaseRef   string
	HeadRef   string
	HeadSHA   string

	CreatedAt time.Time
	UpdatedAt time.Time
	// PushedAt is when the head commit was committed.
	PushedAt time.Time

	Additions    int
	Deletions    int
	ChangedFiles int
	Commits      int

	ReviewDecision string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED
	Conflicts      bool
	// Checks is the state of CI on the head commit.
	Checks CheckState
	Labels []string

	// Requested reports whether the PR matched the queue's query: review is
	// requested from the viewer, or from one of the viewer's teams.
	Requested bool
	// Direct reports whether the viewer is requested by name.
	Direct bool
	// Via are the requested teams the viewer is in.
	Via []string
	// AskedAt is when review was last requested from the viewer or one of
	// their teams, and AskedBy who asked. Zero when GitHub's last events on
	// the PR do not say.
	AskedAt time.Time
	AskedBy string

	// RequestedUsers and RequestedTeams still owe a review.
	RequestedUsers []string
	RequestedTeams []string
	// Reviews are the latest approval or change request of each reviewer
	// other than the viewer.
	Reviews []Review
	// Mine is the viewer's latest review, or nil.
	Mine *Review
}

// Review is one person's latest review of a pull request.
type Review struct {
	Login string
	State string // APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED, PENDING
	At    time.Time
	// Commit is the head commit the review was made on.
	Commit string
}

// Version changes whenever the PR's details may have: its description, files,
// commits, checks, or review threads.
func (pr *ReviewPR) Version() string {
	return fmt.Sprintf("%d|%s|%d", pr.UpdatedAt.Unix(), pr.HeadSHA, pr.Checks)
}

// ReviewDetail is the part of a pull request that is fetched one PR at a
// time, when it is looked at.
type ReviewDetail struct {
	Number int
	// Body is the description, in Markdown, cut to maxBody.
	Body string
	// Files are the files the PR changes, up to maxFiles of them.
	Files []ChangedFile
	// Commits are the latest commits, newest first.
	Commits        []PRCommit
	Checks         Rollup
	Unresolved     int
	UnresolvedBots int
}

// ChangedFile is one file a pull request changes.
type ChangedFile struct {
	Path      string
	Status    string // A, M, D, R, C
	Additions int
	Deletions int
	// Generated is not GitHub's to say: whoever has a checkout sets it, from
	// the checkout's .gitattributes.
	Generated bool
}

// PRCommit is one commit of a pull request.
type PRCommit struct {
	SHA     string
	Subject string
	Time    time.Time
}

// maxBody is the most of a PR description that is kept, in runes.
const maxBody = 6000

// queueFields requests one PR of the review queue. It is cheap enough to ask
// for fifty PRs at once: no check runs, files, or review threads, which
// [Client.FetchReviewDetails] asks for.
const queueFields = `number url title state isDraft merged reviewDecision mergeable
  author { __typename login }
  baseRefName headRefName headRefOid
  createdAt updatedAt additions deletions changedFiles
  mergeQueueEntry { state }
  labels(first: 8) { nodes { name } }
  commits(last: 1) { totalCount nodes { commit { committedDate statusCheckRollup { state } } } }
  reviewRequests(first: 25) { nodes { requestedReviewer {
    __typename ... on User { login } ... on Team { slug } } } }
  latestOpinionatedReviews(first: 25) { nodes { state submittedAt author { __typename login } } }
  viewerLatestReview { state submittedAt commit { oid } }
  timelineItems(last: 20, itemTypes: [REVIEW_REQUESTED_EVENT]) { nodes {
    ... on ReviewRequestedEvent { createdAt actor { login } requestedReviewer {
      __typename ... on User { login } ... on Team { slug } } } } }`

// queuePageSize is how many PRs one search request returns, and
// queueMaxPages how many requests a search makes.
const (
	queuePageSize = 50
	queueMaxPages = 4
)

// stackMateBatch is how many PRs outside the search are fetched per request.
const stackMateBatch = 10

type queueNode struct {
	Number         int       `json:"number"`
	URL            string    `json:"url"`
	Title          string    `json:"title"`
	State          string    `json:"state"`
	IsDraft        bool      `json:"isDraft"`
	Merged         bool      `json:"merged"`
	ReviewDecision string    `json:"reviewDecision"`
	Mergeable      string    `json:"mergeable"`
	Author         *actor    `json:"author"`
	BaseRefName    string    `json:"baseRefName"`
	HeadRefName    string    `json:"headRefName"`
	HeadRefOID     string    `json:"headRefOid"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Additions      int       `json:"additions"`
	Deletions      int       `json:"deletions"`
	ChangedFiles   int       `json:"changedFiles"`
	MergeQueue     *struct {
		State string `json:"state"`
	} `json:"mergeQueueEntry"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Commits struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			Commit struct {
				CommittedDate     time.Time `json:"committedDate"`
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *actor `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	LatestOpinionatedReviews struct {
		Nodes []struct {
			State       string    `json:"state"`
			SubmittedAt time.Time `json:"submittedAt"`
			Author      *actor    `json:"author"`
		} `json:"nodes"`
	} `json:"latestOpinionatedReviews"`
	ViewerLatestReview *struct {
		State string `json:"state"`
		// A pending review has not been submitted.
		SubmittedAt *time.Time `json:"submittedAt"`
		Commit      *struct {
			OID string `json:"oid"`
		} `json:"commit"`
	} `json:"viewerLatestReview"`
	TimelineItems struct {
		Nodes []struct {
			CreatedAt         time.Time `json:"createdAt"`
			Actor             *actor    `json:"actor"`
			RequestedReviewer *actor    `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"timelineItems"`
}

// toReviewPR reads the node as viewer sees it. teams are the viewer's teams,
// or nil when unknown: then every requested team counts as the viewer's.
func (n *queueNode) toReviewPR(viewer string, teams []string) *ReviewPR {
	pr := &ReviewPR{
		Number:         n.Number,
		URL:            n.URL,
		Title:          n.Title,
		State:          n.State,
		IsDraft:        n.IsDraft,
		Merged:         n.Merged,
		Queued:         n.MergeQueue != nil,
		BaseRef:        n.BaseRefName,
		HeadRef:        n.HeadRefName,
		HeadSHA:        n.HeadRefOID,
		CreatedAt:      n.CreatedAt,
		UpdatedAt:      n.UpdatedAt,
		Additions:      n.Additions,
		Deletions:      n.Deletions,
		ChangedFiles:   n.ChangedFiles,
		Commits:        n.Commits.TotalCount,
		ReviewDecision: n.ReviewDecision,
		Conflicts:      n.Mergeable == "CONFLICTING",
	}
	if n.Author != nil {
		pr.Author = n.Author.Login
		pr.AuthorBot = n.Author.bot()
	}
	for _, l := range n.Labels.Nodes {
		pr.Labels = append(pr.Labels, l.Name)
	}
	if len(n.Commits.Nodes) > 0 {
		c := n.Commits.Nodes[0].Commit
		pr.PushedAt = c.CommittedDate
		if c.StatusCheckRollup != nil {
			pr.Checks = statusContextState(c.StatusCheckRollup.State)
		}
	}
	mine := func(team string) bool { return teams == nil || slices.Contains(teams, team) }
	for _, r := range n.ReviewRequests.Nodes {
		a := r.RequestedReviewer
		switch {
		case a == nil:
		case a.Typename == "Team":
			pr.RequestedTeams = append(pr.RequestedTeams, a.Slug)
			if mine(a.Slug) {
				pr.Via = append(pr.Via, a.Slug)
			}
		case a.Login == viewer:
			pr.Direct = true
		case a.Login != "":
			pr.RequestedUsers = append(pr.RequestedUsers, a.Login)
		}
	}
	slices.Sort(pr.Via)
	for _, r := range n.LatestOpinionatedReviews.Nodes {
		if r.Author == nil || r.Author.Login == viewer {
			continue
		}
		pr.Reviews = append(
			pr.Reviews,
			Review{Login: r.Author.Login, State: r.State, At: r.SubmittedAt},
		)
	}
	if r := n.ViewerLatestReview; r != nil {
		pr.Mine = &Review{Login: viewer, State: r.State}
		if r.SubmittedAt != nil {
			pr.Mine.At = *r.SubmittedAt
		}
		if r.Commit != nil {
			pr.Mine.Commit = r.Commit.OID
		}
	}
	// The events are oldest first.
	for _, e := range slices.Backward(n.TimelineItems.Nodes) {
		a := e.RequestedReviewer
		if a == nil ||
			!(a.Typename == "Team" && mine(a.Slug) || a.Login == viewer && viewer != "") {
			continue
		}
		pr.AskedAt = e.CreatedAt
		if e.Actor != nil {
			pr.AskedBy = e.Actor.Login
		}
		break
	}
	return pr
}

// identity is who the client acts as, learned once.
type identity struct {
	mu sync.Mutex
	// teams are the viewer's teams in the repository's organization. tried
	// is set once they were asked for, whether or not GitHub answered.
	teams []string
	tried bool
}

// viewerTeams returns the viewer's teams in the repository's organization, or
// nil when the owner is not an organization or the token may not read teams.
func (c *Client) viewerTeams(ctx context.Context, viewer string) []string {
	c.id.mu.Lock()
	defer c.id.mu.Unlock()
	if c.id.tried || viewer == "" {
		return c.id.teams
	}
	var data struct {
		Organization *struct {
			Teams struct {
				Nodes []struct {
					Slug string `json:"slug"`
				} `json:"nodes"`
			} `json:"teams"`
		} `json:"organization"`
	}
	const query = `query($org: String!, $login: String!) { organization(login: $org) {
  teams(first: 100, userLogins: [$login]) { nodes { slug } } } }`
	err := c.graphQLRetry(ctx, query, map[string]any{"org": c.Repo.Owner, "login": viewer}, &data)
	if err != nil && ctx.Err() != nil {
		// Cancelled, not refused: ask again next time.
		return nil
	}
	c.id.tried = true
	if err == nil && data.Organization != nil {
		c.id.teams = []string{}
		for _, t := range data.Organization.Teams.Nodes {
			c.id.teams = append(c.id.teams, t.Slug)
		}
	}
	return c.id.teams
}

// searches are the search qualifiers that together find the queue of q.
func (c *Client) searches(q QueueQuery) []string {
	base := fmt.Sprintf("repo:%s/%s is:pr is:open -author:@me ", c.Repo.Owner, c.Repo.Name)
	if len(q.Teams) == 0 {
		return []string{base + "review-requested:@me"}
	}
	out := []string{base + "user-review-requested:@me"}
	for _, t := range q.Teams {
		out = append(out, fmt.Sprintf("%steam-review-requested:%s/%s", base, c.Repo.Owner, t))
	}
	return out
}

const queueSearchQuery = `query($q: String!, $n: Int!, $after: String, $owner: String!, $name: String!) {
  viewer { login }
  repository(owner: $owner, name: $name) { defaultBranchRef { name } }
  search(query: $q, type: ISSUE, first: $n, after: $after) {
    issueCount
    pageInfo { hasNextPage endCursor }
    nodes { ... on PullRequest { ` + queueFields + ` } }
  }
}`

// searchResult is what one search found.
type searchResult struct {
	viewer string
	trunk  string
	total  int
	nodes  []*queueNode
}

// search runs one search, a page at a time.
func (c *Client) search(ctx context.Context, query string) (*searchResult, error) {
	res := &searchResult{}
	vars := map[string]any{
		"q": query, "n": queuePageSize, "after": nil,
		"owner": c.Repo.Owner, "name": c.Repo.Name,
	}
	for range queueMaxPages {
		var data struct {
			Viewer struct {
				Login string `json:"login"`
			} `json:"viewer"`
			Repository *struct {
				DefaultBranchRef *struct {
					Name string `json:"name"`
				} `json:"defaultBranchRef"`
			} `json:"repository"`
			Search struct {
				IssueCount int `json:"issueCount"`
				PageInfo   struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []*queueNode `json:"nodes"`
			} `json:"search"`
		}
		if err := c.graphQLRetry(ctx, queueSearchQuery, vars, &data); err != nil {
			return nil, err
		}
		res.viewer = data.Viewer.Login
		if r := data.Repository; r != nil && r.DefaultBranchRef != nil {
			res.trunk = r.DefaultBranchRef.Name
		}
		res.total = data.Search.IssueCount
		for _, n := range data.Search.Nodes {
			// Nodes that are not pull requests decode to zero values.
			if n != nil && n.Number != 0 {
				res.nodes = append(res.nodes, n)
			}
		}
		if !data.Search.PageInfo.HasNextPage {
			break
		}
		vars["after"] = data.Search.PageInfo.EndCursor
	}
	return res, nil
}

// FetchQueue returns the pull requests that wait on the viewer's review, and
// the stacks they are part of with every pull request of those stacks.
func (c *Client) FetchQueue(ctx context.Context, q QueueQuery) (*Queue, error) {
	searches := c.searches(q)
	results := make([]*searchResult, len(searches))
	var stacks []Stack
	errs := make([]error, len(searches)+1)
	var wg sync.WaitGroup
	for i, s := range searches {
		wg.Go(func() { results[i], errs[i] = c.search(ctx, s) })
	}
	wg.Go(func() { stacks, errs[len(searches)] = c.listStacks(ctx) })
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}

	queue := &Queue{Viewer: results[0].viewer, Trunk: results[0].trunk}
	queue.Teams = c.viewerTeams(ctx, queue.Viewer)
	known := map[int]struct{}{}
	for _, res := range results {
		for _, n := range res.nodes {
			if _, ok := known[n.Number]; ok {
				continue
			}
			known[n.Number] = struct{}{}
			pr := n.toReviewPR(queue.Viewer, queue.Teams)
			pr.Requested = true
			queue.PRs = append(queue.PRs, pr)
		}
		// Searches overlap, so the largest is the best lower bound.
		queue.Total = max(queue.Total, res.total)
	}
	queue.Total = max(queue.Total, len(queue.PRs))

	var mates []int
	for _, s := range stacks {
		if !slices.ContainsFunc(s.PRs, func(n int) bool {
			_, ok := known[n]
			return ok
		}) {
			continue
		}
		queue.Stacks = append(queue.Stacks, s)
		for _, n := range s.PRs {
			if _, ok := known[n]; !ok {
				known[n] = struct{}{}
				mates = append(mates, n)
			}
		}
	}
	found, err := c.fetchQueuePRs(ctx, mates, queue.Viewer, queue.Teams)
	if err != nil {
		return nil, err
	}
	queue.PRs = append(queue.PRs, found...)
	return queue, nil
}

// fetchQueuePRs fetches PRs by number, in the order of numbers. PRs that no
// longer resolve are left out.
func (c *Client) fetchQueuePRs(
	ctx context.Context,
	numbers []int,
	viewer string,
	teams []string,
) ([]*ReviewPR, error) {
	byNumber := map[int]*ReviewPR{}
	var mu sync.Mutex
	err := inBatchesOf(
		ctx,
		len(numbers),
		stackMateBatch,
		func(ctx context.Context, lo, hi int) error {
			var q strings.Builder
			q.WriteString(
				"query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {\n",
			)
			for i, n := range numbers[lo:hi] {
				fmt.Fprintf(&q, "p%d: pullRequest(number: %d) { %s }\n", i, n, queueFields)
			}
			q.WriteString("} }")
			var data struct {
				Repository map[string]*queueNode `json:"repository"`
			}
			vars := map[string]any{"owner": c.Repo.Owner, "name": c.Repo.Name}
			err := c.graphQLRetry(ctx, q.String(), vars, &data)
			if err != nil && (!partial(err) || data.Repository == nil) {
				return err
			}
			mu.Lock()
			defer mu.Unlock()
			for _, node := range data.Repository {
				if node != nil {
					byNumber[node.Number] = node.toReviewPR(viewer, teams)
				}
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	var out []*ReviewPR
	for _, n := range numbers {
		if pr := byNumber[n]; pr != nil {
			out = append(out, pr)
		}
	}
	return out, nil
}

// stacksPageSize is how many stacks one request lists, and stacksMaxPages
// how many requests list them.
const (
	stacksPageSize = 100
	stacksMaxPages = 20
)

// listStacks returns the repository's open stacks, newest first. A
// repository without stacked pull requests has none.
func (c *Client) listStacks(ctx context.Context) ([]Stack, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var out []Stack
	for page := 1; page <= stacksMaxPages; page++ {
		path := fmt.Sprintf(
			"repos/%s/%s/stacks?per_page=%d&page=%d",
			c.Repo.Owner, c.Repo.Name, stacksPageSize, page,
		)
		var batch []struct {
			Number int  `json:"number"`
			Open   bool `json:"open"`
			Base   struct {
				Ref string `json:"ref"`
			} `json:"base"`
			PullRequests []struct {
				Number int `json:"number"`
			} `json:"pull_requests"`
		}
		if err := c.rest.DoWithContext(ctx, "GET", path, nil, &batch); err != nil {
			var httpErr *api.HTTPError
			if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
				// Stacked pull requests are not enabled here.
				return nil, nil
			}
			return nil, fmt.Errorf("listing stacks: %w", err)
		}
		for _, s := range batch {
			if !s.Open {
				continue
			}
			st := Stack{Number: s.Number, Base: s.Base.Ref}
			for _, pr := range s.PullRequests {
				st.PRs = append(st.PRs, pr.Number)
			}
			out = append(out, st)
		}
		if len(batch) < stacksPageSize {
			break
		}
	}
	return out, nil
}

// fileFields requests one page of a PR's files.
const fileFields = `pageInfo { hasNextPage endCursor }
    nodes { path additions deletions changeType }`

// maxFiles is the most files fetched for one PR. A PR with more shows
// GitHub's line counts, which cannot leave generated files out.
const maxFiles = 1000

// detailFields requests what [ReviewDetail] holds.
const detailFields = `number body
  files(first: 100) { ` + fileFields + ` }
  reviewThreads(first: 100) { nodes { isResolved
    comments(first: 1) { nodes { author { __typename login } } } } }
  recent: commits(last: 30) { nodes { commit { oid messageHeadline committedDate } } }
  commits(last: 1) { nodes { commit { oid statusCheckRollup { state contexts(first: 100) { nodes {
    __typename
    ... on CheckRun { name status conclusion detailsUrl startedAt
      checkSuite { workflowRun { url workflow { name } } app { name } } }
    ... on StatusContext { context state targetUrl }
  } } } } } }`

type detailNode struct {
	// prNode's review threads and check contexts.
	prNode
	Body   string    `json:"body"`
	Files  filesNode `json:"files"`
	Recent struct {
		Nodes []struct {
			Commit struct {
				OID             string    `json:"oid"`
				MessageHeadline string    `json:"messageHeadline"`
				CommittedDate   time.Time `json:"committedDate"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"recent"`
}

// filesNode is one page of a PR's files.
type filesNode struct {
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Nodes []struct {
		Path       string `json:"path"`
		Additions  int    `json:"additions"`
		Deletions  int    `json:"deletions"`
		ChangeType string `json:"changeType"`
	} `json:"nodes"`
}

func (n filesNode) files() []ChangedFile {
	var out []ChangedFile
	for _, f := range n.Nodes {
		out = append(out, ChangedFile{
			Path: f.Path, Status: changeStatus(f.ChangeType),
			Additions: f.Additions, Deletions: f.Deletions,
		})
	}
	return out
}

// changeStatus is the letter git shows for a GraphQL PatchStatus.
func changeStatus(changeType string) string {
	switch changeType {
	case "ADDED":
		return "A"
	case "DELETED":
		return "D"
	case "RENAMED":
		return "R"
	case "COPIED":
		return "C"
	}
	return "M"
}

func (n *detailNode) toDetail() *ReviewDetail {
	pr := n.toPR()
	d := &ReviewDetail{
		Number:         n.Number,
		Body:           strings.TrimSpace(n.Body),
		Checks:         pr.Checks,
		Unresolved:     pr.Unresolved,
		UnresolvedBots: pr.UnresolvedBots,
	}
	if r := []rune(d.Body); len(r) > maxBody {
		d.Body = string(r[:maxBody]) + "…"
	}
	d.Files = n.Files.files()
	for _, c := range slices.Backward(n.Recent.Nodes) {
		d.Commits = append(d.Commits, PRCommit{
			SHA: c.Commit.OID, Subject: c.Commit.MessageHeadline, Time: c.Commit.CommittedDate,
		})
	}
	return d
}

// FetchReviewDetails returns the details of each PR number. PRs that no
// longer resolve are absent from the result.
func (c *Client) FetchReviewDetails(
	ctx context.Context,
	numbers []int,
) (map[int]*ReviewDetail, error) {
	out := map[int]*ReviewDetail{}
	var mu sync.Mutex
	err := inBatches(ctx, len(numbers), func(ctx context.Context, lo, hi int) error {
		var q strings.Builder
		q.WriteString(
			"query($owner: String!, $name: String!) { repository(owner: $owner, name: $name) {\n",
		)
		for i, n := range numbers[lo:hi] {
			fmt.Fprintf(&q, "d%d: pullRequest(number: %d) { %s }\n", i, n, detailFields)
		}
		q.WriteString("} }")
		var data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		}
		vars := map[string]any{"owner": c.Repo.Owner, "name": c.Repo.Name}
		err := c.graphQLRetry(ctx, q.String(), vars, &data)
		if err != nil && (!partial(err) || data.Repository == nil) {
			return err
		}
		for _, raw := range data.Repository {
			var node *detailNode
			if err := json.Unmarshal(raw, &node); err != nil {
				return err
			}
			if node == nil {
				continue
			}
			d := node.toDetail()
			if info := node.Files.PageInfo; info.HasNextPage {
				more, err := c.moreFiles(ctx, node.Number, info.EndCursor, maxFiles-len(d.Files))
				if err != nil {
					return err
				}
				d.Files = append(d.Files, more...)
			}
			mu.Lock()
			out[node.Number] = d
			mu.Unlock()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

const moreFilesQuery = `query($owner: String!, $name: String!, $number: Int!, $after: String!) {
  repository(owner: $owner, name: $name) { pullRequest(number: $number) {
    files(first: 100, after: $after) { ` + fileFields + ` }
  } }
}`

// moreFiles returns the files of PR number after cursor, at most limit of
// them and a page or so more.
func (c *Client) moreFiles(
	ctx context.Context,
	number int,
	cursor string,
	limit int,
) ([]ChangedFile, error) {
	var out []ChangedFile
	for len(out) < limit {
		var data struct {
			Repository *struct {
				PullRequest *struct {
					Files filesNode `json:"files"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		vars := map[string]any{
			"owner": c.Repo.Owner, "name": c.Repo.Name, "number": number, "after": cursor,
		}
		if err := c.graphQLRetry(ctx, moreFilesQuery, vars, &data); err != nil {
			return nil, err
		}
		if data.Repository == nil || data.Repository.PullRequest == nil {
			break
		}
		page := data.Repository.PullRequest.Files
		out = append(out, page.files()...)
		if !page.PageInfo.HasNextPage {
			break
		}
		cursor = page.PageInfo.EndCursor
	}
	return out, nil
}
