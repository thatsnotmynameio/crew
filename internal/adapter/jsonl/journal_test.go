package jsonl_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/adapter/jsonl"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// path is where the tests' journals are, as the engine names it.
const path = ".crew/logs/runs.jsonl"

// repository is the repository the tests load their events in, and
// process the crew process that appends them.
const (
	repository crew.RepositoryID = "R_journal"
	process    string            = "2026-10-07T09:00:00Z"
)

var t0 = time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)

// journal returns a journal in a fresh repository root, and that root.
func journal(t *testing.T) (*jsonl.Journal, string) {
	t.Helper()
	root := t.TempDir()
	return jsonl.New(root, path, process), root
}

// head is the head of an event of run development-1 at second n, of the
// rule development on issue #9.
func head(n int) crew.EventHead {
	return crew.EventHead{
		Run: "development-1", At: t0.Add(time.Duration(n) * time.Second),
		IssueID: crew.IssueID{Repository: repository, Key: "9"}, IssueRef: "#9", Rule: "development",
	}
}

// space is the workspace of the action lfg on issue #9.
var space = crew.Workspace{Name: "issue-9-lfg", Branch: "crew/issue-9-lfg"}

const logPath = ".crew/logs/issue-9-lfg.log"

var (
	usage = crew.Usage{
		Cost: crew.Some(12.40), Tokens: crew.Some(crew.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40}),
		Turns: crew.Some(7), Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"},
	}
	pr45 = crew.PullRequestFound{Ref: "#45", URL: "https://example.test/pull/45"}
)

// everyEvent is one event of each type, every field set, in the order a
// run could have them.
func everyEvent() []crew.RunEvent {
	return slices.Concat(takeEvents(), actionEvents(), verdictEvents())
}

// takeEvents are the events of a take, and a stop.
func takeEvents() []crew.RunEvent {
	return []crew.RunEvent{
		crew.RunTaken{
			EventHead: head(0), From: "ready", To: "in progress", Continues: crew.Some[crew.RuleRunID]("development-0"),
			Issue: crew.IssueData{
				ID: head(0).IssueID, Ref: "#9", Title: "Fix the login", URL: "https://example.test/issues/9",
				Created: t0.Add(-time.Hour), Priority: 2, States: []crew.State{"ready"}, Blocked: true,
				Kind: crew.KindPullRequest,
			},
			Actions: []crew.ActionTaken{
				{Name: "lfg", Resume: crew.Some(crew.ResumePoint{
					Workspace: space, Log: logPath, Reason: crew.NewSessionText("no pull request was found"),
				})},
				{Name: "review"},
			},
		},
		crew.TakeMoved{EventHead: head(1), From: "ready", To: "in progress"},
		crew.RunStopped{EventHead: head(2)},
	}
}

// failure is how the lfg action fails.
var failure = crew.EndFailed{Reason: crew.NewSessionText("tests fail"), Cause: crew.CauseCheck}

