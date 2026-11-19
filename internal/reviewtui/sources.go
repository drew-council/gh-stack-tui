package reviewtui

import (
	"context"
	"time"

	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
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

// SnoozeStore keeps what the viewer snoozed, across runs and across the TUIs
// open at once. [state.Store] is a real one.
type SnoozeStore interface {
	// Snoozes returns the snoozes of repo that have not woken by now.
	Snoozes(repo github.Repo, now time.Time) (review.Snoozes, error)
	// Snooze puts targets off until wakes.
	Snooze(repo github.Repo, targets []review.Target, wakes, now time.Time) error
	// Wake ends the snoozes of targets.
	Wake(repo github.Repo, targets []review.Target) error
}
