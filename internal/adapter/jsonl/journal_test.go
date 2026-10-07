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

// space is the run's worktree on issue #9.
var space = crew.Workspace{Name: "issue-9-development", Branch: "crew/issue-9-development"}

const logPath = ".crew/logs/issue-9-development.log"

var (
	usage = crew.Usage{
		Cost: crew.Some(12.40), Tokens: crew.Some(crew.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40}),
		Turns: crew.Some(7), Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"},
	}
	pr45      = crew.PullRequestFound{Ref: "#45", URL: "https://example.test/pull/45"}
	developer = crew.Bot{Name: "crew-developer"}
	// resumed is the start of a run that resumes at lfg the work of a run
	// that ended through no-pr.
	resumed = crew.StartAt{
		Workspace: space, Log: logPath, Action: "lfg", Route: "no-pr",
		Reason:  crew.NewSessionText("no pull request was found"),
		Session: crew.Some(crew.LatestSession{Action: "lfg", Bot: developer}),
	}
)

// everyEvent is one event of each type, every field set, in the order a
// run could have them.
func everyEvent() []crew.RunEvent {
	return slices.Concat(takeEvents(), actionEvents(), routeEvents())
}

// takeEvents are the events of a take, a stop, time-up and the run's
// worktree.
func takeEvents() []crew.RunEvent {
	return []crew.RunEvent{
		crew.RunTaken{
			EventHead: head(0), From: "ready", To: "in progress", Continues: crew.Some[crew.RuleRunID]("development-0"),
			Issue: crew.IssueData{
				ID: head(0).IssueID, Ref: "#9", Title: "Fix the login", URL: "https://example.test/issues/9",
				Created: t0.Add(-time.Hour), Priority: 2, States: []crew.State{"ready"}, Blocked: true,
				Kind: crew.KindPullRequest,
			},
			Actions: []crew.ActionName{"install", "lfg", "judge"}, Start: resumed,
		},
		crew.TakeMoved{EventHead: head(1), From: "ready", To: "in progress"},
		crew.RunStopped{EventHead: head(2)},
		crew.RunOutOfTime{EventHead: head(2)},
		crew.WorkspaceAsked{EventHead: head(3), Reopen: crew.Some(space)},
		crew.WorkspaceMissing{EventHead: head(4), Workspace: space},
		crew.WorkspaceAsked{EventHead: head(4)},
		crew.WorkspaceOpened{EventHead: head(5), Workspace: space, Log: logPath, Resumed: true},
	}
}

// failure is how the judge action fails.
var failure = crew.EndFailed{Reason: crew.NewSessionText("judge: exit status 1"), Cause: crew.CauseShell}

// actionEvents are the events of a session action and a shell action,
// from their starts to their ends.
func actionEvents() []crew.RunEvent {
	return []crew.RunEvent{
		crew.ActionSessionAsked{EventHead: head(6), Action: "lfg"},
		crew.ActionSessionStarted{EventHead: head(7), Action: "lfg", Bot: developer},
		crew.ActionSessionStopAsked{EventHead: head(8), Action: "lfg"},
		crew.ActionSessionEnded{
			EventHead: head(9), Action: "lfg", Usage: usage,
			Outcome: crew.Outcome{Succeeded: true, Reason: crew.NewSessionText("done")},
		},
		crew.ActionEnded{
			EventHead: head(10), Action: "lfg", End: crew.EndSucceeded{Reason: crew.NewSessionText("done")},
			Verdict: crew.Passed, Target: crew.Next{}, SessionStarted: crew.Some(t0.Add(7 * time.Second)), Usage: usage,
		},
		crew.ActionShellAsked{EventHead: head(11), Action: "judge", Bot: developer},
		crew.ActionShellStopAsked{EventHead: head(12), Action: "judge"},
		crew.ActionShellEnded{EventHead: head(13), Action: "judge", Outcome: crew.ShellOutcome{
			Status: crew.Some(1), Reason: crew.NewShellReason("judge: exit status 1"),
		}},
		crew.ActionEnded{
			EventHead: head(14), Action: "judge", End: failure, Verdict: crew.Failed,
			Target: crew.ToRoute{Route: crew.FailedRoute},
		},
	}
}