// actionEvents are the events of two actions, from their workspaces to
// their ends.
func actionEvents() []crew.RunEvent {
	return []crew.RunEvent{
		crew.ActionWorkspaceAsked{EventHead: head(3), Action: "lfg", Reopen: crew.Some(space)},
		crew.ActionWorkspaceAsked{EventHead: head(3), Action: "review"},
		crew.WorkspaceMissing{EventHead: head(4), Action: "lfg", Workspace: space},
		crew.ActionOpened{EventHead: head(5), Action: "lfg", Workspace: space, Log: logPath, Resumed: true},
		crew.ActionSessionAsked{EventHead: head(6), Action: "lfg"},
		crew.ActionSessionStarted{EventHead: head(7), Action: "lfg", Workspace: space, Log: logPath, Resumed: true},
		crew.ActionSessionStopAsked{EventHead: head(8), Action: "lfg"},
		crew.ActionSessionEnded{
			EventHead: head(9), Action: "lfg", Usage: usage,
			Outcome: crew.Outcome{Succeeded: true, Reason: crew.NewSessionText("done")},
		},
		crew.ActionLookupAsked{EventHead: head(10), Action: "lfg"},
		crew.ActionCheckAsked{EventHead: head(11), Action: "lfg", Check: "pr-open"},
		crew.ActionCheckStopAsked{EventHead: head(12), Action: "lfg"},
		crew.ActionCheckEnded{EventHead: head(13), Action: "lfg", Result: crew.CheckResult{
			Name: "pr-open", Reason: crew.NewCheckReason("the check failed: no pull request"),
		}},
		crew.ActionLookupDone{EventHead: head(14), Action: "lfg", PullRequest: pr45},
		crew.ActionFinishing{EventHead: head(15), Action: "lfg", End: failure},
		crew.ActionEnded{
			EventHead: head(16), Action: "lfg", End: failure,
			Workspace:      crew.Some(crew.OpenedWorkspace{Workspace: space, Log: logPath, Resumed: true, Opened: t0}),
			SessionStarted: crew.Some(t0.Add(7 * time.Second)), Usage: usage, PullRequest: pr45,
		},
		crew.ActionEnded{
			EventHead: head(17), Action: "review", PullRequest: crew.PullRequestNone{},
			End: crew.EndSucceeded{Reason: crew.NewSessionText("crew stopped")},
		},
	}
}

// verdictEvents are the events of a verdict and the run's release.
func verdictEvents() []crew.RunEvent {
	return []crew.RunEvent{
		crew.RunJudged{EventHead: head(18), Verdict: crew.Verdict{To: "needs attention", Failures: []crew.ActionFailure{
			{Action: "lfg", Workspace: "issue-9-lfg", Log: logPath}, {Action: "review"},
		}}},
		crew.VerdictMoved{EventHead: head(19), From: "in progress", To: "needs attention"},
		crew.VerdictDropped{EventHead: head(20), To: "needs attention", Reason: "the issue moved meanwhile"},
		crew.FailureReported{EventHead: head(21)},
		crew.FailureReportDropped{EventHead: head(22)},
		crew.RunReleased{EventHead: head(23)},
	}
}

// appendAll appends events to j, failing the test on the first error.
func appendAll(t *testing.T, j *jsonl.Journal, events ...crew.RunEvent) {
	t.Helper()
	for _, e := range events {
		if err := j.Append(e); err != nil {
			t.Fatalf("Append(%#v): %v", e, err)
		}
	}
}

// lines returns the journal under root, one decoded object per line.
func lines(t *testing.T, root string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for text := range strings.Lines(string(data)) {
		var l map[string]any
		if err := json.Unmarshal([]byte(text), &l); err != nil {
			t.Fatalf("journal line %q: %v", text, err)
		}
		out = append(out, l)
	}
	return out
}

func TestEveryRunEventAppendsAndLoadsBackEqualInOrder(t *testing.T) {
	j, _ := journal(t)
	want := everyEvent()
	appendAll(t, j, want...)

	got, err := j.Load(repository)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\n got %#v\nwant %#v", got, want)
	}
}

func TestEveryLineCarriesTheCrewRunAndTheEndedOnesAreOnePerEndedAction(t *testing.T) {
	j, root := journal(t)
	appendAll(t, j, everyEvent()...)

	var ended, started []map[string]any
	for _, l := range lines(t, root) {
		if l["run"] != process || l["v"] != 2.0 {
			t.Errorf("line %v, want version 2 of the crew run %s", l, process)
		}
		switch l["event"] {
		case "ended":
			ended = append(ended, l)
		case "started":
			started = append(started, l)
		}
	}
	if len(ended) != 2 || ended[0]["action"] != "lfg" || ended[1]["action"] != "review" {
		t.Errorf("ended lines = %v, want lfg's then review's", ended)
	}
	if len(started) != 1 || started[0]["type"] != "action_opened" {
		t.Errorf("started lines = %v, want lfg's start", started)
	}
}

