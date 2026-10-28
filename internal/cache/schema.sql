-- Any change here moves the cache to a new file; see fileName in cache.go.

CREATE TABLE IF NOT EXISTS prs (
  repo TEXT NOT NULL,
  number INTEGER NOT NULL,
  branch TEXT NOT NULL,
  state TEXT NOT NULL,
  fetched_at INTEGER NOT NULL,
  -- github.PR as JSON.
  data BLOB NOT NULL,
  PRIMARY KEY (repo, number)
);

CREATE INDEX IF NOT EXISTS prs_branch ON prs (repo, branch);

CREATE TABLE IF NOT EXISTS suggested (
  repo TEXT NOT NULL,
  number INTEGER NOT NULL,
  head TEXT NOT NULL,
  fetched_at INTEGER NOT NULL,
  -- Logins as a JSON array.
  logins BLOB NOT NULL,
  PRIMARY KEY (repo, number)
);

CREATE TABLE IF NOT EXISTS reviewers (
  repo TEXT PRIMARY KEY,
  fetched_at INTEGER NOT NULL,
  -- []github.Reviewer as JSON.
  data BLOB NOT NULL
);
