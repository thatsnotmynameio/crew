"""The decision ledger: an append-only SQLite file, `.crew/typesafe/ledger.sqlite` (KTD6).

One ``Ledger`` serves a process. Each thread borrows its own connection for one read or
one write, so the threaded HTTP server never shares a connection between threads.
Connections run in SQLite's autocommit mode, where ``commit()``, ``rollback()`` and
``with con:`` do nothing, so a write runs ``BEGIN IMMEDIATE`` and ``COMMIT`` or
``ROLLBACK`` as SQL. A read runs in a deferred transaction: one snapshot, never the
write lock.

Migrations are literal SQL steps, each applied in its own ``BEGIN IMMEDIATE`` with the
``user_version`` it sets. A process re-reads the version inside the transaction, so two
processes opening a fresh ledger migrate it once.
"""

import json
import os
import sqlite3
import threading
import time
from contextlib import contextmanager
from dataclasses import dataclass, field
from pathlib import Path
from typing import TYPE_CHECKING, Literal, Self, TypeAlias, cast

import rfc8785

from typesafe_judge.evidence import EvidenceReads, EvidenceWrites
from typesafe_judge.keys import JSON, canonical_identifiers, canonical_state, state_hash
from typesafe_judge.schema import MIGRATIONS, SCHEMA_VERSION

if TYPE_CHECKING:
    from collections.abc import Iterator, Mapping

    from typesafe_judge.bank import Bank, Question

DIRECTORY = Path(".crew/typesafe")
"""The judge's directory, relative to the repository root."""

FILE = "ledger.sqlite"

MIN_SQLITE = (3, 51, 3)
"""The first SQLite release without the WAL-reset corruption bug (sqlite.org/wal.html)."""

DIRECTORY_MODE = 0o700
FILE_MODE = 0o600
BUSY_TIMEOUT = 30.0

StageName: TypeAlias = Literal["shadow", "confirm", "act"]
"""A stage as the ledger stores it: ``Stage.value``."""


class LedgerError(Exception):
    """A ledger the judge cannot use: a newer one, an old SQLite, or an unwritable directory."""


@dataclass(frozen=True, slots=True)
class NewCall:
    """A TypeSafe request that answered, to record."""

    state_hash: str
    model_asked: str
    model: str
    response: bytes
    usage: Mapping[str, JSON]
    request_id: str | None
    attempts: int


@dataclass(frozen=True, slots=True)
class NewAnswer:
    """One question's answer within a recorded call: the response's answer object for it."""

    call_id: int
    question: str
    version: str
    content_key: str
    replay_key: str
    answer: Mapping[str, JSON]


@dataclass(frozen=True, slots=True)
class NewAsk:
    """One question of a caller request: an ``answer_id``, or a ``reason`` and ``attempts``."""

    question: str
    version: str
    state_hash: str
    replay_key: str
    effective_stage: StageName
    identifiers: Mapping[str, str | int] = field(default_factory=dict)
    fresh: bool = False
    replayed: bool = False
    answer_id: int | None = None
    reason: str | None = None
    attempts: int | None = None


@dataclass(frozen=True, slots=True)
class Answer:
    """A recorded answer, with the call it came in."""

    id: int
    call_id: int
    question: str
    version: str
    model_asked: str
    model: str
    answer: dict[str, JSON]


@dataclass(frozen=True, slots=True)
class AskRecord:
    """A recorded ask, with the answer that served it, if any."""

    id: int
    version: str
    state_hash: str
    identifiers: dict[str, str | int]
    replayed: bool
    effective_stage: StageName
    reason: str | None
    content_key: str | None
    answer: dict[str, JSON] | None


@dataclass(frozen=True, slots=True)
class AskOrigin:
    """What a recorded ask was asked under: its question, version, state and effective stage."""

    id: int
    question: str
    version: str
    state_hash: str
    effective_stage: StageName


@dataclass(frozen=True, slots=True)
class VersionRecord:
    """A recorded question version: its content key, primitive and full definition."""

    content_key: str
    primitive: Literal["noul", "choice", "score"]
    definition: dict[str, JSON]


@dataclass(frozen=True, slots=True)
class AnsweredAsk:
    """An ask an answer served, with its state and the latest decision on it, if any."""

    id: int
    state_hash: str
    decision: str | None


