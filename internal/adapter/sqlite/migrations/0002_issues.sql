/* tsqllint-disable set-ansi set-quoted-identifier set-transaction-isolation-level data-compression schema-qualify */
-- SQLite has no SQL Server SET options or table compression; its default
-- database needs no schema qualifier. Suppress only these dialect mismatches.

-- A repository crew works in, keyed by its tracker and the tracker's id for
-- it, with the latest name the tracker gave it.
CREATE TABLE repositories (
    tracker TEXT NOT NULL,
    id TEXT NOT NULL,
    name TEXT NOT NULL,
    PRIMARY KEY (tracker, id)
);

-- An issue crew saw carrying one of its rules' labels, kept from its first
-- sighting. created_at is the tracker's creation date, null when the tracker
-- reports none; seen_at is when crew first saw it; first_label is null when
-- crew first saw it in two crew labels. Times are Unix milliseconds in UTC.
CREATE TABLE issues (
    tracker TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    key TEXT NOT NULL,
    ref TEXT NOT NULL,
    kind TEXT NOT NULL,
    created_at INTEGER,
    seen_at INTEGER NOT NULL,
    first_label TEXT,
    PRIMARY KEY (tracker, repository_id, key)
);

-- A move of an issue from one crew label to another. moved_at is the
-- tracker's time of the move, null when the tracker reports none; seen_at is
-- when crew made or saw it; run_id is null when it was made outside crew.
-- No foreign key: a move outlives a lost issue record.
CREATE TABLE label_moves (
    id INTEGER PRIMARY KEY,
    tracker TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    issue_key TEXT NOT NULL,
    from_label TEXT NOT NULL,
    to_label TEXT NOT NULL,
    moved_at INTEGER,
    seen_at INTEGER NOT NULL,
    run_id TEXT
);

-- An issue's moves in order, for its latest move.
CREATE INDEX label_moves_by_issue ON label_moves (tracker, repository_id, issue_key, id);
