"""The question bank: `.crew/typesafe.yaml`, loaded into typed questions (KTD7, KTD14).

The file holds one top-level key, ``questions``, a mapping from each question's name
to its definition::

    questions:
      issue_needs_candidate:
        primitive: noul            # noul, choice or score
        instructions: Does `issue` build on `candidate`?
        criteria: {true: "...", false: "..."}   # optional for a noul
        model: jev-1.13.0          # a pinned version, never jev-latest or jev-preview
        bands: {yes_at: 0.7, no_at: 0.3}
        default: no                # yes, no or uncertain
        stage: shadow              # shadow, confirm or act
        bar: {max_error_yes: 0.1, max_error_no: 0.1, min_examples: 30, max_flip_rate: 0.05}

The shapes that depend on the primitive:

- **noul:** ``criteria`` optionally describes ``true`` and ``false``. ``bands`` are
  ``yes_at`` above ``no_at``. ``default`` is ``yes``, ``no`` or ``uncertain``. ``bar``
  has ``max_error_yes`` and ``max_error_no``, the false-yes and false-no rates allowed.
- **choice:** ``criteria`` maps 1 to 255 options to a description or null; no option
  may be named ``uncertain``. ``default`` is an option or ``uncertain``.
- **score:** ``criteria`` lists 2 to 10 levels, scored from 0. ``default`` is a level
  index or ``uncertain``.
- **choice and score:** ``bands`` hold one confidence ``floor``; ``bar`` has one
  ``max_error``, the rate of wrong answers at or above the floor allowed.

Every bar also has ``min_examples`` (states needed on each side of a calibration, and
in a recheck) and ``max_flip_rate`` (verdicts a recheck may flip).

A question's content key covers its model and its wire form; its version id adds the
bands, the default and the bar. Its stage and its name enter neither.
"""

import re
import threading
from collections.abc import Iterator, Mapping
from dataclasses import dataclass
from enum import Enum
from functools import total_ordering
from typing import TYPE_CHECKING, Annotated, Literal, Self

import rfc8785
import yaml
from pydantic import (
    BaseModel,
    ConfigDict,
    Field,
    PlainValidator,
    ValidationError,
    ValidationInfo,
    field_validator,
    model_validator,
)
from typesafe_sdk import Choice, JSONContent, Noul, NoulCriteria, Score

from typesafe_judge.keys import JSON, content_key, version_id

if TYPE_CHECKING:
    from pathlib import Path

    from pydantic_core import ErrorDetails

UNCERTAIN: Literal["uncertain"] = "uncertain"
"""The verdict, and the default, when an answer falls in no band."""

UNPINNED_MODELS = frozenset({"jev-latest", "jev-preview"})


@total_ordering
class Stage(Enum):
    """How far a question's answers are trusted, ordered shadow < confirm < act."""

    SHADOW = "shadow"
    CONFIRM = "confirm"
    ACT = "act"

    def __lt__(self, other: object) -> bool:
        """Compare by position in the stage order."""
        if not isinstance(other, Stage):
            return NotImplemented
        order = list(Stage)
        return order.index(self) < order.index(other)


class BankError(Exception):
    """A bank that cannot be loaded; the message names the file, the question and the field."""

    def __init__(
        self, source: str, reason: str, *, question: str | None = None, field: str | None = None
    ) -> None:
        """Keep where the bank is wrong, and say it in one line."""
        where = source
        if question is not None:
            where += f": question {question}"
        if field is not None:
            where += f", field {field}" if question is not None else f": field {field}"
        super().__init__(f"{where}: {reason}")
        self.source = source
        self.question = question
        self.field = field


def _content(value: object) -> JSONContent:
    if not isinstance(value, str | list | dict):
        msg = "must be text, a JSON object or a JSON array"
        raise ValueError(msg)  # noqa: TRY004 - pydantic reports a ValueError as a field error
    try:
        rfc8785.dumps(value)
    except rfc8785.CanonicalizationError as error:
        msg = f"is not plain JSON ({error})"
        raise ValueError(msg) from error
    return value