@dataclass(frozen=True, slots=True)
class NewOutcome:
    """A real outcome to record on an ask; matched_by holds the identifiers it was matched by."""

    ask_id: int
    value: JSON
    source: str
    strength: Literal["strong", "weak"]
    matched_by: Mapping[str, str | int] | None


@dataclass(frozen=True, slots=True)
class NewRecheck:
    """A recheck's result, counts, the bar it was judged by and its report, to record."""

    question: str
    from_version: str
    to_version: str
    stage: StageName
    result: Literal["passed", "failed", "insufficient", "incomplete"]
    compared: int
    flipped: int
    cannot_judge: int
    bar: Mapping[str, JSON]
    report: Mapping[str, JSON]


class Reads(EvidenceReads):
    """Queries over one connection; they never write."""

    def __init__(self, con: sqlite3.Connection) -> None:
        """Query through con."""
        self._con = con

    def first_answer(self, replay_key: str) -> Answer | None:
        """Return the answer of the lowest call for replay_key, the one replays serve."""
        row = self._con.execute(
            "SELECT a.id, a.call_id, a.question, a.version, c.model_asked, c.model, a.answer"
            " FROM answers a JOIN calls c ON c.id = a.call_id WHERE a.replay_key = ?"
            " ORDER BY a.call_id, a.id LIMIT 1",
            (replay_key,),
        ).fetchone()
        if row is None:
            return None
        answer_id, call_id, question, version, model_asked, model, answer = row
        return Answer(answer_id, call_id, question, version, model_asked, model, _object(answer))

    def versions(self, name: str) -> list[str]:
        """Return the versions of the question named name, in the order they were first recorded."""
        rows = self._con.execute(
            "SELECT version_id FROM question_versions WHERE name = ? ORDER BY id", (name,)
        )
        return [version for (version,) in rows]

    def observed_stages(self, name: str, version: str) -> list[StageName]:
        """Return the declared stages recorded for a version, oldest first."""
        rows = self._con.execute(
            "SELECT stage FROM stage_observations WHERE question = ? AND version = ? ORDER BY id",
            (name, version),
        )
        return [stage for (stage,) in rows]

    def passed_rechecks(self, name: str, version: str) -> list[tuple[str, StageName]]:
        """Return the earlier version and the stage of each passing recheck to a version."""
        rows = self._con.execute(
            "SELECT from_version, stage FROM rechecks"
            " WHERE question = ? AND to_version = ? AND result = 'passed' ORDER BY id",
            (name, version),
        )
        return [(earlier, stage) for earlier, stage in rows]

    def passed_calibrations(self, name: str, version: str) -> list[StageName]:
        """Return the stage of each passing calibration of a version."""
        rows = self._con.execute(
            "SELECT stage FROM calibrations"
            " WHERE question = ? AND version = ? AND result = 'passed' ORDER BY id",
            (name, version),
        )
        return [stage for (stage,) in rows]

    def asks(self, question: str, identifiers: Mapping[str, str | int]) -> list[AskRecord]:
        """Return a question's asks whose identifiers hold every one given, oldest first.

        Types are significant, so ``5`` does not match ``"5"`` (KTD13).
        """
        rows = self._con.execute(
            "SELECT k.id, k.version, k.state_hash, k.identifiers, k.replayed, k.effective_stage,"
            " k.reason, a.content_key, a.answer"
            " FROM asks k LEFT JOIN answers a ON a.id = k.answer_id"
            " WHERE k.question = ? ORDER BY k.id",
            (question,),
        )
        records: list[AskRecord] = []
        for ask_id, version, digest, held, replayed, stage, reason, key, answer in rows:
            held_identifiers = cast("dict[str, str | int]", json.loads(held))
            if all(_same(held_identifiers.get(k), v) for k, v in identifiers.items()):
                records.append(
                    AskRecord(
                        id=ask_id,
                        version=version,
                        state_hash=digest,
                        identifiers=held_identifiers,
                        replayed=bool(replayed),
                        effective_stage=stage,
                        reason=reason,
                        content_key=key,
                        answer=None if answer is None else _object(answer),
                    )
                )
        return records

    def ask_origin(self, ask_id: int) -> AskOrigin | None:
        """Return what the ask with ask_id was asked under, or None when there is none."""
        row = self._con.execute(
            "SELECT id, question, version, state_hash, effective_stage FROM asks WHERE id = ?",
            (ask_id,),
        ).fetchone()
        return None if row is None else AskOrigin(*row)

    def version(self, name: str, version: str) -> VersionRecord | None:
        """Return a recorded version of the question named name."""
        row = self._con.execute(
            "SELECT content_key, primitive, definition FROM question_versions"
            " WHERE name = ? AND version_id = ?",
            (name, version),
        ).fetchone()
        return None if row is None else VersionRecord(row[0], row[1], _object(row[2]))

    def answered_asks(self, name: str, content_key: str) -> list[AnsweredAsk]:
        """Return a question's asks served an answer to content_key, oldest first."""
        rows = self._con.execute(
            "SELECT k.id, k.state_hash, (SELECT d.decision FROM decisions d"
            "  WHERE d.ask_id = k.id ORDER BY d.id DESC LIMIT 1)"
            " FROM asks k JOIN answers a ON a.id = k.answer_id"
            " WHERE k.question = ? AND a.content_key = ? ORDER BY k.id",
            (name, content_key),
        )
        return [AnsweredAsk(*row) for row in rows]

    def state(self, digest: str) -> JSON:
        """Return the recorded state whose hash is digest."""
        row = self._con.execute("SELECT canonical FROM states WHERE hash = ?", (digest,))
        return cast("JSON", json.loads(row.fetchone()[0]))

    def latest_outcome(self, question: str, digest: str, source: str) -> tuple[str, str] | None:
        """Return the canonical value and strength of the latest outcome for a question's state."""
        row = self._con.execute(
            "SELECT o.value, o.strength FROM outcomes o JOIN asks k ON k.id = o.ask_id"
            " WHERE k.question = ? AND k.state_hash = ? AND o.source = ?"
            " ORDER BY o.id DESC LIMIT 1",
            (question, digest, source),
        ).fetchone()
        return None if row is None else (row[0], row[1])

    def bank_changes(self, bank: Bank) -> list[tuple[Question, bool]]:
        """Return the questions whose version, or declared stage, the ledger lacks.

        Each comes with whether its version is new to the ledger.
        """
        changes: list[tuple[Question, bool]] = []
        for question in bank.values():
            row = self._con.execute(
                "SELECT stage FROM stage_observations WHERE question = ? AND version = ?"
                " ORDER BY id DESC LIMIT 1",
                (question.name, question.version_id),
            ).fetchone()
            if row is None or row[0] != question.stage.value:
                changes.append((question, row is None))
        return changes


