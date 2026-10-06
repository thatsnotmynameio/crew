from __future__ import annotations

from typing import TYPE_CHECKING, cast

import pytest

from typesafe_judge.asking import ask
from typesafe_judge.bank import Bank, load_bank, parse_bank
from typesafe_judge.calibrate import calibrate
from typesafe_judge.ledger import Ledger
from typesafe_judge.recheck import recheck
from typesafe_judge.records import Outcome, decide, record_outcome
from typesafe_judge.stages import stage_report

if TYPE_CHECKING:
    from collections.abc import Iterator
    from pathlib import Path

    from conftest import FakeTypeSafe

    from typesafe_judge.client import TypeSafe
    from typesafe_judge.keys import JSON

BANK = """\
questions:
  issue_needs_candidate:
    primitive: noul
    instructions: {instructions}
    model: jev-1.13.0
    bands: {{yes_at: 0.7, no_at: 0.3}}
    default: no
    stage: {stage}
    bar: {{max_error_yes: {max_error}, max_error_no: {max_error}, min_examples: 1,
          max_flip_rate: 0.5}}
"""
Q = "issue_needs_candidate"
A = "Does `issue` build on `candidate`?"
B = "Does `issue` need `candidate`?"


def bank(*, stage: str = "act", instructions: str = A, max_error: float = 1.0) -> Bank:
    text = BANK.format(stage=stage, instructions=instructions, max_error=max_error)
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


@pytest.fixture
def client(typesafe: FakeTypeSafe) -> Iterator[TypeSafe]:
    with typesafe.client() as opened:
        yield opened


def labelled(of: Bank, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe) -> list[int]:
    """Ask twenty states, half answered and labelled yes, half no; return the ask ids."""
    asks = []
    for index in range(20):
        yes = index % 2 == 0
        typesafe.answers[Q] = {"type": "noul", "noul": 0.9 if yes else 0.1}
        state = {"issue": f"Issue {index}", "candidate": "Add a bank"}
        ask_id = ask(of, ledger, client, [Q], state)[Q].ask_id
        record_outcome(ledger, Outcome(value=yes, source="test", strength="strong", ask_id=ask_id))
        asks.append(ask_id)
    return asks


def entry(of: Bank, ledger: Ledger) -> dict[str, JSON]:
    return cast("dict[str, JSON]", stage_report(of, ledger)[0])


def test_a_first_version_keeps_its_declared_stage_on_first_sight(ledger: Ledger) -> None:
    v1 = bank(stage="act")

    found = entry(v1, ledger)

    assert found == {
        "name": Q,
        "version": v1[Q].version_id,
        "declared_stage": "act",
        "effective_stage": "act",
        "declared_on_first_sight": True,
        "latest_recheck": None,
        "latest_calibration": None,
        "acted_in_shadow": 0,
        "next_stage": None,
    }


def test_an_edited_version_is_shadow_and_acting_on_its_answers_is_counted(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    v1 = bank()
    on_act = labelled(v1, ledger, client, typesafe)
    v2 = bank(instructions=B)
    state = {"issue": "Issue 99", "candidate": "Add a bank"}
    on_shadow = ask(v2, ledger, client, [Q], state)[Q].ask_id
    decide(ledger, on_shadow, "acted")
    decide(ledger, on_shadow, "acted")
    decide(ledger, on_shadow, "sent_to_person")
    decide(ledger, on_act[0], "acted")

    found = entry(v2, ledger)

    assert (found["declared_stage"], found["effective_stage"]) == ("act", "shadow")
    assert found["declared_on_first_sight"] is False
    assert found["acted_in_shadow"] == 2


def test_a_stage_raised_after_first_sight_is_no_longer_declared_on_first_sight(
    ledger: Ledger,
) -> None:
    entry(bank(stage="shadow"), ledger)

    found = entry(bank(stage="confirm"), ledger)

    assert (found["effective_stage"], found["declared_on_first_sight"]) == ("shadow", False)


def test_the_latest_recheck_and_calibration_of_the_current_version_are_listed(
    ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe
) -> None:
    v1 = bank()
    labelled(v1, ledger, client, typesafe)
    v2 = bank(instructions=B)
    typesafe.answers[Q] = {"type": "noul", "noul": 0.5}  # every rechecked verdict flips
    recheck(v2, ledger, client, Q)
    calibrate(v2, ledger, Q)

    found = entry(v2, ledger)

    assert found["latest_recheck"] == {
        "from_version": v1[Q].version_id,
        "stage": "act",
        "result": "failed",
    }
    # Every answer of the edited version sits at 0.5, in neither band.
    assert found["latest_calibration"] == {"stage": "act", "result": "insufficient"}
    assert found["next_stage"] is None


@pytest.mark.parametrize(
    ("stage", "effective", "next_stage"),
    [("shadow", "shadow", "confirm"), ("confirm", "confirm", "act"), ("act", "act", None)],
)
def test_a_passing_calibration_allows_the_stage_above_the_effective_one(
    root: Path,
    ledger: Ledger,
    client: TypeSafe,
    typesafe: FakeTypeSafe,
    stage: str,
    effective: str,
    next_stage: str | None,
) -> None:
    # An edited version, so its stage comes from the calibration, not from first sight.
    labelled(bank(stage="shadow"), ledger, client, typesafe)
    edited = bank(stage=stage, instructions=B)
    labelled(edited, ledger, client, typesafe)
    bank_file = root / ".crew/typesafe.yaml"
    bank_file.write_text(BANK.format(stage=stage, instructions=B, max_error=1.0), "utf-8")
    on_disk = load_bank(bank_file)
    before = bank_file.read_bytes(), bank_file.stat().st_mtime_ns

    calibrated = calibrate(on_disk, ledger, Q)
    found = entry(on_disk, ledger)

    assert calibrated.result == "passed"
    assert found["latest_calibration"] == {"stage": stage, "result": "passed"}
    assert (found["effective_stage"], found["next_stage"]) == (effective, next_stage)
    assert (bank_file.read_bytes(), bank_file.stat().st_mtime_ns) == before