// routeEvents are the events of the run's route, one step of each kind,
// and its release.
func routeEvents() []crew.RunEvent {
	return []crew.RunEvent{
		crew.RouteChosen{EventHead: head(15), Route: crew.FailedRoute, Action: "judge", Steps: []crew.StepPlan{
			{Kind: crew.StepComment}, {Kind: crew.StepReport}, {Kind: crew.StepShell, Shell: "notify"},
			{Kind: crew.StepMove, To: "needs attention"}, {Kind: crew.StepClose},
		}},
		crew.RunLookupAsked{EventHead: head(16)},
		crew.RunLookupDone{EventHead: head(17), PullRequest: pr45},
		crew.StepAsked{EventHead: head(18), Step: 0},
		crew.StepEnded{EventHead: head(19), Step: 0, Outcome: crew.StepLanded{}},
		crew.StepAsked{EventHead: head(20), Step: 2},
		crew.StepShellStopAsked{EventHead: head(21), Step: 2},
		crew.StepEnded{EventHead: head(22), Step: 2, Outcome: crew.StepStopped{Reason: crew.NewShellReason("stopped")}},
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

// writeJournal writes lines, each a JSON object, as the journal under
// root.
func writeJournal(t *testing.T, root string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".crew", "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// roundTrip appends want and fails the test unless the journal loads it
// back equal.
func roundTrip(t *testing.T, want []crew.RunEvent) {
	t.Helper()
	j, _ := journal(t)
	appendAll(t, j, want...)
	got, err := j.Load(repository)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Load =\n%#v, %v\nwant\n%#v", got, err, want)
	}
}

func TestEveryRunEventAppendsAndLoadsBackEqualInOrder(t *testing.T) {
	roundTrip(t, everyEvent())
}

func TestEveryLineIsVersion3OfTheCrewRunAndAnActionsLinesMarkItsStartAndEnd(t *testing.T) {
	j, root := journal(t)
	appendAll(t, j, everyEvent()...)

	var ended, started []any
	for _, l := range lines(t, root) {
		if l["run"] != process || l["v"] != 3.0 {
			t.Errorf("line %v, want version 3 of the crew run %s", l, process)
		}
		switch l["event"] {
		case "ended":
			ended = append(ended, l["action"])
		case "started":
			started = append(started, l["action"])
		}
	}
	if want := []any{"lfg", "judge"}; !reflect.DeepEqual(ended, want) || !reflect.DeepEqual(started, want) {
		t.Errorf("started %v and ended %v, want lfg's then judge's", started, ended)
	}
}

func TestTheEndedLineIsTheCostRecord(t *testing.T) {
	j, root := journal(t)
	appendAll(t, j, actionEvents()[4])

	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":3,"type":"action_ended","event":"ended","time":"2026-10-07T09:00:10Z",` +
		`"run":"2026-10-07T09:00:00Z","rule_run":"development-1","issue":"9","ref":"#9","stage":"development",` +
		`"action":"lfg","succeeded":true,"reason":"done","verdict":"passed","target":"next",` +
		`"session_started":"2026-10-07T09:00:07Z","duration_ms":3000,"cost_usd":12.4,"input_tokens":10,` +
		`"output_tokens":20,"cache_read_tokens":300,"cache_write_tokens":40,"turns":7,` +
		`"models":["claude-opus-5-5","claude-sonnet-5-5"]}` + "\n"
	if string(data) != want {
		t.Fatalf("journal:\n got %s\nwant %s", data, want)
	}
}

func TestTheLookupLineWritesWhatItFound(t *testing.T) {
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
		appendAll(t, j, crew.RunLookupDone{EventHead: head(1), PullRequest: tt.pr})
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
	unread := `{"v":4,"type":"run_stopped","issue":"9"}` + "\n" +
		`{"v":3,"type":"run_paused","issue":"9"}` + "\n" +
		`{"v":3,"type":"run_stopped"}` + "\n" +
		`{"v":3,"type":"run_stopped","issue":"9"`
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

func TestVersion1And2LinesAreSkippedSoTheIssueStartsFresh(t *testing.T) {
	j, root := journal(t)
	writeJournal(t, root,
		`{"v":1,"event":"started","time":"2026-10-02T21:00:00Z","issue":"9","ref":"#9","stage":"development",`+
			`"action":"lfg","workspace":"issue-9-lfg","branch":"crew/issue-9-lfg","log":".crew/logs/issue-9-lfg.log"}`,
		`{"v":2,"type":"run_taken","time":"2026-10-07T09:00:00Z","rule_run":"development-1","issue":"9",`+
			`"ref":"#9","stage":"development","actions":[{"name":"lfg","workspace":"issue-9-lfg"}]}`,
		`{"v":2,"type":"action_opened","event":"started","time":"2026-10-07T09:00:05Z","rule_run":"development-1",`+
			`"issue":"9","ref":"#9","stage":"development","action":"lfg","workspace":"issue-9-lfg"}`,
	)

	got, err := j.Load(repository)
	if err != nil || len(got) != 0 {
		t.Fatalf("Load = %#v, %v, want every line skipped", got, err)
	}
	var h crew.History
	for _, e := range got {
		h.Fold(e)
	}
	rule := crew.Rule{Name: "development", Actions: []crew.Action{{Name: "lfg", Kind: crew.SessionSpec{}}}}
	if start := h.Start(head(0).IssueID, rule); start != (crew.StartFresh{}) {
		t.Errorf("Start = %#v, want fresh", start)
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
	const earlier = `{"v":2,"type":"action_ended","event":"ended","time":"2026-01-01T10:00:00Z","issue":"9",` +
		`"ref":"#9","stage":"implement","action":"development","workspace":"issue-9-development",` +
		`"succeeded":true,"reason":"done","cost_usd":6.89}`
	writeJournal(t, root, earlier)

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
