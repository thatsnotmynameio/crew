"""Asking: replay what is recorded, call TypeSafe for the misses, read verdicts and stages.

One caller request names questions of the current bank and one state (KTD8):

- A question whose replay key has a recorded answer is served the answer of the
  lowest call, with no request. ``fresh`` sends every named question instead.
- The other questions go out in one SDK request per pinned model, with no ledger
  transaction open. Each request's call, answers and asks are recorded in one
  transaction as it returns; a plain ask is then served the lowest call for its
  key, so racers all get the answer later replays serve.
- A request that fails records its asks as "cannot judge", with the reason, the
  attempts and the question's conservative default.

Verdicts are read from the recorded answer and the current bands (KTD9). Effective
stages are read from the versions, stage observations, rechecks and calibrations the
ledger holds (KTD11).
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING, Literal, TypeAlias, cast

from typesafe_judge.bank import UNCERTAIN, ChoiceSpec, NoulSpec, Question, ScoreSpec, Stage
from typesafe_judge.client import Failure
from typesafe_judge.keys import canonical_identifiers, replay_key, state_hash
from typesafe_judge.ledger import Answer, NewAnswer, NewAsk, NewCall

if TYPE_CHECKING:
    from collections.abc import Mapping, Sequence

    from typesafe_sdk import JSONContent

    from typesafe_judge.bank import Bank
    from typesafe_judge.client import Reply, TypeSafe
    from typesafe_judge.keys import JSON
    from typesafe_judge.ledger import Ledger, Reads, Writes

Verdict: TypeAlias = str | int
"""A noul's yes, no or uncertain; a choice's option; a score's level; or uncertain."""

Probabilities: TypeAlias = float | dict[str, float]
"""A noul's probability of yes, or a choice's or a score's probability per option or level."""


class UnknownQuestionError(ValueError):
    """A request naming questions the bank does not hold; nothing was asked or recorded."""

    def __init__(self, names: list[str]) -> None:
        """Name every unknown question."""
        label = "question" if len(names) == 1 else "questions"
        super().__init__(f"unknown {label}: {', '.join(names)}")
        self.names = names


@dataclass(frozen=True, slots=True)
class AskResult:
    """One question's answer to a caller, or why the judge cannot judge it."""

    ask_id: int
    version: str
    status: Literal["answered", "cannot_judge"]
    replayed: bool
    verdict: Verdict | None
    probabilities: Probabilities | None
    effective_stage: Stage
    declared_stage: Stage
    model_asked: str
    model: str | None
    reason: str | None = None
    attempts: int | None = None
    default: str | int | None = None
    recorded_answer_exists: bool | None = None

    def to_json(self) -> dict[str, JSON]:
        """Return the result as the ask exchange carries it."""
        result: dict[str, JSON] = {
            "ask_id": self.ask_id,
            "version": self.version,
            "status": self.status,
            "replayed": self.replayed,
            "verdict": self.verdict,
            "probabilities": cast("JSON", self.probabilities),
            "effective_stage": self.effective_stage.value,
            "declared_stage": self.declared_stage.value,
            "model_asked": self.model_asked,
            "model": self.model,
        }
        if self.status == "cannot_judge":
            result["reason"] = self.reason
            result["attempts"] = self.attempts
            result["default"] = self.default
            result["recorded_answer_exists"] = self.recorded_answer_exists
        return result


@dataclass(frozen=True, slots=True)
class _Request:
    """What every ask of one caller request shares."""

    state: object
    state_hash: str
    identifiers: dict[str, str | int]
    fresh: bool
    stages: dict[str, Stage]
    recorded: dict[str, Answer | None]

    def key(self, question: Question) -> str:
        return replay_key(question.content_key, self.state_hash)

    def new_ask(
        self,
        question: Question,
        *,
        served: Answer | None = None,
        replayed: bool = False,
        failure: Failure | None = None,
    ) -> NewAsk:
        return NewAsk(
            question=question.name,
            version=question.version_id,
            state_hash=self.state_hash,
            replay_key=self.key(question),
            effective_stage=self.stages[question.name].value,
            identifiers=self.identifiers,
            fresh=self.fresh,
            replayed=replayed,
            answer_id=None if served is None else served.id,
            reason=None if failure is None else failure.reason,
            attempts=None if failure is None else failure.attempts,
        )


