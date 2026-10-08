// Package sqlite is the statistics store in a SQLite file, statistics.db in
// crew's data folder, which several crew processes share at once (KTD7).
// It opens the file, creating the folder and migrating it, on its first
// record, so a store that cannot open fails its records and nothing else
// (KTD6).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guard.
var _ port.Statistics = (*Store)(nil)

// settings is the store's config section. It has no key; any key is an
// error.
type settings struct{}

// Factory returns the factory of the store registered as sqlite. It
// validates the section and keeps the folder, and touches no file.
func Factory() port.StatisticsFactory {
	return func(decode port.Decode, dir string) (port.Statistics, error) {
		var s settings
		if err := decode(&s); err != nil {
			return nil, err
		}
		return &Store{dir: dir}, nil
	}
}

// Store is the statistics store in dir's statistics.db. Only one goroutine
// calls it at a time, as port.Statistics says.
type Store struct {
	dir string
	db  *sql.DB // nil until a record opens it
}

const insertProcess = `INSERT INTO processes (id, version, folder, started_at) VALUES (?, ?, ?, ?)`

// Record implements port.Statistics. It opens the store first when no
// record has yet; a failed open fails the record and leaves the store
// unopened, so the next record tries again.
func (s *Store) Record(ctx context.Context, st crew.Statistic) error {
	if s.db == nil {
		db, err := open(ctx, s.dir)
		if err != nil {
			return err
		}
		s.db = db
	}
	p, ok := st.(crew.Process)
	if !ok {
		return fmt.Errorf("record a %T: not a statistic the store knows", st)
	}
	return s.insert(ctx, "the process "+string(p.ID), insertProcess,
		string(p.ID), p.Version, p.Folder, p.Start.UnixMilli())
}

// Close implements port.Statistics: it closes the file when a record opened
// it.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close the statistics store: %w", err)
	}
	return nil
}

// insert runs query with args in a transaction, naming what in its error.
func (s *Store) insert(ctx context.Context, what, query string, args ...any) error {
	err := inTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return errors.Join(err)
	})
	if err != nil {
		return fmt.Errorf("record %s: %w", what, err)
	}
	return nil
}

// inTx runs f in a transaction of db, which the DSN begins immediately, and
// commits it unless f fails.
func inTx(ctx context.Context, db *sql.DB, f func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin a transaction: %w", err)
	}
	if err := f(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit a transaction: %w", err)
	}
	return nil
}
