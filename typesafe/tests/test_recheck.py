import json
import sqlite3
from typing import TYPE_CHECKING, cast

import pytest

from typesafe_judge.asking import UnknownQuestionError, ask
from typesafe_judge.bank import Bank, Stage, parse_bank
from typesafe_judge.evidence import NewCalibration
from typesafe_judge.ledger import Ledger
from typesafe_judge.recheck import RECHECK_MARKER, recheck
from typesafe_judge.records import InvalidRequestError, decide

if TYPE_CHECKING:
    from collections.abc import Callable, Iterator
    from pathlib import Path

    from conftest import FakeTypeSafe

    from typesafe_judge.client import TypeSafe
    from typesafe_judge.evidence import StageName
    from typesafe_judge.recheck import Recheck

BANK = """\
questions:
  issue_needs_candidate:
    primitive: noul
    instructions: {instructions}
    model: jev-1.13.0
    bands: {{yes_at: {yes_at}, no_at: 0.3}}
    default: no
    stage: {stage}
    bar: {{max_error_yes: 0.1, max_error_no: 0.1, min_examples: {min_examples},
          max_flip_rate: {max_flip_rate}}}
  kind:
    primitive: choice
    instructions: {kind_instructions}
    criteria: {{bug: null, feature: null}}
    model: jev-1.13.0
    bands: {{floor: 0.5}}
    default: uncertain
    stage: act
    bar: {{max_error: 0.1, min_examples: 2, max_flip_rate: 0.5}}
"""
Q1 = "issue_needs_candidate"
A = "Does `issue` build on `candidate`?"
B = "Does `issue` need `candidate`?"


def bank(
    *,
    instructions: str = A,
    yes_at: float = 0.7,
    stage: str = "act",
    min_examples: int = 5,
    max_flip_rate: float = 0.2,
    kind_instructions: str = "Which kind of change is `issue`?",
) -> Bank:
    text = BANK.format(
        instructions=instructions,
        yes_at=yes_at,
        stage=stage,
        min_examples=min_examples,
        max_flip_rate=max_flip_rate,
        kind_instructions=kind_instructions,
    )
    return parse_bank(text, source="typesafe.yaml")


def state(index: int) -> dict[str, str]:
    return {"issue": f"Issue {index}", "candidate": "Add a bank"}


@pytest.fixture
def root(tmp_path: Path) -> Path:
    (tmp_path / ".crew").mkdir()
    return tmp_path


@pytest.fixture
def ledger(root: Path) -> Iterator[Ledger]:
    opened = Ledger.open(root)
    yield opened
    opened.close()


@pytest.fixture
def client(typesafe: FakeTypeSafe) -> Iterator[TypeSafe]:
    with typesafe.client() as opened:
        yield opened


def rows(root: Path, sql: str, parameters: tuple[object, ...] = ()) -> list[tuple[object, ...]]:
    """Query the ledger file directly, outside the code under test."""
    con = sqlite3.connect(root / ".crew/typesafe/ledger.sqlite", autocommit=True)
    try:
        return con.execute(sql, parameters).fetchall()
    finally:
        con.close()


def script(typesafe: FakeTypeSafe, noul: Callable[[str, int], float]) -> None:
    """Answer the noul with noul(instructions, state index) on every request."""

    def before_answer() -> None:
        body = typesafe.requests()[-1]
        questions = cast("dict[str, dict[str, str]]", body["questions"])
        if Q1 in questions:
            issue = cast("dict[str, str]", body["state"])["issue"]
            p = noul(questions[Q1]["instructions"], int(issue.removeprefix("Issue ")))
            typesafe.answers[Q1] = {"type": "noul", "noul": p}

    typesafe.before_answer = before_answer


def seed(of: Bank, ledger: Ledger, client: TypeSafe, count: int) -> list[int]:
    """Ask the noul over count states; return the ask ids."""
    return [ask(of, ledger, client, [Q1], state(i))[Q1].ask_id for i in range(count)]


def stage_of(of: Bank, ledger: Ledger, client: TypeSafe) -> Stage:
    return ask(of, ledger, client, [Q1], state(0))[Q1].effective_stage


def calibration(ledger: Ledger, of: Bank, stage: StageName) -> None:
    """Record a passing calibration of Q1's version in a bank, for stage."""
    with ledger.write() as writes:
        writes.insert_calibration(
            NewCalibration(
                question=Q1,
                version=of[Q1].version_id,
                stage=stage,
                seed="seed",
                split_key=None,
                strong_only=False,
                result="passed",
                proposed_bands=None,
                report={},
            )
        )