def ask(
    bank: Bank,
    ledger: Ledger,
    client: TypeSafe,
    names: Sequence[str],
    state: object,
    identifiers: Mapping[str, str | int] | None = None,
    *,
    fresh: bool = False,
) -> dict[str, AskResult]:
    """Answer the questions named over state, by name, recording every ask.

    Raise UnknownQuestionError, or StateError for a state or identifiers the judge
    cannot hash, before anything is asked or recorded.
    """
    questions = _resolve(bank, names)
    digest = state_hash(state)
    ids = dict(identifiers or {})
    canonical_identifiers(ids)
    ledger.record_bank(bank)
    with ledger.read() as reads:
        request = _Request(
            state=state,
            state_hash=digest,
            identifiers=ids,
            fresh=fresh,
            stages={q.name: effective_stage(reads, q) for q in questions},
            recorded={
                q.name: reads.first_answer(replay_key(q.content_key, digest)) for q in questions
            },
        )
    replays = [q for q in questions if not fresh and request.recorded[q.name] is not None]
    misses = [q for q in questions if q not in replays]
    results: dict[str, AskResult] = {}
    for model, group in _by_model(misses).items():
        outcome = client.send(
            model, cast("JSONContent", state), {q.name: q.sdk_question() for q in group}
        )
        with ledger.write() as writes:
            writes.store_state(state)
            results.update(_record(writes, request, group, outcome))
            # Replayed asks share the first transaction.
            results.update(_record_replays(writes, request, replays))
            replays = []
    if replays:
        with ledger.write() as writes:
            writes.store_state(state)
            results.update(_record_replays(writes, request, replays))
    return {q.name: results[q.name] for q in questions}


def _resolve(bank: Bank, names: Sequence[str]) -> list[Question]:
    unknown = [name for name in dict.fromkeys(names) if name not in bank]
    if unknown:
        raise UnknownQuestionError(unknown)
    return [bank[name] for name in dict.fromkeys(names)]


def _by_model(questions: list[Question]) -> dict[str, list[Question]]:
    groups: dict[str, list[Question]] = {}
    for question in questions:
        groups.setdefault(question.model, []).append(question)
    return groups


def _record(
    writes: Writes, request: _Request, group: list[Question], outcome: Reply | Failure
) -> dict[str, AskResult]:
    """Record one SDK request's call, answers and asks; return what each ask is served."""
    if isinstance(outcome, Failure):
        return {q.name: _record_cannot_judge(writes, request, q, outcome) for q in group}
    call_id = writes.insert_call(
        NewCall(
            state_hash=request.state_hash,
            model_asked=group[0].model,
            model=outcome.model,
            response=outcome.raw,
            usage=outcome.usage,
            request_id=outcome.request_id,
            attempts=outcome.attempts,
        )
    )
    results: dict[str, AskResult] = {}
    for question in group:
        answer = outcome.answers[question.name]
        answer_id = writes.insert_answer(
            NewAnswer(
                call_id=call_id,
                question=question.name,
                version=question.version_id,
                content_key=question.content_key,
                replay_key=request.key(question),
                answer=answer,
            )
        )
        served: Answer | None = None
        if not request.fresh:
            # The lowest call for the key: a racer may have committed one first.
            served = writes.first_answer(request.key(question))
        if served is None:
            served = Answer(
                answer_id,
                call_id,
                question.name,
                question.version_id,
                question.model,
                outcome.model,
                answer,
            )
        ask_id = writes.insert_ask(request.new_ask(question, served=served))
        results[question.name] = _answered(question, request, ask_id, served, replayed=False)
    return results


def _record_replays(
    writes: Writes, request: _Request, questions: list[Question]
) -> dict[str, AskResult]:
    results: dict[str, AskResult] = {}
    for question in questions:
        served = cast("Answer", request.recorded[question.name])
        ask_id = writes.insert_ask(request.new_ask(question, served=served, replayed=True))
        results[question.name] = _answered(question, request, ask_id, served, replayed=True)
    return results


