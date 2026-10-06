"""Calibration: bands proposed from labelled states on dev, judged on held-out (KTD12).

``POST /v1/calibrations``, admin scope::

    request:  {"question": name, "split_key"?: identifier key, "strong_only"?: bool}
    response: {"calibration": {id, question, version, stage, seed, split_key, strong_only,
               result, proposed_bands, dev: {side: {threshold, examples, errors,
               upper_bound}}, held_out: {proposed, current}, counts: {rows, unlabelled,
               labels: {strong, weak}, excluded: {reason: n}, dev, held_out},
               calibrations: {version, question}}}

**Rows.** One per distinct state the version's content answered, read through its first
answer, the one replays serve. A state takes one label from the outcomes recorded for the
question's name on it: the latest strong one, else the latest weak one. A label counts
only when the version its ask was asked under has the version's primitive
(``other_primitive``) and its options or levels (``other_options_or_levels``);
``strong_only`` drops states with weak labels only (``weak_only``).

**Split.** By the caller identifier ``split_key`` names, else by state hash. A state is
excluded when one of its asks lacks the key (``no_split_key``) or two disagree on it
(``split_key_disagrees``); the asks a recheck records carry only their marker and are
ignored. A group is held out when a keyed hash of the question's seed, derived from its
name, and the group value falls under HELD_OUT, so a group keeps its side as labels
accumulate.

**Sides.** A noul has two: ``yes``, the false yeses among states at or above ``yes_at``,
and ``no``, the false noes at or below ``no_at``. A choice or a score has one, ``floor``:
the answers at or above the confidence floor whose option, or most likely level, is not
the label. A side passes a threshold when the exact one-sided 95% upper bound
(Clopper-Pearson) on its error rate is within the bar's maximum.

**Proposal, on dev only.** A side's candidates are its distinct dev scores, scanned strict
to lax. The proposal is the laxest threshold of the run of passes that starts at the
first pass: the first failure after it stops the scan. Failures before it do not, because
too few states pass a strict threshold to bound their errors. Bands whose sides found no
threshold, or whose ``yes_at`` is not above ``no_at``, are not proposed.

**Held-out.** The proposed and the current bands are each scored once. A side with fewer
than ``min_examples`` states in its band is ``insufficient``, with the count ``needed``;
otherwise it is ``passed`` within the bar, else ``failed``. Bands fail when a side fails,
are insufficient when a side is, and pass otherwise. The calibration's result is the
current bands', recorded for the version's declared stage: a pass gives the version that
stage (KTD11), so adopting proposed bands makes a version that must pass its own
calibration. ``calibrations`` counts this version's runs and the question name's, this one
included, since they all score the same held-out groups. Calibration never writes the
bank.
"""

import hashlib
import hmac
import math
from collections import Counter
from dataclasses import dataclass, field
from typing import TYPE_CHECKING, Literal, TypeAlias, cast

import rfc8785

from typesafe_judge.asking import UnknownQuestionError
from typesafe_judge.bank import NoulSpec
from typesafe_judge.evidence import NewCalibration
from typesafe_judge.keys import replay_key
from typesafe_judge.recheck import RECHECK_MARKER
from typesafe_judge.records import InvalidRequestError

if TYPE_CHECKING:
    from collections.abc import Mapping

    from typesafe_judge.bank import Bank, Question
    from typesafe_judge.evidence import Label
    from typesafe_judge.keys import JSON
    from typesafe_judge.ledger import Answer, AskRecord, Ledger, Reads, VersionRecord

HELD_OUT = 0.4
"""The share of groups held out, as in TypeSafe's cookbook split of 1,200 dev and 800 held-out."""

CONFIDENCE = 0.95
"""The confidence of every upper bound on an error rate."""

_BISECTIONS = 50
"""Halvings of [0, 1] when solving for a bound; fewer than 53, so the midpoint never reaches 1."""

_BAND = {"yes": "yes_at", "no": "no_at", "floor": "floor"}

Side: TypeAlias = Literal["dev", "held_out"]
Result: TypeAlias = Literal["passed", "failed", "insufficient"]
Group: TypeAlias = str | int


@dataclass(frozen=True, slots=True)
class Calibration:
    """A recorded calibration: its id, result, proposed bands, report, and each state's side."""

    id: int
    result: Result
    proposed_bands: dict[str, float] | None
    report: dict[str, JSON]
    sides: dict[str, Side]
    """The side of each state calibrated, by state hash; the report carries only counts."""

    def to_json(self) -> dict[str, JSON]:
        """Return the calibration as the calibrations endpoint answers it."""
        return {"id": self.id, **self.report}