class Writes(Reads, EvidenceWrites):
    """Inserts within one write transaction."""

    def record_bank(self, bank: Bank) -> bool:
        """Record the versions and declared stages bank holds that the ledger lacks."""
        changes = self.bank_changes(bank)
        for question, new in changes:
            if new:
                self._con.execute(
                    "INSERT INTO question_versions"
                    " (name, version_id, content_key, model, primitive, definition)"
                    " VALUES (?, ?, ?, ?, ?, ?)",
                    (
                        question.name,
                        question.version_id,
                        question.content_key,
                        question.model,
                        question.primitive,
                        _json(question.spec.model_dump(mode="json")),
                    ),
                )
            self._con.execute(
                "INSERT INTO stage_observations (question, version, stage) VALUES (?, ?, ?)",
                (question.name, question.version_id, question.stage.value),
            )
        return bool(changes)

    def store_state(self, state: object) -> str:
        """Store state once per hash and return its hash; raise StateError when it cannot."""
        digest = state_hash(state)
        self._con.execute(
            "INSERT INTO states (hash, canonical) VALUES (?, ?) ON CONFLICT (hash) DO NOTHING",
            (digest, canonical_state(state).decode()),
        )
        return digest

    def insert_call(self, call: NewCall) -> int:
        """Record a call and return its id."""
        return self._insert(
            "INSERT INTO calls"
            " (state_hash, model_asked, model, response, usage, request_id, attempts)"
            " VALUES (?, ?, ?, ?, ?, ?, ?)",
            (
                call.state_hash,
                call.model_asked,
                call.model,
                call.response,
                _json(dict(call.usage)),
                call.request_id,
                call.attempts,
            ),
        )

    def insert_answer(self, answer: NewAnswer) -> int:
        """Record one question's answer within a call and return its id."""
        return self._insert(
            "INSERT INTO answers (call_id, question, version, content_key, replay_key, answer)"
            " VALUES (?, ?, ?, ?, ?, ?)",
            (
                answer.call_id,
                answer.question,
                answer.version,
                answer.content_key,
                answer.replay_key,
                _json(dict(answer.answer)),
            ),
        )

    def insert_ask(self, ask: NewAsk) -> int:
        """Record an ask and return its id; raise StateError on invalid identifiers."""
        return self._insert(
            "INSERT INTO asks (question, version, state_hash, replay_key, identifiers, fresh,"
            " replayed, effective_stage, answer_id, reason, attempts)"
            " VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            (
                ask.question,
                ask.version,
                ask.state_hash,
                ask.replay_key,
                canonical_identifiers(dict(ask.identifiers)).decode(),
                ask.fresh,
                ask.replayed,
                ask.effective_stage,
                ask.answer_id,
                ask.reason,
                ask.attempts,
            ),
        )

    def insert_decision(self, ask_id: int, decision: str) -> int:
        """Record what a caller did with an ask's answer and return its id."""
        return self._insert(
            "INSERT INTO decisions (ask_id, decision) VALUES (?, ?)", (ask_id, decision)
        )

    def insert_outcome(self, outcome: NewOutcome) -> int:
        """Record an outcome and return its id."""
        matched_by = outcome.matched_by
        return self._insert(
            "INSERT INTO outcomes (ask_id, value, source, strength, matched_by)"
            " VALUES (?, ?, ?, ?, ?)",
            (
                outcome.ask_id,
                _json(outcome.value),
                outcome.source,
                outcome.strength,
                None if matched_by is None else canonical_identifiers(dict(matched_by)).decode(),
            ),
        )

    def insert_recheck(self, recheck: NewRecheck) -> int:
        """Record a recheck and return its id."""
        return self._insert(
            "INSERT INTO rechecks (question, from_version, to_version, stage, result,"
            " compared, flipped, cannot_judge, bar, report)"
            " VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
            (
                recheck.question,
                recheck.from_version,
                recheck.to_version,
                recheck.stage,
                recheck.result,
                recheck.compared,
                recheck.flipped,
                recheck.cannot_judge,
                _json(dict(recheck.bar)),
                _json(dict(recheck.report)),
            ),
        )

    def _insert(self, sql: str, parameters: tuple[object, ...]) -> int:
        return cast("int", self._con.execute(sql, parameters).lastrowid)


