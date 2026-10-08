-- One crew process, recorded once when it starts. started_at is Unix
-- milliseconds in UTC.
CREATE TABLE processes (
    id TEXT PRIMARY KEY,
    version TEXT NOT NULL,
    folder TEXT NOT NULL,
    started_at INTEGER NOT NULL
);
