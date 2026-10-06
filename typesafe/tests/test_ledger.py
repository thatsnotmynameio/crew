import json
import os
import sqlite3
import stat
import threading
from typing import TYPE_CHECKING

import pytest

from typesafe_judge.bank import Bank, Question, parse_bank
from typesafe_judge.keys import replay_key, state_hash
from typesafe_judge.ledger import Ledger, LedgerError, NewAnswer, NewAsk, NewCall
from typesafe_judge.schema import SCHEMA_VERSION

if TYPE_CHECKING:
    from collections.abc import Iterator
    from pathlib import Path

QUESTION = """\
questions:
  issue_needs_candidate:
    primitive: noul
    instructions: {instructions}
    model: jev-1.13.0
    bands: {{yes_at: 0.7, no_at: 0.3}}
    default: no
    stage: {stage}
    bar: {{max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}}
"""
STATE = {"issue": "Add a ledger", "candidate": "Add a bank"}
RESPONSE = b'{"model":"jev-1.13.0","answers":{"issue_needs_candidate":{"type":"noul","noul":0.9}}}'


def bank(*, stage: str = "act", instructions: str = "Does `issue` build on `candidate`?") -> Bank:
    text = QUESTION.format(stage=stage, instructions=instructions)
    return parse_bank(text, source="typesafe.yaml")


@pytest.fixture
def root(tmp_path: Path) -> Path:
    (tmp_path / ".crew").mkdir()
    return tmp_path


@pytest.fixture
def ledger(root: Path) -> Iterator[Ledger]:
    opened = Ledger.open(root)
    yield opened
    opened.close()


def raw(root: Path) -> sqlite3.Connection:
    """Open the ledger file directly, outside the code under test."""
    return sqlite3.connect(root / ".crew/typesafe/ledger.sqlite", autocommit=True)


def rows(root: Path, sql: str) -> list[tuple[object, ...]]:
    con = raw(root)
    try:
        return con.execute(sql).fetchall()
    finally:
        con.close()


def mode(path: Path) -> int:
    return stat.S_IMODE(path.stat().st_mode)


def record_answer(ledger: Ledger, question: Question, state: object, *, noul: float = 0.9) -> int:
    """Record one live call answering question over state, and its ask; return the call id."""
    key = replay_key(question.content_key, state_hash(state))
    with ledger.write() as writes:
        digest = writes.store_state(state)
        call_id = writes.insert_call(
            NewCall(
                state_hash=digest,
                model_asked=question.model,
                model="jev-1.13.0",
                response=RESPONSE,
                usage={"input_tokens": 10, "output_tokens": 2},
                request_id="req_1",
                attempts=1,
            )
        )
        answer_id = writes.insert_answer(
            NewAnswer(
                call_id=call_id,
                question=question.name,
                version=question.version_id,
                content_key=question.content_key,
                replay_key=key,
                answer={"type": "noul", "noul": noul},
            )
        )
        writes.insert_ask(
            NewAsk(
                question=question.name,
                version=question.version_id,
                state_hash=digest,
                replay_key=key,
                effective_stage=question.stage.value,
                answer_id=answer_id,
            )
        )
    return call_id


def test_fresh_root_gets_a_private_directory_and_file_in_wal_mode(
    root: Path, ledger: Ledger
) -> None:
    record_answer(ledger, recorded(ledger), STATE)

    directory = root / ".crew/typesafe"
    assert mode(directory) == 0o700
    assert mode(directory / "ledger.sqlite") == 0o600
    assert mode(directory / "ledger.sqlite-wal") == 0o600
    assert rows(root, "PRAGMA journal_mode") == [("wal",)]
    assert rows(root, "PRAGMA user_version") == [(SCHEMA_VERSION,)]


def test_an_existing_open_directory_is_tightened(root: Path) -> None:
    directory = root / ".crew/typesafe"
    directory.mkdir()
    directory.chmod(0o755)

    Ledger.open(root).close()

    assert mode(directory) == 0o700


def recorded(ledger: Ledger, *, stage: str = "act") -> Question:
    """Record a one-question bank and return its question."""
    loaded = bank(stage=stage)
    ledger.record_bank(loaded)
    return loaded["issue_needs_candidate"]


def fill_every_table(root: Path, ledger: Ledger) -> None:
    question = recorded(ledger)
    record_answer(ledger, question, STATE)
    version = question.version_id
    con = raw(root)
    try:
        con.execute("PRAGMA foreign_keys = ON")
        con.execute("INSERT INTO decisions (ask_id, decision) VALUES (1, 'acted')")
        con.execute(
            "INSERT INTO outcomes (ask_id, value, source, strength)"
            " VALUES (1, 'true', 'gh', 'strong')"
        )
        con.execute(
            "INSERT INTO rechecks (question, from_version, to_version, stage, result,"
            " compared, flipped, cannot_judge, bar, report)"
            " VALUES (?, ?, ?, 'act', 'passed', 30, 0, 0, '{}', '{}')",
            (question.name, version, version),
        )
        con.execute(
            "INSERT INTO calibrations (question, version, stage, seed, strong_only, result, report)"
            " VALUES (?, ?, 'act', 'seed', 0, 'insufficient', '{}')",
            (question.name, version),
        )
    finally:
        con.close()


