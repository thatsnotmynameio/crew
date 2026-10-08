package sqlite_test

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/adapter/sqlite"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// migrations is how many migration files the adapter embeds.
const migrations = 1

var t0 = time.Date(2026, 10, 8, 21, 2, 3, 456_000_000, time.UTC)

// process is the n-th crew process the tests record.
func process(n int) crew.Process {
	return crew.Process{
		ID:      crew.ProcessID(fmt.Sprintf("0199c4f2-0000-7000-8000-%012d", n)),
		Version: "0.1.1", Folder: "/work/repo", Start: t0.Add(time.Duration(n) * time.Second),
	}
}

// section decodes body as a store's config section, strictly, as crew's
// config does.
func section(body string) port.Decode {
	return func(target any) error {
		if body == "" {
			return nil
		}
		d := yaml.NewDecoder(strings.NewReader(body))
		d.KnownFields(true)
		return d.Decode(target)
	}
}

// open builds a store on dir, closed when the test ends.
func open(t *testing.T, dir string) port.Statistics {
	t.Helper()
	s, err := sqlite.Factory()(section(""), dir)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

// record records p into s and fails the test if it does not.
func record(t *testing.T, s port.Statistics, p crew.Process) {
	t.Helper()
	if err := s.Record(t.Context(), p); err != nil {
		t.Fatalf("Record: %v", err)
	}
}

// read opens the store's file in dir apart from the store.
func read(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "statistics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// processes returns the processes table's rows in dir, in insertion order.
func processes(t *testing.T, dir string) []crew.Process {
	t.Helper()
	rows, err := read(t, dir).QueryContext(t.Context(),
		"SELECT id, version, folder, started_at FROM processes ORDER BY rowid")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []crew.Process
	for rows.Next() {
		var p crew.Process
		var ms int64
		if err := rows.Scan(&p.ID, &p.Version, &p.Folder, &ms); err != nil {
			t.Fatal(err)
		}
		p.Start = time.UnixMilli(ms).UTC()
		got = append(got, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

// userVersion returns the file's user_version in dir.
func userVersion(t *testing.T, dir string) int {
	t.Helper()
	var v int
	if err := read(t, dir).QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRecordCreatesTheStoreAndWritesTheProcess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data", "crew")
	s := open(t, dir)

	record(t, s, process(1))

	got := processes(t, dir)
	if len(got) != 1 || got[0] != process(1) {
		t.Errorf("processes = %+v, want %+v", got, process(1))
	}
	var ms int64
	if err := read(t, dir).QueryRowContext(t.Context(), "SELECT started_at FROM processes").Scan(&ms); err != nil {
		t.Fatal(err)
	}
	if ms != t0.Add(time.Second).UnixMilli() {
		t.Errorf("started_at = %d, want %d", ms, t0.Add(time.Second).UnixMilli())
	}
}

func TestRecordAppendsAndKeepsTheMigrationsVersion(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	record(t, s, process(1))
	record(t, s, process(2))

	if got := processes(t, dir); len(got) != 2 || got[0] != process(1) || got[1] != process(2) {
		t.Errorf("processes = %+v, want processes 1 and 2", got)
	}
	if v := userVersion(t, dir); v != migrations {
		t.Errorf("user_version = %d, want %d", v, migrations)
	}
}

func TestRecordWritesToAStoreANewerCrewMigrated(t *testing.T) {
	dir := t.TempDir()
	record(t, open(t, dir), process(1))
	if _, err := read(t, dir).ExecContext(t.Context(), fmt.Sprintf("PRAGMA user_version = %d", migrations+5)); err != nil {
		t.Fatal(err)
	}

	record(t, open(t, dir), process(2))

	if got := processes(t, dir); len(got) != 2 {
		t.Errorf("processes = %+v, want 2", got)
	}
	if v := userVersion(t, dir); v != migrations+5 {
		t.Errorf("user_version = %d, want the newer crew's %d", v, migrations+5)
	}
}

func TestRecordIntoAStoreWithANegativeVersionFailsAndRetries(t *testing.T) {
	dir := t.TempDir()
	db := read(t, dir)
	if _, err := db.ExecContext(t.Context(), "PRAGMA user_version = -1"); err != nil {
		t.Fatal(err)
	}
	s := open(t, dir)

	for range 2 {
		err := s.Record(t.Context(), process(1))
		if err == nil || !strings.Contains(err.Error(), "the store's version -1 is not one crew writes") {
			t.Errorf("Record = %v, want an error naming the negative version", err)
		}
	}
	if v := userVersion(t, dir); v != -1 {
		t.Errorf("user_version = %d, want -1", v)
	}
	if _, err := db.ExecContext(t.Context(), "PRAGMA user_version = 0"); err != nil {
		t.Fatal(err)
	}
	record(t, s, process(1))
	if got := processes(t, dir); len(got) != 1 || got[0] != process(1) {
		t.Errorf("processes = %+v, want %+v", got, process(1))
	}
}

func TestRecordWithoutADataFolderFailsEveryTime(t *testing.T) {
	for _, dir := range []string{"", filepath.Join("relative", "crew")} {
		t.Run(dir, func(t *testing.T) {
			s := open(t, dir)
			for range 2 {
				err := s.Record(t.Context(), process(1))
				if err == nil || !strings.Contains(err.Error(), "no data folder") {
					t.Errorf("Record = %v, want an error saying there is no data folder", err)
				}
			}
		})
	}
}

func TestRecordIntoAFileThatIsNotADatabaseFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "statistics.db"), bytes.Repeat([]byte("garbage "), 512), 0o600); err != nil {
		t.Fatal(err)
	}
	s := open(t, dir)

	if err := s.Record(t.Context(), process(1)); err == nil || !strings.Contains(err.Error(), "not a database") {
		t.Errorf("Record = %v, want SQLite's not a database", err)
	}
}

func TestRecordIntoAReadOnlyFileFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes a read-only file")
	}
	dir := t.TempDir()
	first := open(t, dir)
	record(t, first, process(1))
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "statistics.db"), 0o400); err != nil {
		t.Fatal(err)
	}

	if err := open(t, dir).Record(t.Context(), process(2)); err == nil {
		t.Error("Record into a read-only file succeeded")
	}
}

