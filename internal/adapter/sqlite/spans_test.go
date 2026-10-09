package sqlite_test

import (
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// opened is the open of run r.1 of rule develop, in queue default, on #315,
// by process p, started at t0 plus one second.
func opened() crew.RuleRunSpan {
	return crew.RuleRunSpan{
		Tracker: "github", Issue: crew.IssueID{Repository: "R_1", Key: "315"}, Run: "r.1", Process: "p",
		Rule: "develop", Queue: crew.Some[crew.QueueName]("default"), Start: t0.Add(time.Second),
	}
}

// ended is the open of r.1 at t0 plus one second, ended at t0 plus n
// seconds with outcome through route.
func ended(n int, outcome crew.RunOutcome, route crew.RouteName) crew.RuleRunSpan {
	sp := opened()
	end := crew.RuleRunEnd{At: t0.Add(time.Duration(n) * time.Second), Outcome: outcome}
	if route != "" {
		end.Route = crew.Some(route)
	}
	sp.End = crew.Some(end)
	return sp
}

// spanRow is a row of the spans table.
type spanRow struct {
	id, kind string
	parent   sql.NullString
	process  string
	started  int64
	endedAt  sql.NullInt64
	outcome  sql.NullString
}

// ruleRunRow is a row of the rule_run_spans table.
type ruleRunRow struct {
	span, tracker, repository, key, rule string
	queue, route, halted, continues      sql.NullString
}

func spans(t *testing.T, dir string) []spanRow {
	t.Helper()
	return rows(t, dir, `SELECT id, kind, parent_id, process_id, started_at, ended_at, outcome FROM spans ORDER BY rowid`,
		func(r *sql.Rows, v *spanRow) error {
			return r.Scan(&v.id, &v.kind, &v.parent, &v.process, &v.started, &v.endedAt, &v.outcome)
		})
}

func ruleRuns(t *testing.T, dir string) []ruleRunRow {
	t.Helper()
	return rows(t, dir, `SELECT span_id, tracker, repository_id, issue_key, rule, queue, route, halted, continues_run_id
		FROM rule_run_spans ORDER BY rowid`,
		func(r *sql.Rows, v *ruleRunRow) error {
			return r.Scan(&v.span, &v.tracker, &v.repository, &v.key, &v.rule, &v.queue, &v.route, &v.halted,
				&v.continues)
		})
}

// some is s as a non-null text.
func some(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// openRow is the spans row of r.1's open, started at t0 plus one second.
func openRow() spanRow {
	return spanRow{id: "r.1", kind: "rule_run", process: "p", started: ms(1)}
}

// endedRow is the spans row of r.1 opened at t0 plus one second and ended
// at t0 plus two seconds with outcome.
func endedRow(outcome string) spanRow {
	row := openRow()
	row.endedAt, row.outcome = sql.NullInt64{Int64: ms(2), Valid: true}, some(outcome)
	return row
}

// ruleRunOf is the rule_run_spans row of r.1, through route when it is not
// empty.
func ruleRunOf(route string) ruleRunRow {
	row := ruleRunRow{
		span: "r.1", tracker: "github", repository: "R_1", key: "315", rule: "develop", queue: some("default"),
	}
	if route != "" {
		row.route = some(route)
	}
	return row
}

// wantSpan fails the test unless the store in dir holds only span and
// ruleRun.
func wantSpan(t *testing.T, dir string, span spanRow, ruleRun ruleRunRow) {
	t.Helper()
	if got := spans(t, dir); !slices.Equal(got, []spanRow{span}) {
		t.Errorf("spans = %+v, want %+v", got, span)
	}
	if got := ruleRuns(t, dir); !slices.Equal(got, []ruleRunRow{ruleRun}) {
		t.Errorf("rule_run_spans = %+v, want %+v", got, ruleRun)
	}
}

// Covers R9, AE11: the open alone writes a rule run span with no parent,
// no end and no outcome, under its issue, which reads back open.
func TestAnOpenRecordsARuleRunSpanWithNoEnd(t *testing.T) {
	dir := t.TempDir()

	record(t, open(t, dir), opened())

	wantSpan(t, dir, openRow(), ruleRunOf(""))
}

// Covers R9: the end of an open span sets its end, outcome and route.
func TestAnEndCompletesTheOpenSpan(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	record(t, s, opened())
	record(t, s, ended(2, crew.OutcomeRouted, crew.PassedRoute))

	wantSpan(t, dir, endedRow("routed"), ruleRunOf("passed"))
}

// Covers KTD5: an end whose open was never written writes the whole span.
func TestAnEndWithoutItsOpenWritesTheWholeSpan(t *testing.T) {
	dir := t.TempDir()

	record(t, open(t, dir), ended(2, crew.OutcomeRouteDropped, crew.FailedRoute))

	wantSpan(t, dir, endedRow("route_dropped"), ruleRunOf("failed"))
}

// Covers KTD5: a second open after the end changes nothing, and a second
// end keeps the first.
func TestASecondOpenOrEndKeepsTheFirstEnd(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	record(t, s, opened())
	record(t, s, ended(2, crew.OutcomeRouteGivenUp, crew.PassedRoute))

	record(t, s, opened())
	record(t, s, ended(9, crew.OutcomeRouted, crew.FailedRoute))

	wantSpan(t, dir, endedRow("route_given_up"), ruleRunOf("passed"))
}

// Covers KTD3: a take that did not land ends with no route.
func TestAnEndNotTakenStoresNoRoute(t *testing.T) {
	dir := t.TempDir()

	record(t, open(t, dir), ended(2, crew.OutcomeNotTaken, ""))

	wantSpan(t, dir, endedRow("not_taken"), ruleRunOf(""))
}

// Covers KTD3: the halt that chose the route is stored, and none is a
// null.
func TestAnEndStoresItsHalt(t *testing.T) {
	for _, tc := range []struct {
		name string
		halt crew.Optional[crew.RunHalt]
		want sql.NullString
	}{
		{"stop", crew.Some(crew.HaltStop), some("stop")},
		{"run time limit", crew.Some(crew.HaltRunTimeLimit), some("run_time_limit")},
		{"none", crew.Optional[crew.RunHalt]{}, sql.NullString{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			sp := ended(2, crew.OutcomeRouted, crew.FailedRoute)
			end, _ := sp.End.Get()
			end.Halt = tc.halt
			sp.End = crew.Some(end)

			record(t, open(t, dir), sp)

			want := ruleRunOf("failed")
			want.halted = tc.want
			wantSpan(t, dir, endedRow("routed"), want)
		})
	}
}

// Covers KTD6: an open with no queue, of a core built without config,
// stores a null queue.
func TestAnOpenWithNoQueueStoresANullQueue(t *testing.T) {
	dir := t.TempDir()
	sp := opened()
	sp.Queue = crew.Optional[crew.QueueName]{}

	record(t, open(t, dir), sp)

	want := ruleRunOf("")
	want.queue = sql.NullString{}
	wantSpan(t, dir, openRow(), want)
}

// Covers KTD1, F3: a span that continues an earlier run names it.
func TestASpanThatContinuesARunNamesIt(t *testing.T) {
	dir := t.TempDir()
	sp := opened()
	sp.Continues = crew.Some[crew.RuleRunID]("q.3")

	record(t, open(t, dir), sp)

	want := ruleRunOf("")
	want.continues = some("q.3")
	wantSpan(t, dir, openRow(), want)
}

// Covers KTD4: a store an older crew migrated to version 2, holding a
// process and a move, migrates to the spans' version and keeps its rows.
func TestAStoreOfVersionTwoMigratesAndKeepsItsRows(t *testing.T) {
	dir := t.TempDir()
	db := read(t, dir)
	for _, file := range []string{"migrations/0001_processes.sql", "migrations/0002_issues.sql"} {
		script, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), string(script)); err != nil {
			t.Fatal(err)
		}
	}
	p := process(1)
	for _, q := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO processes (id, version, folder, started_at) VALUES (?, ?, ?, ?)",
			[]any{string(p.ID), p.Version, p.Folder, p.Start.UnixMilli()}},
		{`INSERT INTO label_moves (tracker, repository_id, issue_key, from_label, to_label, seen_at)
			VALUES ('github', 'R_1', '42', 'ready', 'done', ?)`, []any{ms(1)}},
		{"PRAGMA user_version = 2", nil},
	} {
		if _, err := db.ExecContext(t.Context(), q.query, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	record(t, open(t, dir), opened())

	if got := processes(t, dir); len(got) != 1 || got[0] != process(1) {
		t.Errorf("processes = %+v, want %+v", got, process(1))
	}
	if got, want := moves(t, dir), []moveRow{outsideRow(1, "ready", "done")}; !slices.Equal(got, want) {
		t.Errorf("moves = %+v, want %+v", got, want)
	}
	wantSpan(t, dir, openRow(), ruleRunOf(""))
	if v := userVersion(t, dir); v != migrations {
		t.Errorf("user_version = %d, want %d", v, migrations)
	}
}
