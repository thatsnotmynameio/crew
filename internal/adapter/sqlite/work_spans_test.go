package sqlite_test

import (
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// at is t0 plus n seconds.
func at(n int) time.Time { return t0.Add(time.Duration(n) * time.Second) }

// opus and sonnet are the tokens an lfg session reported for two models.
var (
	opus = crew.ModelTokens{
		Model: "claude-opus", Tokens: crew.Tokens{Input: 10, Output: 20, CacheRead: 30, CacheWrite: 40},
	}
	sonnet = crew.ModelTokens{
		Model: "claude-sonnet", Tokens: crew.Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4},
	}
)

// action is the span of action name of r.1 on #315, run by process p from
// t0 plus two seconds to t0 plus five, with the verdict passed, doing work.
func action(name crew.ActionName, outcome crew.WorkOutcome, work crew.Work) crew.ActionSpan {
	return crew.ActionSpan{
		Issue: crew.IssueID{Repository: "R_1", Key: "315"}, Rule: "develop", Run: "r.1", Action: name, Process: "p",
		Start: at(2), End: at(5), Outcome: outcome, Verdict: crew.Passed, Work: work,
	}
}

// lfg is AE4's session: claude through crew-developer, 12 turns, with
// tokens for Opus and Sonnet.
func lfg() crew.SessionWork {
	return crew.SessionWork{
		Agent: "claude", Harness: "claude", Bot: "crew-developer", PromptHash: "4bbac29b",
		Started: crew.Some(at(3)), Turns: crew.Some(12), Tokens: []crew.ModelTokens{opus, sonnet},
	}
}

// route is the open of r.1's route passed, chosen by the action review
// with the verdict passed at t0 plus six seconds.
func route() crew.RouteSpan {
	return crew.RouteSpan{
		Issue: crew.IssueID{Repository: "R_1", Key: "315"}, Rule: "develop", Run: "r.1", Process: "p",
		Route: crew.PassedRoute, ChosenBy: crew.Some(crew.RouteChoice{Action: "review", Verdict: crew.Passed}),
		Start: at(6),
	}
}

// routeEnded is route ended at t0 plus n seconds with outcome.
func routeEnded(n int, outcome crew.RunOutcome) crew.RouteSpan {
	sp := route()
	sp.End = crew.Some(crew.RouteEnd{At: at(n), Outcome: outcome})
	return sp
}

// step is the span of r.1's route step index, name, run by process p from
// t0 plus seven seconds to t0 plus eight, doing work.
func step(index int, name crew.ActionName, work crew.Work) crew.StepSpan {
	return crew.StepSpan{
		Issue: crew.IssueID{Repository: "R_1", Key: "315"}, Rule: "develop", Run: "r.1", Index: index, Name: name,
		Process: "p", Start: at(7), End: at(8), Outcome: crew.WorkSucceeded, Work: work,
	}
}

// workRow is a row of the work_spans table.
type workRow struct {
	span, name, kind string
	verdict          sql.NullString
}

// sessionRow is a row of the session_spans table.
type sessionRow struct {
	span, agent, harness string
	bot                  sql.NullString
	promptHash           string
	started, turns       sql.NullInt64
}

// tokenRow is a row of the session_tokens table.
type tokenRow struct {
	span, model                          string
	input, output, cacheRead, cacheWrite int64
}

// shellRow is a row of the shell_spans table.
type shellRow struct {
	span       string
	exitStatus sql.NullInt64
}

// functionRow is a row of the function_spans table.
type functionRow struct {
	span, function string
	verdict        sql.NullString
}

// routeRow is a row of the route_spans table.
type routeRow struct {
	span, route string
	verdict     sql.NullString
}

func works(t *testing.T, dir string) []workRow {
	t.Helper()
	return rows(t, dir, `SELECT span_id, name, kind, verdict FROM work_spans ORDER BY rowid`,
		func(r *sql.Rows, v *workRow) error { return r.Scan(&v.span, &v.name, &v.kind, &v.verdict) })
}

func sessions(t *testing.T, dir string) []sessionRow {
	t.Helper()
	return rows(t, dir, `SELECT span_id, agent, harness, bot, prompt_hash, session_started_at, turns
		FROM session_spans ORDER BY rowid`,
		func(r *sql.Rows, v *sessionRow) error {
			return r.Scan(&v.span, &v.agent, &v.harness, &v.bot, &v.promptHash, &v.started, &v.turns)
		})
}

func tokens(t *testing.T, dir string) []tokenRow {
	t.Helper()
	return rows(t, dir, `SELECT span_id, model, input, output, cache_read, cache_write
		FROM session_tokens ORDER BY span_id, model`,
		func(r *sql.Rows, v *tokenRow) error {
			return r.Scan(&v.span, &v.model, &v.input, &v.output, &v.cacheRead, &v.cacheWrite)
		})
}

