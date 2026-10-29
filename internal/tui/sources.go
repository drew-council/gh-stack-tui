package tui

import (
	"context"

	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/stack"
)

// Local is where stacks come from. [stack.Source] reads them from a git
// repository.
type Local interface {
	// Dir is where commands run.
	Dir() string
	Load(pinned, previous string) (*stack.Snapshot, error)
	Stacks() ([]stack.Ref, error)
	// Fingerprint changes whenever Load would return something new.
	Fingerprint() (string, error)
	CurrentBranch() string
}

// Remote is where pull requests come from. [github.Client] talks to GitHub.
type Remote interface {
	Repository() github.Repo
	FetchPRs(ctx context.Context, lookups []github.Lookup) (map[string]*github.PR, error)
	FetchReviewers(ctx context.Context) ([]github.Reviewer, error)
	FetchSuggested(ctx context.Context, numbers []int) (map[int][]string, error)
	RequestReviewers(ctx context.Context, number int, logins []string) error
}

// connectGitHub connects to the stack's repository on GitHub as the gh CLI's
// user. repository is the stack file's "host:owner/name".
func connectGitHub(repository string) (Remote, error) {
	repo, err := resolveRepo(repository)
	if err != nil {
		return nil, err
	}
	client, err := github.NewClient(repo)
	if err != nil {
		return nil, err
	}
	return client, nil
}