def flips(result: Recheck) -> list[tuple[str, object, object]]:
    return [(flip.state_hash, flip.before, flip.after) for flip in result.flips]


def test_ae2_a_yes_band_moved_from_70_to_60_lists_the_flips_without_a_typesafe_request(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    script(typesafe, lambda _, i: (50 + i) / 100)
    v1 = bank(yes_at=0.7, min_examples=30, max_flip_rate=0.05)
    asks = seed(v1, ledger, client, 50)
    decide(ledger, asks[12], "sent_to_person")
    decide(ledger, asks[12], "acted")
    requests = len(typesafe.requests())
    v2 = bank(yes_at=0.6, min_examples=30, max_flip_rate=0.05)

    result = recheck(v2, ledger, client, Q1)

    assert len(typesafe.requests()) == requests
    assert (result.from_version, result.to_version) == (v1[Q1].version_id, v2[Q1].version_id)
    assert (result.compared, result.flipped, result.cannot_judge) == (50, 10, 0)
    assert result.flips_by_kind == {"uncertain->yes": 10}
    # The states answered 0.60 to 0.69: uncertain under 0.70, yes under 0.60.
    assert [flip.asks for flip in result.flips] == [[asks[i]] for i in range(10, 20)]
    assert [flip.decisions for flip in result.flips if flip.decisions] == [[(asks[12], "acted")]]
    assert {(f.before, f.after) for f in result.flips} == {("uncertain", "yes")}
    assert result.result == "failed"
    assert rows(root, "SELECT count(*) FROM asks") == [(50,)]
    report = rows(root, "SELECT result, compared, flipped, cannot_judge, report FROM rechecks")
    assert report[0][:4] == ("failed", 50, 10, 0)
    assert json.loads(cast("str", report[0][4]))["flips_by_kind"] == {"uncertain->yes": 10}


def test_an_instructions_change_asks_live_once_per_distinct_earlier_state(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    script(typesafe, lambda text, i: 0.5 if text == B and i == 0 else 0.8)
    v1 = bank()
    seed(v1, ledger, client, 5)
    seed(v1, ledger, client, 2)  # replays: still five distinct states
    requests = len(typesafe.requests())
    v2 = bank(instructions=B)

    result = recheck(v2, ledger, client, Q1)

    sent = typesafe.requests()[requests:]
    assert len(sent) == 5
    assert {
        cast("dict[str, dict[str, str]]", b["questions"])[Q1]["instructions"] for b in sent
    } == {B}
    assert rows(
        root,
        "SELECT count(*), count(DISTINCT c.state_hash) FROM answers a"
        " JOIN calls c ON c.id = a.call_id WHERE a.version = ?",
        (v2[Q1].version_id,),
    ) == [(5, 5)]
    marker = json.dumps({RECHECK_MARKER: v1[Q1].version_id}, separators=(",", ":"))
    assert rows(
        root,
        "SELECT count(*) FROM asks WHERE version = ? AND identifiers = ?",
        (v2[Q1].version_id, marker),
    ) == [(5,)]
    assert (result.compared, result.flipped, result.flip_rate) == (5, 1, 0.2)
    assert result.flips_by_kind == {"yes->uncertain": 1}
    assert rows(root, "SELECT stage, result, compared, flipped FROM rechecks") == [
        ("act", "passed", 5, 1)
    ]
    report = json.loads(cast("str", rows(root, "SELECT report FROM rechecks")[0][0]))
    assert report["flip_rate"] == 0.2


def test_a_recheck_within_the_flip_rate_gives_the_version_its_declared_stage(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    script(typesafe, lambda text, i: 0.5 if text == B and i == 0 else 0.8)
    seed(bank(), ledger, client, 5)
    v2 = bank(instructions=B)
    before = stage_of(v2, ledger, client)

    result = recheck(v2, ledger, client, Q1)

    assert (before, result.result, result.effective_stage) == (Stage.SHADOW, "passed", Stage.ACT)
    assert stage_of(v2, ledger, client) is Stage.ACT


def test_a_recheck_beyond_the_flip_rate_fails_and_the_version_stays_shadow(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    script(typesafe, lambda text, i: 0.1 if text == B and i < 2 else 0.8)
    seed(bank(), ledger, client, 5)
    v2 = bank(instructions=B)

    result = recheck(v2, ledger, client, Q1)

    assert (result.flipped, result.result) == (2, "failed")
    assert result.flips_by_kind == {"yes->no": 2}
    assert result.effective_stage is Stage.SHADOW
    assert stage_of(v2, ledger, client) is Stage.SHADOW


def test_a_recheck_over_fewer_states_than_min_examples_is_insufficient(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    seed(bank(), ledger, client, 4)
    v2 = bank(instructions=B)

    result = recheck(v2, ledger, client, Q1)

    assert (result.compared, result.flipped, result.result) == (4, 0, "insufficient")
    assert stage_of(v2, ledger, client) is Stage.SHADOW
    assert rows(root, "SELECT result, compared FROM rechecks") == [("insufficient", 4)]


def test_a_recheck_with_a_state_left_cannot_judge_is_incomplete_and_a_rerun_asks_only_it(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    seed(bank(), ledger, client, 5)
    v2 = bank(instructions=B)
    requests = len(typesafe.requests())
    typesafe.faults = [400]  # the first live ask of the recheck fails, without retries

    incomplete = recheck(v2, ledger, client, Q1)
    rerun = recheck(v2, ledger, client, Q1)

    assert (incomplete.result, incomplete.compared, incomplete.cannot_judge) == ("incomplete", 4, 1)
    assert incomplete.effective_stage is Stage.SHADOW
    assert (rerun.result, rerun.compared, rerun.cannot_judge) == ("passed", 5, 0)
    assert len(typesafe.requests()) == requests + 5 + 1
    assert rows(root, "SELECT result, cannot_judge FROM rechecks ORDER BY id") == [
        ("incomplete", 1),
        ("passed", 0),
    ]


def test_a_recheck_stopped_midway_and_rerun_replays_the_calls_it_made(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    seed(bank(), ledger, client, 5)
    v2 = bank(instructions=B)
    requests = len(typesafe.requests())

    def stop_on_the_third() -> None:
        if len(typesafe.requests()) == requests + 3:
            msg = "stopped"
            raise RuntimeError(msg)

    typesafe.before_answer = stop_on_the_third
    with pytest.raises(RuntimeError, match="stopped"):
        recheck(v2, ledger, client, Q1)
    typesafe.before_answer = None
    paid = rows(root, "SELECT count(*) FROM calls")
    result = recheck(v2, ledger, client, Q1)

    assert paid == [(5 + 2,)]
    assert rows(root, "SELECT count(*) FROM rechecks") == [(1,)]
    # Three attempts before the stop, then only the three states not yet answered.
    assert len(typesafe.requests()) == requests + 3 + 3
    assert rows(root, "SELECT count(*) FROM calls") == [(5 + 5,)]
    assert (result.result, result.compared) == ("passed", 5)


def test_a_version_that_raises_max_flip_rate_is_judged_by_the_trusted_versions_bar(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    script(typesafe, lambda text, i: 0.5 if text == B and i == 0 else 0.8)
    seed(bank(max_flip_rate=0.05), ledger, client, 5)
    loosened = bank(instructions=B, max_flip_rate=0.5)

    result = recheck(loosened, ledger, client, Q1)

    assert (result.flip_rate, result.result) == (0.2, "failed")
    assert result.bar == {"min_examples": 5, "max_flip_rate": 0.05}
    assert json.loads(cast("str", rows(root, "SELECT bar FROM rechecks")[0][0])) == result.bar
    assert stage_of(loosened, ledger, client) is Stage.SHADOW


def test_a_band_only_edit_after_a_failed_one_stays_shadow_until_rechecked_from_the_first(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    script(typesafe, lambda text, _: 0.65 if text == B else 0.8)
    v1 = bank()
    seed(v1, ledger, client, 5)
    v2 = bank(instructions=B)
    failed = recheck(v2, ledger, client, Q1)
    requests = len(typesafe.requests())
    v3 = bank(instructions=B, yes_at=0.6)
    before = stage_of(v3, ledger, client)

    passed = recheck(v3, ledger, client, Q1)

    assert (failed.result, failed.flips_by_kind) == ("failed", {"yes->uncertain": 5})
    assert before is Stage.SHADOW
    assert passed.from_version == v1[Q1].version_id
    assert (passed.result, passed.flipped, passed.effective_stage) == ("passed", 0, Stage.ACT)
    # Version 3 shares version 2's content, whose answers the first recheck paid for.
    assert len(typesafe.requests()) == requests
    assert stage_of(v3, ledger, client) is Stage.ACT


def test_a_recheck_from_a_shadow_version_cannot_make_an_edit_act(
    ledger: Ledger, client: TypeSafe
) -> None:
    v1 = bank(stage="shadow")
    seed(v1, ledger, client, 5)
    v2 = bank(instructions=B, stage="act")

    with pytest.raises(InvalidRequestError, match="no earlier version") as refused:
        recheck(v2, ledger, client, Q1)
    named = recheck(v2, ledger, client, Q1, from_version=v1[Q1].version_id)

    assert refused.value.code == "no_trusted_version"
    assert (named.result, named.stage, named.effective_stage) == ("passed", Stage.ACT, Stage.SHADOW)
    assert stage_of(v2, ledger, client) is Stage.SHADOW


def test_a_stage_only_raise_of_the_first_version_needs_evidence_before_it_can_be_trusted(
    ledger: Ledger, client: TypeSafe
) -> None:
    seed(bank(stage="shadow"), ledger, client, 5)
    raised = bank(stage="confirm")
    v2 = bank(instructions=B, stage="confirm")
    raised_stage = stage_of(raised, ledger, client)

    with pytest.raises(InvalidRequestError, match="no earlier version"):
        recheck(v2, ledger, client, Q1)
    calibration(ledger, raised, "confirm")
    result = recheck(v2, ledger, client, Q1)

    assert raised_stage is Stage.SHADOW
    assert (result.result, result.effective_stage) == ("passed", Stage.CONFIRM)
    assert stage_of(v2, ledger, client) is Stage.CONFIRM


def test_a_stage_only_raise_after_a_recheck_needs_a_recheck_for_the_new_stage(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    seed(bank(stage="act"), ledger, client, 5)
    confirm = bank(instructions=B, stage="confirm")
    at_confirm = recheck(confirm, ledger, client, Q1)
    act = bank(instructions=B, stage="act")
    raised = stage_of(act, ledger, client)
    requests = len(typesafe.requests())

    at_act = recheck(act, ledger, client, Q1)

    assert (at_confirm.stage, at_confirm.effective_stage) == (Stage.CONFIRM, Stage.CONFIRM)
    assert raised is Stage.SHADOW
    assert (at_act.stage, at_act.result, at_act.effective_stage) == (
        Stage.ACT,
        "passed",
        Stage.ACT,
    )
    assert len(typesafe.requests()) == requests
    assert stage_of(act, ledger, client) is Stage.ACT


def test_a_choice_recheck_reports_flips_between_options(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    kind = "kind"
    for i in range(2):
        ask(bank(), ledger, client, [kind], state(i))
    edited = bank(kind_instructions="Is `issue` a bug or a feature?")
    typesafe.answers[kind] = {
        "type": "choice",
        "choice": "feature",
        "confidence": 0.8,
        "probabilities": {"bug": 0.1, "feature": 0.9},
    }

    result = recheck(edited, ledger, client, kind)

    assert result.flips_by_kind == {"bug->feature": 2}
    assert result.result == "failed"
    assert result.to_json()["flips_by_kind"] == {"bug->feature": 2}


def test_a_recheck_refuses_an_unknown_question_or_a_version_not_earlier(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    v1 = bank()
    seed(v1, ledger, client, 1)
    v2 = bank(instructions=B)
    ledger.record_bank(v2)

    with pytest.raises(UnknownQuestionError):
        recheck(v2, ledger, client, "unknown")
    for version in (v2[Q1].version_id, "unknown"):
        with pytest.raises(InvalidRequestError, match="from_version") as refused:
            recheck(v2, ledger, client, Q1, from_version=version)
        assert refused.value.code == "unknown_version"
    with pytest.raises(InvalidRequestError, match="no earlier version"):
        recheck(v1, ledger, client, Q1)
    assert rows(root, "SELECT count(*) FROM rechecks") == [(0,)]


def test_a_recheck_serializes_its_report(ledger: Ledger, client: TypeSafe) -> None:
    asks = seed(bank(), ledger, client, 5)
    decide(ledger, asks[0], "acted")
    result = recheck(bank(yes_at=0.95), ledger, client, Q1)

    body = result.to_json()

    assert (body["result"], body["flipped"]) == ("failed", 5)
    assert body["flips"] == [
        {
            "state_hash": flip.state_hash,
            "from": "yes",
            "to": "uncertain",
            "kind": "yes->uncertain",
            "asks": [asks[i]],
            "decisions": [{"ask_id": asks[0], "decision": "acted"}] if i == 0 else [],
        }
        for i, flip in enumerate(result.flips)
    ]
    assert {k: body[k] for k in ("id", "question", "stage", "effective_stage", "flip_rate")} == {
        "id": result.id,
        "question": Q1,
        "stage": "act",
        "effective_stage": "shadow",
        "flip_rate": 1.0,
    }