def _optional_content(value: object) -> JSONContent | None:
    return None if value is None else _content(value)


type Content = Annotated[JSONContent, PlainValidator(_content)]
type OptionalContent = Annotated[JSONContent | None, PlainValidator(_optional_content)]
type Probability = Annotated[float, Field(ge=0.0, le=1.0, allow_inf_nan=False)]
type Rate = Annotated[float, Field(ge=0.0, le=1.0, allow_inf_nan=False)]


class _Strict(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True, frozen=True)


class NoulCriteriaSpec(_Strict):
    """Descriptions of a noul's yes (``true``) and no (``false``) outcomes."""

    true: OptionalContent = None
    false: OptionalContent = None

    @model_validator(mode="before")
    @classmethod
    def _boolean_keys(cls, data: object) -> object:
        # YAML reads the keys `true:` and `false:` as booleans.
        if isinstance(data, dict):
            return {str(k).lower() if isinstance(k, bool) else k: v for k, v in data.items()}
        return data


class NoulBands(_Strict):
    """A noul's verdict bands: yes at or above ``yes_at``, no at or below ``no_at``."""

    yes_at: Probability
    no_at: Probability

    @model_validator(mode="after")
    def _ordered(self) -> Self:
        if self.yes_at <= self.no_at:
            msg = f"yes_at ({self.yes_at}) must be above no_at ({self.no_at})"
            raise ValueError(msg)
        return self


class FloorBands(_Strict):
    """A choice's or score's verdict band: the answer at or above ``floor`` confidence."""

    floor: Probability


class NoulBar(_Strict):
    """The evidence a noul needs to move up a stage."""

    max_error_yes: Rate
    max_error_no: Rate
    min_examples: Annotated[int, Field(ge=1)]
    max_flip_rate: Rate


class FloorBar(_Strict):
    """The evidence a choice or a score needs to move up a stage."""

    max_error: Rate
    min_examples: Annotated[int, Field(ge=1)]
    max_flip_rate: Rate


class _Spec(_Strict):
    instructions: Content
    model: Annotated[str, Field(min_length=1)]
    stage: Annotated[Stage, Field(strict=False)]

    @field_validator("model")
    @classmethod
    def _pinned(cls, model: str) -> str:
        if model in UNPINNED_MODELS:
            msg = f"{model} is an alias that moves; pin a model version such as jev-1.13.0"
            raise ValueError(msg)
        return model


class NoulSpec(_Spec):
    """A yes/no question as the bank declares it."""

    primitive: Literal["noul"]
    criteria: NoulCriteriaSpec | None = None
    bands: NoulBands
    default: Literal["yes", "no", "uncertain"]
    bar: NoulBar


class ChoiceSpec(_Spec):
    """A question that picks one of named options, as the bank declares it."""

    primitive: Literal["choice"]
    criteria: Annotated[dict[str, OptionalContent], Field(min_length=1, max_length=255)]
    bands: FloorBands
    default: str
    bar: FloorBar

    @field_validator("criteria")
    @classmethod
    def _no_uncertain_option(cls, criteria: dict[str, JSONContent | None]) -> object:
        if UNCERTAIN in criteria:
            msg = f"no option may be named {UNCERTAIN}, the verdict outside every band"
            raise ValueError(msg)
        return criteria

    @field_validator("default")
    @classmethod
    def _known_option(cls, default: str, info: ValidationInfo) -> str:
        options = info.data.get("criteria")
        if options is not None and default != UNCERTAIN and default not in options:
            msg = f"{default} is not an option or {UNCERTAIN}"
            raise ValueError(msg)
        return default


