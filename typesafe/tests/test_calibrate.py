from __future__ import annotations

import json
import math
import sqlite3
from typing import TYPE_CHECKING, cast

import pytest

from typesafe_judge.asking import UnknownQuestionError, ask, effective_stage
from typesafe_judge.bank import Bank, Stage, load_bank, parse_bank
from typesafe_judge.calibrate import HELD_OUT, calibrate, parse_calibration, upper_bound
from typesafe_judge.keys import state_hash
from typesafe_judge.ledger import Ledger
from typesafe_judge.recheck import recheck
from typesafe_judge.records import InvalidRequestError, Outcome, record_outcome

if TYPE_CHECKING:
    from collections.abc import Iterator
    from pathlib import Path

    from conftest import FakeTypeSafe

    from typesafe_judge.calibrate import Calibration
    from typesafe_judge.client import TypeSafe
    from typesafe_judge.keys import JSON
    from typesafe_judge.records import Strength

NOUL = """\
questions:
  issue_needs_candidate:
    primitive: noul
    instructions: {instructions}
    model: jev-1.13.0
    bands: {{yes_at: {yes_at}, no_at: {no_at}}}
    default: no
    stage: {stage}
    bar: {{max_error_yes: {max_error}, max_error_no: {max_error}, min_examples: {min_examples},
          max_flip_rate: 0.05}}
"""
CHOICE = """\
questions:
  kind:
    primitive: choice
    instructions: Which kind of change is `issue`?
    criteria: {bug: null, feature: null}
    model: jev-1.13.0
    bands: {floor: 0.5}
    default: uncertain
    stage: confirm
    bar: {max_error: 0.2, min_examples: 10, max_flip_rate: 0.05}
"""
SCORE = """\
questions:
  size:
    primitive: score
    instructions: How large is `issue`?
    criteria: {levels}
    model: jev-1.13.0
    bands: {{floor: 0.5}}
    default: uncertain
    stage: shadow
    bar: {{max_error: 0.2, min_examples: 1, max_flip_rate: 0.05}}
"""
Q = "issue_needs_candidate"

# Five levels of probability with their labels: 0.5 is a coin toss, the rest are clear.
SEPARABLE: list[tuple[float, bool]] = (
    [(0.9, True)] * 30
    + [(0.7, True)] * 30
    + [(0.5, True), (0.5, False)] * 20
    + [(0.3, False)] * 30
    + [(0.1, False)] * 30
)


def noul_bank(
    *,
    yes_at: float = 0.7,
    no_at: float = 0.3,
    stage: str = "act",
    max_error: float = 0.2,
    min_examples: int = 10,
    instructions: str = "Does `issue` build on `candidate`?",
) -> Bank:
    text = NOUL.format(
        yes_at=yes_at,
        no_at=no_at,
        stage=stage,
        max_error=max_error,
        min_examples=min_examples,
        instructions=instructions,
    )
    return parse_bank(text, source="typesafe.yaml")


def state(index: int) -> dict[str, str]:
    return {"issue": f"Issue {index}", "candidate": "Add a bank"}


