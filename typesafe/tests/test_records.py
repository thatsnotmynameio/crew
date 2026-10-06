import sqlite3
from typing import TYPE_CHECKING

import pytest

from typesafe_judge.asking import ask
from typesafe_judge.bank import Bank, Stage, parse_bank
from typesafe_judge.ledger import Ledger
from typesafe_judge.records import Counts, InvalidRequestError, Outcome, decide, record_outcome

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
    instructions: Does `issue` build on `candidate`?
    model: jev-1.13.0
    bands: {{yes_at: 0.7, no_at: 0.3}}
    default: no
    stage: shadow
    bar: {{max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}}
  kind:
    primitive: choice
    instructions: Which kind of change is `issue`?
    criteria: {{bug: null, feature: null{extra_option}}}
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
    stage: act
    bar: {{max_error: 0.1, min_examples: 30, max_flip_rate: 0.05}}
"""
Q1 = "issue_needs_candidate"
STATE = {"issue": "Add a ledger", "candidate": "Add a bank"}
OTHER_STATE = {"issue": "Add a recheck", "candidate": "Add a calibration"}
THIRD_STATE = {"issue": "Add a stage report", "candidate": "Add a bank"}


def bank(extra_option: str = "") -> Bank:
    return parse_bank(BANK.format(extra_option=extra_option), source="typesafe.yaml")


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


def asked(
    ledger: Ledger,
    client: TypeSafe,
    name: str,
    state: dict[str, str],
    identifiers: dict[str, str | int] | None = None,
    of: Bank | None = None,
) -> int:
    return ask(of or bank(), ledger, client, [name], state, identifiers)[name].ask_id


def outcome(
    value: JSON,
    *,
    ask_id: int | None = None,
    question: str | None = None,
    identifiers: dict[str, str | int] | None = None,
) -> Outcome:
    return Outcome(
        value=value,
        source="blocked_by",
        strength="strong",
        ask_id=ask_id,
        question=question,
        identifiers=identifiers,
    )


def test_a_decision_on_an_unknown_ask_is_rejected_and_adds_nothing(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    ask_id = asked(ledger, client, Q1, STATE)

    with pytest.raises(InvalidRequestError, match=f"unknown ask id {ask_id + 1}") as refused:
        decide(ledger, ask_id + 1, "acted")

    assert refused.value.code == "unknown_ask"
    assert rows(root, "SELECT count(*) FROM decisions") == [(0,)]


def test_a_second_decision_on_one_ask_is_recorded_and_becomes_the_latest(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    ask_id = asked(ledger, client, Q1, STATE)

    first = decide(ledger, ask_id, "sent_to_person")
    second = decide(ledger, ask_id, "ignored_in_shadow")

    assert second.id > first.id
    assert rows(root, "SELECT ask_id, decision FROM decisions ORDER BY id") == [
        (ask_id, "sent_to_person"),
        (ask_id, "ignored_in_shadow"),
    ]
    with ledger.read() as reads:
        content_key = bank()[Q1].content_key
        assert [a.decision for a in reads.answered_asks(Q1, content_key)] == ["ignored_in_shadow"]


def test_an_acted_decision_on_a_shadow_ask_is_accepted_and_counted(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    ask_id = asked(ledger, client, Q1, STATE)

    decision = decide(ledger, ask_id, "acted")

    assert (decision.ask_id, decision.decision) == (ask_id, "acted")
    assert decision.effective_stage is Stage.SHADOW
    assert decision.to_json() == {
        "decision_id": decision.id,
        "ask_id": ask_id,
        "decision": "acted",
        "effective_stage": "shadow",
    }
    against_stage = (
        "SELECT count(*) FROM decisions d JOIN asks k ON k.id = d.ask_id"
        " WHERE d.decision = 'acted' AND k.effective_stage = 'shadow'"
    )
    assert rows(root, against_stage) == [(1,)]


def test_an_outcome_matched_by_identifiers_labels_every_matching_asks_state(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    asked(ledger, client, Q1, STATE, {"issue": 5, "candidate": 1})
    latest = asked(ledger, client, Q1, STATE, {"issue": 5, "candidate": 1})
    other = asked(ledger, client, Q1, OTHER_STATE, {"issue": 5, "candidate": 2})
    asked(ledger, client, Q1, THIRD_STATE, {"issue": 6, "candidate": 1})

    counts = record_outcome(ledger, outcome(value=True, question=Q1, identifiers={"issue": 5}))

    assert counts == Counts(recorded=2, acknowledged=0)
    assert counts.to_json() == {"recorded": 2, "acknowledged": 0}
    # One row per state, on the latest matching ask of that state.
    assert rows(root, "SELECT ask_id, value, source, strength, matched_by FROM outcomes") == [
        (latest, "true", "blocked_by", "strong", '{"issue":5}'),
        (other, "true", "blocked_by", "strong", '{"issue":5}'),
    ]


def test_an_outcome_matching_nothing_is_rejected_and_adds_nothing(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    ask_id = asked(ledger, client, Q1, STATE, {"issue": 5})

    for unmatched in (
        outcome(value=True, question=Q1, identifiers={"issue": 7}),
        outcome(value=True, question="unknown", identifiers={"issue": 5}),
        outcome(value=True, ask_id=ask_id + 1),
    ):
        with pytest.raises(InvalidRequestError) as refused:
            record_outcome(ledger, unmatched)
        assert refused.value.code == "no_match"

    assert rows(root, "SELECT count(*) FROM outcomes") == [(0,)]


def test_an_integer_identifier_does_not_match_the_same_digits_as_text(
    ledger: Ledger, client: TypeSafe
) -> None:
    asked(ledger, client, Q1, STATE, {"issue": "5"})

    with pytest.raises(InvalidRequestError, match="matches no ask"):
        record_outcome(ledger, outcome(value=True, question=Q1, identifiers={"issue": 5}))

    counts = record_outcome(ledger, outcome(value=True, question=Q1, identifiers={"issue": "5"}))
    assert counts == Counts(recorded=1, acknowledged=0)


def test_an_outcome_by_identifiers_must_name_at_least_one(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    asked(ledger, client, Q1, STATE, {"issue": 5})

    with pytest.raises(InvalidRequestError, match="identifiers") as refused:
        record_outcome(ledger, outcome(value=True, question=Q1, identifiers={}))

    assert refused.value.code == "bad_request"
    assert rows(root, "SELECT count(*) FROM outcomes") == [(0,)]


def test_an_identical_outcome_is_acknowledged_and_a_changed_one_becomes_the_latest(
    root: Path, ledger: Ledger, client: TypeSafe
) -> None:
    ask_id = asked(ledger, client, Q1, STATE)

    first = record_outcome(ledger, outcome(value=True, ask_id=ask_id))
    again = record_outcome(ledger, outcome(value=True, ask_id=ask_id))
    changed = record_outcome(ledger, outcome(value=False, ask_id=ask_id))
    back = record_outcome(ledger, outcome(value=True, ask_id=ask_id))
    weaker = record_outcome(
        ledger, Outcome(value=True, source="blocked_by", strength="weak", ask_id=ask_id)
    )
    elsewhere = record_outcome(
        ledger, Outcome(value=True, source="refinement", strength="strong", ask_id=ask_id)
    )

    assert first == Counts(recorded=1, acknowledged=0)
    assert again == Counts(recorded=0, acknowledged=1)
    assert changed == Counts(recorded=1, acknowledged=0)
    # Equal to an older outcome but not to the latest, so it is new again.
    assert back == Counts(recorded=1, acknowledged=0)
    assert weaker == Counts(recorded=1, acknowledged=0)
    assert elsewhere == Counts(recorded=1, acknowledged=0)
    assert rows(root, "SELECT value, source, strength, matched_by FROM outcomes ORDER BY id") == [
        ("true", "blocked_by", "strong", None),
        ("false", "blocked_by", "strong", None),
        ("true", "blocked_by", "strong", None),
        ("true", "blocked_by", "weak", None),
        ("true", "refinement", "strong", None),
    ]


def test_an_identical_outcome_matched_by_identifiers_is_acknowledged_per_state(
    ledger: Ledger, client: TypeSafe
) -> None:
    asked(ledger, client, Q1, STATE, {"issue": 5})
    record_outcome(ledger, outcome(value=True, question=Q1, identifiers={"issue": 5}))
    asked(ledger, client, Q1, OTHER_STATE, {"issue": 5})

    counts = record_outcome(ledger, outcome(value=True, question=Q1, identifiers={"issue": 5}))

    assert counts == Counts(recorded=1, acknowledged=1)


@pytest.mark.parametrize(
    ("name", "value"),
    [
        (Q1, 1),
        (Q1, "yes"),
        (Q1, None),
        ("kind", "chore"),
        ("kind", True),
        ("size", 5),
        ("size", -1),
        ("size", True),
        ("size", "2"),
        ("size", 2.0),
    ],
)
def test_an_outcome_value_of_the_wrong_type_for_the_primitive_is_rejected(
    root: Path, ledger: Ledger, client: TypeSafe, name: str, value: JSON
) -> None:
    ask_id = asked(ledger, client, name, STATE)

    with pytest.raises(InvalidRequestError) as refused:
        record_outcome(ledger, outcome(value=value, ask_id=ask_id))

    assert refused.value.code == "invalid_value"
    assert rows(root, "SELECT count(*) FROM outcomes") == [(0,)]


@pytest.mark.parametrize(("name", "value"), [(Q1, False), ("kind", "feature"), ("size", 4)])
def test_an_outcome_value_valid_for_the_primitive_is_recorded(
    root: Path, ledger: Ledger, client: TypeSafe, name: str, value: JSON
) -> None:
    ask_id = asked(ledger, client, name, STATE)

    assert record_outcome(ledger, outcome(value=value, ask_id=ask_id)).recorded == 1
    assert rows(root, "SELECT ask_id FROM outcomes") == [(ask_id,)]


def test_a_choice_outcome_is_checked_against_the_options_of_the_version_asked(
    ledger: Ledger, client: TypeSafe
) -> None:
    before = asked(ledger, client, "kind", STATE)
    after = asked(ledger, client, "kind", OTHER_STATE, of=bank(extra_option=", chore: null"))

    with pytest.raises(InvalidRequestError, match="chore"):
        record_outcome(ledger, outcome(value="chore", ask_id=before))
    counts = record_outcome(ledger, outcome(value="chore", ask_id=after))

    assert counts == Counts(recorded=1, acknowledged=0)
