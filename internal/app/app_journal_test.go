package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// journalOf returns the run journal of r's repository, as text.
func journalOf(t *testing.T, r *crewRun) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.opts.Root, ".crew", "logs", "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// endedLines returns the lines of journal, the run journal's text, whose
// event is ended, failing the test unless every line is of the crew run
// testRun.
func endedLines(t *testing.T, journal string) []map[string]any {
	t.Helper()
	var ended []map[string]any
	for line := range strings.Lines(journal) {
		var l map[string]any
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("journal line %q: %v", line, err)
		}
		if l["run"] != testRun {
			t.Errorf("line %v, want it of the crew run %s", l, testRun)
		}
		if l["event"] == "ended" {
			ended = append(ended, l)
		}
	}
	return ended
}

// R20: a run that failed before crew journaled run events still resumes.
func TestAFailedRunInAVersion1JournalResumesInItsWorkspace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h := fake.NewHarness()
		r := options(t, oneAction, tr, h)
		// The journal as crew wrote it before: the run's start and its end.
		journal := `{"v":1,"event":"started","time":"2026-10-02T21:00:00Z","run":"2026-10-02T20:00:00Z",` +
			`"issue":"1","ref":"#1","stage":"implement","action":"development","workspace":"issue-1-development",` +
			`"branch":"crew/issue-1-development","log":".crew/logs/issue-1-development.log"}` + "\n" +
			`{"v":1,"event":"ended","time":"2026-10-02T21:05:00Z","run":"2026-10-02T20:00:00Z",` +
			`"issue":"1","ref":"#1","stage":"implement","action":"development","workspace":"issue-1-development",` +
			`"branch":"crew/issue-1-development","log":".crew/logs/issue-1-development.log","succeeded":false,` +
			`"reason":"no pull request was found","duration_ms":300000,"pull_request_lookup":"not looked up"}` + "\n"
		logs := filepath.Join(r.opts.Root, ".crew", "logs")
		if err := os.MkdirAll(logs, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(logs, "runs.jsonl"), []byte(journal), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(r.opts.Root, ".crew", "worktrees", "issue-1-development"), 0o750); err != nil {
			t.Fatal(err)
		}
		r.start()

		session := next(t, h)
		if got := filepath.Base(session.Run().Dir); got != "issue-1-development" {
			t.Errorf("session runs in %s, want the failed run's issue-1-development", got)
		}
		if p := session.Run().Prompt; !strings.Contains(p, `That run failed: "no pull request was found".`) {
			t.Errorf("prompt does not resume the failed run:\n%s", p)
		}
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
	})
}

// The ended lines are the boss's cost record, read with jq's
// select(.event == "ended") and grouped by run; the journal keeps no
// session's words, no machine path and no prompt.
func TestTheJournalHoldsOneEndedLinePerActionAndNoSessionTextPathOrPrompt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h := fake.NewMessagingHarness()
		r := options(t, oneAction, tr, h)
		r.start()
		session := next(t, h)
		session.SetLastMessage("the session's last words")
		session.End(port.Verdict{Succeeded: true, Reason: "done"})
		synctest.Wait()
		if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Fatalf("#1 is in %v, want ready to review", got)
		}
		r.signals <- syscall.SIGTERM
		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}

		text := journalOf(t, r)
		if ended := endedLines(t, text); len(ended) != 1 || ended[0]["action"] != "development" ||
			ended[0]["succeeded"] != true {
			t.Errorf("ended lines = %v, want development's success alone:\n%s", ended, text)
		}
		for _, held := range []string{"the session's last words", r.opts.Root, session.Run().Prompt} {
			if strings.Contains(text, held) {
				t.Errorf("journal holds %q:\n%s", held, text)
			}
		}
	})
}
