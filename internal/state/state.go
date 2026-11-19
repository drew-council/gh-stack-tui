// Package state keeps what the user decides in the TUIs, such as snoozes, in
// a SQLite database that every TUI on the machine shares.
//
// Unlike the cache, whose file every build replaces, the state outlives
// upgrades. Its schema grows by migrations, in migrations/, which run in
// order and once each. Queries live in queries.sql; statedb is generated
// from both with sqlc generate.
package state

//go:generate sqlc generate -f ../../sqlc.yaml

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/drew-council/gh-stack-tui/internal/github"
	"github.com/drew-council/gh-stack-tui/internal/review"
	"github.com/drew-council/gh-stack-tui/internal/state/statedb"
)

//go:embed migrations/*.sql
var migrations embed.FS

// keepWoken is how long a woken snooze stays in the database before it is
// deleted.
const keepWoken = 7 * 24 * time.Hour

// Store is the shared database.
type Store struct {
	db *sql.DB
	q  *statedb.Queries
}

// Dir is where the state lives: $XDG_STATE_HOME/gh-stack-tui, else
// $HOME/.local/state/gh-stack-tui, on every OS.
func Dir() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	// The XDG spec says to ignore a relative path.
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "gh-stack-tui"), nil
}

// Open opens the state in dir, creating it if needed, and brings its schema
// up to date.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "state.db")
	// The state names PRs of private repositories. SQLite gives its -wal and
	// -shm files the database file's mode.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	db, err := sql.Open(
		"sqlite3",
		path+"?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_txlock=immediate",
	)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, q: statedb.New(db)}
	if err := s.setUp(time.Now()); err != nil {
		db.Close()
		return nil, fmt.Errorf("state %s: %w", path, err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) setUp(now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := migrate(tx); err != nil {
		return err
	}
	cutoff := now.Add(-keepWoken).UnixMilli()
	if err := s.q.WithTx(tx).DeleteSnoozesBefore(context.Background(), cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

// migrate runs the migrations that the database's user_version says have
// not run yet.
func migrate(tx *sql.Tx) error {
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	var version int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(names) {
		return fmt.Errorf("schema version %d is newer than this build's %d", version, len(names))
	}
	for i, name := range names[version:] {
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sql)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version+i+1)); err != nil {
			return err
		}
	}
	return nil
}

// repoKey identifies repo. GitHub names are case-insensitive, and git
// remotes may spell them differently.
func repoKey(repo github.Repo) string {
	return strings.ToLower(repo.Host + "/" + repo.Owner + "/" + repo.Name)
}

func kind(t review.Target) string {
	if t.Stack {
		return "stack"
	}
	return "pr"
}

// Snoozes returns the snoozes of repo that have not woken by now.
func (s *Store) Snoozes(repo github.Repo, now time.Time) (review.Snoozes, error) {
	rows, err := s.q.Snoozes(context.Background(), statedb.SnoozesParams{
		Repo: repoKey(repo), Now: now.UnixMilli(),
	})
	if err != nil {
		return nil, err
	}
	out := review.Snoozes{}
	for _, r := range rows {
		t := review.Target{Stack: r.Kind == "stack", Number: int(r.Number)}
		out[t] = time.UnixMilli(r.WakesAt)
	}
	return out, nil
}

// Snooze snoozes targets of repo until wakes. A target that is snoozed
// already wakes at the new time instead.
func (s *Store) Snooze(repo github.Repo, targets []review.Target, wakes, now time.Time) error {
	return s.inTx(func(q *statedb.Queries) error {
		var errs []error
		for _, t := range targets {
			errs = append(errs, q.UpsertSnooze(context.Background(), statedb.UpsertSnoozeParams{
				Repo:      repoKey(repo),
				Kind:      kind(t),
				Number:    int64(t.Number),
				WakesAt:   wakes.UnixMilli(),
				CreatedAt: now.UnixMilli(),
			}))
		}
		return errors.Join(errs...)
	})
}

// Wake ends the snoozes of targets of repo.
func (s *Store) Wake(repo github.Repo, targets []review.Target) error {
	return s.inTx(func(q *statedb.Queries) error {
		var errs []error
		for _, t := range targets {
			errs = append(errs, q.DeleteSnooze(context.Background(), statedb.DeleteSnoozeParams{
				Repo: repoKey(repo), Kind: kind(t), Number: int64(t.Number),
			}))
		}
		return errors.Join(errs...)
	})
}

func (s *Store) inTx(f func(q *statedb.Queries) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}