func TestFactoryRefusesAKeyAndTouchesNoFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crew")

	s, err := sqlite.Factory()(section("path: /tmp/elsewhere.db\n"), dir)

	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Errorf("factory error = %v, want one naming path", err)
	}
	if s != nil {
		t.Errorf("factory built %v, want nil", s)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("stat %s = %v, want it never created", dir, err)
	}
}

func TestCloseBeforeAnyRecordReleasesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crew")
	s, err := sqlite.Factory()(section(""), dir)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close = %v, want nil", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("stat %s = %v, want it never created", dir, err)
	}
}

func TestRecordAfterAFailedOpenTriesAgain(t *testing.T) {
	dir := t.TempDir()
	db := read(t, dir)
	if _, err := db.ExecContext(t.Context(), "CREATE TABLE processes (other TEXT)"); err != nil {
		t.Fatal(err)
	}
	s := open(t, dir)

	if err := s.Record(t.Context(), process(1)); err == nil || !strings.Contains(err.Error(), "apply the migration 1") {
		t.Fatalf("Record = %v, want the first migration's failure", err)
	}
	if _, err := db.ExecContext(t.Context(), "DROP TABLE processes"); err != nil {
		t.Fatal(err)
	}
	record(t, s, process(2))

	if got := processes(t, dir); len(got) != 1 || got[0] != process(2) {
		t.Errorf("processes = %+v, want %+v", got, process(2))
	}
}

func TestRecordingTheSameProcessTwiceFails(t *testing.T) {
	s := open(t, t.TempDir())
	record(t, s, process(1))

	err := s.Record(t.Context(), process(1))
	if err == nil || !strings.Contains(err.Error(), "record the process "+string(process(1).ID)) {
		t.Errorf("Record = %v, want an error naming the process", err)
	}
}

func TestRecordWhereAFileHoldsTheDataFolderFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crew")
	if err := os.WriteFile(dir, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	err := open(t, dir).Record(t.Context(), process(1))
	if err == nil || !strings.Contains(err.Error(), "make the data folder") {
		t.Errorf("Record = %v, want an error making the data folder", err)
	}
}

// unknown is a statistic the store does not know: it is a Process only
// through the method it promotes.
type unknown struct{ crew.Process }

func TestRecordingAStatisticTheStoreDoesNotKnowFails(t *testing.T) {
	dir := t.TempDir()
	s := open(t, dir)

	if err := s.Record(t.Context(), unknown{process(1)}); err == nil || !strings.Contains(err.Error(), "not a statistic") {
		t.Errorf("Record = %v, want an error saying it is not a statistic the store knows", err)
	}
	if got := processes(t, dir); len(got) != 0 {
		t.Errorf("processes = %+v, want none", got)
	}
}
