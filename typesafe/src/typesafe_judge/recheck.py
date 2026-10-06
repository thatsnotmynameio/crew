"""Rechecks: a question's current version judged over the states an earlier version answered.

``POST /v1/rechecks``, admin scope::

    request:  {"question": name, "from_version"?: version id}
    response: {"recheck": {id, question, from_version, to_version, stage, result,
               compared, flipped, cannot_judge, flip_rate, bar: {min_examples, max_flip_rate},
               flips_by_kind: {"yes->uncertain": n, ...},
               flips: [{state_hash, from, to, kind, asks: [ask id, ...],
                        decisions: [{ask_id, decision}, ...]}, ...],
               effective_stage}}

The earlier version defaults to the trusted version for the stage the current version
declares (KTD11); a request may name another earlier version, whose passing recheck is
recorded but earns no stage unless it is the trusted one.

The states compared are the distinct states whose asks were served an answer to the
earlier version's content, each read through its first answer, the one replays serve.
When the current version shares that content, a band-only change, its verdicts are read
from the same answers and TypeSafe is not called. Otherwise each state is asked through
``asking.ask`` with the identifiers ``{"recheck_from": <earlier version id>}``: every live
call is recorded as it lands, under the current version, and a rerun replays it.

A flip is any change of verdict, uncertain included; ``decisions`` lists the latest
decision on each flipped ask that has one. The recheck is judged by the earlier version's
bar, so an edit cannot loosen its own gate: ``incomplete`` when any state was left
"cannot judge", ``insufficient`` under ``min_examples`` states compared, ``failed`` above
``max_flip_rate``, else ``passed``. It is recorded, whatever its result, for the stage the
current version declares, and ``effective_stage`` is the current version's stage after it.
"""

from __future__ import annotations

from collections import Counter
from dataclasses import dataclass, replace
from typing import TYPE_CHECKING, Literal, TypeAlias, cast

from typesafe_judge.asking import (
    UnknownQuestionError,
    ask,
    effective_stage,
    trusted_version,
    verdict,
)
from typesafe_judge.bank import ChoiceSpec, NoulSpec, Question, ScoreSpec, Stage
from typesafe_judge.keys import replay_key
from typesafe_judge.ledger import NewRecheck
from typesafe_judge.records import InvalidRequestError

if TYPE_CHECKING:
    from typesafe_judge.asking import Verdict
    from typesafe_judge.bank import Bank, FloorBar, NoulBar
    from typesafe_judge.client import TypeSafe
    from typesafe_judge.keys import JSON
    from typesafe_judge.ledger import Answer, Ledger, Reads, VersionRecord

RECHECK_MARKER = "recheck_from"
"""The identifier key of the asks a recheck records; its value is the earlier version id."""

Result: TypeAlias = Literal["passed", "failed", "insufficient", "incomplete"]

_SPECS: dict[str, type[NoulSpec | ChoiceSpec | ScoreSpec]] = {
    "noul": NoulSpec,
    "choice": ChoiceSpec,
    "score": ScoreSpec,
}


@dataclass(frozen=True, slots=True)
class Flip:
    """A state whose verdict changed, with the asks on it and their latest decisions."""

    state_hash: str
    before: Verdict
    after: Verdict
    asks: list[int]
    decisions: list[tuple[int, str]]

    @property
    def kind(self) -> str:
        """Return the change, such as ``yes->uncertain``."""
        return f"{self.before}->{self.after}"

    def to_json(self) -> dict[str, JSON]:
        """Return the flip as a recheck reports it."""
        return {
            "state_hash": self.state_hash,
            "from": self.before,
            "to": self.after,
            "kind": self.kind,
            "asks": cast("list[JSON]", self.asks),
            "decisions": [{"ask_id": a, "decision": d} for a, d in self.decisions],
        }


@dataclass(frozen=True, slots=True)
class Recheck:
    """A recorded recheck: its versions, stage, result, counts, bar and flips."""

    id: int
    question: str
    from_version: str
    to_version: str
    stage: Stage
    result: Result
    compared: int
    cannot_judge: int
    bar: dict[str, JSON]
    flips: list[Flip]
    effective_stage: Stage

    @property
    def flipped(self) -> int:
        """Return how many states changed verdict."""
        return len(self.flips)

    @property
    def flip_rate(self) -> float:
        """Return the share of the states compared that changed verdict."""
        return self.flipped / self.compared if self.compared else 0.0

    @property
    def flips_by_kind(self) -> dict[str, int]:
        """Return how many states flipped, by kind of change."""
        return dict(Counter(flip.kind for flip in self.flips))

    def report(self) -> dict[str, JSON]:
        """Return what the ledger keeps beside the counts: the flip rate and the flips."""
        return {
            "flip_rate": self.flip_rate,
            "flips_by_kind": cast("dict[str, JSON]", self.flips_by_kind),
            "flips": [flip.to_json() for flip in self.flips],
        }

    def row(self) -> NewRecheck:
        """Return the recheck as the ledger records it."""
        return NewRecheck(
            question=self.question,
            from_version=self.from_version,
            to_version=self.to_version,
            stage=self.stage.value,
            result=self.result,
            compared=self.compared,
            flipped=self.flipped,
            cannot_judge=self.cannot_judge,
            bar=self.bar,
            report=self.report(),
        )

    def to_json(self) -> dict[str, JSON]:
        """Return the recheck as the rechecks endpoint answers it."""
        return {
            "id": self.id,
            "question": self.question,
            "from_version": self.from_version,
            "to_version": self.to_version,
            "stage": self.stage.value,
            "result": self.result,
            "compared": self.compared,
            "flipped": self.flipped,
            "cannot_judge": self.cannot_judge,
            "bar": self.bar,
            **self.report(),
            "effective_stage": self.effective_stage.value,
        }


