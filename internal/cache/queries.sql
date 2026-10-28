-- name: PRsByNumber :many
SELECT number, fetched_at, data FROM prs
WHERE repo = @repo AND number IN (sqlc.slice('numbers'));

-- name: OpenPRsByBranch :many
SELECT branch, fetched_at, data FROM prs
WHERE repo = @repo AND state = 'OPEN' AND branch IN (sqlc.slice('branches'))
ORDER BY number DESC;

-- name: UpsertPR :exec
-- Upserts keep a row that another TUI fetched later than the one written.
INSERT INTO prs (repo, number, branch, state, fetched_at, data)
VALUES (@repo, @number, @branch, @state, @fetched_at, @data)
ON CONFLICT (repo, number) DO UPDATE SET
  branch = excluded.branch,
  state = excluded.state,
  fetched_at = excluded.fetched_at,
  data = excluded.data
WHERE excluded.fetched_at >= prs.fetched_at;

-- name: Suggested :many
SELECT number, head, logins FROM suggested
WHERE repo = @repo AND number IN (sqlc.slice('numbers'));

-- name: UpsertSuggested :exec
INSERT INTO suggested (repo, number, head, fetched_at, logins)
VALUES (@repo, @number, @head, @fetched_at, @logins)
ON CONFLICT (repo, number) DO UPDATE SET
  head = excluded.head,
  fetched_at = excluded.fetched_at,
  logins = excluded.logins
WHERE excluded.fetched_at >= suggested.fetched_at;

-- name: Reviewers :one
SELECT fetched_at, data FROM reviewers WHERE repo = @repo;

-- name: UpsertReviewers :exec
INSERT INTO reviewers (repo, fetched_at, data)
VALUES (@repo, @fetched_at, @data)
ON CONFLICT (repo) DO UPDATE SET
  fetched_at = excluded.fetched_at,
  data = excluded.data
WHERE excluded.fetched_at >= reviewers.fetched_at;

-- name: DeletePRsBefore :exec
DELETE FROM prs WHERE fetched_at < @cutoff;

-- name: DeleteSuggestedBefore :exec
DELETE FROM suggested WHERE fetched_at < @cutoff;

-- name: DeleteReviewersBefore :exec
DELETE FROM reviewers WHERE fetched_at < @cutoff;
