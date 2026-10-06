from __future__ import annotations

import hashlib
import random  # noqa: TC003 - Hypothesis reads the tests' annotations at run time

import pytest
from hypothesis import given
from hypothesis import strategies as st

from typesafe_judge.keys import (
    JSON,
    MAX_SAFE_INTEGER,
    StateError,
    canonical_identifiers,
    canonical_state,
    content_key,
    replay_key,
    state_hash,
    version_id,
)

json_scalars = (
    st.none() | st.booleans() | st.text() | st.integers(-MAX_SAFE_INTEGER, MAX_SAFE_INTEGER)
)
json_values = st.recursive(
    json_scalars,
    lambda children: (
        st.lists(children, max_size=4) | st.dictionaries(st.text(), children, max_size=4)
    ),
    max_leaves=12,
)


def shuffled(value: object, rng: random.Random) -> object:
    """Rebuild every object in value with its keys in a random order."""
    if isinstance(value, dict):
        items = list(value.items())
        rng.shuffle(items)
        return {key: shuffled(item, rng) for key, item in items}
    if isinstance(value, list):
        return [shuffled(item, rng) for item in value]
    return value


@given(st.dictionaries(st.text(), json_values, min_size=1, max_size=6), st.randoms())
def test_key_order_in_a_state_changes_no_hash(state: dict[str, object], rng: random.Random) -> None:
    assert state_hash(shuffled(state, rng)) == state_hash(state)


def test_a_state_is_canonical_json() -> None:
    assert canonical_state({"b": [1, "é", None], "a": True}) == (
        '{"a":true,"b":[1,"é",null]}'.encode()
    )


def test_a_state_hash_is_sha256_hex_under_a_versioned_prefix() -> None:
    expected = hashlib.sha256(b"typesafe-judge/state/v1\x00" + b'{"a":1}').hexdigest()
    assert state_hash({"a": 1}) == expected


def test_a_state_holding_one_point_zero_is_refused_with_the_reason() -> None:
    with pytest.raises(StateError) as error:
        canonical_state({"issue": {"weight": 1.0}})
    message = str(error.value)
    assert "state.issue.weight" in message
    assert "float" in message


def test_a_state_holding_two_to_the_53_is_refused() -> None:
    with pytest.raises(StateError, match=r"state\[1\]"):
        canonical_state(["a", 2**53])
    with pytest.raises(StateError):
        canonical_state({"n": -(2**53)})


def test_a_state_holding_the_largest_safe_integer_is_accepted() -> None:
    assert canonical_state({"n": 2**53 - 1}) == b'{"n":9007199254740991}'
    assert canonical_state({"n": -(2**53) + 1}) == b'{"n":-9007199254740991}'


def test_unicode_text_hashes_as_given_without_normalisation() -> None:
    composed = {"title": "café"}
    decomposed = {"title": "café"}
    assert state_hash(composed) != state_hash(decomposed)
    assert canonical_state(decomposed) == '{"title":"café"}'.encode()


@pytest.mark.parametrize(
    ("state", "where"),
    [
        (7, "state"),
        (None, "state"),
        (True, "state"),
        ({"a": {1: "x"}}, "state.a"),
        ({"a": ("tuple",)}, "state.a"),
        ({"a": float("nan")}, "state.a"),
    ],
)
def test_a_state_that_is_not_a_json_text_object_or_array_is_refused(
    state: object, where: str
) -> None:
    with pytest.raises(StateError, match=f"^{where.replace('.', r'\.')}:"):
        canonical_state(state)


def test_a_state_may_be_text_or_an_array() -> None:
    assert canonical_state("the issue body") == b'"the issue body"'
    assert canonical_state([1, 2]) == b"[1,2]"


def test_identifiers_are_a_flat_object_of_strings_and_integers() -> None:
    assert canonical_identifiers({"issue": 203, "repo": "crew"}) == b'{"issue":203,"repo":"crew"}'
    assert canonical_identifiers({}) == b"{}"


@pytest.mark.parametrize(
    ("identifiers", "where"),
    [
        ({"issue": {"number": 203}}, "identifiers.issue"),
        ({"issue": 203.0}, "identifiers.issue"),
        ({"issue": [203]}, "identifiers.issue"),
        ({"issue": True}, "identifiers.issue"),
        ({"issue": None}, "identifiers.issue"),
        ({"issue": 2**53}, "identifiers.issue"),
        ({3: "x"}, "identifiers"),
        (["issue"], "identifiers"),
    ],
)
def test_identifiers_holding_a_nested_value_or_a_float_are_refused(
    identifiers: object, where: str
) -> None:
    with pytest.raises(StateError, match=f"^{where.replace('.', r'\.')}:"):
        canonical_identifiers(identifiers)


def test_the_content_key_is_sha256_of_the_model_and_the_wire_form() -> None:
    wire: dict[str, JSON] = {"type": "score", "criteria": ["low", "high"], "instructions": "How?"}
    body = b'{"model":"jev-1.13.0","question":{"criteria":["low","high"],'
    body += b'"instructions":"How?","type":"score"}}'
    expected = hashlib.sha256(b"typesafe-judge/content/v1\x00" + body).hexdigest()
    assert content_key("jev-1.13.0", wire) == expected


def test_the_model_is_part_of_the_content_key() -> None:
    wire = {"type": "noul", "instructions": "Is it?"}
    assert content_key("jev-1.13.0", wire) != content_key("jev-1.14.0", wire)


def test_the_version_id_covers_the_bands_the_default_and_the_bar() -> None:
    key = content_key("jev-1.13.0", {"type": "noul", "instructions": "Is it?"})
    bands = {"yes_at": 0.7, "no_at": 0.3}
    bar = {"max_error_yes": 0.1, "max_error_no": 0.1, "min_examples": 30, "max_flip_rate": 0.05}
    base = version_id(key, bands, "no", bar)
    assert base != key
    assert version_id(key, {"yes_at": 0.6, "no_at": 0.3}, "no", bar) != base
    assert version_id(key, bands, "uncertain", bar) != base
    assert version_id(key, bands, "no", {**bar, "min_examples": 31}) != base
    assert version_id(key, {"no_at": 0.3, "yes_at": 0.7}, "no", bar) == base


def test_the_replay_key_joins_the_content_key_and_the_state_hash() -> None:
    key = content_key("jev-1.13.0", {"type": "noul", "instructions": "Is it?"})
    one, two = state_hash({"n": 1}), state_hash({"n": 2})
    assert replay_key(key, one) == replay_key(key, one)
    assert replay_key(key, one) != replay_key(key, two)
    assert replay_key(key, one) not in {key, one}
