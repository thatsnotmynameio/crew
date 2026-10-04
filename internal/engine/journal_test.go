package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// journalEngine is an engine rooted in a fresh directory, for the journal.
func journalEngine(t *testing.T) *Engine {
	t.Helper()
	return &Engine{cfg: Config{Root: t.TempDir()}}
}

// record is a record of event for the lfg action of the issue keyed key.
func record(event core.RunEvent, key string) core.RunRecord {
	const action = "lfg"
	name := "issue-" + key + "-" + action
	r := core.RunRecord{
		Event: event, At: time.Date(2026, 10, 2, 21, 5, 0, 0, time.UTC), IssueKey: key, IssueRef: "#" + key,
		Stage: "development", Action: action, Workspace: name, Branch: "crew/" + name, Log: ".crew/logs/" + name + ".log",
	}
	if event == core.RunEnded {
		r.Reason = "no pull request was found"
	}
	return r
}

func TestTheJournalReadsBackWhatWasAppendedInOrder(t *testing.T) {
	e := journalEngine(t)
	want := []core.RunRecord{
		record(core.RunStarted, "9"),
		record(core.RunStarted, "10"),
		record(core.RunEnded, "9"),
	}
	want[2].Succeeded = false
	for _, r := range want {
		if err := e.appendJournal(r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := e.readJournal()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("records:\n got %#v\nwant %#v", got, want)
	}
	data, err := os.ReadFile(filepath.Join(e.cfg.Root, ".crew", "logs", "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	wantEnded := `{"v":1,"event":"ended","time":"2026-10-02T21:05:00Z","issue":"9","ref":"#9","stage":"development",` +
		`"action":"lfg","workspace":"issue-9-lfg","branch":"crew/issue-9-lfg","log":".crew/logs/issue-9-lfg.log",` +
		`"succeeded":false,"reason":"no pull request was found","pull_request_lookup":"not looked up"}`
	if lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"); len(lines) != 3 || lines[2] != wantEnded {
		t.Fatalf("journal = %q, want its last line %s", data, wantEnded)
	}
}

func TestAJournalLineCutShortIsSkippedAndTheNextAppendStartsItsOwnLine(t *testing.T) {
	e := journalEngine(t)
	first := record(core.RunStarted, "9")
	if err := e.appendJournal(first); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.cfg.Root, ".crew", "logs", "runs.jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"v":1,"event":"ended","issue":"9"`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	next := record(core.RunStarted, "10")
	if err := e.appendJournal(next); err != nil {
		t.Fatal(err)
	}
	got, err := e.readJournal()
	if err != nil {
		t.Fatal(err)
	}
	if want := []core.RunRecord{first, next}; !reflect.DeepEqual(got, want) {
		t.Fatalf("records:\n got %#v\nwant %#v", got, want)
	}
}

func TestAMissingJournalHoldsNoRecords(t *testing.T) {
	got, err := journalEngine(t).readJournal()
	if err != nil || got != nil {
		t.Fatalf("readJournal = %v, %v, want no records and no error", got, err)
	}
}

func TestAJournalThatCannotBeReadFailsPrepareNamingIt(t *testing.T) {
	e := journalEngine(t)
	// A directory where the journal goes cannot be read as a file.
	if err := os.MkdirAll(filepath.Join(e.cfg.Root, ".crew", "logs", "runs.jsonl"), 0o750); err != nil {
		t.Fatal(err)
	}

	err := e.Prepare(t.Context())
	if err == nil || !strings.Contains(err.Error(), ".crew/logs/runs.jsonl") {
		t.Fatalf("Prepare = %v, want an error naming .crew/logs/runs.jsonl", err)
	}
}

func TestAReportedZeroCostIsWrittenAndAnUnreportedOneLeftOut(t *testing.T) {
	r := record(core.RunEnded, "9")
	r.SessionStarted = r.At.Add(-time.Minute)
	r.Usage = crew.Usage{HasCost: true}
	data, err := json.Marshal(lineOf(r, "run"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"duration_ms":60000,"cost_usd":0,`) || strings.Contains(string(data), "tokens") {
		t.Fatalf("line = %s, want a cost of 0 and no tokens", data)
	}
}

func TestAnEndWithoutAWorkspaceIsWrittenAndSkippedOnRead(t *testing.T) {
	e := journalEngine(t)
	r := record(core.RunEnded, "9")
	r.Workspace, r.Branch, r.Log = "", "", ""
	if err := e.appendJournal(r); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(e.cfg.Root, ".crew", "logs", "runs.jsonl"))
	if err != nil || !strings.Contains(string(data), `"event":"ended"`) {
		t.Fatalf("journal = %q, %v, want the end written", data, err)
	}
	if got, err := e.readJournal(); err != nil || len(got) != 0 {
		t.Fatalf("readJournal = %#v, %v, want it skipped", got, err)
	}
}

// appendAll appends records to e's journal, the first at start and each
// next one a minute later.
func appendAll(t *testing.T, e *Engine, start time.Time, records ...core.RunRecord) {
	t.Helper()
	for i, r := range records {
		r.At = start.Add(time.Duration(i) * time.Minute)
		if err := e.appendJournal(r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
}

func TestUnendedRunsAreTheWorkspacesWhoseLatestRecordIsAStart(t *testing.T) {
	e := journalEngine(t)
	start := time.Date(2026, 10, 3, 14, 2, 0, 0, time.UTC)
	appendAll(t, e, start,
		record(core.RunStarted, "9"),  // ended below: not returned
		record(core.RunStarted, "10"), // ended, then resumed: the second start
		record(core.RunEnded, "9"),
		record(core.RunEnded, "10"),
		record(core.RunStarted, "11"), // never ended
		record(core.RunStarted, "10"),
	)

	got, err := UnendedRuns(e.cfg.Root)
	if err != nil {
		t.Fatalf("UnendedRuns: %v", err)
	}
	want := map[string]time.Time{
		"issue-11-lfg": start.Add(4 * time.Minute),
		"issue-10-lfg": start.Add(5 * time.Minute),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UnendedRuns = %v, want %v", got, want)
	}
}

func TestUnendedRunsSkipALineCutShortAndCountTheLinesAroundIt(t *testing.T) {
	e := journalEngine(t)
	start := time.Date(2026, 10, 3, 14, 2, 0, 0, time.UTC)
	appendAll(t, e, start, record(core.RunStarted, "9"), record(core.RunStarted, "10"))
	path := filepath.Join(e.cfg.Root, ".crew", "logs", "runs.jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The end of issue 9's run, cut short by a crash.
	if _, err := f.WriteString(`{"v":1,"event":"ended","issue":"9","workspace":"issue-9-lfg"`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	ended := record(core.RunEnded, "10")
	ended.At = start.Add(time.Hour)
	if err := e.appendJournal(ended); err != nil {
		t.Fatal(err)
	}

	got, err := UnendedRuns(e.cfg.Root)
	if err != nil {
		t.Fatalf("UnendedRuns: %v", err)
	}
	if want := map[string]time.Time{"issue-9-lfg": start}; !reflect.DeepEqual(got, want) {
		t.Fatalf("UnendedRuns = %v, want %v", got, want)
	}
}

func TestAMissingJournalHoldsNoUnendedRuns(t *testing.T) {
	got, err := UnendedRuns(t.TempDir())
	if err != nil || len(got) != 0 {
		t.Fatalf("UnendedRuns = %v, %v, want none and no error", got, err)
	}
}

func TestAJournalThatCannotBeReadFailsUnendedRunsNamingIt(t *testing.T) {
	root := t.TempDir()
	// A directory where the journal goes cannot be read as a file.
	if err := os.MkdirAll(filepath.Join(root, ".crew", "logs", "runs.jsonl"), 0o750); err != nil {
		t.Fatal(err)
	}

	got, err := UnendedRuns(root)
	if err == nil || !strings.Contains(err.Error(), ".crew/logs/runs.jsonl") || got != nil {
		t.Fatalf("UnendedRuns = %v, %v, want an error naming .crew/logs/runs.jsonl", got, err)
	}
}