class Ledger:
    """The repository's ledger; one per process, safe to use from many threads."""

    def __init__(self, path: Path) -> None:
        """Use the migrated ledger at path; ``Ledger.open`` makes one."""
        self.path = path
        self._lock = threading.Lock()
        self._idle: list[sqlite3.Connection] = []
        self._closed = False

    @classmethod
    def open(
        cls, root: Path, *, sqlite_version: tuple[int, int, int] = sqlite3.sqlite_version_info
    ) -> Self:
        """Open, creating and migrating as needed, the ledger of the repository at root.

        Raise LedgerError when SQLite is older than MIN_SQLITE, the ledger is newer than
        this code, or its directory or file cannot be made or opened.
        """
        if sqlite_version < MIN_SQLITE:
            found, floor = (".".join(map(str, v)) for v in (sqlite_version, MIN_SQLITE))
            msg = (
                f"SQLite {found} is older than {floor}, the first release without WAL-reset"
                " corruption; run the judge on uv's managed Python"
            )
            raise LedgerError(msg)
        ledger = cls(_create_file(root / DIRECTORY))
        try:
            with ledger._connection() as con:
                _migrate(con, ledger.path)
        except sqlite3.Error as error:
            ledger.close()
            msg = f"cannot open the ledger {ledger.path}: {error}"
            raise LedgerError(msg) from error
        except LedgerError:
            ledger.close()
            raise
        return ledger

    def close(self) -> None:
        """Close every connection; one in use is closed when it is given back."""
        with self._lock:
            self._closed = True
            idle, self._idle = self._idle, []
        for con in idle:
            con.close()

    @contextmanager
    def read(self) -> Iterator[Reads]:
        """Query one snapshot of the ledger, without taking the write lock."""
        with self._connection() as con:
            con.execute("BEGIN DEFERRED")
            try:
                yield Reads(con)
            finally:
                con.execute("ROLLBACK")

    @contextmanager
    def write(self) -> Iterator[Writes]:
        """Insert in one transaction, committed when the block ends and rolled back if it raises."""
        with self._connection() as con:
            con.execute("BEGIN IMMEDIATE")
            try:
                yield Writes(con)
                con.execute("COMMIT")
            finally:
                if con.in_transaction:
                    con.execute("ROLLBACK")

    def record_bank(self, bank: Bank) -> bool:
        """Record what a bank load holds that the ledger lacks; False when nothing was new.

        That is each version the first time a load holds it, with its full definition,
        and each version's declared stage when it differs from the last one recorded.
        """
        with self.read() as reads:
            if not reads.bank_changes(bank):
                return False
        with self.write() as writes:
            return writes.record_bank(bank)

    @contextmanager
    def _connection(self) -> Iterator[sqlite3.Connection]:
        with self._lock:
            con = self._idle.pop() if self._idle else None
        if con is None:
            con = _connect(self.path)
        try:
            yield con
        finally:
            with self._lock:
                closed = self._closed
                if not closed:
                    self._idle.append(con)
            if closed:
                con.close()


