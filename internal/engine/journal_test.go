package engine

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
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
		`"succeeded":false,"reason":"no pull request was found"}`
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
