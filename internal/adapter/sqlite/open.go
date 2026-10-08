package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// The store's file in the data folder, and the folder's mode when the store
// makes it: the statistics are the boss's own.
const (
	file    = "statistics.db"
	dirPerm = 0o700
)

// The open's retries of SQLITE_BUSY, which a brand-new file opened by several
// processes at once can return while it switches to WAL: the first wait,
// the longest, and how long they may take in all before the open fails.
const (
	firstWait = 10 * time.Millisecond
	maxWait   = 250 * time.Millisecond
	busyFor   = 5 * time.Second
)

// open opens dir's statistics.db for several writers, creating dir, and
// migrates it. It retries SQLITE_BUSY until busyFor has passed. dir must be
// absolute: an empty or relative one means crew found no data folder.
func open(ctx context.Context, dir string) (*sql.DB, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("no data folder: set XDG_DATA_HOME or HOME")
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("make the data folder: %w", err)
	}
	path := filepath.Join(dir, file)
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open the statistics store %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if err := retryBusy(ctx, func() error { return migrate(ctx, db) }); err != nil {
		return nil, fmt.Errorf("open the statistics store %s: %w", path, errors.Join(err, db.Close()))
	}
	return db, nil
}

// dsn is the driver's name for the database at the absolute path: a file:
// URI that waits for other writers' locks, journals in WAL and begins every
// transaction immediately, so one that reads and then writes waits for the
// lock instead of failing with SQLITE_BUSY.
func dsn(path string) string {
	q := url.Values{
		"_pragma": {"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(NORMAL)"},
		"_txlock": {"immediate"},
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}).String()
}

// retryBusy runs f until it returns something other than SQLITE_BUSY, or
// busyFor has passed, or ctx ends, waiting longer after each try.
func retryBusy(ctx context.Context, f func() error) error {
	deadline := time.Now().Add(busyFor)
	wait := firstWait
	for {
		err := f()
		if !busy(err) || time.Now().Add(wait).After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for the statistics store: %w", errors.Join(err, ctx.Err()))
		case <-time.After(wait):
		}
		wait = min(wait+wait, maxWait)
	}
}

// busy reports whether err is SQLite's SQLITE_BUSY, extended codes included.
func busy(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && e.Code()&0xff == sqlite3.SQLITE_BUSY
}