def test_every_table_refuses_update_and_delete(root: Path, ledger: Ledger) -> None:
    fill_every_table(root, ledger)
    tables = [
        name
        for (name,) in rows(
            root, "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
        )
    ]
    assert len(tables) == 10

    con = raw(root)
    try:
        for table in tables:
            assert con.execute(f"SELECT count(*) FROM {table}").fetchone()[0] > 0, table  # noqa: S608 - a table name from sqlite_schema
            with pytest.raises(sqlite3.IntegrityError, match="append-only"):
                con.execute(f"UPDATE {table} SET rowid = rowid")  # noqa: S608 - as above
            with pytest.raises(sqlite3.IntegrityError, match="append-only"):
                con.execute(f"DELETE FROM {table}")  # noqa: S608 - as above
    finally:
        con.close()


def test_a_newer_ledger_is_refused_naming_both_versions(root: Path) -> None:
    Ledger.open(root).close()
    con = raw(root)
    con.execute(f"PRAGMA user_version = {SCHEMA_VERSION + 7}")
    con.close()

    with pytest.raises(LedgerError) as error:
        Ledger.open(root)

    message = str(error.value)
    assert f"version {SCHEMA_VERSION + 7}" in message
    assert f"version {SCHEMA_VERSION}" in message
    assert "\n" not in message


@pytest.mark.parametrize("version", [(3, 51, 2), (3, 50, 4), (3, 44, 6), (2, 99, 99)])
def test_sqlite_older_than_3_51_3_is_refused(root: Path, version: tuple[int, int, int]) -> None:
    with pytest.raises(LedgerError) as error:
        Ledger.open(root, sqlite_version=version)

    message = str(error.value)
    assert ".".join(map(str, version)) in message
    assert "3.51.3" in message
    assert "\n" not in message
    assert not (root / ".crew/typesafe").exists()


def test_sqlite_3_51_3_is_accepted(root: Path) -> None:
    Ledger.open(root, sqlite_version=(3, 51, 3)).close()

    assert rows(root, "PRAGMA user_version") == [(SCHEMA_VERSION,)]


@pytest.mark.skipif(os.geteuid() == 0, reason="root writes through any permission")
def test_an_unwritable_crew_directory_is_refused(root: Path) -> None:
    (root / ".crew").chmod(0o500)
    try:
        with pytest.raises(
            LedgerError, match=r"cannot create the ledger in .*typesafe: Permission denied"
        ):
            Ledger.open(root)
    finally:
        (root / ".crew").chmod(0o700)


def test_opening_a_fresh_ledger_waits_for_another_connection_to_leave(root: Path) -> None:
    directory = root / ".crew/typesafe"
    directory.mkdir()
    other = sqlite3.connect(directory / "ledger.sqlite", autocommit=True, check_same_thread=False)
    # A write lock held elsewhere makes the switch to WAL fail at once, past the busy timeout.
    other.execute("BEGIN IMMEDIATE")
    leave = threading.Timer(0.1, lambda: other.execute("ROLLBACK"))
    leave.start()

    try:
        Ledger.open(root).close()
    finally:
        leave.join()
        other.close()

    assert rows(root, "PRAGMA journal_mode") == [("wal",)]
    assert rows(root, "PRAGMA user_version") == [(SCHEMA_VERSION,)]


def test_a_file_that_is_not_a_ledger_is_refused(root: Path) -> None:
    directory = root / ".crew/typesafe"
    directory.mkdir()
    (directory / "ledger.sqlite").write_bytes(b"not a database, but long enough to be read" * 4)

    with pytest.raises(LedgerError, match=r"cannot open the ledger .*ledger\.sqlite"):
        Ledger.open(root)


def test_reopening_a_ledger_keeps_its_records(root: Path, ledger: Ledger) -> None:
    question = recorded(ledger)
    first = record_answer(ledger, question, STATE)
    ledger.close()

    reopened = Ledger.open(root)
    try:
        with reopened.read() as reads:
            answer = reads.first_answer(replay_key(question.content_key, state_hash(STATE)))
    finally:
        reopened.close()

    assert answer is not None
    assert answer.call_id == first