class ScoreSpec(_Spec):
    """A question that rates on ordered levels, as the bank declares it."""

    primitive: Literal["score"]
    criteria: Annotated[list[Content], Field(min_length=2, max_length=10)]
    bands: FloorBands
    default: int | Literal["uncertain"]
    bar: FloorBar

    @field_validator("default", mode="plain")
    @classmethod
    def _known_level(cls, default: object, info: ValidationInfo) -> int | Literal["uncertain"]:
        if default == UNCERTAIN:
            return UNCERTAIN
        if isinstance(default, bool) or not isinstance(default, int):
            msg = f"must be a level index (an integer) or {UNCERTAIN}"
            raise ValueError(msg)  # noqa: TRY004 - pydantic reports a ValueError as a field error
        levels = info.data.get("criteria")
        if levels is not None and not 0 <= default < len(levels):
            msg = f"level {default} is not one of the {len(levels)} levels, counted from 0"
            raise ValueError(msg)
        return default


type Spec = NoulSpec | ChoiceSpec | ScoreSpec


class _BankFile(_Strict):
    questions: dict[str, Annotated[Spec, Field(discriminator="primitive")]] = Field(
        default_factory=dict
    )


@dataclass(frozen=True, slots=True)
class Question:
    """A named question of the bank, with the keys of what it sends."""

    name: str
    spec: Spec
    content_key: str
    version_id: str

    @classmethod
    def build(cls, name: str, spec: Spec) -> Question:
        """Return the question named name, with its content key and version id."""
        key = content_key(spec.model, _sdk_question(spec).model_dump(mode="json"))
        bands: dict[str, JSON] = spec.bands.model_dump(mode="json")
        bar: dict[str, JSON] = spec.bar.model_dump(mode="json")
        return cls(name, spec, key, version_id(key, bands, spec.default, bar))

    @property
    def primitive(self) -> Literal["noul", "choice", "score"]:
        """Return noul, choice or score."""
        return self.spec.primitive

    @property
    def model(self) -> str:
        """Return the pinned model version."""
        return self.spec.model

    @property
    def stage(self) -> Stage:
        """Return the stage the bank declares."""
        return self.spec.stage

    @property
    def default(self) -> str | int:
        """Return the conservative verdict for when the judge cannot judge."""
        return self.spec.default

    def sdk_question(self) -> Noul | Choice | Score:
        """Return the SDK's question object, the one the judge sends."""
        return _sdk_question(self.spec)

    def wire(self) -> dict[str, JSON]:
        """Return the question as the SDK sends it."""
        wire: dict[str, JSON] = self.sdk_question().model_dump(mode="json")
        return wire


def _sdk_question(spec: Spec) -> Noul | Choice | Score:
    match spec:
        case NoulSpec():
            # An undescribed outcome is left out, so null and absent send, and hash, alike.
            criteria: NoulCriteria = {}
            if spec.criteria is not None and spec.criteria.true is not None:
                criteria["true"] = spec.criteria.true
            if spec.criteria is not None and spec.criteria.false is not None:
                criteria["false"] = spec.criteria.false
            return Noul(instructions=spec.instructions, criteria=criteria or None)
        case ChoiceSpec():
            return Choice(instructions=spec.instructions, criteria=spec.criteria)
        case ScoreSpec():
            return Score(instructions=spec.instructions, criteria=spec.criteria)


class Bank(Mapping[str, Question]):
    """The questions of one bank file, by name; ``present`` is false when there is no file."""

    def __init__(self, questions: Mapping[str, Question], *, present: bool) -> None:
        """Hold a copy of questions."""
        self._questions = dict(questions)
        self.present = present

    def __getitem__(self, name: str) -> Question:
        """Return the question named name."""
        return self._questions[name]

    def __iter__(self) -> Iterator[str]:
        """Iterate over the names, in the file's order."""
        return iter(self._questions)

    def __len__(self) -> int:
        """Return how many questions the bank holds."""
        return len(self._questions)


def load_bank(path: Path) -> Bank:
    """Load the bank at path; an absent file is an empty bank. Raise BankError when invalid."""
    try:
        text = path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return Bank({}, present=False)
    except (OSError, UnicodeDecodeError) as error:
        raise BankError(str(path), f"cannot read the bank: {error}") from error
    return parse_bank(text, source=str(path))


