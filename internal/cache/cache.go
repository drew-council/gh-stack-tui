// Package cache keeps GitHub results between runs in a SQLite database that
// every TUI on the machine shares, so a TUI shows the last known state at once
// while it refreshes.
//
// Queries live in queries.sql and the schema in schema.sql; cachedb is
// generated from them with sqlc generate.
package cache

//go:generate sqlc generate -f ../../sqlc.yaml

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/drew-council/gh-stack-tui/internal/cache/cachedb"
	"github.com/drew-council/gh-stack-tui/internal/github"
)

//go:embed schema.sql
var schema string

// maxAge is how long a row lives without being written again.
const maxAge = 30 * 24 * time.Hour

// Cache is the shared database. A nil *Cache is valid: it finds nothing and
// saves nothing.
type Cache struct {
	db *sql.DB
	q  *cachedb.Queries
}

// Suggestion is GitHub's reviewer suggestions for a PR at one head commit.
type Suggestion struct {
	Head   string
	Logins []string
}

// Dir is where caches live: gh-stack-tui under the user's cache directory.
func Dir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gh-stack-tui"), nil
}

// fileName names the cache after the schema and the shape of the types stored
// as JSON. A build that changes either uses a new file, so the cache never
// needs migrating, and TUIs of different builds never misread each other's
// rows.
func fileName() string {
	h := sha256.New()
	io.WriteString(h, schema)
	for _, v := range []any{github.PR{}, []github.Reviewer{}} {
		writeShape(h, reflect.TypeOf(v))
	}
	return fmt.Sprintf("cache-%x.db", h.Sum(nil)[:6])
}

// writeShape describes t's layout as JSON encodes it.
func writeShape(w io.Writer, t reflect.Type) {
	fmt.Fprint(w, t.Kind(), "(")
	switch t.Kind() {
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			fmt.Fprintf(w, "%s %q ", f.Name, f.Tag)
			writeShape(w, f.Type)
		}
	case reflect.Pointer, reflect.Slice, reflect.Array:
		writeShape(w, t.Elem())
	case reflect.Map:
		writeShape(w, t.Key())
		writeShape(w, t.Elem())
	}
	fmt.Fprint(w, ")")
}

// Open opens this build's cache in dir, creating it if needed. Caches of other
// builds are deleted, and rows older than maxAge.
func Open(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	name := fileName()
	// A TUI of another build keeps its open file until it exits.
	if old, err := filepath.Glob(filepath.Join(dir, "cache-*.db*")); err == nil {
		for _, p := range old {
			if !strings.HasPrefix(filepath.Base(p), name) {
				os.Remove(p)
			}
		}
	}
	path := filepath.Join(dir, name)
	// The cache holds PR titles from private repositories. SQLite gives its
	// -wal and -shm files the database file's mode.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	// Immediate transactions take the write lock up front, so concurrent
	// writers wait out busy_timeout instead of failing to upgrade a read lock.
	db, err := sql.Open(
		"sqlite3",
		path+"?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL&_txlock=immediate",
	)
	if err != nil {
		return nil, err
	}
	c := &Cache{db: db, q: cachedb.New(db)}
	if err := c.setUp(time.Now()); err != nil {
		db.Close()
		return nil, err
	}
	return c, nil
}

// Close closes the database.
func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	return c.db.Close()
}

func (c *Cache) setUp(now time.Time) error {
	return c.inTx(func(q *cachedb.Queries, tx *sql.Tx) error {
		if _, err := tx.Exec(schema); err != nil {
			return err
		}
		ctx, cutoff := context.Background(), now.Add(-maxAge).UnixMilli()
		return errors.Join(
			q.DeletePRsBefore(ctx, cutoff),
			q.DeleteSuggestedBefore(ctx, cutoff),
			q.DeleteReviewersBefore(ctx, cutoff),
		)
	})
}

func (c *Cache) inTx(f func(q *cachedb.Queries, tx *sql.Tx) error) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := f(c.q.WithTx(tx), tx); err != nil {
		return err
	}
	return tx.Commit()
}

// repoKey identifies repo. GitHub names are case-insensitive, and the stack
// file and git remotes may spell them differently.
func repoKey(repo github.Repo) string {
	return strings.ToLower(repo.Host + "/" + repo.Owner + "/" + repo.Name)
}