def test_loading_one_bank_twice_records_each_version_and_stage_once(
    root: Path, ledger: Ledger
) -> None:
    loaded = bank(stage="confirm")

    assert ledger.record_bank(loaded) is True
    assert ledger.record_bank(loaded) is False
    assert ledger.record_bank(bank(stage="confirm")) is False

    question = loaded["issue_needs_candidate"]
    versions = rows(
        root,
        "SELECT name, version_id, content_key, model, primitive, definition FROM question_versions",
    )
    assert len(versions) == 1
    name, version, content, model, primitive, definition = versions[0]
    assert (name, version, content, model, primitive) == (
        "issue_needs_candidate",
        question.version_id,
        question.content_key,
        "jev-1.13.0",
        "noul",
    )
    assert isinstance(definition, str)
    assert json.loads(definition) == question.spec.model_dump(mode="json")
    assert rows(root, "SELECT question, version, stage FROM stage_observations") == [
        ("issue_needs_candidate", question.version_id, "confirm")
    ]


def test_a_stage_only_change_records_one_observation_and_no_version(
    root: Path, ledger: Ledger
) -> None:
    ledger.record_bank(bank(stage="shadow"))
    ledger.record_bank(bank(stage="act"))
    ledger.record_bank(bank(stage="act"))

    assert rows(root, "SELECT count(*) FROM question_versions") == [(1,)]
    assert rows(root, "SELECT stage FROM stage_observations ORDER BY id") == [("shadow",), ("act",)]


def test_the_bank_object_last_recorded_is_not_read_again(root: Path, ledger: Ledger) -> None:
    loaded = bank(stage="act")
    version = loaded["issue_needs_candidate"].version_id
    ledger.record_bank(loaded)
    con = raw(root)
    try:
        con.execute(
            "INSERT INTO stage_observations (question, version, stage) VALUES (?, ?, 'shadow')",
            ("issue_needs_candidate", version),
        )
    finally:
        con.close()

    again = ledger.record_bank(loaded)
    reloaded = ledger.record_bank(bank(stage="act"))

    assert (again, reloaded) == (False, True)
    assert rows(root, "SELECT stage FROM stage_observations ORDER BY id") == [
        ("act",),
        ("shadow",),
        ("act",),
    ]


def test_an_edited_question_records_a_new_version_after_the_first(
    root: Path, ledger: Ledger
) -> None:
    first = bank()["issue_needs_candidate"]
    edited = bank(instructions="Does `issue` need `candidate` merged first?")[
        "issue_needs_candidate"
    ]

    ledger.record_bank(bank())
    ledger.record_bank(bank(instructions="Does `issue` need `candidate` merged first?"))
    ledger.record_bank(bank())

    assert rows(root, "SELECT version_id FROM question_versions ORDER BY id") == [
        (first.version_id,),
        (edited.version_id,),
    ]
    assert rows(root, "SELECT version, stage FROM stage_observations ORDER BY id") == [
        (first.version_id, "act"),
        (edited.version_id, "act"),
    ]


def test_a_state_is_stored_once_per_hash(root: Path, ledger: Ledger) -> None:
    with ledger.write() as writes:
        first = writes.store_state({"b": 1, "a": "x"})
        second = writes.store_state({"a": "x", "b": 1})

    assert first == second == state_hash({"a": "x", "b": 1})
    assert rows(root, "SELECT hash, canonical FROM states") == [(first, '{"a":"x","b":1}')]


def test_the_first_call_for_a_replay_key_is_the_one_served(ledger: Ledger) -> None:
    question = recorded(ledger)
    first = record_answer(ledger, question, STATE, noul=0.9)
    record_answer(ledger, question, STATE, noul=0.8)

    with ledger.read() as reads:
        answer = reads.first_answer(replay_key(question.content_key, state_hash(STATE)))
        other = reads.first_answer(replay_key(question.content_key, state_hash({"other": 1})))

    assert answer is not None
    assert answer.call_id == first
    assert (answer.question, answer.version) == (question.name, question.version_id)
    assert (answer.model_asked, answer.model) == ("jev-1.13.0", "jev-1.13.0")
    assert answer.answer == {"type": "noul", "noul": 0.9}
    assert other is None


def test_a_call_keeps_its_raw_response_usage_request_id_and_attempts(
    root: Path, ledger: Ledger
) -> None:
    record_answer(ledger, recorded(ledger), STATE)

    assert rows(
        root,
        "SELECT state_hash, model_asked, model, response, usage, request_id, attempts FROM calls",
    ) == [
        (
            state_hash(STATE),
            "jev-1.13.0",
            "jev-1.13.0",
            RESPONSE,
            '{"input_tokens":10,"output_tokens":2}',
            "req_1",
            1,
        )
    ]


