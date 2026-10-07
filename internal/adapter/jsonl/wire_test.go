package jsonl_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The wire names of a run's ending are spelled out here, not taken from the
// adapter's constants, so a rename of the events cannot change the journal's
// lines unnoticed.
func TestARunsEndingKeepsItsWireNames(t *testing.T) {
	j, root := journal(t)
	events := endingEvents()[:3]
	appendAll(t, j, events...)

	got := lines(t, root)
	for i, want := range []string{"run_judged", "verdict_moved", "verdict_dropped"} {
		if got[i]["type"] != want {
			t.Errorf("line %d type = %v, want %s", i, got[i]["type"], want)
		}
	}
	if failures, ok := got[0]["failures"].([]any); !ok || len(failures) != 2 {
		t.Errorf("run_judged line = %v, want its two failures under failures", got[0])
	}
}

func TestVersion2LinesOfARunsEndingDecodeToItsEvents(t *testing.T) {
	j, root := journal(t)
	const head = `"rule_run":"development-1","issue":"9","ref":"#9","stage":"development"`
	written := `{"v":2,"type":"run_judged","time":"2026-10-07T09:00:18Z",` + head +
		`,"to":"needs attention","failures":[{"action":"lfg","workspace":"issue-9-lfg",` +
		`"log":".crew/logs/issue-9-lfg.log"},{"action":"review"}]}` + "\n" +
		`{"v":2,"type":"verdict_moved","time":"2026-10-07T09:00:19Z",` + head +
		`,"from":"in progress","to":"needs attention"}` + "\n" +
		`{"v":2,"type":"verdict_dropped","time":"2026-10-07T09:00:20Z",` + head +
		`,"to":"needs attention","reason":"the issue moved meanwhile"}` + "\n"
	if err := os.MkdirAll(filepath.Join(root, ".crew", "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := j.Load(repository)
	if want := endingEvents()[:3]; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %#v, %v, want %#v", got, err, want)
	}
}