func shells(t *testing.T, dir string) []shellRow {
	t.Helper()
	return rows(t, dir, `SELECT span_id, exit_status FROM shell_spans ORDER BY rowid`,
		func(r *sql.Rows, v *shellRow) error { return r.Scan(&v.span, &v.exitStatus) })
}

func functions(t *testing.T, dir string) []functionRow {
	t.Helper()
	return rows(t, dir, `SELECT span_id, function, verdict FROM function_spans ORDER BY rowid`,
		func(r *sql.Rows, v *functionRow) error { return r.Scan(&v.span, &v.function, &v.verdict) })
}

func routes(t *testing.T, dir string) []routeRow {
	t.Helper()
	return rows(t, dir, `SELECT span_id, route, verdict FROM route_spans ORDER BY rowid`,
		func(r *sql.Rows, v *routeRow) error { return r.Scan(&v.span, &v.route, &v.verdict) })
}

// count is n as a non-null integer.
func count(n int64) sql.NullInt64 { return sql.NullInt64{Int64: n, Valid: true} }

// actionRow is the spans row of action name of r.1, started at t0 plus two
// seconds and ended at t0 plus five with outcome.
func actionRow(name, outcome string) spanRow {
	return spanRow{
		id: "r.1/action/" + name, kind: "action", parent: some("r.1"), process: "p", started: ms(2),
		endedAt: count(ms(5)), outcome: some(outcome),
	}
}

// want fails the test unless got equals want, naming table.
func want[T comparable](t *testing.T, table string, got, want []T) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %+v, want %+v", table, got, want)
	}
}

// Covers AE4, R12: a session span writes its action's span under the rule
// run, its work, its session and one token record per model.
func TestASessionSpanWritesItsSessionAndItsTokensByModel(t *testing.T) {
	dir := t.TempDir()

	record(t, open(t, dir), action("lfg", crew.WorkSucceeded, lfg()))

	want(t, "spans", spans(t, dir), []spanRow{actionRow("lfg", "succeeded")})
	want(t, "work_spans", works(t, dir), []workRow{{"r.1/action/lfg", "lfg", "session", some("passed")}})
	want(t, "session_spans", sessions(t, dir), []sessionRow{{
		span: "r.1/action/lfg", agent: "claude", harness: "claude", bot: some("crew-developer"),
		promptHash: "4bbac29b", started: count(ms(3)), turns: count(12),
	}})
	want(t, "session_tokens", tokens(t, dir), []tokenRow{
		{"r.1/action/lfg", "claude-opus", 10, 20, 30, 40},
		{"r.1/action/lfg", "claude-sonnet", 1, 2, 3, 4},
	})
}

// Covers AE6, R14: a stopped session that reported nothing has no token
// record and no turns, and a sum of the issue's tokens counts the session
// that reported and says one session that started did not.
func TestAStoppedSessionThatReportedNothingHasNoTokens(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	stopped := lfg()
	stopped.Turns, stopped.Tokens = crew.Optional[int]{}, nil
	neverStarted := stopped
	neverStarted.Started = crew.Optional[time.Time]{}

	record(t, s, opened())
	record(t, s, action("lfg", crew.WorkStopped, stopped))
	record(t, s, action("review", crew.WorkSucceeded, lfg()))
	record(t, s, action("fix", crew.WorkStoppedBeforeStart, neverStarted))

	got := sessions(t, dir)
	if len(got) != 3 || got[0].turns.Valid || !got[0].started.Valid || got[2].started.Valid {
		t.Errorf("session_spans = %+v, want lfg with a start and no turns, fix with no start", got)
	}
	var sum, silent int64
	err := read(t, dir).QueryRowContext(t.Context(), `
		SELECT
			(SELECT COALESCE(SUM(t.input + t.output + t.cache_read + t.cache_write), 0)
				FROM session_tokens t JOIN spans a ON a.id = t.span_id
				JOIN rule_run_spans r ON r.span_id = a.parent_id WHERE r.issue_key = '315'),
			(SELECT COUNT(*) FROM session_spans s JOIN spans a ON a.id = s.span_id
				JOIN rule_run_spans r ON r.span_id = a.parent_id
				WHERE r.issue_key = '315' AND s.session_started_at IS NOT NULL
				AND NOT EXISTS (SELECT 1 FROM session_tokens t WHERE t.span_id = s.span_id))`).Scan(&sum, &silent)
	if err != nil {
		t.Fatal(err)
	}
	if sum != opus.Tokens.Total()+sonnet.Tokens.Total() || silent != 1 {
		t.Errorf("the issue's tokens = %d, with %d silent sessions; want %d, with 1", sum, silent,
			opus.Tokens.Total()+sonnet.Tokens.Total())
	}
}