def test_a_cannot_judge_ask_records_its_reason_attempts_and_identifiers(
    root: Path, ledger: Ledger
) -> None:
    question = recorded(ledger)
    with ledger.write() as writes:
        digest = writes.store_state(STATE)
        writes.insert_ask(
            NewAsk(
                question=question.name,
                version=question.version_id,
                state_hash=digest,
                replay_key=replay_key(question.content_key, digest),
                effective_stage=question.stage.value,
                identifiers={"issue": 5, "repo": "crew"},
                fresh=True,
                reason="no key",
                attempts=0,
            )
        )

    assert rows(
        root,
        "SELECT identifiers, fresh, replayed, effective_stage, answer_id, reason, attempts"
        " FROM asks",
    ) == [('{"issue":5,"repo":"crew"}', 1, 0, "act", None, "no key", 0)]


@pytest.mark.parametrize(
    "served",
    [
        {},
        {"answer_id": 1, "reason": "timeout", "attempts": 3},
        {"reason": "timeout"},
        {"reason": "no key", "attempts": 0, "replayed": True},
    ],
)
def test_an_ask_is_either_answered_or_cannot_judge(
    root: Path, ledger: Ledger, served: dict[str, object]
) -> None:
    question = recorded(ledger)
    record_answer(ledger, question, STATE)
    digest = state_hash(STATE)
    ask = NewAsk(
        question=question.name,
        version=question.version_id,
        state_hash=digest,
        replay_key=replay_key(question.content_key, digest),
        effective_stage=question.stage.value,
        **served,  # type: ignore[arg-type]
    )

    with pytest.raises(sqlite3.IntegrityError), ledger.write() as writes:
        writes.insert_ask(ask)

    assert rows(root, "SELECT count(*) FROM asks") == [(1,)]


def test_an_ask_of_an_unrecorded_version_is_refused(ledger: Ledger) -> None:
    question = bank()["issue_needs_candidate"]
    with ledger.write() as writes:
        digest = writes.store_state(STATE)
    ask = NewAsk(
        question=question.name,
        version=question.version_id,
        state_hash=digest,
        replay_key=replay_key(question.content_key, digest),
        effective_stage=question.stage.value,
        reason="no key",
        attempts=0,
    )

    with pytest.raises(sqlite3.IntegrityError, match="FOREIGN KEY"), ledger.write() as writes:
        writes.insert_ask(ask)


def store_then_fail(ledger: Ledger) -> None:
    with ledger.write() as writes:
        writes.store_state(STATE)
        raise RuntimeError("stop")  # noqa: EM101 - a test's own failure


def test_a_failed_write_records_nothing_and_leaves_the_ledger_usable(
    root: Path, ledger: Ledger
) -> None:
    question = recorded(ledger)
    with pytest.raises(RuntimeError, match="stop"):
        store_then_fail(ledger)

    assert rows(root, "SELECT count(*) FROM states") == [(0,)]
    record_answer(ledger, question, STATE)
    assert rows(root, "SELECT count(*) FROM calls") == [(1,)]


def test_a_read_leaves_other_writers_free(root: Path, ledger: Ledger) -> None:
    question = recorded(ledger)
    record_answer(ledger, question, STATE)
    key = replay_key(question.content_key, state_hash(STATE))

    with ledger.read() as reads:
        before = reads.first_answer(key)
        con = raw(root)
        con.execute("PRAGMA busy_timeout = 0")
        con.execute("BEGIN IMMEDIATE")
        con.execute("INSERT INTO states (hash, canonical) VALUES ('h', '\"s\"')")
        con.execute("COMMIT")
        con.close()

    assert before is not None
    assert rows(root, "SELECT count(*) FROM states") == [(2,)]


def test_threads_writing_at_once_lose_no_records(root: Path, ledger: Ledger) -> None:
    question = recorded(ledger)
    threads = 8
    barrier = threading.Barrier(threads)
    calls: list[int] = []
    served: list[int] = []
    key = replay_key(question.content_key, state_hash(STATE))

    def ask() -> None:
        barrier.wait()
        calls.append(record_answer(ledger, question, STATE))
        with ledger.read() as reads:
            answer = reads.first_answer(key)
        assert answer is not None
        served.append(answer.call_id)

    workers = [threading.Thread(target=ask) for _ in range(threads)]
    for worker in workers:
        worker.start()
    for worker in workers:
        worker.join()

    assert sorted(calls) == list(range(1, threads + 1))
    assert served == [1] * threads
    assert rows(root, "SELECT count(*) FROM asks") == [(threads,)]


def test_a_closed_ledger_closes_connections_returned_later(root: Path) -> None:
    ledger = Ledger.open(root)
    key = replay_key("content", state_hash(STATE))
    with ledger.read() as reads:
        ledger.close()
        assert reads.first_answer(key) is None

    with pytest.raises(sqlite3.ProgrammingError):
        reads.first_answer(key)
