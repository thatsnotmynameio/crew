import os
import random  # noqa: TC003 - Hypothesis reads the tests' annotations at run time
from typing import TYPE_CHECKING

import pytest
import typesafe_sdk
import yaml
from hypothesis import given
from hypothesis import strategies as st

from typesafe_judge.bank import (
    Bank,
    BankError,
    BankFile,
    ChoiceSpec,
    FloorBands,
    FloorBar,
    NoulBands,
    NoulBar,
    NoulSpec,
    ScoreSpec,
    Stage,
    load_bank,
    parse_bank,
)

if TYPE_CHECKING:
    from pathlib import Path

NOUL = """\
  issue_needs_candidate:
    primitive: noul
    instructions: Does the work `issue` asks for build on the change `candidate` asks for?
    criteria:
      true: "`issue` needs something `candidate` adds or changes."
      false: "`issue` can be built and merged without `candidate`."
    model: jev-1.13.0
    bands: {yes_at: 0.7, no_at: 0.3}
    default: no
    stage: shadow
    bar: {max_error_yes: 0.1, max_error_no: 0.2, min_examples: 30, max_flip_rate: 0.05}
"""
INSTRUCTIONS = (
    "    instructions: Does the work `issue` asks for build on the change `candidate` asks for?\n"
)
CHOICE = """\
  issue_kind:
    primitive: choice
    instructions: What kind of work does the issue ask for?
    criteria:
      feature: Something new for users.
      fix: A correction of wrong behaviour.
      chore: null
    model: jev-1.13.0
    bands: {floor: 0.6}
    default: uncertain
    stage: confirm
    bar: {max_error: 0.1, min_examples: 20, max_flip_rate: 0.1}
"""
SCORE = """\
  issue_size:
    primitive: score
    instructions: How large is the change the issue asks for?
    criteria: [A one-line change., A change to one package., A change across packages.]
    model: jev-1.13.0
    bands: {floor: 0.5}
    default: 1
    stage: act
    bar: {max_error: 0.15, min_examples: 25, max_flip_rate: 0.02}
"""


def bank_text(*questions: str) -> str:
    return "questions:\n" + "".join(questions)


def write(path: Path, text: str) -> Path:
    path.write_text(text, encoding="utf-8")
    return path


def test_a_bank_with_a_noul_a_choice_and_a_score_loads_with_typed_fields(tmp_path: Path) -> None:
    bank = load_bank(write(tmp_path / "typesafe.yaml", bank_text(NOUL, CHOICE, SCORE)))

    assert bank.present
    assert list(bank) == ["issue_needs_candidate", "issue_kind", "issue_size"]

    noul = bank["issue_needs_candidate"]
    assert noul.name == "issue_needs_candidate"
    assert noul.primitive == "noul"
    assert noul.model == "jev-1.13.0"
    assert noul.stage is Stage.SHADOW
    assert noul.default == "no"
    assert isinstance(noul.spec, NoulSpec)
    assert noul.spec.bands == NoulBands(yes_at=0.7, no_at=0.3)
    assert noul.spec.bar == NoulBar(
        max_error_yes=0.1, max_error_no=0.2, min_examples=30, max_flip_rate=0.05
    )
    assert noul.spec.criteria is not None
    assert noul.spec.criteria.true == "`issue` needs something `candidate` adds or changes."

    choice = bank["issue_kind"]
    assert isinstance(choice.spec, ChoiceSpec)
    assert choice.spec.criteria == {
        "feature": "Something new for users.",
        "fix": "A correction of wrong behaviour.",
        "chore": None,
    }
    assert choice.spec.bands == FloorBands(floor=0.6)
    assert choice.spec.bar == FloorBar(max_error=0.1, min_examples=20, max_flip_rate=0.1)
    assert choice.default == "uncertain"
    assert choice.stage is Stage.CONFIRM

    score = bank["issue_size"]
    assert isinstance(score.spec, ScoreSpec)
    assert len(score.spec.criteria) == 3
    assert score.default == 1
    assert score.stage is Stage.ACT