@dataclass(frozen=True, slots=True)
class _Row:
    state_hash: str
    answer: dict[str, JSON]
    label: Label


@dataclass(frozen=True, slots=True)
class _Points:
    """One side's states: (score, wrong) pairs, the score signed so its band is at or above."""

    points: list[tuple[float, bool]]
    max_error: float
    sign: int


@dataclass(slots=True)
class _Counts:
    rows: int = 0
    unlabelled: int = 0
    excluded: Counter[str] = field(default_factory=Counter)


def calibrate(
    bank: Bank,
    ledger: Ledger,
    name: str,
    split_key: str | None = None,
    *,
    strong_only: bool = False,
) -> Calibration:
    """Calibrate the bank's version of the question named name, and record the result.

    Raise UnknownQuestionError for a name the bank lacks; nothing is recorded then.
    """
    if name not in bank:
        raise UnknownQuestionError([name])
    question = bank[name]
    ledger.record_bank(bank)
    counts = _Counts()
    with ledger.read() as reads:
        records = reads.asks(name, {})
        rows = _rows(reads, question, records, counts, strong_only=strong_only)
    groups = _groups(records, rows, split_key, counts)
    seed = seed_of(name)
    sides = {digest: _side(seed, group) for digest, group in groups.items()}
    used = [row for row in rows if row.state_hash in sides]
    dev = _sides(question, [row for row in used if sides[row.state_hash] == "dev"])
    held = _sides(question, [row for row in used if sides[row.state_hash] == "held_out"])
    scans = {side: _scan(points) for side, points in dev.items()}
    proposed = _proposal(question, scans)
    min_examples = question.spec.bar.min_examples
    current = _score(held, question.spec.bands.model_dump(), min_examples)
    report: dict[str, JSON] = {
        "question": name,
        "version": question.version_id,
        "stage": question.stage.value,
        "seed": seed,
        "split_key": split_key,
        "strong_only": strong_only,
        "result": current["result"],
        "proposed_bands": cast("JSON", proposed),
        "dev": cast("dict[str, JSON]", scans),
        "held_out": {
            "proposed": None if proposed is None else _score(held, proposed, min_examples),
            "current": current,
        },
        "counts": _count(counts, used, sides),
    }
    return _record(ledger, question, report, sides)


def _record(
    ledger: Ledger, question: Question, report: dict[str, JSON], sides: dict[str, Side]
) -> Calibration:
    result = cast("Result", report["result"])
    proposed = cast("dict[str, float] | None", report["proposed_bands"])
    with ledger.write() as writes:
        earlier = writes.calibrations(question.name)
        report["calibrations"] = {
            "version": 1 + sum(1 for e in earlier if e.version == question.version_id),
            "question": 1 + len(earlier),
        }
        calibration_id = writes.insert_calibration(
            NewCalibration(
                question=question.name,
                version=question.version_id,
                stage=question.stage.value,
                seed=cast("str", report["seed"]),
                split_key=cast("str | None", report["split_key"]),
                strong_only=cast("bool", report["strong_only"]),
                result=result,
                proposed_bands=proposed,
                report=report,
            )
        )
    return Calibration(calibration_id, result, proposed, report, sides)


def _rows(
    reads: Reads,
    question: Question,
    records: list[AskRecord],
    counts: _Counts,
    *,
    strong_only: bool,
) -> list[_Row]:
    """Return a labelled row per distinct state the version's content answered."""
    labels: dict[str, list[Label]] = {}
    for label in reads.labels(question.name):
        labels.setdefault(label.state_hash, []).append(label)
    mismatches: dict[str, str | None] = {}
    rows: list[_Row] = []
    for digest in dict.fromkeys(
        r.state_hash for r in records if r.content_key == question.content_key
    ):
        counts.rows += 1
        state_labels = labels.get(digest, [])
        for label in state_labels:
            if label.version not in mismatches:
                mismatches[label.version] = _mismatch(reads, question, label.version)
        chosen = _label(state_labels, mismatches, counts, strong_only=strong_only)
        if chosen is not None:
            first = cast("Answer", reads.first_answer(replay_key(question.content_key, digest)))
            rows.append(_Row(digest, first.answer, chosen))
    return rows


