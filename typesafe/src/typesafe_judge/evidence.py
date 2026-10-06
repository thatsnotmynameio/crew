"""The ledger's queries for calibration and the stage report, and the calibration insert.

``EvidenceReads`` and ``EvidenceWrites`` are mixed into ``ledger.Reads`` and
``ledger.Writes``, so they run on the connection and in the transaction of the read or
the write that holds them.
"""

import json
from dataclasses import dataclass
from typing import TYPE_CHECKING, Literal, TypeAlias, cast

from typesafe_judge.keys import canonical_text

if TYPE_CHECKING:
    import sqlite3
    from collections.abc import Mapping

    from typesafe_judge.keys import JSON

StageName: TypeAlias = Literal["shadow", "confirm", "act"]
"""A stage as the ledger stores it: ``Stage.value``."""


@dataclass(frozen=True, slots=True)
class Label:
    """A recorded outcome of a question's state, with the version its ask was asked under."""

    state_hash: str
    version: str
    value: JSON
    strength: Literal["strong", "weak"]


@dataclass(frozen=True, slots=True)
class Evidence:
    """A recorded recheck or calibration of a version: its stage and result.

    from_version is the earlier version of a recheck, and None for a calibration.
    """

    version: str
    stage: StageName
    result: str
    from_version: str | None = None


@dataclass(frozen=True, slots=True)
class NewCalibration:
    """A calibration's result and report, to record."""

    question: str
    version: str
    stage: StageName
    seed: str
    split_key: str | None
    strong_only: bool
    result: Literal["passed", "failed", "insufficient"]
    proposed_bands: Mapping[str, JSON] | None
    report: Mapping[str, JSON]


class EvidenceReads:
    """Queries over the evidence a question's versions gathered."""

    _con: sqlite3.Connection

    def labels(self, name: str) -> list[Label]:
        """Return every outcome recorded on the question's asks, oldest first."""
        rows = self._con.execute(
            "SELECT k.state_hash, k.version, o.value, o.strength"
            " FROM outcomes o JOIN asks k ON k.id = o.ask_id WHERE k.question = ? ORDER BY o.id",
            (name,),
        )
        return [
            Label(digest, version, cast("JSON", json.loads(value)), strength)
            for digest, version, value, strength in rows
        ]

    def calibrations(self, name: str) -> list[Evidence]:
        """Return the calibrations of every version of the question, oldest first."""
        rows = self._con.execute(
            "SELECT version, stage, result FROM calibrations WHERE question = ? ORDER BY id",
            (name,),
        )
        return [Evidence(*row) for row in rows]

    def rechecks(self, name: str, version: str) -> list[Evidence]:
        """Return the rechecks to a version, whatever their result, oldest first."""
        rows = self._con.execute(
            "SELECT to_version, stage, result, from_version FROM rechecks"
            " WHERE question = ? AND to_version = ? ORDER BY id",
            (name, version),
        )
        return [Evidence(*row) for row in rows]

    def acted_in_shadow(self, name: str) -> int:
        """Return how many ``acted`` decisions were recorded on the question's shadow asks."""
        row = self._con.execute(
            "SELECT count(*) FROM decisions d JOIN asks k ON k.id = d.ask_id"
            " WHERE k.question = ? AND k.effective_stage = 'shadow' AND d.decision = 'acted'",
            (name,),
        ).fetchone()
        return cast("int", row[0])


class EvidenceWrites:
    """The calibration insert."""

    _con: sqlite3.Connection

    def insert_calibration(self, calibration: NewCalibration) -> int:
        """Record a calibration and return its id."""
        bands = calibration.proposed_bands
        cursor = self._con.execute(
            "INSERT INTO calibrations (question, version, stage, seed, split_key, strong_only,"
            " result, proposed_bands, report) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
            (
                calibration.question,
                calibration.version,
                calibration.stage,
                calibration.seed,
                calibration.split_key,
                calibration.strong_only,
                calibration.result,
                None if bands is None else canonical_text(dict(bands)),
                canonical_text(dict(calibration.report)),
            ),
        )
        return cast("int", cursor.lastrowid)
