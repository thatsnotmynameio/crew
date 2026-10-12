/* tsqllint-disable set-ansi set-quoted-identifier set-transaction-isolation-level data-compression schema-qualify */
-- SQLite has no SQL Server SET options or table compression; its default
-- database needs no schema qualifier. Suppress only these dialect mismatches.

-- spans.kind is now also action, route or step. An action's parent is its
-- rule run's span; a route's is the span of the action whose verdict chose
-- it, else its rule run's; a step's is its route's. A span's children.
CREATE INDEX spans_by_parent ON spans (parent_id);

-- An action's or a route step's span: its name, its kind (session, shell,
-- function, question or check) and, for an action, its verdict, null for a
-- step. No table holds a reason, a log or a prompt's text. No foreign key,
-- as in spans.
CREATE TABLE work_spans (
    span_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    verdict TEXT
);

-- A session's span: its agent, harness and bot, null for the tracker's own
-- identity, the SHA-256 of its action's prompt template, when the session
-- started, null when it never did, and its turns, null when not reported.
CREATE TABLE session_spans (
    span_id TEXT PRIMARY KEY,
    agent TEXT NOT NULL,
    harness TEXT NOT NULL,
    bot TEXT,
    prompt_hash TEXT NOT NULL,
    session_started_at INTEGER,
    turns INTEGER
);

-- The tokens a session's harness reported for one model, the model empty
-- when it reported only a sum. A session with no row reported none, which
-- is not zero.
CREATE TABLE session_tokens (
    span_id TEXT NOT NULL,
    model TEXT NOT NULL,
    input INTEGER NOT NULL,
    output INTEGER NOT NULL,
    cache_read INTEGER NOT NULL,
    cache_write INTEGER NOT NULL,
    PRIMARY KEY (span_id, model)
);

-- A shell action's or step's span: its script's exit status, null when the
-- script did not exit.
CREATE TABLE shell_spans (
    span_id TEXT PRIMARY KEY,
    exit_status INTEGER
);

-- A function action's or step's span: the function it called and the
-- verdict the function returned, null when it returned none.
CREATE TABLE function_spans (
    span_id TEXT PRIMARY KEY,
    function TEXT NOT NULL,
    verdict TEXT
);

-- A route's span: the route and the verdict that chose it, null when no
-- action of its rule run chose it.
CREATE TABLE route_spans (
    span_id TEXT PRIMARY KEY,
    route TEXT NOT NULL,
    verdict TEXT
);