def _label(
    labels: list[Label], mismatches: dict[str, str | None], counts: _Counts, *, strong_only: bool
) -> Label | None:
    """Return a state's label, or None, counting why it has none."""
    if not labels:
        counts.unlabelled += 1
        return None
    usable = [label for label in labels if mismatches[label.version] is None]
    if not usable:
        counts.excluded[cast("str", mismatches[labels[-1].version])] += 1
        return None
    strong = [label for label in usable if label.strength == "strong"]
    if strong_only and not strong:
        counts.excluded["weak_only"] += 1
        return None
    return (strong or usable)[-1]


def _mismatch(reads: Reads, question: Question, version: str) -> str | None:
    """Return why labels asked under version do not carry to question, or None when they do."""
    record = cast("VersionRecord", reads.version(question.name, version))
    if record.primitive != question.primitive:
        return "other_primitive"
    current = question.spec.model_dump(mode="json").get("criteria")
    if _options(record.primitive, record.definition.get("criteria")) != _options(
        question.primitive, current
    ):
        return "other_options_or_levels"
    return None


def _options(primitive: str, criteria: object) -> object:
    """Return what a label names: a choice's options, in any order, or a score's levels."""
    if primitive == "choice":
        return sorted(cast("dict[str, JSON]", criteria))
    return criteria if primitive == "score" else None


def _groups(
    records: list[AskRecord], rows: list[_Row], split_key: str | None, counts: _Counts
) -> dict[str, Group]:
    """Return each row's group by state hash, leaving out the rows split_key cannot place."""
    if split_key is None:
        return {row.state_hash: row.state_hash for row in rows}
    held: dict[str, list[dict[str, str | int]]] = {}
    for record in records:
        if RECHECK_MARKER not in record.identifiers:
            held.setdefault(record.state_hash, []).append(record.identifiers)
    groups: dict[str, Group] = {}
    for row in rows:
        identifiers = held.get(row.state_hash, [])
        if not identifiers or any(split_key not in ids for ids in identifiers):
            counts.excluded["no_split_key"] += 1
        elif len({rfc8785.dumps(ids[split_key]) for ids in identifiers}) > 1:
            counts.excluded["split_key_disagrees"] += 1
        else:
            groups[row.state_hash] = identifiers[0][split_key]
    return groups


def seed_of(name: str) -> str:
    """Return the seed of the question named name, which decides each group's side."""
    return hashlib.sha256(b"typesafe-judge/calibration-seed/v1\x00" + name.encode()).hexdigest()


def _side(seed: str, group: Group) -> Side:
    digest = hmac.digest(bytes.fromhex(seed), rfc8785.dumps(group), "sha256")
    return "held_out" if int.from_bytes(digest[:8]) / 2**64 < HELD_OUT else "dev"


def _sides(question: Question, rows: list[_Row]) -> dict[str, _Points]:
    spec = question.spec
    if isinstance(spec, NoulSpec):
        p = [(cast("float", row.answer["noul"]), row.label.value) for row in rows]
        return {
            "yes": _Points([(x, label is False) for x, label in p], spec.bar.max_error_yes, 1),
            "no": _Points([(-x, label is True) for x, label in p], spec.bar.max_error_no, -1),
        }
    points = [
        (cast("float", r.answer["confidence"]), _chosen(r.answer) != r.label.value) for r in rows
    ]
    return {"floor": _Points(points, spec.bar.max_error, 1)}


def _chosen(answer: Mapping[str, JSON]) -> JSON:
    """Return a choice's option, or a score's most likely level, the lowest winning a tie."""
    if answer["type"] == "choice":
        return answer["choice"]
    levels = cast("dict[str, float]", answer["probabilities"])
    return min((-p, int(level)) for level, p in levels.items())[1]


def _scan(side: _Points) -> dict[str, JSON]:
    """Return the threshold a side's dev states allow, with its counts and bound."""
    chosen: tuple[float, int, int] | None = None
    examples = errors = 0
    ordered = sorted(side.points, key=lambda point: point[0], reverse=True)
    for index, (score, wrong) in enumerate(ordered):
        examples += 1
        errors += wrong
        if index + 1 < len(ordered) and ordered[index + 1][0] == score:
            continue
        if _within(errors, examples, side.max_error):
            chosen = (score, examples, errors)
        elif chosen is not None:
            break
    if chosen is None:
        return {"threshold": None}
    score, examples, errors = chosen
    return {
        "threshold": side.sign * score,
        "examples": examples,
        "errors": errors,
        "upper_bound": upper_bound(errors, examples),
    }


