package jsonl_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// writeV1 writes lines, each a JSON object, as the journal under root.
func writeV1(t *testing.T, root string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".crew", "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The version 1 lines of a run of lfg in development on issue #9, as
// crew wrote them before it journaled run events: its start, then its end
// after a minute's session that used something and found no pull request.
const (
	v1Started = `{"v":1,"event":"started","time":"2026-10-02T21:00:00Z","run":"2026-10-02T20:00:00Z","issue":"9",` +
		`"ref":"#9","stage":"development","action":"lfg","workspace":"issue-9-lfg","branch":"crew/issue-9-lfg",` +
		`"log":".crew/logs/issue-9-lfg.log"}`
	v1Ended = `{"v":1,"event":"ended","time":"2026-10-02T21:05:00Z","run":"2026-10-02T20:00:00Z","issue":"9",` +
		`"ref":"#9","stage":"development","action":"lfg","workspace":"issue-9-lfg","branch":"crew/issue-9-lfg",` +
		`"log":".crew/logs/issue-9-lfg.log","succeeded":false,"reason":"no pull request was found",` +
		`"duration_ms":60000,"cost_usd":1.5,"pull_request_lookup":"none"}`
)

// v1Head is the head of a version 1 line of issue #9 in development, in
// repository, at minute m past 21:00 on 2 October 2026, written by the
// crew run process.
func v1Head(process string, m int) crew.EventHead {
	return crew.EventHead{
		Run: crew.RuleRunID("v1/" + process + "/9/development"), At: time.Date(2026, 10, 2, 21, m, 0, 0, time.UTC),
		IssueID: crew.IssueID{Repository: repository, Key: "9"}, IssueRef: "#9", Rule: "development",
	}
}

func TestAVersion1StartAndEndLoadAsAnActionsStartAndEnd(t *testing.T) {
	j, root := journal(t)
	writeV1(t, root, v1Started, v1Ended)

	got, err := j.Load(repository)
	if err != nil {
		t.Fatal(err)
	}
	h := v1Head("2026-10-02T20:00:00Z", 0)
	ended := v1Head("2026-10-02T20:00:00Z", 5)
	want := []crew.RunEvent{
		crew.ActionOpened{EventHead: h, Action: "lfg", Workspace: space, Log: logPath},
		crew.ActionEnded{
			EventHead: ended, Action: "lfg",
			End:            crew.EndFailed{Reason: crew.NewSessionText("no pull request was found")},
			Workspace:      crew.Some(crew.OpenedWorkspace{Workspace: space, Log: logPath}),
			SessionStarted: crew.Some(ended.At.Add(-time.Minute)), Usage: crew.Usage{Cost: crew.Some(1.5)},
			PullRequest: crew.PullRequestNone{},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\n got %#v\nwant %#v", got, want)
	}
}

func TestOlderVersion1LinesStillLoad(t *testing.T) {
	j, root := journal(t)
	// Before the session cost, lines held no crew run and no duration.
	older := strings.Replace(v1Ended, `"run":"2026-10-02T20:00:00Z",`, "", 1)
	older = strings.Replace(older, `"duration_ms":60000,`, "", 1)
	writeV1(t, root, older)

	got, err := j.Load(repository)
	if err != nil || len(got) != 1 {
		t.Fatalf("Load = %#v, %v, want one event", got, err)
	}
	e, ok := got[0].(crew.ActionEnded)
	if !ok || e.Run != "v1//9/development" {
		t.Fatalf("event = %#v, want an end in the run v1//9/development", got[0])
	}
	// Its reason is the one a resume quotes, so it names a session start.
	if started, ok := e.SessionStarted.Get(); !ok || !started.Equal(e.At) {
		t.Errorf("session started %v, want at the line's time", e.SessionStarted)
	}
}

func TestVersion1LinesItCannotUnderstandAreSkipped(t *testing.T) {
	j, root := journal(t)
	writeV1(t, root,
		// An end without a workspace: its action never had one.
		strings.Replace(v1Ended, `"workspace":"issue-9-lfg",`, "", 1),
		// An end without its outcome.
		strings.Replace(v1Ended, `"succeeded":false,`, "", 1),
		// An event version 1 never had.
		strings.Replace(v1Started, `"event":"started"`, `"event":"paused"`, 1),
		// A line without its issue.
		strings.Replace(v1Started, `"issue":"9",`, "", 1),
	)

	if got, err := j.Load(repository); err != nil || len(got) != 0 {
		t.Fatalf("Load = %#v, %v, want every line skipped", got, err)
	}
}

func TestAVersion1ReasonWithAControlByteLoadsWithASpaceInItsPlace(t *testing.T) {
	j, root := journal(t)
	writeV1(t, root, strings.Replace(v1Ended, "no pull request was found", `bo\u0000om`, 1))

	got, err := j.Load(repository)
	if err != nil || len(got) != 1 {
		t.Fatalf("Load = %#v, %v, want one event", got, err)
	}
	e, ok := got[0].(crew.ActionEnded)
	if !ok || e.End.Outcome().Reason.String() != "bo om" {
		t.Errorf("event = %#v, want an end whose reason is %q", got[0], "bo om")
	}
}