def test_each_question_carries_the_sdk_question_and_its_wire_form(tmp_path: Path) -> None:
    bank = load_bank(write(tmp_path / "typesafe.yaml", bank_text(NOUL, CHOICE, SCORE)))

    noul = bank["issue_needs_candidate"].sdk_question()
    assert isinstance(noul, typesafe_sdk.Noul)
    assert isinstance(bank["issue_kind"].sdk_question(), typesafe_sdk.Choice)
    assert isinstance(bank["issue_size"].sdk_question(), typesafe_sdk.Score)
    assert bank["issue_kind"].wire() == {
        "type": "choice",
        "instructions": "What kind of work does the issue ask for?",
        "criteria": {
            "feature": "Something new for users.",
            "fix": "A correction of wrong behaviour.",
            "chore": None,
        },
    }
    assert bank["issue_needs_candidate"].wire() == noul.model_dump(mode="json")


def test_an_absent_bank_file_loads_as_an_empty_bank(tmp_path: Path) -> None:
    bank = load_bank(tmp_path / "typesafe.yaml")
    assert len(bank) == 0
    assert not bank.present


def test_a_score_may_default_to_uncertain() -> None:
    bank = parse_bank(bank_text(SCORE.replace("default: 1", "default: uncertain")), source="b")
    assert bank["issue_size"].default == "uncertain"


def test_an_empty_bank_file_is_an_empty_bank(tmp_path: Path) -> None:
    bank = load_bank(write(tmp_path / "typesafe.yaml", ""))
    assert len(bank) == 0
    assert bank.present


def test_stages_are_ordered_shadow_confirm_act() -> None:
    assert Stage.SHADOW < Stage.CONFIRM < Stage.ACT
    assert Stage.ACT >= Stage.CONFIRM
    assert sorted([Stage.ACT, Stage.SHADOW, Stage.CONFIRM]) == [
        Stage.SHADOW,
        Stage.CONFIRM,
        Stage.ACT,
    ]
    assert Stage("confirm") is Stage.CONFIRM


def test_stages_do_not_compare_with_text() -> None:
    with pytest.raises(TypeError):
        assert Stage.SHADOW < "act"


@pytest.mark.parametrize(
    ("text", "question", "field", "reason"),
    [
        (
            bank_text(NOUL.replace("    stage: shadow\n", "    stage: shadow\n    owner: me\n")),
            "issue_needs_candidate",
            "owner",
            "unknown key",
        ),
        (
            bank_text(NOUL.replace("    model: jev-1.13.0\n", "")),
            "issue_needs_candidate",
            "model",
            "required",
        ),
        (
            bank_text(NOUL.replace("jev-1.13.0", "jev-latest")),
            "issue_needs_candidate",
            "model",
            "jev-latest",
        ),
        (
            bank_text(NOUL.replace("jev-1.13.0", "jev-preview")),
            "issue_needs_candidate",
            "model",
            "jev-preview",
        ),
        (
            bank_text(NOUL.replace("{yes_at: 0.7, no_at: 0.3}", "{yes_at: 0.7}")),
            "issue_needs_candidate",
            "bands.no_at",
            "required",
        ),
        (
            bank_text(NOUL.replace("{yes_at: 0.7, no_at: 0.3}", "{yes_at: 0.3, no_at: 0.6}")),
            "issue_needs_candidate",
            "bands",
            "yes_at (0.3) must be above no_at (0.6)",
        ),
        (
            bank_text(NOUL.replace("stage: shadow", "stage: production")),
            "issue_needs_candidate",
            "stage",
            "'shadow', 'confirm' or 'act'",
        ),
        (
            bank_text(
                SCORE.replace(
                    "[A one-line change., A change to one package., A change across packages.]",
                    "[A one-line change.]",
                ).replace("default: 1", "default: 0")
            ),
            "issue_size",
            "criteria",
            "at least 2",
        ),
        (
            bank_text(
                CHOICE.replace(
                    "      chore: null\n", "".join(f"      option{n}: null\n" for n in range(254))
                )
            ),
            "issue_kind",
            "criteria",
            "at most 255",
        ),
    ],
    ids=[
        "unknown key",
        "missing model",
        "jev-latest",
        "jev-preview",
        "noul without both bands",
        "yes_at below no_at",
        "unknown stage",
        "score with one level",
        "choice with 256 options",
    ],
)
def test_an_invalid_question_is_refused_naming_the_question_and_the_field(
    tmp_path: Path, text: str, question: str, field: str, reason: str
) -> None:
    path = write(tmp_path / "typesafe.yaml", text)
    with pytest.raises(BankError) as raised:
        load_bank(path)
    error = raised.value
    assert error.question == question
    assert error.field == field
    assert f"question {question}, field {field}:" in str(error)
    assert reason in str(error)
    assert str(path) in str(error)


