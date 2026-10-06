"""Decisions and outcomes on recorded asks (KTD13, KTD17).

``POST /v1/decisions``, ask scope, records what a caller did with an answer::

    request:  {"ask_id": int, "decision": "acted" | "sent_to_person" | "ignored_in_shadow"}
    response: {"decision_id": int, "ask_id": int, "decision": str, "effective_stage": str}

An ask may get several decisions, and the latest counts. A decision against the ask's
effective stage, such as ``acted`` on a shadow ask, is accepted: the stage report counts it.

``POST /v1/outcomes``, admin scope, records a real outcome, matched by an ask id or by
the question's name and caller identifiers::

    request:  {"ask_id": int} or {"question": str, "identifiers": {key: str | int, ...}},
              with "value", "source" (free text) and "strength" ("strong" or "weak")
    response: {"recorded": int, "acknowledged": int}

Identifiers match every ask of the question whose identifiers hold them all, types
significant, and must name at least one. Each matched state gets one row, on its latest
matched ask. The value must suit that ask's version: a boolean for a noul, one of its
options for a choice, a level index for a score. A state whose latest outcome from the
same source already has the same value and strength is acknowledged and adds no row, so
an import can run again. An outcome matching nothing, or with a value that does not
suit, is refused and records nothing.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING, Literal, TypeAlias, cast, get_args

import rfc8785

from typesafe_judge.bank import Stage
from typesafe_judge.keys import canonical_identifiers
from typesafe_judge.ledger import AskOrigin, NewOutcome

if TYPE_CHECKING:
    from collections.abc import Mapping

    from typesafe_judge.keys import JSON
    from typesafe_judge.ledger import Ledger, VersionRecord, Writes

DecisionName: TypeAlias = Literal["acted", "sent_to_person", "ignored_in_shadow"]
Strength: TypeAlias = Literal["strong", "weak"]

DECISIONS: tuple[str, ...] = get_args(DecisionName)
STRENGTHS: tuple[str, ...] = get_args(Strength)


class InvalidRequestError(ValueError):
    """A request the judge refuses before it records anything; code names the reason."""

    def __init__(self, message: str, code: str = "bad_request") -> None:
        """Keep the message and the error code a response carries."""
        super().__init__(message)
        self.code = code


@dataclass(frozen=True, slots=True)
class Decision:
    """A recorded decision, with the effective stage its ask was answered at."""

    id: int
    ask_id: int
    decision: DecisionName
    effective_stage: Stage

    def to_json(self) -> dict[str, JSON]:
        """Return the decision as the decisions endpoint answers it."""
        return {
            "decision_id": self.id,
            "ask_id": self.ask_id,
            "decision": self.decision,
            "effective_stage": self.effective_stage.value,
        }


@dataclass(frozen=True, slots=True)
class Outcome:
    """A real outcome, for an ask id, or for a question's asks holding identifiers."""

    value: JSON
    source: str
    strength: Strength
    ask_id: int | None = None
    question: str | None = None
    identifiers: dict[str, str | int] | None = None


@dataclass(frozen=True, slots=True)
class Counts:
    """How many states an outcome labelled anew, and how many already had it."""

    recorded: int
    acknowledged: int

    def to_json(self) -> dict[str, JSON]:
        """Return the counts as the outcomes endpoint answers them."""
        return {"recorded": self.recorded, "acknowledged": self.acknowledged}


def decide(ledger: Ledger, ask_id: int, decision: DecisionName) -> Decision:
    """Record a decision on a recorded ask; raise InvalidRequestError for an unknown ask."""
    with ledger.write() as writes:
        origin = writes.ask_origin(ask_id)
        if origin is None:
            msg = f"unknown ask id {ask_id}"
            raise InvalidRequestError(msg, "unknown_ask")
        decision_id = writes.insert_decision(ask_id, decision)
    return Decision(decision_id, ask_id, decision, Stage(origin.effective_stage))


def record_outcome(ledger: Ledger, outcome: Outcome) -> Counts:
    """Label each state the outcome matches; raise InvalidRequestError, recording nothing."""
    recorded = acknowledged = 0
    # One transaction: a refusal part way rolls back the states already labelled.
    with ledger.write() as writes:
        for origin in _matched(writes, outcome):
            _check_value(outcome.value, cast("VersionRecord", writes.version(*_key(origin))))
            value = rfc8785.dumps(outcome.value).decode()
            latest = writes.latest_outcome(origin.question, origin.state_hash, outcome.source)
            if latest == (value, outcome.strength):
                acknowledged += 1
                continue
            writes.insert_outcome(
                NewOutcome(
                    ask_id=origin.id,
                    value=outcome.value,
                    source=outcome.source,
                    strength=outcome.strength,
                    matched_by=outcome.identifiers if outcome.ask_id is None else None,
                )
            )
            recorded += 1
    return Counts(recorded, acknowledged)