func TestAnActionsStartAndEndKeepTheirVersion1Keys(t *testing.T) {
	j, root := journal(t)
	events := everyEvent()
	appendAll(t, j, events[6], events[17])

	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":2,"type":"action_opened","event":"started","time":"2026-10-07T09:00:05Z",` +
		`"run":"2026-10-07T09:00:00Z","rule_run":"development-1","issue":"9","ref":"#9","stage":"development",` +
		`"action":"lfg","workspace":"issue-9-lfg","branch":"crew/issue-9-lfg","log":".crew/logs/issue-9-lfg.log",` +
		`"resumed":true}` + "\n" +
		`{"v":2,"type":"action_ended","event":"ended","time":"2026-10-07T09:00:16Z",` +
		`"run":"2026-10-07T09:00:00Z","rule_run":"development-1","issue":"9","ref":"#9","stage":"development",` +
		`"action":"lfg","workspace":"issue-9-lfg","branch":"crew/issue-9-lfg","log":".crew/logs/issue-9-lfg.log",` +
		`"resumed":true,"opened":"2026-10-07T09:00:00Z","succeeded":false,"reason":"tests fail","cause":"check",` +
		`"session_started":"2026-10-07T09:00:07Z","duration_ms":9000,"cost_usd":12.4,"input_tokens":10,` +
		`"output_tokens":20,"cache_read_tokens":300,"cache_write_tokens":40,"turns":7,` +
		`"models":["claude-opus-5-5","claude-sonnet-5-5"],"pull_request":"#45",` +
		`"pull_request_url":"https://example.test/pull/45","pull_request_lookup":"found"}` + "\n"
	if string(data) != want {
		t.Fatalf("journal:\n got %s\nwant %s", data, want)
	}
}

func TestTheEndedLineWritesWhatTheLookupFound(t *testing.T) {
	for _, tt := range []struct {
		pr   crew.PullRequest
		want map[string]any
	}{
		{pr45, map[string]any{
			"pull_request": "#45", "pull_request_url": "https://example.test/pull/45", "pull_request_lookup": "found",
		}},
		{crew.PullRequestNone{}, map[string]any{"pull_request_lookup": "none"}},
		{crew.PullRequestNotLookedUp{}, map[string]any{"pull_request_lookup": "not looked up"}},
		{nil, map[string]any{"pull_request_lookup": "not looked up"}},
	} {
		j, root := journal(t)
		appendAll(t, j, crew.ActionEnded{EventHead: head(1), Action: "lfg", End: crew.EndSucceeded{}, PullRequest: tt.pr})
		l := lines(t, root)[0]
		for _, k := range []string{"pull_request", "pull_request_url", "pull_request_lookup"} {
			if l[k] != tt.want[k] {
				t.Errorf("%T: %s = %v, want %v", tt.pr, k, l[k], tt.want[k])
			}
		}
	}
}

func TestAReportedZeroCostIsWrittenAndAnUnreportedOneLeftOut(t *testing.T) {
	j, root := journal(t)
	appendAll(t, j, crew.ActionEnded{
		EventHead: head(60), Action: "lfg", End: crew.EndSucceeded{}, SessionStarted: crew.Some(t0),
		Usage: crew.Usage{Cost: crew.Some(0.0)},
	})

	l := lines(t, root)[0]
	if cost, ok := l["cost_usd"]; !ok || cost != 0.0 || l["duration_ms"] != 60_000.0 {
		t.Errorf("line = %v, want a cost of 0 and a minute's duration", l)
	}
	for _, k := range []string{"input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "turns"} {
		if v, ok := l[k]; ok {
			t.Errorf("line has %s = %v, want none", k, v)
		}
	}
}

