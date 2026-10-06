"""Canonical bytes and hashes of what the judge sends and records (KTD7).

Every hash is the SHA-256 hex digest of a versioned domain prefix, a NUL byte and
the RFC 8785 (JCS) form of its input, so keys from different domains never meet:

- ``content_key``: the pinned model and the question in the SDK's wire form;
- ``version_id``: the content key, the bands, the default and the bar;
- ``state_hash``: a caller's state;
- ``replay_key``: a content key and a state hash.

States and identifiers are refused, with ``StateError``, when they hold a float
or an integer outside ±(2^53-1): JSON reads ``1.0`` as a float, and a float's
text can change on the way, so a state holding one would not hash stably.
"""

import hashlib
from typing import TYPE_CHECKING, TypeAlias

import rfc8785

if TYPE_CHECKING:
    from collections.abc import Mapping

JSON: TypeAlias = "bool | int | float | str | list[JSON] | dict[str, JSON] | None"

MAX_SAFE_INTEGER = 2**53 - 1
"""The largest integer RFC 8785 carries exactly; its negation is the smallest."""


class StateError(ValueError):
    """A state or identifiers object the judge cannot hash; the message names where."""


def canonical_state(state: object) -> bytes:
    """Return a state's canonical JSON: text, an object or an array, without floats."""
    if not isinstance(state, str | list | dict):
        msg = "state: must be text, an object or an array"
        raise StateError(msg)
    return rfc8785.dumps(_checked(state, "state"))


def state_hash(state: object) -> str:
    """Return the hash of a state's canonical JSON."""
    return _digest("state", canonical_state(state))


def canonical_identifiers(identifiers: object) -> bytes:
    """Return the canonical JSON of a caller's identifiers: a flat object of text and integers."""
    if not isinstance(identifiers, dict):
        msg = "identifiers: must be an object"
        raise StateError(msg)
    for key, value in identifiers.items():
        if not isinstance(key, str):
            msg = f"identifiers: key {key!r} is not text"
            raise StateError(msg)
        if isinstance(value, bool) or not isinstance(value, str | int):
            msg = f"identifiers.{key}: must be text or an integer"
            raise StateError(msg)
    return rfc8785.dumps(_checked(identifiers, "identifiers"))


def content_key(model: str, wire: Mapping[str, JSON]) -> str:
    """Return the key of what is sent: the model and the question's wire form."""
    return _digest("content", rfc8785.dumps({"model": model, "question": dict(wire)}))


def version_id(
    content_key: str, bands: Mapping[str, JSON], default: str | int, bar: Mapping[str, JSON]
) -> str:
    """Return the id of a question version: its content key, bands, default and bar."""
    body = {"content_key": content_key, "bands": dict(bands), "default": default, "bar": dict(bar)}
    return _digest("version", rfc8785.dumps(body))


def replay_key(content_key: str, state_hash: str) -> str:
    """Return the key recorded answers are replayed by."""
    return _digest("replay", rfc8785.dumps([content_key, state_hash]))


def _digest(domain: str, body: bytes) -> str:
    prefix = f"typesafe-judge/{domain}/v1".encode() + b"\x00"
    return hashlib.sha256(prefix + body).hexdigest()


def _checked(value: object, where: str) -> JSON:
    """Return value typed as JSON, or raise StateError naming where it is not."""
    if value is None or isinstance(value, bool | str):
        return value
    if isinstance(value, int):
        if abs(value) > MAX_SAFE_INTEGER:
            msg = f"{where}: {value} is outside ±(2^53-1), the integers JSON carries exactly"
            raise StateError(msg)
        return value
    if isinstance(value, float):
        msg = (
            f"{where}: {value!r} is a float, and the judge takes no floats: JSON reads 1.0"
            " as a float, whose text can change on the way; write an integer or text"
        )
        raise StateError(msg)
    if isinstance(value, list):
        return [_checked(item, f"{where}[{index}]") for index, item in enumerate(value)]
    if isinstance(value, dict):
        checked: dict[str, JSON] = {}
        for key, item in value.items():
            if not isinstance(key, str):
                msg = f"{where}: key {key!r} is not text"
                raise StateError(msg)
            checked[key] = _checked(item, f"{where}.{key}")
        return checked
    msg = f"{where}: a {type(value).__name__} is not a JSON value"
    raise StateError(msg)