// PRs returns the cached PR for each lookup, keyed by branch, and when the
// oldest of them was fetched. A lookup without a number finds the branch's
// newest open PR.
func (c *Cache) PRs(
	repo github.Repo,
	lookups []github.Lookup,
) (map[string]*github.PR, time.Time, error) {
	if c == nil {
		return nil, time.Time{}, nil
	}
	ctx, key := context.Background(), repoKey(repo)
	var numbers []int64
	var branches []string
	for _, l := range lookups {
		if l.Number != 0 {
			numbers = append(numbers, int64(l.Number))
		} else {
			branches = append(branches, l.Branch)
		}
	}

	type found struct {
		data []byte
		at   int64
	}
	byNumber := map[int]found{}
	byBranch := map[string]found{}
	if len(numbers) > 0 {
		rows, err := c.q.PRsByNumber(ctx, cachedb.PRsByNumberParams{Repo: key, Numbers: numbers})
		if err != nil {
			return nil, time.Time{}, err
		}
		for _, r := range rows {
			byNumber[int(r.Number)] = found{r.Data, r.FetchedAt}
		}
	}
	if len(branches) > 0 {
		rows, err := c.q.OpenPRsByBranch(
			ctx,
			cachedb.OpenPRsByBranchParams{Repo: key, Branches: branches},
		)
		if err != nil {
			return nil, time.Time{}, err
		}
		// Newest first, so the first row of each branch wins.
		for _, r := range rows {
			if _, ok := byBranch[r.Branch]; !ok {
				byBranch[r.Branch] = found{r.Data, r.FetchedAt}
			}
		}
	}

	out := map[string]*github.PR{}
	var oldest time.Time
	for _, l := range lookups {
		f, ok := byNumber[l.Number]
		if l.Number == 0 {
			f, ok = byBranch[l.Branch]
		}
		if !ok {
			continue
		}
		var pr github.PR
		if err := json.Unmarshal(f.data, &pr); err != nil {
			return nil, time.Time{}, err
		}
		out[l.Branch] = &pr
		if t := time.UnixMilli(f.at); oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
	}
	return out, oldest, nil
}

// SavePRs stores prs, keyed by branch, as fetched at at.
func (c *Cache) SavePRs(repo github.Repo, prs map[string]*github.PR, at time.Time) error {
	if c == nil || len(prs) == 0 {
		return nil
	}
	return c.inTx(func(q *cachedb.Queries, _ *sql.Tx) error {
		for branch, pr := range prs {
			data, err := json.Marshal(pr)
			if err != nil {
				return err
			}
			err = q.UpsertPR(context.Background(), cachedb.UpsertPRParams{
				Repo:      repoKey(repo),
				Number:    int64(pr.Number),
				Branch:    branch,
				State:     pr.State,
				FetchedAt: at.UnixMilli(),
				Data:      data,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Suggested returns the cached suggestions for PR numbers that have them.
func (c *Cache) Suggested(repo github.Repo, numbers []int) (map[int]Suggestion, error) {
	if c == nil || len(numbers) == 0 {
		return nil, nil
	}
	ns := make([]int64, len(numbers))
	for i, n := range numbers {
		ns[i] = int64(n)
	}
	rows, err := c.q.Suggested(
		context.Background(),
		cachedb.SuggestedParams{Repo: repoKey(repo), Numbers: ns},
	)
	if err != nil {
		return nil, err
	}
	out := map[int]Suggestion{}
	for _, r := range rows {
		s := Suggestion{Head: r.Head}
		if err := json.Unmarshal(r.Logins, &s.Logins); err != nil {
			return nil, err
		}
		out[int(r.Number)] = s
	}
	return out, nil
}

// SaveSuggested stores suggestions by PR number, as fetched at at.
func (c *Cache) SaveSuggested(
	repo github.Repo,
	suggested map[int]Suggestion,
	at time.Time,
) error {
	if c == nil || len(suggested) == 0 {
		return nil
	}
	return c.inTx(func(q *cachedb.Queries, _ *sql.Tx) error {
		for n, s := range suggested {
			logins, err := json.Marshal(s.Logins)
			if err != nil {
				return err
			}
			err = q.UpsertSuggested(context.Background(), cachedb.UpsertSuggestedParams{
				Repo:      repoKey(repo),
				Number:    int64(n),
				Head:      s.Head,
				FetchedAt: at.UnixMilli(),
				Logins:    logins,
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Reviewers returns the cached reviewer list and when it was fetched, or nil
// when there is none.
func (c *Cache) Reviewers(repo github.Repo) ([]github.Reviewer, time.Time, error) {
	if c == nil {
		return nil, time.Time{}, nil
	}
	row, err := c.q.Reviewers(context.Background(), repoKey(repo))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	var reviewers []github.Reviewer
	if err := json.Unmarshal(row.Data, &reviewers); err != nil {
		return nil, time.Time{}, err
	}
	return reviewers, time.UnixMilli(row.FetchedAt), nil
}

// SaveReviewers stores the reviewer list, as fetched at at.
func (c *Cache) SaveReviewers(repo github.Repo, reviewers []github.Reviewer, at time.Time) error {
	if c == nil {
		return nil
	}
	data, err := json.Marshal(reviewers)
	if err != nil {
		return err
	}
	return c.q.UpsertReviewers(context.Background(), cachedb.UpsertReviewersParams{
		Repo:      repoKey(repo),
		FetchedAt: at.UnixMilli(),
		Data:      data,
	})
}
