"""The ledger's schema, as migration steps of literal SQL (KTD6).

Step n creates or changes what version n of the schema holds, and ``Ledger.open`` sets
``user_version`` to n after applying it. A released step never changes: a later change
is a new step appended to ``MIGRATIONS``.
"""

from __future__ import annotations

# Version 1. Each table's comment says what one row is. JSON columns hold canonical JSON.
_SCHEMA_1 = """
-- A state a caller sent, once per hash, as its canonical JSON.
CREATE TABLE states (
  hash TEXT PRIMARY KEY, canonical TEXT NOT NULL,
  recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))) STRICT;

-- A question version, with its full definition, the first time a bank load held it.
CREATE TABLE question_versions (
  id INTEGER PRIMARY KEY, name TEXT NOT NULL, version_id TEXT NOT NULL,
  content_key TEXT NOT NULL, model TEXT NOT NULL,
  primitive TEXT NOT NULL CHECK (primitive IN ('noul', 'choice', 'score')),
  definition TEXT NOT NULL,
  recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  UNIQUE (name, version_id)) STRICT;

-- The stage a bank load declared for a version, when it differs from the last recorded.
CREATE TABLE stage_observations (
  id INTEGER PRIMARY KEY, question TEXT NOT NULL, version TEXT NOT NULL,
  stage TEXT NOT NULL CHECK (stage IN ('shadow', 'confirm', 'act')),
  observed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY (question, version) REFERENCES question_versions (name, version_id)) STRICT;

-- A TypeSafe request that answered: its raw response, models, usage, request id, attempts.
CREATE TABLE calls (
  id INTEGER PRIMARY KEY, state_hash TEXT NOT NULL REFERENCES states (hash),
  model_asked TEXT NOT NULL, model TEXT NOT NULL, response BLOB NOT NULL,
  usage TEXT NOT NULL, request_id TEXT, attempts INTEGER NOT NULL CHECK (attempts >= 1),
  recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))) STRICT;

-- One question's answer object within a call, under the version asked.
CREATE TABLE answers (
  id INTEGER PRIMARY KEY, call_id INTEGER NOT NULL REFERENCES calls (id),
  question TEXT NOT NULL, version TEXT NOT NULL, content_key TEXT NOT NULL,
  replay_key TEXT NOT NULL, answer TEXT NOT NULL, UNIQUE (call_id, question),
  FOREIGN KEY (question, version) REFERENCES question_versions (name, version_id)) STRICT;
CREATE INDEX answers_by_replay_key ON answers (replay_key, call_id);

-- One question of a caller request: served by an answer, or "cannot judge" with a reason.
CREATE TABLE asks (
  id INTEGER PRIMARY KEY, question TEXT NOT NULL, version TEXT NOT NULL,
  state_hash TEXT NOT NULL REFERENCES states (hash), replay_key TEXT NOT NULL,
  identifiers TEXT NOT NULL, fresh INTEGER NOT NULL CHECK (fresh IN (0, 1)),
  replayed INTEGER NOT NULL CHECK (replayed IN (0, 1)),
  effective_stage TEXT NOT NULL CHECK (effective_stage IN ('shadow', 'confirm', 'act')),
  answer_id INTEGER REFERENCES answers (id), reason TEXT,
  attempts INTEGER CHECK (attempts >= 0),
  asked_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY (question, version) REFERENCES question_versions (name, version_id),
  CHECK ((answer_id IS NULL) = (reason IS NOT NULL)),
  CHECK ((reason IS NULL) = (attempts IS NULL)),
  CHECK (replayed = 0 OR answer_id IS NOT NULL)) STRICT;
CREATE INDEX asks_by_version ON asks (question, version);
CREATE INDEX asks_by_state ON asks (question, state_hash);

-- What a caller did with an ask's answer; the latest per ask counts.
CREATE TABLE decisions (
  id INTEGER PRIMARY KEY, ask_id INTEGER NOT NULL REFERENCES asks (id),
  decision TEXT NOT NULL CHECK (decision IN ('acted', 'sent_to_person', 'ignored_in_shadow')),
  decided_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))) STRICT;
CREATE INDEX decisions_by_ask ON decisions (ask_id);

-- A real outcome (a JSON value) for an ask's question and state, matched by the ask's id,
-- or by the caller identifiers matched_by holds.
CREATE TABLE outcomes (
  id INTEGER PRIMARY KEY, ask_id INTEGER NOT NULL REFERENCES asks (id),
  value TEXT NOT NULL, source TEXT NOT NULL,
  strength TEXT NOT NULL CHECK (strength IN ('strong', 'weak')), matched_by TEXT,
  recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))) STRICT;
CREATE INDEX outcomes_by_ask ON outcomes (ask_id);

-- A recheck of a version from an earlier one, for a stage: the bar it was judged by,
-- its result, counts and report (the flips by kind, with their decisions).
CREATE TABLE rechecks (
  id INTEGER PRIMARY KEY, question TEXT NOT NULL, from_version TEXT NOT NULL,
  to_version TEXT NOT NULL, stage TEXT NOT NULL CHECK (stage IN ('shadow', 'confirm', 'act')),
  result TEXT NOT NULL CHECK (result IN ('passed', 'failed', 'insufficient', 'incomplete')),
  compared INTEGER NOT NULL CHECK (compared >= 0),
  flipped INTEGER NOT NULL CHECK (flipped >= 0),
  cannot_judge INTEGER NOT NULL CHECK (cannot_judge >= 0),
  bar TEXT NOT NULL, report TEXT NOT NULL,
  recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY (question, from_version) REFERENCES question_versions (name, version_id),
  FOREIGN KEY (question, to_version) REFERENCES question_versions (name, version_id)) STRICT;
CREATE INDEX rechecks_by_version ON rechecks (question, to_version);

-- A calibration of a version, for a stage: its seed, the identifier key it split by
-- (null: by state hash), its label restriction, result, proposed bands (null when the
-- labels are insufficient) and report.
CREATE TABLE calibrations (
  id INTEGER PRIMARY KEY, question TEXT NOT NULL, version TEXT NOT NULL,
  stage TEXT NOT NULL CHECK (stage IN ('shadow', 'confirm', 'act')),
  seed TEXT NOT NULL, split_key TEXT,
  strong_only INTEGER NOT NULL CHECK (strong_only IN (0, 1)),
  result TEXT NOT NULL CHECK (result IN ('passed', 'failed', 'insufficient')),
  proposed_bands TEXT, report TEXT NOT NULL,
  recorded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
  FOREIGN KEY (question, version) REFERENCES question_versions (name, version_id)) STRICT;
CREATE INDEX calibrations_by_version ON calibrations (question, version);

-- The ledger only grows.
CREATE TRIGGER states_no_update BEFORE UPDATE ON states
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER states_no_delete BEFORE DELETE ON states
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER question_versions_no_update BEFORE UPDATE ON question_versions
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER question_versions_no_delete BEFORE DELETE ON question_versions
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER stage_observations_no_update BEFORE UPDATE ON stage_observations
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER stage_observations_no_delete BEFORE DELETE ON stage_observations
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER calls_no_update BEFORE UPDATE ON calls
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER calls_no_delete BEFORE DELETE ON calls
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER answers_no_update BEFORE UPDATE ON answers
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER answers_no_delete BEFORE DELETE ON answers
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER asks_no_update BEFORE UPDATE ON asks
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER asks_no_delete BEFORE DELETE ON asks
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER decisions_no_update BEFORE UPDATE ON decisions
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER decisions_no_delete BEFORE DELETE ON decisions
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER outcomes_no_update BEFORE UPDATE ON outcomes
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER outcomes_no_delete BEFORE DELETE ON outcomes
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER rechecks_no_update BEFORE UPDATE ON rechecks
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER rechecks_no_delete BEFORE DELETE ON rechecks
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER calibrations_no_update BEFORE UPDATE ON calibrations
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
CREATE TRIGGER calibrations_no_delete BEFORE DELETE ON calibrations
  BEGIN SELECT RAISE(ABORT, 'the ledger is append-only'); END;
"""

MIGRATIONS: tuple[str, ...] = (_SCHEMA_1,)
"""The schema steps in order; step n sets ``user_version`` to n. Only ever append."""

SCHEMA_VERSION = len(MIGRATIONS)