// Covers R14, KTD7: a reported zero is a row of zeros, a sum with no model
// a row with an empty model, and a session with no bot a null bot.
func TestASessionsTokensAndBotAreStoredAsReported(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tokens []crew.ModelTokens
		want   []tokenRow
	}{
		{"zero for one model", []crew.ModelTokens{{Model: "claude-opus"}},
			[]tokenRow{{span: "r.1/action/lfg", model: "claude-opus"}}},
		{"a sum with no model", []crew.ModelTokens{{Tokens: crew.Tokens{Input: 7}}},
			[]tokenRow{{span: "r.1/action/lfg", input: 7}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			work := lfg()
			work.Bot, work.Tokens = "", tc.tokens

			record(t, open(t, dir), action("lfg", crew.WorkSucceeded, work))

			want(t, "session_tokens", tokens(t, dir), tc.want)
			if got := sessions(t, dir); len(got) != 1 || got[0].bot.Valid {
				t.Errorf("session_spans = %+v, want one with a null bot", got)
			}
		})
	}
}

// Covers AE7, R13: a shell action stores its exit status, null when its
// script did not exit, and no token record.
func TestAShellActionStoresItsExitStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit crew.Optional[int]
		want sql.NullInt64
	}{
		{"exited 1", crew.Some(1), count(1)},
		{"did not exit", crew.Optional[int]{}, sql.NullInt64{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			record(t, open(t, dir), action("lint", crew.WorkFailed, crew.ShellWork{ExitStatus: tc.exit}))

			want(t, "spans", spans(t, dir), []spanRow{actionRow("lint", "failed")})
			want(t, "work_spans", works(t, dir), []workRow{{"r.1/action/lint", "lint", "shell", some("passed")}})
			want(t, "shell_spans", shells(t, dir), []shellRow{{"r.1/action/lint", tc.want}})
			want(t, "session_tokens", tokens(t, dir), nil)
		})
	}
}

// Covers KTD6: a function action stores its function and the verdict it
// returned, null when it returned none.
func TestAFunctionActionStoresItsFunctionAndVerdict(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict crew.Optional[crew.Verdict]
		want    sql.NullString
	}{
		{"a verdict", crew.Some[crew.Verdict]("blocked"), some("blocked")},
		{"none", crew.Optional[crew.Verdict]{}, sql.NullString{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()

			record(t, open(t, dir), action("judge", crew.WorkSucceeded,
				crew.FunctionWork{Function: "session-finished", Verdict: tc.verdict}))

			want(t, "function_spans", functions(t, dir), []functionRow{{"r.1/action/judge", "session-finished", tc.want}})
		})
	}
}

// A question and a check write their span and work only.
func TestAQuestionAndACheckWriteTheirWorkOnly(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	record(t, s, action("ask", crew.WorkSucceeded, crew.QuestionWork{}))
	record(t, s, action("check", crew.WorkSucceeded, crew.CheckWork{}))

	want(t, "spans", spans(t, dir), []spanRow{actionRow("ask", "succeeded"), actionRow("check", "succeeded")})
	want(t, "work_spans", works(t, dir), []workRow{
		{"r.1/action/ask", "ask", "question", some("passed")},
		{"r.1/action/check", "check", "check", some("passed")},
	})
	want(t, "session_spans", sessions(t, dir), nil)
	want(t, "shell_spans", shells(t, dir), nil)
	want(t, "function_spans", functions(t, dir), nil)
}

// Covers KTD1: the same action recorded twice leaves one row in each
// table.
func TestRecordingTheSameActionTwiceKeepsOne(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	record(t, s, action("lfg", crew.WorkSucceeded, lfg()))
	record(t, s, action("lfg", crew.WorkFailed, lfg()))

	want(t, "spans", spans(t, dir), []spanRow{actionRow("lfg", "succeeded")})
	if w, ss, tk := works(t, dir), sessions(t, dir), tokens(t, dir); len(w) != 1 || len(ss) != 1 || len(tk) != 2 {
		t.Errorf("work_spans %d, session_spans %d, session_tokens %d rows; want 1, 1 and 2", len(w), len(ss), len(tk))
	}
}

// routeSpanRow is the spans row of r.1's route under parent, opened at t0
// plus six seconds.
func routeSpanRow(parent string) spanRow {
	return spanRow{id: "r.1/route", kind: "route", parent: some(parent), process: "p", started: ms(6)}
}

