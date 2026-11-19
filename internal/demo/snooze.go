package demo

import (
	"maps"
	"sync"
	"time"

	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
)

// Snoozes keeps snoozes in memory, so the demo leaves nothing on disk.
type Snoozes struct {
	mu sync.Mutex
	m  review.Snoozes
}

func (s *Snoozes) Snoozes(_ github.Repo, now time.Time) (review.Snoozes, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := review.Snoozes{}
	for t, w := range s.m {
		if w.After(now) {
			out[t] = w
		}
	}
	return out, nil
}

func (s *Snoozes) Snooze(_ github.Repo, targets []review.Target, wakes, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = review.Snoozes{}
	}
	for _, t := range targets {
		s.m[t] = wakes
	}
	return nil
}

func (s *Snoozes) Wake(_ github.Repo, targets []review.Target) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range targets {
		delete(s.m, t)
	}
	return nil
}

// All returns every snooze, woken ones included.
func (s *Snoozes) All() review.Snoozes {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.m)
}
