"""The ledger's tables, with one literal statement per table and use.

SQLite cannot bind a table name, so a test that walks the tables would build
its SQL from a name. These literals keep every statement fixed;
``assert_every_table`` fails as soon as the schema gains or loses a table.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import sqlite3

COUNT = {
    "states": "SELECT count(*) FROM states",
    "question_versions": "SELECT count(*) FROM question_versions",
    "stage_observations": "SELECT count(*) FROM stage_observations",
    "calls": "SELECT count(*) FROM calls",
    "answers": "SELECT count(*) FROM answers",
    "asks": "SELECT count(*) FROM asks",
    "decisions": "SELECT count(*) FROM decisions",
    "outcomes": "SELECT count(*) FROM outcomes",
    "rechecks": "SELECT count(*) FROM rechecks",
    "calibrations": "SELECT count(*) FROM calibrations",
}

UPDATE = {
    "states": "UPDATE states SET rowid = rowid",
    "question_versions": "UPDATE question_versions SET rowid = rowid",
    "stage_observations": "UPDATE stage_observations SET rowid = rowid",
    "calls": "UPDATE calls SET rowid = rowid",
    "answers": "UPDATE answers SET rowid = rowid",
    "asks": "UPDATE asks SET rowid = rowid",
    "decisions": "UPDATE decisions SET rowid = rowid",
    "outcomes": "UPDATE outcomes SET rowid = rowid",
    "rechecks": "UPDATE rechecks SET rowid = rowid",
    "calibrations": "UPDATE calibrations SET rowid = rowid",
}

DELETE = {
    "states": "DELETE FROM states",
    "question_versions": "DELETE FROM question_versions",
    "stage_observations": "DELETE FROM stage_observations",
    "calls": "DELETE FROM calls",
    "answers": "DELETE FROM answers",
    "asks": "DELETE FROM asks",
    "decisions": "DELETE FROM decisions",
    "outcomes": "DELETE FROM outcomes",
    "rechecks": "DELETE FROM rechecks",
    "calibrations": "DELETE FROM calibrations",
}


def assert_every_table(con: sqlite3.Connection) -> None:
    """Fail unless the statements above name exactly the ledger's tables."""
    listed = {
        name
        for (name,) in con.execute(
            "SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%'"
        )
    }
    assert listed == set(COUNT) == set(UPDATE) == set(DELETE), sorted(listed)


def count(con: sqlite3.Connection, table: str) -> int:
    """Count one table's rows."""
    row = con.execute(COUNT[table]).fetchone()
    assert row is not None
    return int(row[0])


def total(con: sqlite3.Connection) -> int:
    """Count the rows of every ledger table."""
    assert_every_table(con)
    return sum(count(con, table) for table in COUNT)