def parse_bank(text: str, *, source: str) -> Bank:
    """Parse a bank file's text; source names it in errors."""
    try:
        data = _load_yaml(text)
    except yaml.YAMLError as error:
        raise BankError(source, _yaml_reason(error)) from error
    try:
        document = _BankFile.model_validate(data or {})
    except ValidationError as error:
        raise _bank_error(source, error) from error
    questions = {name: Question.build(name, spec) for name, spec in document.questions.items()}
    return Bank(questions, present=True)


class BankFile:
    """A bank file the service watches, reloaded when its modification time or size changes."""

    def __init__(self, path: Path) -> None:
        """Load the bank at path, raising BankError when it is invalid."""
        self._path = path
        self._lock = threading.Lock()
        self._signature = _signature(path)
        self._bank = load_bank(path)
        self._error: BankError | None = None

    def current(self) -> tuple[Bank, BankError | None]:
        """Return the last valid bank, and the error of the last reload when it failed."""
        with self._lock:
            signature = _signature(self._path)
            if signature != self._signature:
                # Taken before reading, so a write that lands during the read reloads again.
                self._signature = signature
                try:
                    self._bank = load_bank(self._path)
                    self._error = None
                except BankError as error:
                    self._error = error
            return self._bank, self._error


def _signature(path: Path) -> tuple[int, int] | None:
    try:
        stat = path.stat()
    except OSError:
        return None
    return stat.st_mtime_ns, stat.st_size


class _Loader(yaml.SafeLoader):
    """YAML's safe loader, with only true and false read as booleans (YAML 1.2).

    YAML 1.1 also reads yes, no, on and off as booleans, which would turn a noul's
    ``default: no`` or a choice's ``yes:`` option into one.
    """


_Loader.yaml_implicit_resolvers = {
    first: [(tag, regexp) for tag, regexp in resolvers if tag != "tag:yaml.org,2002:bool"]
    for first, resolvers in yaml.SafeLoader.yaml_implicit_resolvers.items()
}
_Loader.add_implicit_resolver(
    "tag:yaml.org,2002:bool",
    re.compile(r"^(?:true|True|TRUE|false|False|FALSE)$"),
    list("tTfF"),
)


def _load_yaml(text: str) -> object:
    loader = _Loader(text)
    try:
        return loader.get_single_data()
    finally:
        loader.dispose()


def _yaml_reason(error: yaml.YAMLError) -> str:
    """Return a YAML error on one line, where it is when known."""
    mark = getattr(error, "problem_mark", None)
    problem = getattr(error, "problem", None)
    if mark is None or problem is None:
        return "invalid YAML: " + " ".join(str(error).split())
    return f"invalid YAML at line {mark.line + 1}, column {mark.column + 1}: {problem}"


_REASONS = {"extra_forbidden": "unknown key", "missing": "required"}


def _bank_error(source: str, error: ValidationError) -> BankError:
    """Return the first error of a validation, naming its question and its field."""
    details = error.errors()
    first = details[0]
    reason = _reason(first)
    if len(details) > 1:
        reason += f" (and {len(details) - 1} more)"
    loc = list(first["loc"])
    if len(loc) < 2 or loc[0] != "questions":  # noqa: PLR2004 - ("questions", name, ...)
        return BankError(source, reason, field=_field(loc) or None)
    question = str(loc[1])
    if first["type"] in {"union_tag_invalid", "union_tag_not_found"}:
        return BankError(source, reason, question=question, field="primitive")
    # loc[2] is the primitive that selected the question's model.
    return BankError(source, reason, question=question, field=_field(loc[3:]) or None)


def _reason(detail: ErrorDetails) -> str:
    if detail["type"] in _REASONS:
        return _REASONS[detail["type"]]
    if detail["type"] == "union_tag_not_found":
        return "required: noul, choice or score"
    if detail["type"] == "value_error":
        return str(detail.get("ctx", {}).get("error", detail["msg"]))
    return detail["msg"]


def _field(loc: list[int | str]) -> str:
    field = ""
    for part in loc:
        field += f"[{part}]" if isinstance(part, int) else f".{part}" if field else str(part)
    return field
