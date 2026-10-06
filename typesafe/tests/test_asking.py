import json
import logging
import sqlite3
import threading
from typing import TYPE_CHECKING

import pytest

from typesafe_judge.asking import AskResult, UnknownQuestionError, ask
from typesafe_judge.bank import Bank, Stage, load_bank, parse_bank
from typesafe_judge.keys import StateError
from typesafe_judge.ledger import Ledger

if TYPE_CHECKING:
    from collections.abc import Iterator
    from pathlib import Path

    from conftest import FakeTypeSafe

    from typesafe_judge.client import TypeSafe

BANK = """\
questions:
  issue_needs_candidate:
    primitive: noul
    instructions: {instructions}
    model: jev-1.13.0
    bands: {{yes_at: {yes_at}, no_at: 0.3}}
    default: no
    stage: {stage}
    bar: {{max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}}
  candidate_needs_issue:
    primitive: noul
    instructions: Does `candidate` build on `issue`?
    model: jev-1.13.0
    bands: {{yes_at: 0.7, no_at: 0.3}}
    default: no
    stage: shadow
    bar: {{max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}}
  older_model:
    primitive: noul
    instructions: Is `issue` a duplicate of `candidate`?
    model: jev-1.12.0
    bands: {{yes_at: 0.7, no_at: 0.3}}
    default: no
    stage: shadow
    bar: {{max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}}
  kind:
    primitive: choice
    instructions: Which kind of change is `issue`?
    criteria: {{bug: null, feature: null}}
    model: jev-1.13.0
    bands: {{floor: 0.5}}
    default: uncertain
    stage: shadow
    bar: {{max_error: 0.1, min_examples: 30, max_flip_rate: 0.05}}
  size:
    primitive: score
    instructions: How large is `issue`?
    criteria: [tiny, small, medium, large, huge]
    model: jev-1.13.0
    bands: {{floor: 0.3}}
    default: 1
    stage: shadow
    bar: {{max_error: 0.1, min_examples: 30, max_flip_rate: 0.05}}
"""
INSTRUCTIONS = "Does `issue` build on `candidate`?"
STATE = {"issue": "Add a ledger", "candidate": "Add a bank"}
OTHER_STATE = {"issue": "Add a recheck", "candidate": "Add a calibration"}
Q1 = "issue_needs_candidate"
Q2 = "candidate_needs_issue"


def bank(*, stage: str = "act", instructions: str = INSTRUCTIONS, yes_at: float = 0.7) -> Bank:
    return parse_bank(
        BANK.format(stage=stage, instructions=instructions, yes_at=yes_at), source="typesafe.yaml"
    )


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


def insert(root: Path, sql: str, parameters: tuple[object, ...]) -> None:
    """Record evidence the way the recheck and calibration units will."""
    con = sqlite3.connect(root / ".crew/typesafe/ledger.sqlite", autocommit=True)
    try:
        con.execute("PRAGMA foreign_keys = ON")
        con.execute(sql, parameters)
    finally:
        con.close()


def recheck(root: Path, from_bank: Bank, to_bank: Bank, stage: str, result: str = "passed") -> None:
    insert(
        root,
        "INSERT INTO rechecks (question, from_version, to_version, stage, result,"
        " compared, flipped, cannot_judge, bar, report)"
        " VALUES (?, ?, ?, ?, ?, 30, 0, 0, '{}', '{}')",
        (Q1, from_bank[Q1].version_id, to_bank[Q1].version_id, stage, result),
    )


def calibration(root: Path, of: Bank, stage: str, result: str = "passed") -> None:
    insert(
        root,
        "INSERT INTO calibrations (question, version, stage, seed, strong_only, result, report)"
        " VALUES (?, ?, ?, 'seed', 0, ?, '{}')",
        (Q1, of[Q1].version_id, stage, result),
    )