@pytest.mark.parametrize(
    ("text", "field", "reason"),
    [
        (bank_text(NOUL.replace(INSTRUCTIONS, "")), "instructions", "required"),
        (bank_text(NOUL.replace("primitive: noul", "primitive: rank")), "primitive", "rank"),
        (
            bank_text(NOUL.replace("    primitive: noul\n", "")),
            "primitive",
            "noul, choice or score",
        ),
        (
            bank_text(
                NOUL.replace(
                    NOUL[NOUL.index("    criteria:") : NOUL.index("    model:")],
                    "    criteria: Yes or no.\n",
                )
            ),
            "criteria",
            "valid dictionary",
        ),
        (bank_text(NOUL.replace("default: no", "default: maybe")), "default", "'yes', 'no'"),
        (
            bank_text(CHOICE.replace("default: uncertain", "default: refactor")),
            "default",
            "refactor is not an option",
        ),
        (bank_text(SCORE.replace("default: 1", "default: 3")), "default", "level 3"),
        (bank_text(SCORE.replace("default: 1", "default: true")), "default", "integer"),
        (
            bank_text(CHOICE.replace("      chore: null\n", "      uncertain: null\n")),
            "criteria",
            "uncertain",
        ),
        (bank_text(CHOICE.replace("{floor: 0.6}", "{floor: 1.5}")), "bands.floor", "less than"),
        (bank_text(CHOICE.replace("min_examples: 20", "min_examples: 0")), "bar.min_examples", "1"),
        (
            bank_text(NOUL.replace("max_error_yes: 0.1", "max_error: 0.1")),
            "bar.max_error_yes",
            "required",
        ),
        (
            bank_text(NOUL.replace(INSTRUCTIONS, "    instructions: {weight: .nan}\n")),
            "instructions",
            "JSON",
        ),
        (
            bank_text(NOUL.replace(INSTRUCTIONS, "    instructions: 2024-01-05\n")),
            "instructions",
            "JSON",
        ),
        (
            bank_text(SCORE.replace("A one-line change.", "{size: 99999999999999999}")),
            "criteria[0]",
            "JSON",
        ),
    ],
    ids=[
        "missing instructions",
        "unknown primitive",
        "missing primitive",
        "noul criteria as text",
        "noul default",
        "choice default",
        "score default out of range",
        "score default not an integer",
        "choice option named uncertain",
        "floor above one",
        "no examples",
        "noul bar with a choice field",
        "instructions holding nan",
        "instructions holding a date",
        "score level holding a huge integer",
    ],
)
def test_other_invalid_fields_are_refused_by_name(
    tmp_path: Path, text: str, field: str, reason: str
) -> None:
    with pytest.raises(BankError) as raised:
        load_bank(write(tmp_path / "typesafe.yaml", text))
    assert raised.value.field == field
    assert reason in str(raised.value)


@pytest.mark.parametrize(
    ("text", "reason"),
    [
        ("questions: [a, b]\n", "valid dictionary"),
        ("- questions\n", "valid dictionary"),
        ("questions: {a: 1\n", "invalid YAML at line 2, column 1: expected ',' or '}'"),
        ("questions: !!python/name:os.system\n", "could not determine a constructor"),
        ("questions: \x00\n", "invalid YAML: unacceptable character #x0000"),
        ("rules: {}\n", "unknown key"),
    ],
)
def test_an_invalid_bank_outside_any_question_is_refused(
    tmp_path: Path, text: str, reason: str
) -> None:
    with pytest.raises(BankError) as raised:
        load_bank(write(tmp_path / "typesafe.yaml", text))
    assert raised.value.question is None
    assert reason in str(raised.value)
    assert "\n" not in str(raised.value)


def test_an_unreadable_bank_is_refused(tmp_path: Path) -> None:
    (tmp_path / "typesafe.yaml").mkdir()
    with pytest.raises(BankError, match="cannot read"):
        load_bank(tmp_path / "typesafe.yaml")


def test_a_bank_that_is_not_utf8_is_refused(tmp_path: Path) -> None:
    (tmp_path / "typesafe.yaml").write_bytes(b"questions: {\xff: 1}\n")
    with pytest.raises(BankError, match="cannot read"):
        load_bank(tmp_path / "typesafe.yaml")