def _create_file(directory: Path) -> Path:
    """Create directory at 0700, tightening it if it exists, and the ledger file at 0600."""
    path = directory / FILE
    try:
        directory.mkdir(mode=DIRECTORY_MODE, exist_ok=True)
        directory.chmod(DIRECTORY_MODE)
        # Created before SQLite opens it, so the file, and its -wal and -shm, are 0600.
        os.close(os.open(path, os.O_RDWR | os.O_CREAT | os.O_CLOEXEC, FILE_MODE))
    except OSError as error:
        msg = f"cannot create the ledger in {directory}: {error.strerror}"
        raise LedgerError(msg) from error
    return path


def _connect(path: Path) -> sqlite3.Connection:
    # A connection serves one thread at a time; the pool hands it from thread to thread.
    con = sqlite3.connect(path, autocommit=True, timeout=BUSY_TIMEOUT, check_same_thread=False)
    con.execute("PRAGMA foreign_keys = ON")
    con.execute("PRAGMA synchronous = FULL")
    return con


def _user_version(con: sqlite3.Connection, path: Path) -> int:
    version = cast("int", con.execute("PRAGMA user_version").fetchone()[0])
    if version > SCHEMA_VERSION:
        msg = (
            f"the ledger {path} is at schema version {version}, newer than the schema"
            f" version {SCHEMA_VERSION} this judge knows; run a newer typesafe-judge"
        )
        raise LedgerError(msg)
    return version


def _migrate(con: sqlite3.Connection, path: Path) -> None:
    version = _user_version(con, path)
    if version == 0:
        _use_wal(con)
    for target, step in enumerate(MIGRATIONS, start=1):
        if target <= version:
            continue
        con.execute("BEGIN IMMEDIATE")
        try:
            # Another process may have migrated since the version was read.
            if _user_version(con, path) < target:
                con.executescript(f"{step}\nPRAGMA user_version = {target};")
            con.execute("COMMIT")
        finally:
            if con.in_transaction:
                con.execute("ROLLBACK")


def _use_wal(con: sqlite3.Connection) -> None:
    """Switch a new ledger to WAL, which persists in the file.

    While another connection holds the write lock, as a second process migrating a
    fresh ledger does, the switch fails at once instead of waiting out the busy
    timeout, so it is retried here within the same timeout.
    """
    deadline = time.monotonic() + BUSY_TIMEOUT
    while True:
        try:
            con.execute("PRAGMA journal_mode = WAL")
        except sqlite3.OperationalError as error:
            if error.sqlite_errorcode != sqlite3.SQLITE_BUSY or time.monotonic() > deadline:
                raise
            time.sleep(0.01)
        else:
            return


def _same(held: str | int | None, wanted: str | int) -> bool:
    return type(held) is type(wanted) and held == wanted


def _json(value: JSON) -> str:
    return rfc8785.dumps(value).decode()


def _object(text: str) -> dict[str, JSON]:
    return cast("dict[str, JSON]", json.loads(text))
