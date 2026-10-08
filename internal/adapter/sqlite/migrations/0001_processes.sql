/* tsqllint-disable set-ansi set-quoted-identifier set-transaction-isolation-level data-compression schema-qualify */
-- SQLite has no SQL Server SET options or table compression; its default
-- database needs no schema qualifier. Suppress only these dialect mismatches.

-- One crew process, recorded once when it starts. started_at is Unix
-- milliseconds in UTC.
CREATE TABLE processes (
    id TEXT PRIMARY KEY,
    version TEXT NOT NULL,
    folder TEXT NOT NULL,
    started_at INTEGER NOT NULL
);