// Covers AE5, R11: a route opens under the action that chose it, with its
// verdict, and its end sets its end and outcome.
func TestARouteOpensUnderItsActionAndEnds(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	record(t, s, route())

	want(t, "spans", spans(t, dir), []spanRow{routeSpanRow("r.1/action/review")})
	want(t, "route_spans", routes(t, dir), []routeRow{{"r.1/route", "passed", some("passed")}})

	record(t, s, routeEnded(9, crew.OutcomeRouted))

	ended := routeSpanRow("r.1/action/review")
	ended.endedAt, ended.outcome = count(ms(9)), some("routed")
	want(t, "spans", spans(t, dir), []spanRow{ended})
	want(t, "route_spans", routes(t, dir), []routeRow{{"r.1/route", "passed", some("passed")}})
}

// Covers KTD2: a route's end without its open writes the whole span, and a
// second end keeps the first.
func TestARoutesEndWithoutItsOpenWritesTheSpanAndKeepsTheFirstEnd(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	record(t, s, routeEnded(9, crew.OutcomeRouteDropped))
	record(t, s, routeEnded(12, crew.OutcomeRouted))

	ended := routeSpanRow("r.1/action/review")
	ended.endedAt, ended.outcome = count(ms(9)), some("route_dropped")
	want(t, "spans", spans(t, dir), []spanRow{ended})
	want(t, "route_spans", routes(t, dir), []routeRow{{"r.1/route", "passed", some("passed")}})
}

// Covers KTD9: a route no action of the run chose hangs from the rule run,
// with a null verdict.
func TestARouteNoActionChoseHangsFromItsRun(t *testing.T) {
	dir := t.TempDir()
	sp := route()
	sp.ChosenBy = crew.Optional[crew.RouteChoice]{}

	record(t, open(t, dir), sp)

	want(t, "spans", spans(t, dir), []spanRow{routeSpanRow("r.1")})
	want(t, "route_spans", routes(t, dir), []routeRow{{"r.1/route", "passed", sql.NullString{}}})
}

// Covers AE5, R11, R13: a route's shell and function steps hang from the
// route with their detail, and count no token.
func TestARoutesStepsHangFromIt(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	record(t, s, step(0, "notify", crew.ShellWork{ExitStatus: crew.Some(0)}))
	record(t, s, step(1, "judge", crew.FunctionWork{Function: "session-finished", Verdict: crew.Some(crew.Passed)}))

	stepRow := func(index string) spanRow {
		return spanRow{
			id: "r.1/route/" + index, kind: "step", parent: some("r.1/route"), process: "p", started: ms(7),
			endedAt: count(ms(8)), outcome: some("succeeded"),
		}
	}
	want(t, "spans", spans(t, dir), []spanRow{stepRow("0"), stepRow("1")})
	want(t, "work_spans", works(t, dir), []workRow{
		{"r.1/route/0", "notify", "shell", sql.NullString{}},
		{"r.1/route/1", "judge", "function", sql.NullString{}},
	})
	want(t, "shell_spans", shells(t, dir), []shellRow{{"r.1/route/0", count(0)}})
	want(t, "function_spans", functions(t, dir), []functionRow{{"r.1/route/1", "session-finished", some("passed")}})
	want(t, "session_tokens", tokens(t, dir), nil)
}

// Covers KTD6: a store at version 3, holding a rule run span, migrates to
// the work spans' version and keeps its rows.
func TestAStoreOfVersionThreeMigratesAndKeepsItsSpans(t *testing.T) {
	dir := t.TempDir()
	db := read(t, dir)
	for _, file := range []string{
		"migrations/0001_processes.sql", "migrations/0002_issues.sql", "migrations/0003_spans.sql",
	} {
		script, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(t.Context(), string(script)); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO spans (id, kind, process_id, started_at) VALUES ('r.1', 'rule_run', 'p', 1)`,
		`INSERT INTO rule_run_spans (span_id, tracker, repository_id, issue_key, rule)
			VALUES ('r.1', 'github', 'R_1', '315', 'develop')`,
		"PRAGMA user_version = 3",
	} {
		if _, err := db.ExecContext(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}

	record(t, open(t, dir), action("lfg", crew.WorkSucceeded, lfg()))

	want(t, "spans", spans(t, dir), []spanRow{
		{id: "r.1", kind: "rule_run", process: "p", started: 1}, actionRow("lfg", "succeeded"),
	})
	if got := ruleRuns(t, dir); len(got) != 1 || got[0].span != "r.1" {
		t.Errorf("rule_run_spans = %+v, want r.1's", got)
	}
	if v := userVersion(t, dir); v != migrations {
		t.Errorf("user_version = %d, want %d", v, migrations)
	}
}