def _key(origin: AskOrigin) -> tuple[str, str]:
    return origin.question, origin.version


def _matched(writes: Writes, outcome: Outcome) -> list[AskOrigin]:
    """Return the latest matched ask of each matched state, in the order states were first asked."""
    if outcome.ask_id is not None:
        origin = writes.ask_origin(outcome.ask_id)
        if origin is None:
            msg = f"unknown ask id {outcome.ask_id}: the outcome matches no ask"
            raise InvalidRequestError(msg, "no_match")
        return [origin]
    question, identifiers = outcome.question, outcome.identifiers
    if question is None or not identifiers:
        msg = "identifiers: name at least one, with the question, or give an ask_id"
        raise InvalidRequestError(msg)
    canonical_identifiers(identifiers)
    latest: dict[str, AskOrigin] = {}
    for record in writes.asks(question, identifiers):
        origin = AskOrigin(
            record.id, question, record.version, record.state_hash, record.effective_stage
        )
        # Asks come oldest first; the dict keeps each state's first place.
        latest[record.state_hash] = origin
    if not latest:
        msg = f"the outcome matches no ask of {question} with those identifiers"
        raise InvalidRequestError(msg, "no_match")
    return list(latest.values())


def _check_value(value: JSON, version: VersionRecord) -> None:
    """Raise InvalidRequestError unless value suits the version's primitive."""
    criteria = version.definition.get("criteria")
    match version.primitive:
        case "noul":
            ok, expected = isinstance(value, bool), "true or false"
        case "choice":
            options = list(cast("Mapping[str, JSON]", criteria))
            ok, expected = isinstance(value, str) and value in options, f"one of {options}"
        case "score":
            levels = len(cast("list[JSON]", criteria))
            level = isinstance(value, int) and not isinstance(value, bool)
            ok = level and 0 <= cast("int", value) < levels
            expected = f"a level index from 0 to {levels - 1}"
    if not ok:
        msg = f"value: a {version.primitive} outcome must be {expected}, not {value!r}"
        raise InvalidRequestError(msg, "invalid_value")


def parse_decision(body: Mapping[str, object]) -> tuple[int, DecisionName]:
    """Return a decisions request's ask id and decision; raise InvalidRequestError."""
    ask_id, decision = body["ask_id"], body["decision"]
    if isinstance(ask_id, bool) or not isinstance(ask_id, int):
        msg = "ask_id: must be an integer"
        raise InvalidRequestError(msg)
    if decision not in DECISIONS:
        msg = f"decision: must be one of {', '.join(DECISIONS)}"
        raise InvalidRequestError(msg)
    return ask_id, cast("DecisionName", decision)


def parse_outcome(body: Mapping[str, object]) -> Outcome:
    """Return an outcomes request as an Outcome; raise InvalidRequestError."""
    source, strength = body["source"], body["strength"]
    if not isinstance(source, str) or not source:
        msg = "source: must be non-empty text"
        raise InvalidRequestError(msg)
    if strength not in STRENGTHS:
        msg = f"strength: must be one of {', '.join(STRENGTHS)}"
        raise InvalidRequestError(msg)
    by_ask = "ask_id" in body
    if by_ask == ("question" in body or "identifiers" in body):
        msg = "give either ask_id, or question and identifiers"
        raise InvalidRequestError(msg)
    ask_id, question, identifiers = (
        body.get("ask_id"),
        body.get("question"),
        body.get("identifiers"),
    )
    if by_ask and (isinstance(ask_id, bool) or not isinstance(ask_id, int)):
        msg = "ask_id: must be an integer"
        raise InvalidRequestError(msg)
    if not by_ask and (not isinstance(question, str) or not isinstance(identifiers, dict)):
        msg = "question and identifiers: must be text and an object"
        raise InvalidRequestError(msg)
    return Outcome(
        value=cast("JSON", body["value"]),
        source=source,
        strength=cast("Strength", strength),
        ask_id=cast("int | None", ask_id),
        question=cast("str | None", question),
        identifiers=cast("dict[str, str | int] | None", identifiers),
    )