def test_yes_and_no_stay_text_and_noul_criteria_take_true_and_false_keys() -> None:
    text = bank_text(CHOICE.replace("feature:", "yes:").replace("fix:", "no:"))
    choice = parse_bank(text, source="bank")["issue_kind"]
    assert isinstance(choice.spec, ChoiceSpec)
    assert list(choice.spec.criteria) == ["yes", "no", "chore"]
    noul = parse_bank(bank_text(NOUL.replace("default: no", "default: yes")), source="bank")
    assert noul["issue_needs_candidate"].default == "yes"


def test_an_undescribed_noul_outcome_hashes_like_a_missing_one() -> None:
    absent = NOUL.replace(
        '      false: "`issue` can be built and merged without `candidate`."\n', ""
    )
    null = NOUL.replace('false: "`issue` can be built and merged without `candidate`."', "false:")
    one = parse_bank(bank_text(absent), source="bank")["issue_needs_candidate"]
    other = parse_bank(bank_text(null), source="bank")["issue_needs_candidate"]
    assert one.content_key == other.content_key
    assert one.wire()["criteria"] == {
        "true": "`issue` needs something `candidate` adds or changes."
    }


def test_a_noul_without_criteria_sends_none() -> None:
    text = NOUL.replace(NOUL[NOUL.index("    criteria:") : NOUL.index("    model:")], "")
    question = parse_bank(bank_text(text), source="bank")["issue_needs_candidate"]
    assert "criteria" not in question.wire()


def test_ae4_editing_instructions_changes_the_content_key_and_the_version_id() -> None:
    before = parse_bank(bank_text(NOUL), source="bank")["issue_needs_candidate"]
    edited = NOUL.replace("asks for build on", "asks for depend on")
    after = parse_bank(bank_text(edited), source="bank")["issue_needs_candidate"]
    assert after.content_key != before.content_key
    assert after.version_id != before.version_id


def test_editing_only_a_band_changes_the_version_id_and_not_the_content_key() -> None:
    before = parse_bank(bank_text(NOUL), source="bank")["issue_needs_candidate"]
    edited = NOUL.replace("yes_at: 0.7", "yes_at: 0.6")
    after = parse_bank(bank_text(edited), source="bank")["issue_needs_candidate"]
    assert after.content_key == before.content_key
    assert after.version_id != before.version_id


def test_editing_the_model_changes_the_content_key() -> None:
    before = parse_bank(bank_text(NOUL), source="bank")["issue_needs_candidate"]
    after = parse_bank(bank_text(NOUL.replace("jev-1.13.0", "jev-1.14.0")), source="bank")
    assert after["issue_needs_candidate"].content_key != before.content_key


def test_editing_the_default_or_the_bar_changes_only_the_version_id() -> None:
    before = parse_bank(bank_text(NOUL), source="bank")["issue_needs_candidate"]
    for edited in (
        NOUL.replace("default: no", "default: uncertain"),
        NOUL.replace("min_examples: 30", "min_examples: 40"),
    ):
        after = parse_bank(bank_text(edited), source="bank")["issue_needs_candidate"]
        assert after.content_key == before.content_key
        assert after.version_id != before.version_id


def test_editing_only_the_stage_changes_neither_the_content_key_nor_the_version_id() -> None:
    before = parse_bank(bank_text(NOUL), source="bank")["issue_needs_candidate"]
    edited = NOUL.replace("stage: shadow", "stage: act")
    after = parse_bank(bank_text(edited), source="bank")["issue_needs_candidate"]
    assert after.stage is Stage.ACT
    assert after.content_key == before.content_key
    assert after.version_id == before.version_id


def test_the_question_name_is_not_part_of_its_keys() -> None:
    before = parse_bank(bank_text(NOUL), source="bank")["issue_needs_candidate"]
    renamed = NOUL.replace("issue_needs_candidate:", "needs:")
    after = parse_bank(bank_text(renamed), source="bank")["needs"]
    assert (after.content_key, after.version_id) == (before.content_key, before.version_id)


# Recorded once from the wire forms above. A change here means the SDK's wire form,
# and so every question's version, changed: review the SDK bump that caused it.
GOLDEN_CONTENT_KEYS = {
    "issue_needs_candidate": "5f9a8e3352e2f8be0e0c76198d49765a91489e06b5e4b7bbf92705f3de83e2c2",
    "issue_kind": "998f9a7350a9f8d6acad160964f8f25045ac58593c9542ad5441227990819f9d",
    "issue_size": "17833b37fa5604edd3bbfb46a89b477546e6692dee6dc4c45efb9c537429c9a7",
}


