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
// move reached, else the one crew first saw it at; a span keeps its first
// open and its first end.
const (
	insertProcess    = `INSERT INTO processes (id, version, folder, started_at) VALUES (?, ?, ?, ?)`
	upsertRepository = `INSERT INTO repositories (tracker, id, name) VALUES (?, ?, ?)
		ON CONFLICT (tracker, id) DO UPDATE SET name = excluded.name WHERE name <> excluded.name`
	insertIssue = `INSERT INTO issues (tracker, repository_id, key, ref, kind, created_at, seen_at, first_label)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (tracker, repository_id, key) DO NOTHING`
	insertMove = `INSERT INTO label_moves
		(tracker, repository_id, issue_key, from_label, to_label, moved_at, seen_at, run_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	selectLastLabel = `SELECT COALESCE(
		(SELECT to_label FROM label_moves WHERE tracker = ? AND repository_id = ? AND issue_key = ? ORDER BY id DESC LIMIT 1),
		(SELECT first_label FROM issues WHERE tracker = ? AND repository_id = ? AND key = ?))`
	insertSpan = `INSERT INTO spans (id, kind, parent_id, process_id, started_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`
	insertRuleRunSpan = `INSERT INTO rule_run_spans
		(span_id, tracker, repository_id, issue_key, rule, queue, continues_run_id) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (span_id) DO NOTHING`
	updateSpanEnd    = `UPDATE spans SET ended_at = ?, outcome = ? WHERE id = ? AND ended_at IS NULL`
	updateRuleRunEnd = `UPDATE rule_run_spans SET route = ?, halted = ? WHERE span_id = ?`
	insertWorkSpan   = `INSERT INTO work_spans (span_id, name, kind, verdict) VALUES (?, ?, ?, ?)
		ON CONFLICT (span_id) DO NOTHING`
	insertSessionSpan = `INSERT INTO session_spans
		(span_id, agent, harness, bot, prompt_hash, session_started_at, turns) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (span_id) DO NOTHING`
	insertModelUsage = `INSERT INTO session_tokens
		(span_id, model, input, output, cache_read, cache_write) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (span_id, model) DO NOTHING`
	insertShellSpan    = `INSERT INTO shell_spans (span_id, exit_status) VALUES (?, ?) ON CONFLICT (span_id) DO NOTHING`
	insertFunctionSpan = `INSERT INTO function_spans (span_id, function, verdict) VALUES (?, ?, ?)
		ON CONFLICT (span_id) DO NOTHING`
	insertRouteSpan = `INSERT INTO route_spans (span_id, route, verdict) VALUES (?, ?, ?) ON CONFLICT (span_id) DO NOTHING`
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
	case crew.RuleRunSpan:
		return s.write(ctx, "the span of rule run "+string(st.Run), func(tx *sql.Tx) error { return span(ctx, tx, st) })
	case crew.ActionSpan:
		return s.write(ctx, fmt.Sprintf("the span of action %s of rule run %s", st.Action, st.Run),
			func(tx *sql.Tx) error { return actionSpan(ctx, tx, st) })
	case crew.RouteSpan:
		return s.write(ctx, "the span of the route of rule run "+string(st.Run),
			func(tx *sql.Tx) error { return routeSpan(ctx, tx, st) })
	case crew.StepSpan:
		return s.write(ctx, fmt.Sprintf("the span of step %d of the route of rule run %s", st.Index, st.Run),
			func(tx *sql.Tx) error { return stepSpan(ctx, tx, st) })
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

// span records sp in tx: its rows when the store lacks them, then, when sp
// has an end and the stored span none, that end (KTD5). The rule run's row
// gets the end only when the span did, so both rows hold the same end.
func span(ctx context.Context, tx *sql.Tx, sp crew.RuleRunSpan) error {
	id := string(sp.Run)
	if err := openSpan(ctx, tx, id, "rule_run", nil, sp.Process, sp.Start); err != nil {
		return err
	}
	err := exec(ctx, tx, insertRuleRunSpan, id, string(sp.Tracker), string(sp.Issue.Repository), sp.Issue.Key,
		string(sp.Rule), text(sp.Queue), text(sp.Continues))
	end, ok := sp.End.Get()
	if err != nil || !ok {
		return err
	}
	ended, err := endSpan(ctx, tx, id, end.At, string(end.Outcome))
	if err != nil || !ended {
		return err
	}
	return update(ctx, tx, updateRuleRunEnd, text(end.Route), text(end.Halt), id)
}

// actionSpan records sp, written once with its end, in tx: its rows when
// the store lacks them, and its end when the stored span has none.
func actionSpan(ctx context.Context, tx *sql.Tx, sp crew.ActionSpan) error {
	id := string(sp.ID())
	if err := openSpan(ctx, tx, id, "action", string(sp.Parent()), sp.Process, sp.Start); err != nil {
		return err
	}
	if _, err := endSpan(ctx, tx, id, sp.End, string(sp.Outcome)); err != nil {
		return err
	}
	return work(ctx, tx, id, sp.Action, string(sp.Verdict), sp.Work)
}

// routeSpan records sp in tx: its rows when the store lacks them, then,
// when sp has an end and the stored span none, that end. Nothing else of a
// route changes at its end.
func routeSpan(ctx context.Context, tx *sql.Tx, sp crew.RouteSpan) error {
	id := string(sp.ID())
	if err := openSpan(ctx, tx, id, "route", string(sp.Parent()), sp.Process, sp.Start); err != nil {
		return err
	}
	var verdict any
	if chosen, ok := sp.ChosenBy.Get(); ok {
		verdict = string(chosen.Verdict)
	}
	err := exec(ctx, tx, insertRouteSpan, id, string(sp.Route), verdict)
	end, ok := sp.End.Get()
	if err != nil || !ok {
		return err
	}
	_, err = endSpan(ctx, tx, id, end.At, string(end.Outcome))
	return err
}

// stepSpan records sp, written once with its end, in tx, as actionSpan
// records an action's, with no verdict.
func stepSpan(ctx context.Context, tx *sql.Tx, sp crew.StepSpan) error {
	id := string(sp.ID())
	if err := openSpan(ctx, tx, id, "step", string(sp.Parent()), sp.Process, sp.Start); err != nil {
		return err
	}
	if _, err := endSpan(ctx, tx, id, sp.End, string(sp.Outcome)); err != nil {
		return err
	}
	return work(ctx, tx, id, sp.Name, nil, sp.Work)
}

// openSpan inserts, in tx, the spans row of id, a span of kind under parent,
// nil for none, when the store lacks it.
func openSpan(ctx context.Context, tx *sql.Tx, id, kind string, parent any, process crew.ProcessID,
	start time.Time,
) error {
	return exec(ctx, tx, insertSpan, id, kind, parent, string(process), start.UnixMilli())
}

// endSpan sets, in tx, the end of span id when it has none, and returns
// whether it did.
func endSpan(ctx context.Context, tx *sql.Tx, id string, at time.Time, outcome string) (bool, error) {
	res, err := tx.ExecContext(ctx, updateSpanEnd, at.UnixMilli(), outcome, id)
	if err != nil {
		return false, fmt.Errorf("update: %w", err)
	}
	ended, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("update: %w", err)
	}
	return ended > 0, nil
}

// work records, in tx, what the action or step of span id, named name, did:
// its work_spans row, with verdict, nil for a step's, and its kind's detail.
func work(ctx context.Context, tx *sql.Tx, id string, name crew.ActionName, verdict any, w crew.Work) error {
	insert := func(kind string) error { return exec(ctx, tx, insertWorkSpan, id, string(name), kind, verdict) }
	switch w := w.(type) {
	case crew.SessionWork:
		if err := insert("session"); err != nil {
			return err
		}
		return session(ctx, tx, id, w)
	case crew.ShellWork:
		if err := insert("shell"); err != nil {
			return err
		}
		return exec(ctx, tx, insertShellSpan, id, integer(w.ExitStatus))
	case crew.FunctionWork:
		if err := insert("function"); err != nil {
			return err
		}
		return exec(ctx, tx, insertFunctionSpan, id, string(w.Function), text(w.Verdict))
	case crew.QuestionWork:
		return insert("question")
	case crew.CheckWork:
		return insert("check")
	}
	return fmt.Errorf("a %T is not a work the store knows", w)
}

// session records, in tx, the session of span id: its row and one row per
// model its harness reported tokens for.
func session(ctx context.Context, tx *sql.Tx, id string, w crew.SessionWork) error {
	var bot any
	if w.Bot != "" {
		bot = string(w.Bot)
	}
	if err := exec(ctx, tx, insertSessionSpan, id, string(w.Agent), string(w.Harness), bot, w.PromptHash,
		unixMilli(w.Started), integer(w.Turns)); err != nil {
		return err
	}
	for _, m := range w.Tokens {
		if err := exec(ctx, tx, insertModelUsage, id, m.Model, m.Tokens.Input, m.Tokens.Output,
			m.Tokens.CacheRead, m.Tokens.CacheWrite); err != nil {
			return err
		}
	}
	return nil
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

// integer is n, or nil, a null, when it is absent.
func integer(n crew.Optional[int]) any {
	if v, ok := n.Get(); ok {
		return v
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

// update runs query with args in tx.
func update(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("update: %w", err)
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
