/* tsqllint-disable set-ansi set-quoted-identifier set-transaction-isolation-level data-compression schema-qualify */
-- SQLite has no SQL Server SET options or table compression; its default
-- database needs no schema qualifier. Suppress only these dialect mismatches.

-- A span: work with a start and an end, of one kind, under its parent span.
-- A rule run's span has the rule run's id, the kind rule_run and no parent
-- span: its parent is its issue, which rule_run_spans names. process_id is
-- the crew process that started it; ended_at and outcome are null until it
-- ends, and stay null when a killed crew left it. Times are Unix
-- milliseconds in UTC.
CREATE TABLE spans (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    parent_id TEXT,
    process_id TEXT NOT NULL,
    started_at INTEGER NOT NULL,
    ended_at INTEGER,
    outcome TEXT
);

-- A rule run's span: the issue it ran for, its rule and queue, null for a
-- rule of no named queue, the route it ended through and the halt that
-- chose that route, each null when there is none, and the run of the same
-- issue and rule it continues, null when there was none. No foreign key: a
-- span outlives a lost issue record.
CREATE TABLE rule_run_spans (
    span_id TEXT PRIMARY KEY,
    tracker TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    issue_key TEXT NOT NULL,
    rule TEXT NOT NULL,
    queue TEXT,
    route TEXT,
    halted TEXT,
    continues_run_id TEXT
);

-- An issue's rule runs.
CREATE INDEX rule_run_spans_by_issue ON rule_run_spans (tracker, repository_id, issue_key);