def test_golden_content_keys_match_so_a_wire_form_change_fails() -> None:
    bank = parse_bank(bank_text(NOUL, CHOICE, SCORE), source="bank")
    assert {name: question.content_key for name, question in bank.items()} == GOLDEN_CONTENT_KEYS


options = st.dictionaries(
    st.text(min_size=1).filter(lambda name: name != "uncertain"),
    st.none() | st.text(),
    min_size=1,
    max_size=8,
)


@given(options, st.randoms())
def test_key_order_in_the_criteria_changes_no_hash(
    criteria: dict[str, str | None], rng: random.Random
) -> None:
    items = list(criteria.items())
    rng.shuffle(items)
    keys = []
    for order in (criteria, dict(items)):
        document = {
            "questions": {
                "q": {
                    "primitive": "choice",
                    "instructions": "Which?",
                    "criteria": order,
                    "model": "jev-1.13.0",
                    "bands": {"floor": 0.5},
                    "default": "uncertain",
                    "stage": "shadow",
                    "bar": {"max_error": 0.1, "min_examples": 10, "max_flip_rate": 0.1},
                }
            }
        }
        question = parse_bank(yaml.safe_dump(document, sort_keys=False), source="bank")["q"]
        keys.append((question.content_key, question.version_id))
    assert keys[0] == keys[1]


def test_an_unchanged_bank_file_is_not_read_again(tmp_path: Path) -> None:
    path = write(tmp_path / "typesafe.yaml", bank_text(NOUL))
    watched = BankFile(path)
    before = path.stat()
    # Same size and modification time, different content: only a re-read would see it.
    write(path, bank_text(NOUL.replace("jev-1.13.0", "jev-1.13.9")))
    os.utime(path, ns=(before.st_atime_ns, before.st_mtime_ns))

    bank, error = watched.current()
    assert error is None
    assert bank["issue_needs_candidate"].model == "jev-1.13.0"


def test_a_changed_bank_file_reloads(tmp_path: Path) -> None:
    path = write(tmp_path / "typesafe.yaml", bank_text(NOUL))
    watched = BankFile(path)
    write(path, bank_text(NOUL.replace("stage: shadow", "stage: confirm")))

    bank, error = watched.current()
    assert error is None
    assert bank["issue_needs_candidate"].stage is Stage.CONFIRM


def test_ae7_a_question_added_to_the_bank_is_found_by_name(tmp_path: Path) -> None:
    path = write(tmp_path / "typesafe.yaml", bank_text(NOUL))
    watched = BankFile(path)
    write(path, bank_text(NOUL, SCORE))

    bank, _ = watched.current()
    assert bank["issue_size"].spec.instructions == "How large is the change the issue asks for?"


def test_an_invalid_reload_keeps_the_last_valid_bank_and_exposes_the_error(
    tmp_path: Path,
) -> None:
    path = write(tmp_path / "typesafe.yaml", bank_text(NOUL))
    watched = BankFile(path)
    valid, _ = watched.current()

    write(path, bank_text(NOUL.replace("jev-1.13.0", "jev-latest")))
    bank, error = watched.current()
    assert bank is valid
    assert error is not None
    assert error.question == "issue_needs_candidate"
    assert error.field == "model"
    assert watched.current() == (valid, error)

    write(path, bank_text(NOUL, CHOICE))
    bank, error = watched.current()
    assert error is None
    assert list(bank) == ["issue_needs_candidate", "issue_kind"]


def test_a_bank_file_that_appears_loads_and_one_that_goes_is_empty(tmp_path: Path) -> None:
    path = tmp_path / "typesafe.yaml"
    watched = BankFile(path)
    bank, _ = watched.current()
    assert not bank.present

    write(path, bank_text(NOUL))
    bank, _ = watched.current()
    assert list(bank) == ["issue_needs_candidate"]

    path.unlink()
    bank, error = watched.current()
    assert (len(bank), bank.present, error) == (0, False, None)


def test_an_invalid_bank_at_start_is_raised(tmp_path: Path) -> None:
    path = write(tmp_path / "typesafe.yaml", bank_text(NOUL.replace("stage: shadow", "stage: x")))
    with pytest.raises(BankError):
        BankFile(path)


def test_a_bank_is_a_read_only_mapping() -> None:
    bank = Bank({}, present=False)
    assert bank.get("missing") is None
    assert "missing" not in bank
