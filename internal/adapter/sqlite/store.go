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
	"time"

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

// The store's statements. A repository keeps its latest name; an issue
// keeps its first sighting; the issue's last label is the label its latest
// move reached, else the one crew first saw it at.
const (
	insertProcess    = `INSERT INTO processes (id, version, folder, started_at) VALUES (?, ?, ?, ?)`
	upsertRepository = `INSERT INTO repositories (tracker, id, name) VALUES (?, ?, ?)
		ON CONFLICT (tracker, id) DO UPDATE SET name = excluded.name`
	insertIssue = `INSERT INTO issues (tracker, repository_id, key, ref, kind, created_at, seen_at, first_label)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (tracker, repository_id, key) DO NOTHING`
	insertMove = `INSERT INTO label_moves
		(tracker, repository_id, issue_key, from_label, to_label, moved_at, seen_at, run_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	selectLastLabel = `SELECT COALESCE(
		(SELECT to_label FROM label_moves WHERE tracker = ? AND repository_id = ? AND issue_key = ? ORDER BY id DESC LIMIT 1),
		(SELECT first_label FROM issues WHERE tracker = ? AND repository_id = ? AND key = ?))`
)

// Record implements port.Statistics. It opens the store first when no
// record has yet; a failed open fails the record and leaves the store
// unopened, so the next record tries again. Each record is one immediate
// transaction, so the store's checks hold across processes writing at once.
func (s *Store) Record(ctx context.Context, st crew.Statistic) error {
	if s.db == nil {
		db, err := open(ctx, s.dir)
		if err != nil {
			return err
		}
		s.db = db
	}
	switch st := st.(type) {
	case crew.Process:
		return s.write(ctx, "the process "+string(st.ID), func(tx *sql.Tx) error {
			return exec(ctx, tx, insertProcess, string(st.ID), st.Version, st.Folder, st.Start.UnixMilli())
		})
	case crew.RepositoryRecord:
		return s.write(ctx, "the repository "+st.Repository.Name, func(tx *sql.Tx) error {
			return exec(ctx, tx, upsertRepository, string(st.Tracker), string(st.Repository.ID), st.Repository.Name)
		})
	case crew.IssueSighting:
		return s.write(ctx, "issue "+st.Ref, func(tx *sql.Tx) error { return sight(ctx, tx, st) })
	case crew.LabelMove:
		return s.write(ctx, "a label move of #"+st.Issue.String(), func(tx *sql.Tx) error { return move(ctx, tx, st) })
	}
	return fmt.Errorf("record a %T: not a statistic the store knows", st)
}

// sight records the sighting si in tx: the issue when the store lacks it.
// When the store has it, and its last label is not the one state si saw,
// the issue moved outside crew while no crew process saw it, such as
// between two processes, and sight records that move at si's time.
func sight(ctx context.Context, tx *sql.Tx, si crew.IssueSighting) error {
	res, err := tx.ExecContext(ctx, insertIssue, string(si.Tracker), string(si.Issue.Repository), si.Issue.Key, si.Ref,
		kind(si.Kind), unixMilli(si.Created), si.Seen.UnixMilli(), text(si.State))
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	state, ok := si.State.Get()
	if inserted > 0 || !ok {
		return nil
	}
	last, err := lastLabel(ctx, tx, si.Tracker, si.Issue)
	if err != nil || !last.Valid || last.String == string(state) {
		return err
	}
	return insertLabelMove(ctx, tx, crew.LabelMove{
		Tracker: si.Tracker, Issue: si.Issue, From: crew.State(last.String), To: state, Seen: si.Seen,
	})
}

// move records m in tx. A move made outside crew is recorded only when the
// issue's last label is not already its To, so two processes that see the
// same move record it once.
func move(ctx context.Context, tx *sql.Tx, m crew.LabelMove) error {
	if _, byRun := m.Run.Get(); !byRun {
		last, err := lastLabel(ctx, tx, m.Tracker, m.Issue)
		if err != nil || (last.Valid && last.String == string(m.To)) {
			return err
		}
	}
	return insertLabelMove(ctx, tx, m)
}

// insertLabelMove inserts m in tx.
func insertLabelMove(ctx context.Context, tx *sql.Tx, m crew.LabelMove) error {
	return exec(ctx, tx, insertMove, string(m.Tracker), string(m.Issue.Repository), m.Issue.Key, string(m.From),
		string(m.To), unixMilli(m.Moved), m.Seen.UnixMilli(), text(m.Run))
}

// lastLabel returns, in tx, the label the issue's latest move reached, else
// the label crew first saw it at; it is null when the store has neither.
func lastLabel(ctx context.Context, tx *sql.Tx, tracker crew.TrackerName, issue crew.IssueID) (sql.NullString, error) {
	var last sql.NullString
	err := tx.QueryRowContext(ctx, selectLastLabel, string(tracker), string(issue.Repository), issue.Key,
		string(tracker), string(issue.Repository), issue.Key).Scan(&last)
	if err != nil {
		return last, fmt.Errorf("read the last label: %w", err)
	}
	return last, nil
}

// kind is how the store writes k.
func kind(k crew.Kind) string {
	if k == crew.KindPullRequest {
		return "pull_request"
	}
	return "issue"
}

// unixMilli is t in Unix milliseconds, or nil, a null, when it is absent.
func unixMilli(t crew.Optional[time.Time]) any {
	if v, ok := t.Get(); ok {
		return v.UnixMilli()
	}
	return nil
}

// text is s as text, or nil, a null, when it is absent.
func text[T ~string](s crew.Optional[T]) any {
	if v, ok := s.Get(); ok {
		return string(v)
	}
	return nil
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

// write runs f in a transaction, naming what in its error.
func (s *Store) write(ctx context.Context, what string, f func(*sql.Tx) error) error {
	if err := inTx(ctx, s.db, f); err != nil {
		return fmt.Errorf("record %s: %w", what, err)
	}
	return nil
}

// exec runs query with args in tx.
func exec(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("insert: %w", err)
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
