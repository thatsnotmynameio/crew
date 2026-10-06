"""Writers in separate processes on one ledger, as two judge instances on one root are.

The children are spawned, so each is a fresh interpreter. They import this module by
the name pytest gives it, ``tests.test_ledger_concurrency``, so the tests put the
directory above ``tests/`` on ``sys.path`` before spawning.
"""

from __future__ import annotations

import multiprocessing
import sqlite3
from pathlib import Path
from typing import TYPE_CHECKING

from typesafe_judge.keys import replay_key, state_hash
from typesafe_judge.ledger import Ledger, NewAnswer, NewAsk, NewCall
from typesafe_judge.schema import SCHEMA_VERSION

if TYPE_CHECKING:
    from multiprocessing.queues import Queue
    from multiprocessing.synchronize import Barrier

    import pytest

BANK = """\
questions:
  issue_needs_candidate:
    primitive: noul
    instructions: Does `issue` build on `candidate`?
    model: jev-1.13.0
    bands: {yes_at: 0.7, no_at: 0.3}
    default: no
    stage: act
    bar: {max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}
"""
STATE = {"issue": "Add a ledger", "candidate": "Add a bank"}
CONTEXT = multiprocessing.get_context("spawn")


def migrate(root: str, barrier: Barrier) -> None:
    barrier.wait()
    Ledger.open(Path(root)).close()


def ask(root: str, barrier: Barrier, question: tuple[str, str, str], results: Queue[int]) -> None:
    """Record one live call and its ask for STATE, then report the call replays serve."""
    name, version, content = question
    ledger = Ledger.open(Path(root))
    key = replay_key(content, state_hash(STATE))
    barrier.wait()
    with ledger.write() as writes:
        digest = writes.store_state(STATE)
        call_id = writes.insert_call(
            NewCall(digest, "jev-1.13.0", "jev-1.13.0", b"{}", {}, None, 1),
        )
        answer_id = writes.insert_answer(
            NewAnswer(call_id, name, version, content, key, {"type": "noul", "noul": 0.9})
        )
        writes.insert_ask(NewAsk(name, version, digest, key, "act", answer_id=answer_id))
    with ledger.read() as reads:
        served = reads.first_answer(key)
    ledger.close()
    results.put(-1 if served is None else served.call_id)


def run(processes: list[multiprocessing.process.BaseProcess]) -> None:
    for process in processes:
        process.start()
    for process in processes:
        process.join(timeout=10)
    assert [process.exitcode for process in processes] == [0] * len(processes)


def importable(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.syspath_prepend(str(Path(__file__).parents[1]))


def test_two_processes_migrating_a_fresh_ledger_migrate_it_once(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    importable(monkeypatch)
    (tmp_path / ".crew").mkdir()
    barrier = CONTEXT.Barrier(2)

    # Applying the schema twice would fail the second process: its tables already exist.
    run([CONTEXT.Process(target=migrate, args=(str(tmp_path), barrier)) for _ in range(2)])

    con = sqlite3.connect(tmp_path / ".crew/typesafe/ledger.sqlite", autocommit=True)
    try:
        assert con.execute("PRAGMA user_version").fetchone() == (SCHEMA_VERSION,)
        assert con.execute("PRAGMA journal_mode").fetchone() == ("wal",)
    finally:
        con.close()


def test_ae5_twelve_processes_asking_one_replay_key_all_record_and_serve_the_first_call(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    # The spawned children import this module and must not pay for pydantic.
    from typesafe_judge.bank import parse_bank  # noqa: PLC0415 - see above

    importable(monkeypatch)
    (tmp_path / ".crew").mkdir()
    bank = parse_bank(BANK, source="typesafe.yaml")
    question = bank["issue_needs_candidate"]
    ledger = Ledger.open(tmp_path)
    ledger.record_bank(bank)
    ledger.close()
    processes = 12
    barrier = CONTEXT.Barrier(processes)
    results: Queue[int] = CONTEXT.Queue()
    identity = (question.name, question.version_id, question.content_key)

    run(
        [
            CONTEXT.Process(target=ask, args=(str(tmp_path), barrier, identity, results))
            for _ in range(processes)
        ]
    )

    served = [results.get(timeout=1) for _ in range(processes)]
    con = sqlite3.connect(tmp_path / ".crew/typesafe/ledger.sqlite", autocommit=True)
    try:
        assert con.execute("SELECT count(*) FROM asks").fetchone() == (processes,)
        calls = [call_id for (call_id,) in con.execute("SELECT id FROM calls ORDER BY id")]
    finally:
        con.close()
    assert len(calls) == processes
    assert served == [calls[0]] * processes
