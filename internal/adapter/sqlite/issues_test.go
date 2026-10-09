package sqlite_test

import (
	"database/sql"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// issue42 is the issue the tests see and move.
var issue42 = crew.IssueID{Repository: "R_1", Key: "42"}

// repository is the record of the repository R_1 under name.
func repository(name string) crew.RepositoryRecord {
	return crew.RepositoryRecord{Tracker: "github", Repository: crew.Repository{ID: "R_1", Name: name}}
}

// sighting is a sighting of #42 at t0 plus n seconds, at state, created
// at t0 minus an hour.
func sighting(n int, state crew.State) crew.IssueSighting {
	return crew.IssueSighting{
		Tracker: "github", Issue: issue42, Ref: "#42", Kind: crew.KindIssue,
		Created: crew.Some(t0.Add(-time.Hour)), Seen: t0.Add(time.Duration(n) * time.Second),
		State: crew.Some(state),
	}
}

// outside is a move of #42 made outside crew, seen at t0 plus n seconds.
func outside(n int, from, to crew.State) crew.LabelMove {
	return crew.LabelMove{Tracker: "github", Issue: issue42, From: from, To: to,
		Seen: t0.Add(time.Duration(n) * time.Second)}
}

// repositoryRow is a row of the repositories table.
type repositoryRow struct{ tracker, id, name string }

// issueRow is a row of the issues table.
type issueRow struct {
	tracker, repository, key, ref, kind string
	created                             sql.NullInt64
	seen                                int64
	first                               sql.NullString
}

// moveRow is a row of the label_moves table, without its id.
type moveRow struct {
	tracker, repository, key, from, to string
	moved                              sql.NullInt64
	seen                               int64
	run                                sql.NullString
}

// rows returns the rows query reads from the store in dir, each scanned by
// scan.
func rows[T any](t *testing.T, dir, query string, scan func(*sql.Rows, *T) error) []T {
	t.Helper()
	r, err := read(t, dir).QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var got []T
	for r.Next() {
		var v T
		if err := scan(r, &v); err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func repositories(t *testing.T, dir string) []repositoryRow {
	t.Helper()
	return rows(t, dir, "SELECT tracker, id, name FROM repositories ORDER BY rowid",
		func(r *sql.Rows, v *repositoryRow) error { return r.Scan(&v.tracker, &v.id, &v.name) })
}

func issues(t *testing.T, dir string) []issueRow {
	t.Helper()
	return rows(t, dir, `SELECT tracker, repository_id, key, ref, kind, created_at, seen_at, first_label
		FROM issues ORDER BY rowid`,
		func(r *sql.Rows, v *issueRow) error {
			return r.Scan(&v.tracker, &v.repository, &v.key, &v.ref, &v.kind, &v.created, &v.seen, &v.first)
		})
}

func moves(t *testing.T, dir string) []moveRow {
	t.Helper()
	return rows(t, dir, `SELECT tracker, repository_id, issue_key, from_label, to_label, moved_at, seen_at, run_id
		FROM label_moves ORDER BY id`,
		func(r *sql.Rows, v *moveRow) error {
			return r.Scan(&v.tracker, &v.repository, &v.key, &v.from, &v.to, &v.moved, &v.seen, &v.run)
		})
}

// ms is t0 plus n seconds, in the store's Unix milliseconds.
func ms(n int) int64 { return t0.Add(time.Duration(n) * time.Second).UnixMilli() }

// outsideRow is the row of a move of #42 made outside crew, seen at t0 plus
// n seconds.
func outsideRow(n int, from, to string) moveRow {
	return moveRow{tracker: "github", repository: "R_1", key: "42", from: from, to: to, seen: ms(n)}
}

func TestRecordingARepositoryKeepsOneRowWithItsLatestName(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	record(t, s, repository("acme/app"))
	record(t, s, repository("acme/app"))
	if got, want := repositories(t, dir), []repositoryRow{{"github", "R_1", "acme/app"}}; !slices.Equal(got, want) {
		t.Errorf("repositories = %+v, want %+v", got, want)
	}

	record(t, s, repository("acme/renamed"))
	if got, want := repositories(t, dir), []repositoryRow{{"github", "R_1", "acme/renamed"}}; !slices.Equal(got, want) {
		t.Errorf("repositories after the rename = %+v, want %+v", got, want)
	}
}

func TestRecordingTheSameRepositoryIDOnTwoTrackersKeepsBoth(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	other := repository("acme/app")
	other.Tracker = "gitlab"

	record(t, s, repository("acme/app"))
	record(t, s, other)

	want := []repositoryRow{{"github", "R_1", "acme/app"}, {"gitlab", "R_1", "acme/app"}}
	if got := repositories(t, dir); !slices.Equal(got, want) {
		t.Errorf("repositories = %+v, want %+v", got, want)
	}
}

func TestRecordingASightingTwiceKeepsTheFirst(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	later := sighting(60, "ready")
	later.Created = crew.Some(t0)
	later.Ref = "#42!"

	record(t, s, sighting(1, "ready"))
	record(t, s, later)

	want := []issueRow{{
		tracker: "github", repository: "R_1", key: "42", ref: "#42", kind: "issue",
		created: sql.NullInt64{Int64: t0.Add(-time.Hour).UnixMilli(), Valid: true}, seen: ms(1),
		first: sql.NullString{String: "ready", Valid: true},
	}}
	if got := issues(t, dir); !slices.Equal(got, want) {
		t.Errorf("issues = %+v, want %+v", got, want)
	}
	if got := moves(t, dir); len(got) != 0 {
		t.Errorf("moves = %+v, want none", got)
	}
}

func TestRecordingASightingWithoutCreationDateOrStateStoresNulls(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	pr := sighting(1, "")
	pr.Created, pr.State, pr.Kind = crew.Optional[time.Time]{}, crew.Optional[crew.State]{}, crew.KindPullRequest

	record(t, s, pr)

	want := []issueRow{{tracker: "github", repository: "R_1", key: "42", ref: "#42", kind: "pull_request", seen: ms(1)}}
	if got := issues(t, dir); !slices.Equal(got, want) {
		t.Errorf("issues = %+v, want %+v", got, want)
	}
}

func TestASightingAtAnotherLabelThanTheLastRecordsAMoveOutsideCrew(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	record(t, s, sighting(1, "ready"))
	byRun := outside(2, "ready", "done")
	byRun.Run = crew.Some(crew.RuleRunID("run-1"))
	record(t, s, byRun)

	record(t, s, sighting(3, "ready"))
	record(t, s, sighting(4, "ready"))

	got := moves(t, dir)
	if len(got) != 2 || got[1] != outsideRow(3, "done", "ready") {
		t.Errorf("moves = %+v, want the run's move, then done to ready at the sighting's time", got)
	}
}

func TestASightingWithNoMoveAtItsFirstLabelRecordsNoMove(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	record(t, s, sighting(1, "ready"))
	record(t, s, sighting(2, "ready"))

	if got := moves(t, dir); len(got) != 0 {
		t.Errorf("moves = %+v, want none", got)
	}
}

func TestASightingWithNoStateOrNoLastLabelRecordsNoMove(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	twoStates := sighting(1, "")
	twoStates.State = crew.Optional[crew.State]{}

	record(t, s, twoStates)
	record(t, s, sighting(2, "ready"))
	record(t, s, twoStates)

	if got := moves(t, dir); len(got) != 0 {
		t.Errorf("moves = %+v, want none", got)
	}
	if got := issues(t, dir); len(got) != 1 || got[0].first.Valid {
		t.Errorf("issues = %+v, want one with no first label", got)
	}
}

func TestRecordingAMoveStoresItsRunAndTimes(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	byRun := outside(2, "in progress", "done")
	byRun.Run = crew.Some(crew.RuleRunID("run-1"))
	byRun.Moved = crew.Some(t0)

	record(t, s, outside(1, "ready", "in progress"))
	record(t, s, byRun)

	second := outsideRow(2, "in progress", "done")
	second.run = sql.NullString{String: "run-1", Valid: true}
	second.moved = sql.NullInt64{Int64: t0.UnixMilli(), Valid: true}
	if got, want := moves(t, dir), []moveRow{outsideRow(1, "ready", "in progress"), second}; !slices.Equal(got, want) {
		t.Errorf("moves = %+v, want %+v", got, want)
	}
}

func TestAMoveOutsideCrewToTheLastLabelIsRecordedOnce(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	record(t, s, sighting(1, "in progress"))

	record(t, s, outside(2, "in progress", "done"))
	record(t, s, outside(3, "in progress", "done"))
	record(t, s, outside(4, "ready", "in progress"))

	want := []moveRow{outsideRow(2, "in progress", "done"), outsideRow(4, "ready", "in progress")}
	if got := moves(t, dir); !slices.Equal(got, want) {
		t.Errorf("moves = %+v, want %+v", got, want)
	}
}

func TestAMoveOutsideCrewToTheFirstLabelIsNotRecorded(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	record(t, s, sighting(1, "done"))

	record(t, s, outside(2, "in progress", "done"))

	if got := moves(t, dir); len(got) != 0 {
		t.Errorf("moves = %+v, want none", got)
	}
}

func TestAMoveByARunToTheLastLabelIsRecorded(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)
	record(t, s, sighting(1, "done"))
	byRun := outside(2, "in progress", "done")
	byRun.Run = crew.Some(crew.RuleRunID("run-1"))

	record(t, s, byRun)

	if got := moves(t, dir); len(got) != 1 || got[0].run.String != "run-1" {
		t.Errorf("moves = %+v, want the run's move", got)
	}
}

func TestAMoveOfAnIssueTheStoreLacksIsRecorded(t *testing.T) {
	dir := t.TempDir()

	record(t, open(t, dir), outside(1, "ready", "done"))

	if got, want := moves(t, dir), []moveRow{outsideRow(1, "ready", "done")}; !slices.Equal(got, want) {
		t.Errorf("moves = %+v, want %+v", got, want)
	}
	if got := issues(t, dir); len(got) != 0 {
		t.Errorf("issues = %+v, want none", got)
	}
}

func TestAStoreOfVersionOneMigratesAndKeepsItsProcesses(t *testing.T) {
	dir := t.TempDir()
	first, err := os.ReadFile("migrations/0001_processes.sql")
	if err != nil {
		t.Fatal(err)
	}
	db := read(t, dir)
	for _, q := range []string{string(first), "PRAGMA user_version = 1"} {
		if _, err := db.ExecContext(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(t.Context(), "INSERT INTO processes (id, version, folder, started_at) VALUES (?, ?, ?, ?)",
		string(process(1).ID), process(1).Version, process(1).Folder, process(1).Start.UnixMilli()); err != nil {
		t.Fatal(err)
	}

	record(t, open(t, dir), repository("acme/app"))

	if got := processes(t, dir); len(got) != 1 || got[0] != process(1) {
		t.Errorf("processes = %+v, want %+v", got, process(1))
	}
	if got := repositories(t, dir); len(got) != 1 {
		t.Errorf("repositories = %+v, want one", got)
	}
	if v := userVersion(t, dir); v != migrations {
		t.Errorf("user_version = %d, want %d", v, migrations)
	}
}
