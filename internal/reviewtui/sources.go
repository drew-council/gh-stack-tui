package reviewtui

import (
	"context"

	"github.com/drew-council/gh-stack-tui/internal/github"
)

// Local is the checkout reviews run in. [git.Repo] is a real one.
type Local interface {
	// Generated returns the paths that the checkout's .gitattributes marks
	// as generated code.
	Generated(paths []string) map[string]struct{}
}

// Remote is where the review queue comes from. [github.Client] talks to
// GitHub.
type Remote interface {
	Repository() github.Repo
	FetchQueue(ctx context.Context, q github.QueueQuery) (*github.Queue, error)
	FetchReviewDetails(ctx context.Context, numbers []int) (map[int]*github.ReviewDetail, error)
}
