-- Migrations run in order, once each; see migrate in state.go.
-- A shipped migration MUST NOT change: add a new file instead.

CREATE TABLE snoozes (
  repo TEXT NOT NULL,
  -- 'pr' or 'stack'.
  kind TEXT NOT NULL CHECK (kind IN ('pr', 'stack')),
  number INTEGER NOT NULL,
  -- When the snooze wakes, in Unix milliseconds.
  wakes_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (repo, kind, number)
);