class Judge:
    """Asks through the real asking path, the fake TypeSafe answering per state index."""

    def __init__(self, ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe) -> None:
        """Ask through ledger and client, with typesafe answering per state index."""
        self.ledger = ledger
        self.client = client
        self.typesafe = typesafe
        self.answers: dict[int, dict[str, object]] = {}
        typesafe.before_answer = self._answer

    def _answer(self) -> None:
        body = self.typesafe.requests()[-1]
        issue = cast("dict[str, str]", body["state"])["issue"]
        answer = self.answers[int(issue.removeprefix("Issue "))]
        for name in cast("dict[str, object]", body["questions"]):
            self.typesafe.answers[name] = answer

    def ask(
        self,
        bank: Bank,
        index: int,
        answer: dict[str, object],
        identifiers: dict[str, str | int] | None = None,
        *,
        name: str = Q,
        fresh: bool = False,
    ) -> int:
        self.answers[index] = answer
        result = ask(bank, self.ledger, self.client, [name], state(index), identifiers, fresh=fresh)
        return result[name].ask_id

    def label(self, ask_id: int, value: JSON, strength: Strength = "strong") -> None:
        outcome = Outcome(value=value, source="test", strength=strength, ask_id=ask_id)
        record_outcome(self.ledger, outcome)

    def populate(
        self, bank: Bank, points: list[tuple[float, bool]], *, start: int = 0, per_group: int = 2
    ) -> list[int]:
        """Ask and label one state per point; states share a "pair" group per_group at a time."""
        asks = []
        for index, (p, label) in enumerate(points, start):
            ask_id = self.ask(bank, index, noul(p), {"pair": index // per_group})
            self.label(ask_id, label)
            asks.append(ask_id)
        return asks


def noul(p: float) -> dict[str, object]:
    return {"type": "noul", "noul": p}


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


@pytest.fixture
def judge(ledger: Ledger, client: TypeSafe, typesafe: FakeTypeSafe) -> Judge:
    return Judge(ledger, client, typesafe)


def rows(root: Path, sql: str, parameters: tuple[object, ...] = ()) -> list[tuple[object, ...]]:
    """Query the ledger file directly, outside the code under test."""
    con = sqlite3.connect(root / ".crew/typesafe/ledger.sqlite", autocommit=True)
    try:
        return con.execute(sql, parameters).fetchall()
    finally:
        con.close()


def report(result: Calibration) -> dict[str, JSON]:
    return result.to_json()


def held_out(result: Calibration, bands: str = "current") -> dict[str, JSON]:
    return cast("dict[str, JSON]", cast("dict[str, JSON]", report(result)["held_out"])[bands])


def side(result: Calibration, name: str, bands: str = "current") -> dict[str, JSON]:
    return cast("dict[str, JSON]", cast("dict[str, JSON]", held_out(result, bands)["sides"])[name])


def counts(result: Calibration) -> dict[str, JSON]:
    return cast("dict[str, JSON]", report(result)["counts"])


def test_ae6_calibration_proposes_bands_and_passes_held_out_without_touching_the_bank(
    root: Path, ledger: Ledger, judge: Judge
) -> None:
    bank_file = root / ".crew/typesafe.yaml"
    bank_file.write_text(
        NOUL.format(
            yes_at=0.7,
            no_at=0.3,
            stage="act",
            max_error=0.2,
            min_examples=10,
            instructions="Does `issue` build on `candidate`?",
        ),
        "utf-8",
    )
    before = bank_file.read_bytes(), bank_file.stat().st_mtime_ns
    bank = load_bank(bank_file)
    judge.populate(bank, SEPARABLE)

    result = calibrate(bank, ledger, Q, "pair")

    # 0.9 and 0.7 hold on dev, and the coin toss at 0.5 stops the scan.
    assert result.proposed_bands == {"yes_at": 0.7, "no_at": 0.3}
    assert result.result == "passed"
    assert held_out(result)["result"] == "passed"
    assert held_out(result, "proposed")["result"] == "passed"
    assert side(result, "yes")["errors"] == 0
    assert cast("float", side(result, "yes")["upper_bound"]) <= 0.2
    assert counts(result)["rows"] == 160
    assert counts(result)["labels"] == {"strong": 160, "weak": 0}
    assert cast("int", counts(result)["dev"]) + cast("int", counts(result)["held_out"]) == 160
    assert (bank_file.read_bytes(), bank_file.stat().st_mtime_ns) == before
    recorded_row = (
        "SELECT question, version, stage, split_key, strong_only, result FROM calibrations"
    )
    assert rows(root, recorded_row) == [(Q, bank[Q].version_id, "act", "pair", 0, "passed")]
    recorded = rows(root, "SELECT seed, proposed_bands, report FROM calibrations")[0]
    assert recorded[0] == report(result)["seed"]
    assert json.loads(cast("str", recorded[1])) == {"no_at": 0.3, "yes_at": 0.7}
    assert json.loads(cast("str", recorded[2]))["counts"] == counts(result)


def test_ae6_labels_on_one_side_only_report_that_side_insufficient_with_the_count_needed(
    ledger: Ledger, judge: Judge
) -> None:
    bank = noul_bank(max_error=0.3)
    judge.populate(bank, [(0.9, True)] * 30)

    result = calibrate(bank, ledger, Q)

    assert side(result, "no") == {
        "examples": 0,
        "errors": 0,
        "upper_bound": None,
        "result": "insufficient",
        "needed": 10,
    }
    assert side(result, "yes")["result"] == "passed"
    assert result.result == "insufficient"
    assert result.proposed_bands is None
    assert held_out(result, "proposed") is None
    dev = cast("dict[str, JSON]", report(result)["dev"])
    assert dev["no"] == {"threshold": None}
    assert cast("dict[str, JSON]", dev["yes"])["threshold"] == 0.9


def test_a_group_never_lands_on_both_sides_and_a_state_under_two_groups_is_excluded(
    ledger: Ledger, judge: Judge
) -> None:
    bank = noul_bank()
    judge.populate(bank, SEPARABLE, per_group=5)
    # State 0 asked again under another group, and state 1 asked once with no group at all.
    judge.ask(bank, 0, noul(0.9), {"pair": 99})
    judge.ask(bank, 1, noul(0.9))

    result = calibrate(bank, ledger, Q, "pair")

    groups: dict[int, set[str]] = {}
    for index in range(2, len(SEPARABLE)):
        groups.setdefault(index // 5, set()).add(result.sides[_hash(index)])
    assert all(len(sides) == 1 for sides in groups.values())
    assert {side for sides in groups.values() for side in sides} == {"dev", "held_out"}
    assert _hash(0) not in result.sides
    assert _hash(1) not in result.sides
    assert counts(result)["excluded"] == {"split_key_disagrees": 1, "no_split_key": 1}
    assert counts(result)["labels"] == {"strong": 158, "weak": 0}


def test_by_default_the_split_is_by_state_and_ignores_identifiers(
    ledger: Ledger, judge: Judge
) -> None:
    bank = noul_bank()
    judge.populate(bank, SEPARABLE)
    judge.ask(bank, 0, noul(0.9), {"pair": 99})

    result = calibrate(bank, ledger, Q)

    assert report(result)["split_key"] is None
    assert counts(result)["excluded"] == {}
    assert len(result.sides) == 160
    held = sum(1 for s in result.sides.values() if s == "held_out")
    assert abs(held / 160 - HELD_OUT) < 0.1


def test_a_recheck_s_asks_do_not_drop_the_states_they_rechecked(
    ledger: Ledger, client: TypeSafe, judge: Judge
) -> None:
    v1 = noul_bank()
    judge.populate(v1, SEPARABLE[:30])
    v2 = noul_bank(instructions="Does `issue` need `candidate`?")
    recheck(v2, ledger, client, Q)

    result = calibrate(v2, ledger, Q, "pair")

    assert counts(result)["rows"] == 30
    assert counts(result)["excluded"] == {}
    assert len(result.sides) == 30


def test_replays_and_fresh_calls_of_one_state_count_once_with_the_first_call_s_answer(
    ledger: Ledger, judge: Judge
) -> None:
    bank = noul_bank(max_error=0.3)
    asks = judge.populate(bank, [(0.9, True)] * 30)
    judge.ask(bank, 0, noul(0.9), {"pair": 0})  # a replay
    judge.ask(bank, 0, noul(0.1), {"pair": 0}, fresh=True)  # a fresh call with another answer
    judge.label(asks[0], value=True)

    result = calibrate(bank, ledger, Q)

    assert counts(result)["rows"] == 30
    assert counts(result)["labels"] == {"strong": 30, "weak": 0}
    # Read through the fresh 0.1, state 0 would leave the yes side, on dev or held out.
    dev_yes = cast("dict[str, dict[str, JSON]]", report(result)["dev"])["yes"]
    assert cast("int", dev_yes["examples"]) + cast("int", side(result, "yes")["examples"]) == 30


def test_calibrating_twice_gives_the_same_split_and_bands_and_counts_both_runs(
    root: Path, ledger: Ledger, judge: Judge
) -> None:
    bank = noul_bank()
    judge.populate(bank, SEPARABLE)

    first = calibrate(bank, ledger, Q, "pair")
    second = calibrate(bank, ledger, Q, "pair")

    assert second.sides == first.sides
    assert second.proposed_bands == first.proposed_bands
    assert report(second)["seed"] == report(first)["seed"]
    assert report(first)["calibrations"] == {"version": 1, "question": 1}
    assert report(second)["calibrations"] == {"version": 2, "question": 2}
    assert second.id == first.id + 1
    assert rows(root, "SELECT count(*) FROM calibrations") == [(2,)]


def test_adding_labelled_states_leaves_every_earlier_group_on_its_side(
    ledger: Ledger, judge: Judge
) -> None:
    bank = noul_bank()
    judge.populate(bank, SEPARABLE[:70])
    before = calibrate(bank, ledger, Q, "pair")
    judge.populate(bank, SEPARABLE[70:], start=70)
    # New states in the earlier pairs 0 to 4.
    for pair in range(5):
        judge.label(judge.ask(bank, 300 + pair, noul(0.9), {"pair": pair}), value=True)

    after = calibrate(bank, ledger, Q, "pair")

    assert {h: after.sides[h] for h in before.sides} == before.sides
    assert all(after.sides[_hash(300 + p)] == before.sides[_hash(2 * p)] for p in range(5))
    assert len(after.sides) == 165


def test_after_a_band_edit_the_report_counts_the_earlier_version_s_calibrations(
    ledger: Ledger, judge: Judge
) -> None:
    v1 = noul_bank()
    judge.populate(v1, SEPARABLE)
    calibrate(v1, ledger, Q, "pair")
    calibrate(v1, ledger, Q, "pair")
    v2 = noul_bank(yes_at=0.8)

    result = calibrate(v2, ledger, Q, "pair")

    assert v2[Q].version_id != v1[Q].version_id
    assert report(result)["calibrations"] == {"version": 1, "question": 3}
    # The same content: the labels and the split carry over.
    assert counts(result)["rows"] == 160


def test_a_strong_label_wins_over_a_weak_one_and_strong_only_drops_weak_only_states(
    ledger: Ledger, judge: Judge
) -> None:
    bank = noul_bank(max_error=0.3)
    asks = judge.populate(bank, [(0.9, True)] * 30)
    judge.label(asks[0], value=False, strength="weak")  # later, but weaker
    weak = judge.ask(bank, 30, noul(0.9), {"pair": 15})
    judge.label(weak, value=True, strength="weak")
    judge.ask(bank, 31, noul(0.9), {"pair": 15})  # never labelled

    mixed = calibrate(bank, ledger, Q)
    strong = calibrate(bank, ledger, Q, strong_only=True)

    assert counts(mixed)["rows"] == 32
    assert counts(mixed)["unlabelled"] == 1
    assert counts(mixed)["labels"] == {"strong": 30, "weak": 1}
    # State 0 kept its strong yes: no false yes anywhere.
    assert side(mixed, "yes")["errors"] == 0
    assert counts(strong)["labels"] == {"strong": 30, "weak": 0}
    assert counts(strong)["excluded"] == {"weak_only": 1}
    assert report(strong)["strong_only"] is True


def test_a_choice_calibrates_its_confidence_floor_against_exact_match_labels(
    ledger: Ledger, judge: Judge
) -> None:
    bank = parse_bank(CHOICE, source="typesafe.yaml")
    points = [(0.9, "bug", "bug")] * 40 + [(0.6, "feature", "feature")] * 40
    points += [(0.3, "bug", "feature")] * 20 + [(0.3, "bug", "bug")] * 10
    for index, (confidence, chosen, label) in enumerate(points):
        other = "feature" if chosen == "bug" else "bug"
        answer: dict[str, object] = {
            "type": "choice",
            "choice": chosen,
            "confidence": confidence,
            "probabilities": {chosen: (1 + confidence) / 2, other: (1 - confidence) / 2},
        }
        judge.label(judge.ask(bank, index, answer, name="kind"), label)

    result = calibrate(bank, ledger, "kind")

    assert result.proposed_bands == {"floor": 0.6}
    assert side(result, "floor")["errors"] == 0
    assert result.result == "passed"
    floor = cast("dict[str, JSON]", cast("dict[str, JSON]", report(result)["dev"])["floor"])
    assert floor["threshold"] == 0.6
    assert floor["errors"] == 0


def score_answer(probabilities: list[float], confidence: float) -> dict[str, object]:
    return {
        "type": "score",
        "score": sum(i * p for i, p in enumerate(probabilities)),
        "confidence": confidence,
        "legend": {str(i): str(i) for i in range(len(probabilities))},
        "probabilities": {str(i): p for i, p in enumerate(probabilities)},
    }


def test_a_score_label_recorded_before_a_level_was_inserted_is_excluded_and_counted(
    ledger: Ledger, judge: Judge
) -> None:
    two = parse_bank(SCORE.format(levels="[small, large]"), source="typesafe.yaml")
    three = parse_bank(SCORE.format(levels="[small, medium, large]"), source="typesafe.yaml")
    for index in range(3):
        judge.label(judge.ask(two, index, score_answer([0.1, 0.9], 0.8), name="size"), 1)
    for index in range(4):
        ask_id = judge.ask(three, index, score_answer([0.0, 0.1, 0.9], 0.8), name="size")
        if index == 2:
            judge.label(ask_id, 2)  # recorded on the edited version: it counts

    result = calibrate(three, ledger, "size")

    assert counts(result)["rows"] == 4
    assert counts(result)["excluded"] == {"other_options_or_levels": 2}
    assert counts(result)["unlabelled"] == 1
    assert counts(result)["labels"] == {"strong": 1, "weak": 0}


def test_a_label_for_another_primitive_is_excluded_and_counted(
    ledger: Ledger, judge: Judge
) -> None:
    as_noul = noul_bank()
    for index in range(3):
        judge.label(judge.ask(as_noul, index, noul(0.9), name=Q), value=True)
    as_choice = parse_bank(CHOICE.replace("kind:", f"{Q}:"), source="typesafe.yaml")
    for index in range(3):
        answer = {"type": "choice", "choice": "bug", "confidence": 0.9}
        judge.ask(as_choice, index, {**answer, "probabilities": {"bug": 0.95, "feature": 0.05}})

    result = calibrate(as_choice, ledger, Q)

    assert counts(result)["excluded"] == {"other_primitive": 3}
    assert result.result == "insufficient"


def test_bands_that_would_cross_are_not_proposed(ledger: Ledger, judge: Judge) -> None:
    bank = noul_bank(max_error=0.8, min_examples=1)
    judge.populate(bank, [(0.6, True), (0.6, False), (0.4, True), (0.4, False)] * 10)

    result = calibrate(bank, ledger, Q)

    dev = cast("dict[str, dict[str, JSON]]", report(result)["dev"])
    assert cast("float", dev["yes"]["threshold"]) <= cast("float", dev["no"]["threshold"])
    assert result.proposed_bands is None


def dev_threshold(result: Calibration, name: str) -> JSON:
    return cast("dict[str, dict[str, JSON]]", report(result)["dev"])[name]["threshold"]


def test_failures_at_the_strict_end_where_few_states_pass_do_not_stop_the_scan(
    ledger: Ledger, judge: Judge
) -> None:
    # A bound within 0.1 needs about 29 states without error: the ten at 0.9 cannot give it.
    bank = noul_bank(max_error=0.1)
    judge.populate(bank, [(0.9, True)] * 10 + [(0.8, True)] * 60 + [(0.2, False)] * 60)

    result = calibrate(bank, ledger, Q)

    assert dev_threshold(result, "yes") == 0.8
    assert dev_threshold(result, "no") == 0.2


def test_the_first_failure_after_a_pass_stops_the_scan_though_a_laxer_threshold_passes(
    ledger: Ledger, judge: Judge
) -> None:
    bank = noul_bank(max_error=0.15)
    judge.populate(bank, [(0.9, True)] * 40 + [(0.7, False)] * 6 + [(0.5, True)] * 150)

    result = calibrate(bank, ledger, Q)

    # Counted from 0.9 down to 0.5, the false yeses at 0.7 are diluted within the bar.
    dev = cast("dict[str, dict[str, JSON]]", report(result)["dev"])["yes"]
    assert dev["threshold"] == 0.9


def test_current_bands_beyond_the_bar_on_held_out_fail(ledger: Ledger, judge: Judge) -> None:
    # The bank says yes from 0.5, where half the labels are no.
    bank = noul_bank(yes_at=0.5, no_at=0.1, max_error=0.2)
    judge.populate(bank, SEPARABLE)

    result = calibrate(bank, ledger, Q, "pair")

    assert side(result, "yes")["result"] == "failed"
    assert result.result == "failed"
    assert held_out(result, "proposed")["result"] == "passed"


def test_adopting_calibrated_bands_with_a_raised_stage_earns_it_by_calibration_despite_the_recheck(
    ledger: Ledger, client: TypeSafe, judge: Judge
) -> None:
    v1 = noul_bank(yes_at=0.95, no_at=0.05, stage="confirm")
    judge.populate(v1, SEPARABLE)
    proposed = cast("dict[str, float]", calibrate(v1, ledger, Q, "pair").proposed_bands)
    v2 = noul_bank(yes_at=proposed["yes_at"], no_at=proposed["no_at"], stage="act")
    flipped = recheck(v2, ledger, client, Q, from_version=v1[Q].version_id)
    with ledger.read() as reads:
        before = effective_stage(reads, v2[Q])

    result = calibrate(v2, ledger, Q, "pair")

    with ledger.read() as reads:
        after = effective_stage(reads, v2[Q])
    assert flipped.flip_rate > 0.05
    assert flipped.result == "failed"
    assert before is Stage.SHADOW
    assert (result.result, report(result)["stage"]) == ("passed", "act")
    assert after is Stage.ACT


def test_an_unknown_question_records_nothing(root: Path, ledger: Ledger) -> None:
    with pytest.raises(UnknownQuestionError):
        calibrate(noul_bank(), ledger, "unknown")
    assert rows(root, "SELECT count(*) FROM calibrations") == [(0,)]


@pytest.mark.parametrize(
    ("examples", "errors", "expected"),
    [
        # No error: the bound has the closed form 1 - 0.05^(1/n).
        (29, 0, 1 - 0.05 ** (1 / 29)),
        (1, 0, 0.95),
        # One error in ten: the one-sided 95% Clopper-Pearson bound, from published tables.
        (10, 1, 0.3942),
        (30, 3, 0.2386),
        (5, 5, 1.0),
    ],
)
def test_the_upper_bound_is_the_exact_one_sided_95_percent_bound(
    examples: int, errors: int, expected: float
) -> None:
    assert math.isclose(upper_bound(errors, examples), expected, abs_tol=1e-4)


@pytest.mark.parametrize(
    "body",
    [
        {"question": 5},
        {"question": Q, "split_key": ""},
        {"question": Q, "split_key": 3},
        {"question": Q, "strong_only": "yes"},
    ],
)
def test_a_bad_calibration_request_is_refused(body: dict[str, object]) -> None:
    with pytest.raises(InvalidRequestError):
        parse_calibration(body)


def test_a_calibration_request_parses() -> None:
    assert parse_calibration({"question": Q}) == (Q, None, False)
    assert parse_calibration({"question": Q, "split_key": "pair", "strong_only": True}) == (
        Q,
        "pair",
        True,
    )


def _hash(index: int) -> str:
    return state_hash(state(index))


@pytest.mark.parametrize(("max_error", "expected"), [(0.0, "failed"), (1.0, "passed")])
def test_a_bar_allowing_no_error_never_passes_and_one_allowing_all_always_does(
    ledger: Ledger, judge: Judge, max_error: float, expected: str
) -> None:
    bank = noul_bank(max_error=max_error)
    judge.populate(bank, SEPARABLE)

    result = calibrate(bank, ledger, Q, "pair")

    assert result.result == expected