func TestLinesItCannotReadAreSkippedAndTheNextAppendStartsItsOwnLine(t *testing.T) {
	j, root := journal(t)
	first := crew.RunStopped{EventHead: head(1)}
	appendAll(t, j, first)
	f, err := os.OpenFile(filepath.Join(root, path), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	unread := `{"v":3,"type":"run_stopped","issue":"9"}` + "\n" +
		`{"v":2,"type":"run_paused","issue":"9"}` + "\n" +
		`{"v":2,"type":"run_stopped","issue":"9"`
	if _, err := f.WriteString(unread); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	next := crew.RunReleased{EventHead: head(2)}
	appendAll(t, j, next)
	got, err := j.Load(repository)
	if err != nil {
		t.Fatal(err)
	}
	if want := []crew.RunEvent{first, next}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\n got %#v\nwant %#v", got, want)
	}
}

func TestAMissingJournalOrAFileWhereItsDirectoryGoesHoldsNoEvents(t *testing.T) {
	j, root := journal(t)
	if got, err := j.Load(repository); err != nil || got != nil {
		t.Fatalf("Load of a missing journal = %v, %v, want no events and no error", got, err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".crew", "logs"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := j.Load(repository); err != nil || got != nil {
		t.Fatalf("Load under a file = %v, %v, want no events and no error", got, err)
	}
}

func TestAJournalThatCannotBeReadIsAnErrorNamingIt(t *testing.T) {
	j, root := journal(t)
	// A directory where the journal goes cannot be read as a file.
	if err := os.MkdirAll(filepath.Join(root, path), 0o750); err != nil {
		t.Fatal(err)
	}

	_, err := j.Load(repository)
	if err == nil || !strings.HasPrefix(err.Error(), "read the run journal .crew/logs/runs.jsonl: ") {
		t.Fatalf("Load = %v, want an error naming .crew/logs/runs.jsonl", err)
	}
}

func TestAJournalThatCannotBeWrittenIsAnErrorNamingIt(t *testing.T) {
	j, root := journal(t)
	appendAll(t, j, crew.RunStopped{EventHead: head(1)})
	if err := os.Chmod(filepath.Join(root, path), 0o400); err != nil {
		t.Fatal(err)
	}

	err := j.Append(crew.RunReleased{EventHead: head(2)})
	if err == nil || !strings.HasPrefix(err.Error(), "open the run journal: open for appending: ") ||
		!strings.Contains(err.Error(), filepath.Join(root, path)) {
		t.Fatalf("Append = %v, want an error opening the journal, naming its path", err)
	}
	if got, _ := j.Load(repository); len(got) != 1 {
		t.Errorf("journal holds %d events, want the one appended before", len(got))
	}
}

func TestNewLinesAreAppendedAndEarlierOnesKeptAsTheyWere(t *testing.T) {
	j, root := journal(t)
	const earlier = `{"v":1,"event":"ended","time":"2026-01-01T10:00:00Z","issue":"9","ref":"#9","stage":"implement",` +
		`"action":"development","workspace":"issue-9-development","branch":"crew/issue-9-development",` +
		`"log":".crew/logs/issue-9-development.log","succeeded":true,"reason":"done","cost_usd":6.89}`
	if err := os.MkdirAll(filepath.Join(root, ".crew", "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(earlier+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	appendAll(t, j, everyEvent()[:2]...)

	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), earlier+"\n") {
		t.Fatalf("journal = %q, want the earlier line first and unchanged", data)
	}
	got := lines(t, root)
	if len(got) != 3 || got[1]["run"] != process || got[2]["run"] != process {
		t.Fatalf("journal = %v, want two new lines of the crew run after the earlier one", got)
	}
}

func TestEveryFailureCauseLoadsBackAsItself(t *testing.T) {
	j, _ := journal(t)
	causes := []crew.FailureCause{
		crew.CauseSession, crew.CauseCheck, crew.CauseStopped, crew.CauseWorkspace, crew.CauseStart, crew.CausePrompt,
	}
	want := make([]crew.RunEvent, 0, len(causes))
	for i, cause := range causes {
		want = append(want, crew.ActionFinishing{
			EventHead: head(i), Action: "lfg", End: crew.EndFailed{Reason: crew.NewSessionText("broke"), Cause: cause},
		})
	}
	appendAll(t, j, want...)

	got, err := j.Load(repository)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %#v, %v, want %#v", got, err, want)
	}
}