def _proposal(question: Question, scans: dict[str, dict[str, JSON]]) -> dict[str, float] | None:
    thresholds = {_BAND[side]: scan["threshold"] for side, scan in scans.items()}
    if any(t is None for t in thresholds.values()):
        return None
    bands = cast("dict[str, float]", thresholds)
    if question.primitive == "noul" and bands["yes_at"] <= bands["no_at"]:
        return None
    return bands


def _score(
    sides: dict[str, _Points], bands: Mapping[str, float], min_examples: int
) -> dict[str, JSON]:
    """Return how bands do on a side's held-out states, side by side."""
    results = {
        name: _held_out(side, bands[_BAND[name]], min_examples) for name, side in sides.items()
    }
    found = {result["result"] for result in results.values()}
    overall = next((r for r in ("failed", "insufficient") if r in found), "passed")
    return {"result": overall, "sides": cast("dict[str, JSON]", results)}


def _held_out(side: _Points, threshold: float, min_examples: int) -> dict[str, JSON]:
    wrong = [w for score, w in side.points if score >= side.sign * threshold]
    examples, errors = len(wrong), sum(wrong)
    if examples < min_examples:
        result: dict[str, JSON] = {"upper_bound": None, "result": "insufficient"}
        result["needed"] = min_examples - examples
    else:
        passed = _within(errors, examples, side.max_error)
        result = {"upper_bound": upper_bound(errors, examples), "needed": 0}
        result["result"] = "passed" if passed else "failed"
    return {"examples": examples, "errors": errors, **result}


def _count(counts: _Counts, used: list[_Row], sides: dict[str, Side]) -> JSON:
    strengths = Counter(row.label.strength for row in used)
    placed = Counter(sides.values())
    return {
        "rows": counts.rows,
        "unlabelled": counts.unlabelled,
        "labels": {"strong": strengths["strong"], "weak": strengths["weak"]},
        "excluded": cast("dict[str, JSON]", dict(counts.excluded)),
        "dev": placed["dev"],
        "held_out": placed["held_out"],
    }


def upper_bound(errors: int, examples: int) -> float:
    """Return the exact one-sided upper confidence bound on an error rate (Clopper-Pearson).

    That is the rate at which seeing errors or fewer in examples has probability
    1 - CONFIDENCE, solved by bisection.
    """
    low, high = 0.0, 1.0
    for _ in range(_BISECTIONS):
        middle = (low + high) / 2
        if _binomial_cdf(errors, examples, middle) > 1 - CONFIDENCE:
            low = middle
        else:
            high = middle
    return high


def _within(errors: int, examples: int, max_error: float) -> bool:
    """Tell whether the upper bound on errors in examples is within max_error."""
    # The bound is above 0 for any example, and never above 1.
    if not 0 < max_error < 1:
        return max_error >= 1
    # The probability of errors or fewer falls as the rate rises, so one evaluation suffices.
    return _binomial_cdf(errors, examples, max_error) <= 1 - CONFIDENCE


def _binomial_cdf(k: int, n: int, p: float) -> float:
    """Return the probability of k or fewer successes in n trials of probability p, 0 < p < 1."""
    log_p, log_q = math.log(p), math.log1p(-p)
    log_n = math.lgamma(n + 1)
    return math.fsum(
        math.exp(log_n - math.lgamma(i + 1) - math.lgamma(n - i + 1) + i * log_p + (n - i) * log_q)
        for i in range(k + 1)
    )


def parse_calibration(body: Mapping[str, object]) -> tuple[str, str | None, bool]:
    """Return a calibrations request's question, split key and strong_only, or refuse it."""
    name, split_key = body["question"], body.get("split_key")
    strong_only = body.get("strong_only", False)
    if not isinstance(name, str):
        msg = "question: must be text"
        raise InvalidRequestError(msg)
    if split_key is not None and (not isinstance(split_key, str) or not split_key):
        msg = "split_key: must be non-empty text, an identifier key"
        raise InvalidRequestError(msg)
    if not isinstance(strong_only, bool):
        msg = "strong_only: must be true or false"
        raise InvalidRequestError(msg)
    return name, split_key, strong_only