@dataclass(frozen=True, slots=True)
class _Earlier:
    """The earlier version and what it answered: each state's verdict, asks and decisions."""

    question: Question
    verdicts: dict[str, Verdict]
    asks: dict[str, list[int]]
    decisions: dict[str, list[tuple[int, str]]]
    states: dict[str, object]


def recheck(
    bank: Bank, ledger: Ledger, client: TypeSafe, name: str, from_version: str | None = None
) -> Recheck:
    """Recheck the bank's version of the question named name, and record the result.

    Raise UnknownQuestionError for a name the bank lacks, and InvalidRequestError when
    from_version is not an earlier version or no earlier version is trusted; nothing is
    recorded then.
    """
    if name not in bank:
        raise UnknownQuestionError([name])
    question = bank[name]
    ledger.record_bank(bank)
    with ledger.read() as reads:
        earlier = _earlier(reads, question, from_version)
    after, cannot_judge = _verdicts_now(bank, ledger, client, question, earlier)
    flips = [
        Flip(digest, earlier.verdicts[digest], now, earlier.asks[digest], earlier.decisions[digest])
        for digest, now in after.items()
        if now != earlier.verdicts[digest]
    ]
    bar = earlier.question.spec.bar
    draft = Recheck(
        id=0,
        question=name,
        from_version=earlier.question.version_id,
        to_version=question.version_id,
        stage=question.stage,
        result=_result(len(after), len(flips), cannot_judge, bar),
        compared=len(after),
        cannot_judge=cannot_judge,
        bar={"min_examples": bar.min_examples, "max_flip_rate": bar.max_flip_rate},
        flips=flips,
        effective_stage=Stage.SHADOW,
    )
    with ledger.write() as writes:
        recheck_id = writes.insert_recheck(draft.row())
    with ledger.read() as reads:
        stage = effective_stage(reads, question)
    return replace(draft, id=recheck_id, effective_stage=stage)


def _earlier(reads: Reads, question: Question, from_version: str | None) -> _Earlier:
    """Return the version to recheck from, with the states its content answered."""
    name = question.name
    versions = reads.versions(name)
    if from_version is None:
        from_version = trusted_version(reads, name, question.version_id, question.stage)
        if from_version is None:
            msg = (
                f"{name}: no earlier version holds stage {question.stage.value}; name"
                " from_version to recheck from another"
            )
            raise InvalidRequestError(msg, "no_trusted_version")
    elif from_version not in versions[: versions.index(question.version_id)]:
        msg = f"from_version: {from_version} is not an earlier version of {name}"
        raise InvalidRequestError(msg, "unknown_version")
    record = cast("VersionRecord", reads.version(name, from_version))
    spec = _SPECS[record.primitive].model_validate(record.definition)
    old = Question(name, spec, record.content_key, from_version)
    earlier = _Earlier(old, {}, {}, {}, {})
    for answered in reads.answered_asks(name, old.content_key):
        digest = answered.state_hash
        if digest not in earlier.verdicts:
            # The ask was served an answer, so the first answer for its key exists.
            first = cast("Answer", reads.first_answer(replay_key(old.content_key, digest)))
            earlier.verdicts[digest] = verdict(old, first.answer)
            earlier.asks[digest], earlier.decisions[digest] = [], []
            if old.content_key != question.content_key:
                earlier.states[digest] = reads.state(digest)
        earlier.asks[digest].append(answered.id)
        if answered.decision is not None:
            earlier.decisions[digest].append((answered.id, answered.decision))
    return earlier


def _verdicts_now(
    bank: Bank, ledger: Ledger, client: TypeSafe, question: Question, earlier: _Earlier
) -> tuple[dict[str, Verdict], int]:
    """Return the current version's verdict on each state it judged, and how many it could not."""
    if question.content_key == earlier.question.content_key:
        # A band-only change: the same answers, read through the current bands.
        with ledger.read() as reads:
            first = {
                d: reads.first_answer(replay_key(question.content_key, d)) for d in earlier.asks
            }
        return {d: verdict(question, cast("Answer", a).answer) for d, a in first.items()}, 0
    verdicts: dict[str, Verdict] = {}
    marker = {RECHECK_MARKER: earlier.question.version_id}
    for digest, state in earlier.states.items():
        answer = ask(bank, ledger, client, [question.name], state, marker)[question.name]
        if answer.verdict is not None:
            verdicts[digest] = answer.verdict
    return verdicts, len(earlier.states) - len(verdicts)


def _result(compared: int, flipped: int, cannot_judge: int, bar: NoulBar | FloorBar) -> Result:
    if cannot_judge:
        return "incomplete"
    if compared < bar.min_examples:
        return "insufficient"
    if flipped / compared > bar.max_flip_rate:
        return "failed"
    return "passed"
