-- name: Snoozes :many
SELECT kind, number, wakes_at FROM snoozes
WHERE repo = @repo AND wakes_at > @now;

-- name: UpsertSnooze :exec
INSERT INTO snoozes (repo, kind, number, wakes_at, created_at)
VALUES (@repo, @kind, @number, @wakes_at, @created_at)
ON CONFLICT (repo, kind, number) DO UPDATE SET
  wakes_at = excluded.wakes_at,
  created_at = excluded.created_at;

-- name: DeleteSnooze :exec
DELETE FROM snoozes WHERE repo = @repo AND kind = @kind AND number = @number;

-- name: DeleteSnoozesBefore :exec
DELETE FROM snoozes WHERE wakes_at < @cutoff;