def test_ae1_a_repeated_ask_replays_the_first_answer_without_a_request(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    typesafe.answers[Q1] = {"type": "noul", "noul": 0.82}
    first = ask(bank(), ledger, client, [Q1], STATE)[Q1]

    again = ask(bank(), ledger, client, [Q1], STATE)[Q1]

    assert len(typesafe.requests()) == 1
    assert first.status == again.status == "answered"
    assert (first.replayed, again.replayed) == (False, True)
    assert again.verdict == "yes"
    assert again.probabilities == 0.82
    assert again.model == again.model_asked == "jev-1.13.0"
    assert again.ask_id != first.ask_id
    asks = rows(root, "SELECT id, answer_id, replayed FROM asks ORDER BY id")
    assert [ask_id for ask_id, _, _ in asks] == [first.ask_id, again.ask_id]
    assert asks[0][1] == asks[1][1]
    assert rows(root, "SELECT count(*) FROM calls") == [(1,)]


def test_ae8_only_the_unrecorded_question_goes_to_typesafe(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    ask(bank(), ledger, client, [Q1], STATE)

    answers = ask(bank(), ledger, client, [Q1, Q2], STATE)

    assert typesafe.sent() == [("jev-1.13.0", [Q1]), ("jev-1.13.0", [Q2])]
    assert list(answers) == [Q1, Q2]
    assert answers[Q1].replayed
    assert not answers[Q2].replayed
    assert {a.status for a in answers.values()} == {"answered"}
    assert rows(root, "SELECT count(*) FROM asks") == [(3,)]


def test_questions_pinned_to_different_models_go_in_one_request_each(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    answers = ask(bank(), ledger, client, [Q1, "older_model", Q2], STATE)

    assert sorted(typesafe.sent()) == [("jev-1.12.0", ["older_model"]), ("jev-1.13.0", [Q1, Q2])]
    assert answers["older_model"].model_asked == "jev-1.12.0"


def test_a_fresh_ask_records_a_new_call_and_later_plain_asks_still_replay_the_first(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    typesafe.answers[Q1] = {"type": "noul", "noul": 0.9}
    ask(bank(), ledger, client, [Q1], STATE)
    typesafe.answers[Q1] = {"type": "noul", "noul": 0.2}

    fresh = ask(bank(), ledger, client, [Q1], STATE, fresh=True)[Q1]
    plain = ask(bank(), ledger, client, [Q1], STATE)[Q1]

    assert (fresh.probabilities, fresh.verdict, fresh.replayed) == (0.2, "no", False)
    assert (plain.probabilities, plain.verdict, plain.replayed) == (0.9, "yes", True)
    assert rows(root, "SELECT count(*) FROM calls") == [(2,)]
    assert rows(root, "SELECT fresh FROM asks ORDER BY id") == [(0,), (1,), (0,)]


def test_ae3_without_a_key_recorded_inputs_replay_and_new_ones_cannot_judge(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    ask(bank(), ledger, client, [Q1], STATE)
    keyless = typesafe.client(api_key=None)

    recorded = ask(bank(), ledger, keyless, [Q1], STATE)[Q1]
    never = ask(bank(), ledger, keyless, [Q1], OTHER_STATE)[Q1]

    assert len(typesafe.requests()) == 1
    assert (recorded.status, recorded.replayed, recorded.verdict) == ("answered", True, "yes")
    assert never.status == "cannot_judge"
    assert (never.reason, never.attempts, never.default) == ("no key", 0, "no")
    assert never.verdict is None
    assert never.probabilities is None
    assert never.recorded_answer_exists is False
    assert rows(
        root, "SELECT reason, attempts, answer_id FROM asks WHERE id = ?", (never.ask_id,)
    ) == [("no key", 0, None)]


def test_without_a_key_a_fresh_ask_of_a_recorded_input_cannot_judge(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    ask(bank(), ledger, client, [Q1], STATE)

    fresh = ask(bank(), ledger, typesafe.client(api_key=None), [Q1], STATE, fresh=True)[Q1]

    assert (fresh.status, fresh.reason, fresh.replayed) == ("cannot_judge", "no key", False)
    assert fresh.recorded_answer_exists is True


def test_a_failed_call_records_cannot_judge_with_its_reason_and_attempts(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    typesafe.faults = [401]

    answers = ask(bank(), ledger, client, [Q1, Q2], STATE)

    assert {(a.status, a.reason, a.attempts) for a in answers.values()} == {
        ("cannot_judge", "HTTP 401", 1)
    }
    assert rows(root, "SELECT count(*) FROM calls") == [(0,)]
    assert rows(root, "SELECT reason, attempts FROM asks") == [("HTTP 401", 1)] * 2


@pytest.mark.parametrize(
    ("noul", "verdict"),
    [(0.7, "yes"), (0.3, "no"), (0.69, "uncertain"), (0.31, "uncertain"), (0.95, "yes")],
)
def test_a_noul_is_yes_at_yes_at_and_no_at_no_at(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe, noul: float, verdict: str
) -> None:
    typesafe.answers[Q1] = {"type": "noul", "noul": noul}

    assert ask(bank(), ledger, client, [Q1], STATE)[Q1].verdict == verdict


def test_a_choice_below_its_floor_is_uncertain_and_at_it_is_the_option(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    # Confidence (p_max - 1/n) / (1 - 1/n): 0.2 below the 0.5 floor, then exactly 0.5.
    typesafe.answers["kind"] = {
        "type": "choice",
        "choice": "bug",
        "confidence": 0.2,
        "probabilities": {"bug": 0.6, "feature": 0.4},
    }
    below = ask(bank(), ledger, client, ["kind"], STATE)["kind"]
    typesafe.answers["kind"] = {
        "type": "choice",
        "choice": "feature",
        "confidence": 0.5,
        "probabilities": {"bug": 0.25, "feature": 0.75},
    }
    at = ask(bank(), ledger, client, ["kind"], OTHER_STATE)["kind"]

    assert below.verdict == "uncertain"
    assert below.probabilities == {"bug": 0.6, "feature": 0.4}
    assert at.verdict == "feature"


def test_a_score_verdict_is_its_most_likely_level_not_its_rounded_score(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    # Score 2.8 rounds to 3, whose probability is 0; the confidence (1/3) is measured
    # around level 2, the most likely one, and clears the 0.3 floor.
    typesafe.answers["size"] = {
        "type": "score",
        "score": 2.8,
        "confidence": 1 / 3,
        "legend": {"0": "tiny", "1": "small", "2": "medium", "3": "large", "4": "huge"},
        "probabilities": {"0": 0.0, "1": 0.0, "2": 0.6, "3": 0.0, "4": 0.4},
    }

    answer = ask(bank(), ledger, client, ["size"], STATE)["size"]

    assert answer.verdict == 2
    assert answer.probabilities == {"0": 0.0, "1": 0.0, "2": 0.6, "3": 0.0, "4": 0.4}


def test_a_score_below_its_floor_is_uncertain(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    typesafe.answers["size"] = {
        "type": "score",
        "score": 2.0,
        "confidence": 0.0,
        "legend": {"0": "tiny", "1": "small", "2": "medium", "3": "large", "4": "huge"},
        "probabilities": {"0": 0.2, "1": 0.2, "2": 0.2, "3": 0.2, "4": 0.2},
    }

    assert ask(bank(), ledger, client, ["size"], STATE)["size"].verdict == "uncertain"


def test_verdicts_are_read_from_the_current_bands(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    typesafe.answers[Q1] = {"type": "noul", "noul": 0.65}
    before = ask(bank(yes_at=0.7), ledger, client, [Q1], STATE)[Q1]

    after = ask(bank(yes_at=0.6), ledger, client, [Q1], STATE)[Q1]

    assert (before.verdict, after.verdict) == ("uncertain", "yes")
    assert after.replayed
    assert after.version != before.version
    assert len(typesafe.requests()) == 1


def test_ae4_an_edited_question_is_shadow_until_a_recheck_passes(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    path = root / ".crew/typesafe.yaml"
    path.write_text(BANK.format(stage="act", instructions=INSTRUCTIONS, yes_at=0.7))
    first = load_bank(path)
    assert ask(first, ledger, client, [Q1], STATE)[Q1].effective_stage is Stage.ACT
    edited_text = BANK.format(
        stage="act", instructions="Does `issue` need `candidate`?", yes_at=0.7
    )
    path.write_text(edited_text)
    edited = load_bank(path)

    before = ask(edited, ledger, client, [Q1], STATE)[Q1]
    recheck(root, first, edited, "act")
    after = ask(edited, ledger, client, [Q1], STATE)[Q1]

    assert (before.effective_stage, before.declared_stage) == (Stage.SHADOW, Stage.ACT)
    assert after.effective_stage is Stage.ACT
    assert path.read_text() == edited_text
    assert rows(root, "SELECT effective_stage FROM asks ORDER BY id") == [
        ("act",),
        ("shadow",),
        ("act",),
    ]


def test_a_question_edited_before_any_ask_is_shadow(ledger: Ledger, client: TypeSafe) -> None:
    ledger.record_bank(bank(stage="act"))

    edited = ask(bank(stage="act", instructions="Edited?"), ledger, client, [Q1], STATE)[Q1]

    assert edited.effective_stage is Stage.SHADOW


def test_a_failed_recheck_or_one_for_a_lower_stage_earns_nothing(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    first, edited = bank(stage="act"), bank(stage="act", instructions="Edited?")
    ledger.record_bank(first)
    ledger.record_bank(edited)
    recheck(root, first, edited, "act", result="failed")
    recheck(root, first, edited, "confirm")

    assert ask(edited, ledger, client, [Q1], STATE)[Q1].effective_stage is Stage.SHADOW


def test_a_recheck_from_a_shadow_version_cannot_lift_an_edit_to_act(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    first, edited = bank(stage="shadow"), bank(stage="act", instructions="Edited?")
    ledger.record_bank(first)
    ledger.record_bank(edited)
    recheck(root, first, edited, "act")

    assert ask(edited, ledger, client, [Q1], STATE)[Q1].effective_stage is Stage.SHADOW


def test_a_recheck_counts_only_from_the_latest_trusted_version(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    v1, v2, v3 = (bank(stage="act", instructions=f"Version {n}?") for n in (1, 2, 3))
    for each in (v1, v2, v3):
        ledger.record_bank(each)
    recheck(root, v1, v2, "act")
    recheck(root, v1, v3, "act")

    skipped = ask(v3, ledger, client, [Q1], STATE)[Q1].effective_stage
    recheck(root, v2, v3, "act")
    trusted = ask(v3, ledger, client, [Q1], STATE)[Q1].effective_stage

    assert (skipped, trusted) == (Stage.SHADOW, Stage.ACT)


def test_the_first_version_keeps_its_declared_stage_until_a_stage_only_raise(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    confirm = ask(bank(stage="confirm"), ledger, client, [Q1], STATE)[Q1]
    lowered = ask(bank(stage="shadow"), ledger, client, [Q1], STATE)[Q1]

    raised = ask(bank(stage="act"), ledger, client, [Q1], STATE)[Q1]
    calibration(root, bank(stage="act"), "confirm")
    calibration(root, bank(stage="act"), "act", result="failed")
    still = ask(bank(stage="act"), ledger, client, [Q1], STATE)[Q1]
    calibration(root, bank(stage="act"), "act")
    calibrated = ask(bank(stage="act"), ledger, client, [Q1], STATE)[Q1]

    assert confirm.effective_stage is Stage.CONFIRM
    assert lowered.effective_stage is Stage.SHADOW
    assert (raised.effective_stage, raised.declared_stage) == (Stage.SHADOW, Stage.ACT)
    assert still.effective_stage is Stage.SHADOW
    assert calibrated.effective_stage is Stage.ACT
    assert confirm.version == raised.version


def test_a_live_ask_writes_no_state_or_body_to_stderr_with_debug_inherited(
    ledger: Ledger, typesafe: FakeTypeSafe, capsys: pytest.CaptureFixture[str]
) -> None:
    sdk = logging.getLogger("typesafe_sdk")
    level = sdk.level
    handler = logging.StreamHandler()
    # What the SDK does on import when TYPESAFE_LOG_LEVEL=debug is inherited.
    sdk.setLevel(logging.DEBUG)
    sdk.addHandler(handler)
    try:
        with typesafe.client() as client:
            ask(bank(), ledger, client, [Q1], {"issue": "SECRET-STATE-TEXT"})
    finally:
        sdk.removeHandler(handler)
        sdk.setLevel(level)

    err = capsys.readouterr().err
    assert "POST" in err  # the SDK's INFO line still goes out
    assert "SECRET-STATE-TEXT" not in err
    assert "instructions" not in err


def test_two_plain_asks_racing_on_one_key_both_get_the_lowest_call(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    barrier = threading.Barrier(2, timeout=10)
    typesafe.before_answer = barrier.wait
    results: list[AskResult] = []

    def racer() -> None:
        results.append(ask(bank(), ledger, client, [Q1], STATE)[Q1])

    threads = [threading.Thread(target=racer) for _ in range(2)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()

    assert len(results) == 2
    assert {r.status for r in results} == {"answered"}
    calls = rows(root, "SELECT id FROM calls ORDER BY id")
    assert len(calls) == 2
    served = rows(
        root, "SELECT DISTINCT a.call_id FROM asks k JOIN answers a ON a.id = k.answer_id"
    )
    assert served == [calls[0]]


def test_identifiers_are_stored_on_the_ask_and_leave_the_replay_key_alone(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    ask(bank(), ledger, client, [Q1], STATE, {"issue": 5, "repo": "crew"})

    again = ask(bank(), ledger, client, [Q1], STATE, {"issue": "5"})[Q1]

    assert again.replayed
    assert len(typesafe.requests()) == 1
    assert rows(root, "SELECT identifiers FROM asks ORDER BY id") == [
        ('{"issue":5,"repo":"crew"}',),
        ('{"issue":"5"}',),
    ]
    assert len(set(rows(root, "SELECT replay_key FROM asks"))) == 1


def test_an_unknown_name_fails_the_whole_request_before_any_call(
    root: Path, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    with pytest.raises(UnknownQuestionError, match="nope, nada") as error:
        ask(bank(), ledger, client, [Q1, "nope", "nada"], STATE)

    assert error.value.names == ["nope", "nada"]
    assert typesafe.requests() == []
    assert rows(root, "SELECT count(*) FROM asks") == [(0,)]


@pytest.mark.parametrize(
    ("state", "identifiers"),
    [({"issue": 1.0}, {}), (STATE, {"issue": 1.5}), (STATE, {"nested": {"a": 1}})],
)
def test_a_state_or_identifiers_the_judge_cannot_hash_fail_before_any_call(
    root: Path,
    ledger: Ledger,
    client: TypeSafe,
    typesafe: FakeTypeSafe,
    state: object,
    identifiers: dict[str, str | int],
) -> None:
    with pytest.raises(StateError):
        ask(bank(), ledger, client, [Q1], state, identifiers)

    assert typesafe.requests() == []
    assert rows(root, "SELECT count(*) FROM asks") == [(0,)]


def test_results_serialize_to_the_ask_exchange_shape(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    typesafe.answers[Q1] = {"type": "noul", "noul": 0.82}
    answered = ask(bank(), ledger, client, [Q1], STATE)[Q1]
    cannot = ask(bank(), ledger, typesafe.client(api_key=None), [Q1], OTHER_STATE)[Q1]

    assert json.loads(json.dumps(answered.to_json())) == {
        "ask_id": answered.ask_id,
        "version": bank()[Q1].version_id,
        "status": "answered",
        "replayed": False,
        "verdict": "yes",
        "probabilities": 0.82,
        "effective_stage": "act",
        "declared_stage": "act",
        "model_asked": "jev-1.13.0",
        "model": "jev-1.13.0",
    }
    assert cannot.to_json() == {
        "ask_id": cannot.ask_id,
        "version": bank()[Q1].version_id,
        "status": "cannot_judge",
        "replayed": False,
        "verdict": None,
        "probabilities": None,
        "effective_stage": "act",
        "declared_stage": "act",
        "model_asked": "jev-1.13.0",
        "model": None,
        "reason": "no key",
        "attempts": 0,
        "default": "no",
        "recorded_answer_exists": False,
    }