def _record_cannot_judge(
    writes: Writes, request: _Request, question: Question, failure: Failure
) -> AskResult:
    ask_id = writes.insert_ask(request.new_ask(question, failure=failure))
    return AskResult(
        ask_id=ask_id,
        version=question.version_id,
        status="cannot_judge",
        replayed=False,
        verdict=None,
        probabilities=None,
        effective_stage=request.stages[question.name],
        declared_stage=question.stage,
        model_asked=question.model,
        model=None,
        reason=failure.reason,
        attempts=failure.attempts,
        default=question.default,
        recorded_answer_exists=request.recorded[question.name] is not None,
    )


def _answered(
    question: Question, request: _Request, ask_id: int, served: Answer, *, replayed: bool
) -> AskResult:
    return AskResult(
        ask_id=ask_id,
        version=question.version_id,
        status="answered",
        replayed=replayed,
        verdict=verdict(question, served.answer),
        probabilities=probabilities(served.answer),
        effective_stage=request.stages[question.name],
        declared_stage=question.stage,
        model_asked=served.model_asked,
        model=served.model,
    )


def verdict(question: Question, answer: Mapping[str, JSON]) -> Verdict:
    """Return the verdict of a recorded answer under the question's current bands (KTD9)."""
    match question.spec:
        case NoulSpec(bands=bands):
            p = cast("float", answer["noul"])
            if p >= bands.yes_at:
                return "yes"
            return "no" if p <= bands.no_at else UNCERTAIN
        case ChoiceSpec(bands=bands):
            if cast("float", answer["confidence"]) < bands.floor:
                return UNCERTAIN
            return cast("str", answer["choice"])
        case ScoreSpec(bands=bands):
            if cast("float", answer["confidence"]) < bands.floor:
                return UNCERTAIN
            # The most likely level, the one TypeSafe's confidence is measured around.
            return top_level(cast("dict[str, float]", answer["probabilities"]))


def top_level(levels: Mapping[str, float]) -> int:
    """Return a score's most likely level, from its probability per level; the lowest wins a tie."""
    return min((-p, int(level)) for level, p in levels.items())[1]


def probabilities(answer: Mapping[str, JSON]) -> Probabilities:
    """Return a recorded answer's raw probabilities."""
    if answer["type"] == "noul":
        return cast("float", answer["noul"])
    return cast("dict[str, float]", answer["probabilities"])


def effective_stage(reads: Reads, question: Question) -> Stage:
    """Return the stage a question's version has earned: its declared stage, or shadow (KTD11)."""
    declared = question.stage
    return declared if _holds(reads, question.name, question.version_id, declared) else Stage.SHADOW


def trusted_version(reads: Reads, name: str, version: str, stage: Stage) -> str | None:
    """Return the version a recheck of version for stage must come from, or None.

    That is the latest earlier version whose effective stage, under the last stage the
    ledger recorded for it, is at or above stage.
    """
    versions = reads.versions(name)
    for earlier in reversed(versions[: versions.index(version)]):
        last = Stage(reads.observed_stages(name, earlier)[-1])
        if last >= stage and _holds(reads, name, earlier, last):
            return earlier
    return None


def _holds(reads: Reads, name: str, version: str, stage: Stage) -> bool:
    """Tell whether a recorded version has earned stage."""
    if stage is Stage.SHADOW:
        return True
    versions = reads.versions(name)
    # Declared on first sight: the first version, its stage not raised since.
    if versions[0] == version and stage <= Stage(reads.observed_stages(name, version)[0]):
        return True
    if any(Stage(passed) >= stage for passed in reads.passed_calibrations(name, version)):
        return True
    rechecks = reads.passed_rechecks(name, version)
    if not any(Stage(passed) >= stage for _, passed in rechecks):
        return False
    trusted = trusted_version(reads, name, version, stage)
    return any(earlier == trusted and Stage(passed) >= stage for earlier, passed in rechecks)
