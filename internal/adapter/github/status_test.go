package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// The scripted prefixes of the status comment's gh calls on issue #74.
var (
	listComments  = []string{"api", "--method", "GET", "--paginate", "repos/{owner}/{repo}/issues/74/comments?per_page=100"}
	createComment = []string{"api", "--method", "POST", "repos/{owner}/{repo}/issues/74/comments"}
	editComment   = []string{"api", "--method", "PATCH"}
)

// updated is the update time of the statuses below.
var updated = time.Date(2026, 10, 2, 14, 3, 27, 0, time.UTC)

// commentJSON is one issue comment as the REST API lists it.
func commentJSON(id int, login, body string) string {
	return fmt.Sprintf(`{"id":%d,"user":{"login":%q},"body":%q}`, id, login, body)
}

// statusBody returns the body field of a recorded comment call.
func statusBody(t *testing.T, args []string) string {
	t.Helper()
	bodies := fieldValues(args, "body")
	if len(bodies) != 1 {
		t.Fatalf("call %q has %d body fields, want 1", args, len(bodies))
	}
	return bodies[0]
}

// endsWithMarker reports whether body's last line is the status marker.
func endsWithMarker(body string) bool {
	return strings.HasSuffix(strings.TrimRight(body, " \t\r\n"), "\n"+statusMarker)
}

func queued74() crew.Status {
	return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusQueued, Slots: 2, Updated: updated}
}

func TestFirstStatusCreatesTheCommentAndTheNextEditsIt(t *testing.T) {
	tr, gh := build(t, "", login,
		reply{prefix: listComments, stdout: "[" + commentJSON(5, "me", "Thanks!") + "]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment},
	)
	for range 2 {
		if err := tr.ReportStatus(context.Background(), queued74()); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}

	if n := len(gh.callsTo(listComments...)); n != 1 {
		t.Errorf("listed the comments %d times, want 1", n)
	}
	creates := gh.callsTo(createComment...)
	if len(creates) != 1 {
		t.Fatalf("created %d comments, want 1", len(creates))
	}
	if body := statusBody(t, creates[0]); !endsWithMarker(body) {
		t.Errorf("created body does not end with the marker line:\n%s", body)
	}
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/101") {
		t.Fatalf("edits = %q, want one of comment 101", edits)
	}
	if body := statusBody(t, edits[0]); !endsWithMarker(body) {
		t.Errorf("edited body does not end with the marker line:\n%s", body)
	}
}

// Covers AE5: after a restart, the tracker finds the comment it wrote before.
func TestARestartedTrackerEditsTheViewersNewestStatusComment(t *testing.T) {
	marked := "crew: an older status.\n\n" + statusMarker + "\n"
	// Two pages, as gh --paginate prints them one after the other.
	page1 := "[" + strings.Join([]string{
		commentJSON(7, "someone-else", marked),
		commentJSON(9, "me", marked),
	}, ",") + "]"
	page2 := "[" + strings.Join([]string{
		commentJSON(12, "me", marked),
		commentJSON(15, "me", "Quoting "+statusMarker+" is not a status."),
		commentJSON(20, "someone-else", marked),
	}, ",") + "]"
	tr, gh := build(t, "", login,
		reply{prefix: listComments, stdout: page1 + "\n" + page2 + "\n"},
		reply{prefix: editComment},
	)
	if err := tr.ReportStatus(context.Background(), queued74()); err != nil {
		t.Fatalf("ReportStatus: %v", err)
	}
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/12") {
		t.Errorf("edits = %q, want one of comment 12", edits)
	}
	if creates := gh.callsTo(createComment...); len(creates) != 0 {
		t.Errorf("created %d comments, want none", len(creates))
	}
}

func TestAnEditOfADeletedCommentCreatesItAgain(t *testing.T) {
	tr, gh := build(t, "", login,
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment, stderr: "gh: Not Found (HTTP 404)\n"},
	)
	for range 2 {
		if err := tr.ReportStatus(context.Background(), queued74()); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	if n := len(gh.callsTo(editComment...)); n != 1 {
		t.Errorf("edited %d times, want 1", n)
	}
	if n := len(gh.callsTo(listComments...)); n != 2 {
		t.Errorf("listed the comments %d times, want 2: once more after the 404", n)
	}
	if n := len(gh.callsTo(createComment...)); n != 2 {
		t.Errorf("created %d comments, want 2: once more after the 404", n)
	}
}

func TestStatusErrorsAreClassifiedFromTheHTTPStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		list, create reply
		want         error // nil: transient
	}{
		"issue gone on listing": {
			list: reply{prefix: listComments, stderr: "gh: Not Found (HTTP 404)\n"},
			want: port.ErrMovedMeanwhile,
		},
		"issue gone on create": {
			create: reply{prefix: createComment, stderr: "gh: Not Found (HTTP 404)\n"},
			want:   port.ErrMovedMeanwhile,
		},
		"issue deleted on create": {
			create: reply{prefix: createComment, stderr: "gh: This issue was deleted (HTTP 410)\n"},
			want:   port.ErrMovedMeanwhile,
		},
		"issue locked": {
			create: reply{prefix: createComment, stderr: "gh: Unable to create comment because issue is locked. (HTTP 403)\n"},
			want:   port.ErrRefused,
		},
		"rate limited": {
			create: reply{prefix: createComment, stderr: "gh: You have exceeded a secondary rate limit. (HTTP 403)\n"},
		},
		"network failure": {
			create: reply{prefix: createComment, stderr: "error connecting to api.github.com\n"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			list, create := tc.list, tc.create
			if list.prefix == nil {
				list = reply{prefix: listComments, stdout: "[]"}
			}
			if create.prefix == nil {
				create = reply{prefix: createComment, stdout: "101\n"}
			}
			tr, _ := build(t, "", login, list, create)
			err := tr.ReportStatus(context.Background(), queued74())
			if err == nil || !strings.Contains(err.Error(), "issue #74") {
				t.Fatalf("ReportStatus = %v, want an error naming issue #74", err)
			}
			for _, sentinel := range []error{port.ErrMovedMeanwhile, port.ErrRefused} {
				if got, want := errors.Is(err, sentinel), errors.Is(tc.want, sentinel); got != want {
					t.Errorf("errors.Is(%v, %v) = %t, want %t", err, sentinel, got, want)
				}
			}
		})
	}
}

// running74 is #74 in implement with one action, lfg, running since started
// and having said said.
func running74(started time.Time, said string) crew.Status {
	return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusRunning,
		Actions: []crew.ActionStatus{{Name: "lfg", State: crew.ActionRunning, Started: started, Said: said}},
		Updated: updated}
}

// Covers AE6: what a session said can neither render nor mention anyone.
func TestASessionsWordsAreFencedAsText(t *testing.T) {
	tr, _ := build(t, "")
	said := "ask @someone why ```make``` failed in ./internal/core/update.go"
	body := tr.renderStatus(running74(updated.Add(-time.Minute), said))
	if !slices.Contains(strings.Split(body, "\n"), "````text") {
		t.Errorf("no ````text fence:\n%s", body)
	}
	if blocks := fenced(t, body); !slices.Equal(blocks, []string{said}) {
		t.Errorf("fenced blocks = %q, want [%q]:\n%s", blocks, said, body)
	}
	if n := strings.Count(body, "@someone"); n != 1 {
		t.Errorf("@someone appears %d times, want once, inside its fence:\n%s", n, body)
	}
}

// Covers AE2.
func TestARunningStatusShowsTheStageElapsedTimeAndLastWords(t *testing.T) {
	tr, _ := build(t, "")
	said := "U1 committed: 168 tests pass. Starting U2."
	body := tr.renderStatus(running74(updated.Add(-42*time.Minute-10*time.Second), said))
	for _, want := range []string{"`implement`", "`lfg`", "running", "42 minutes", "Updated 2026-10-02 14:03 UTC."} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q:\n%s", want, body)
		}
	}
	if blocks := fenced(t, body); !slices.Equal(blocks, []string{said}) {
		t.Errorf("fenced blocks = %q, want [%q]:\n%s", blocks, said, body)
	}
	if !strings.HasPrefix(body, "<!-- crew:entry run= kind=running stage=implement -->\n") {
		t.Errorf("body does not start with its entry marker:\n%s", body)
	}

	body = tr.renderStatus(running74(updated.Add(-40*time.Second), said))
	if !strings.Contains(body, "less than a minute") {
		t.Errorf("an action started 40s ago does not show less than a minute:\n%s", body)
	}
}

// KTD8: an action whose session has not started yet is running, with no time
// and no words.
func TestARunningActionWithoutASessionShowsNoTimeAndNoWords(t *testing.T) {
	tr, _ := build(t, "")
	body := tr.renderStatus(running74(time.Time{}, ""))
	if !strings.Contains(body, "`lfg`** is running.") {
		t.Errorf("body does not show lfg running:\n%s", body)
	}
	if strings.Contains(body, " for ") || len(fenced(t, body)) != 0 {
		t.Errorf("body shows a time or words for an action without a session:\n%s", body)
	}
}

func TestElapsedTimeIsInWholeMinutes(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-time.Second:                    "less than a minute",
		59 * time.Second:                "less than a minute",
		time.Minute:                     "1 minute",
		42*time.Minute + 59*time.Second: "42 minutes",
		time.Hour:                       "1 hour",
		time.Hour + 5*time.Minute:       "1 hour 5 minutes",
		2*time.Hour + 30*time.Second:    "2 hours",
		3*time.Hour + time.Minute:       "3 hours 1 minute",
		26*time.Hour + 10*time.Minute:   "26 hours 10 minutes",
	} {
		if got := elapsed(d); got != want {
			t.Errorf("elapsed(%v) = %q, want %q", d, got, want)
		}
	}
}

// Covers AE1.
func TestAQueuedStatusNamesTheStageAndTheLimit(t *testing.T) {
	tr, _ := build(t, "")
	body := tr.renderStatus(queued74())
	for _, want := range []string{"`implement`", "free slot", "at most 2 issues at once", "Updated 2026-10-02 14:03 UTC."} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q:\n%s", want, body)
		}
	}
	if !strings.HasPrefix(body, "<!-- crew:entry run= kind=queued stage=implement -->\n") {
		t.Errorf("body does not start with its entry marker:\n%s", body)
	}

	one := queued74()
	one.Slots = 1
	if body := tr.renderStatus(one); !strings.Contains(body, "at most 1 issue at once") {
		t.Errorf("body does not say at most 1 issue at once:\n%s", body)
	}
}

// Covers AE3 and AE4.
func TestAnEndedStatusShowsEachActionAndTheMove(t *testing.T) {
	tr, _ := build(t, "")
	status := crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusEnded,
		Actions: []crew.ActionStatus{
			{Name: "development", State: crew.ActionFailed},
			{Name: "acceptance", State: crew.ActionSucceeded},
		},
		To: needsAttention, Updated: updated}
	for move, want := range map[crew.MoveProgress]string{
		crew.MovePending: "moving to `needs attention`",
		crew.MoveDone:    "moved to `needs attention`",
		crew.MoveDropped: "could not move it to `needs attention`",
	} {
		status.Move = move
		body := tr.renderStatus(status)
		for _, want := range []string{"`implement`", "**`development`** failed", "**`acceptance`** succeeded", want} {
			if !strings.Contains(body, want) {
				t.Errorf("move %d: body does not contain %q:\n%s", move, want, body)
			}
		}
		if !strings.HasPrefix(body, "<!-- crew:entry run= kind=ended stage=implement -->\n") {
			t.Errorf("move %d: body does not start with its entry marker:\n%s", move, body)
		}
	}
}

// actionLines renders s and returns the body between the stage's line and
// the update line, which holds the actions' lines.
func actionLines(t *testing.T, tr *Tracker, s crew.Status) string {
	t.Helper()
	body := tr.renderStatus(s)
	_, rest, ok := strings.Cut(body, ".\n\n")
	if !ok {
		t.Fatalf("no stage line:\n%s", body)
	}
	actions, _, ok := strings.Cut(rest, "\nUpdated ")
	if !ok {
		t.Fatalf("no update line:\n%s", body)
	}
	return actions
}

// R11: a resumed action's line names its worktree, whatever its state, and
// a fresh action's line stays as it was.
func TestAResumedActionNamesItsWorktree(t *testing.T) {
	tr, _ := build(t, "")
	said := "U1 committed: 168 tests pass. Starting U2."
	resumed := func(s crew.Status) crew.Status {
		s.Actions[0].Workspace = "issue-9-lfg"
		return s
	}
	ended := func(state crew.ActionState) crew.Status {
		return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusEnded,
			Actions: []crew.ActionStatus{{Name: "lfg", State: state}}, To: needsAttention, Updated: updated}
	}
	failed := func() crew.Status {
		s := ended(crew.ActionFailed)
		s.Actions[0].Cause, s.Actions[0].Log = crew.CauseSession, ".crew/logs/issue-9-lfg.log"
		return s
	}
	tests := []struct {
		name   string
		status crew.Status
		want   string
	}{
		{"resumed running with words", resumed(running74(updated.Add(-5*time.Minute), said)),
			"**`lfg`** resumed in worktree `issue-9-lfg` and has been running for 5 minutes. It last said:\n\n```text\n" + said + "\n```\n"},
		{"resumed running without words", resumed(running74(updated.Add(-5*time.Minute), "")),
			"**`lfg`** resumed in worktree `issue-9-lfg` and has been running for 5 minutes.\n"},
		{"resumed not started", resumed(running74(time.Time{}, "")),
			"**`lfg`** resumed in worktree `issue-9-lfg` and is running.\n"},
		{"resumed failed", resumed(failed()),
			"**`lfg`** resumed in worktree `issue-9-lfg` and failed: its session failed. Its log is `.crew/logs/issue-9-lfg.log`.\n"},
		{"resumed succeeded", resumed(ended(crew.ActionSucceeded)),
			"**`lfg`** resumed in worktree `issue-9-lfg` and succeeded.\n"},
		{"fresh running with words", running74(updated.Add(-5*time.Minute), said),
			"**`lfg`** has been running for 5 minutes. It last said:\n\n```text\n" + said + "\n```\n"},
		{"fresh running without words", running74(updated.Add(-5*time.Minute), ""),
			"**`lfg`** has been running for 5 minutes.\n"},
		{"fresh not started", running74(time.Time{}, ""),
			"**`lfg`** is running.\n"},
		{"fresh failed", failed(), "**`lfg`** failed: its session failed. Its log is `.crew/logs/issue-9-lfg.log`.\n"},
		{"fresh succeeded", ended(crew.ActionSucceeded), "**`lfg`** succeeded.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionLines(t, tr, tt.status)
			if tt.status.Kind == crew.StatusEnded {
				got, _, _ = strings.Cut(got, "\n#74 ")
			}
			if got != tt.want {
				t.Errorf("action lines = %q, want %q", got, tt.want)
			}
		})
	}
}

// tail ends every status comment: a blank line and the marker line.
const tail = "\n\n" + statusMarker + "\n"

// separator is what stands between two entries of a status comment, before
// the second one's marker line.
const separator = "\n\n---\n\n"

// writes returns the body of each status comment created or edited, in the
// order gh was called, failed calls included.
func writes(t *testing.T, gh *fakeGh) []string {
	t.Helper()
	gh.mu.Lock()
	calls := slices.Clone(gh.calls)
	gh.mu.Unlock()
	var bodies []string
	for _, c := range calls {
		if slices.Equal(c[:min(len(c), len(createComment))], createComment) ||
			slices.Equal(c[:min(len(c), len(editComment))], editComment) {
			bodies = append(bodies, statusBody(t, c))
		}
	}
	return bodies
}

// entries splits a status comment body into its entries' texts, at each
// separator followed by an entry marker.
func entries(body string) []string {
	body = strings.TrimSuffix(body, tail)
	parts := strings.Split(body, separator+"<!-- crew:entry ")
	for i := 1; i < len(parts); i++ {
		parts[i] = "<!-- crew:entry " + parts[i]
	}
	return parts
}

// report reports each status in order, failing the test on an error.
func report(t *testing.T, tr *Tracker, statuses ...crew.Status) {
	t.Helper()
	for _, s := range statuses {
		if err := tr.ReportStatus(context.Background(), s); err != nil {
			t.Fatalf("ReportStatus(%s %d): %v", s.Stage, s.Kind, err)
		}
	}
}

// fresh builds a tracker for an issue without a status comment: it creates
// comment 101, and edits succeed.
func fresh(t *testing.T) (*Tracker, *fakeGh) {
	return build(t, "", login,
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment},
	)
}

// restarted builds a tracker with an empty cache, as after a restart, for an
// issue whose status comment is comment 12, by the viewer, holding body. A
// new comment it creates is comment 102.
func restarted(t *testing.T, body string) (*Tracker, *fakeGh) {
	return build(t, "", login,
		reply{prefix: listComments, stdout: "[" + commentJSON(12, "me", body) + "]"},
		reply{prefix: createComment, stdout: "102\n"},
		reply{prefix: editComment},
	)
}

// commentAfter returns the status comment body a tracker leaves after
// reporting statuses in order.
func commentAfter(t *testing.T, statuses ...crew.Status) string {
	t.Helper()
	tr, gh := fresh(t)
	report(t, tr, statuses...)
	w := writes(t, gh)
	return w[len(w)-1]
}

// developmentEnded is #74's development stage, run r1, ended with its lfg
// action failed on its check.
func developmentEnded() crew.Status {
	return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "development", Kind: crew.StatusEnded, Run: "r1",
		Actions: []crew.ActionStatus{{Name: "lfg", State: crew.ActionFailed, Cause: crew.CauseCheck,
			Reason: "no open pull request closes #74", Log: ".crew/logs/issue-74-lfg.log"}},
		To: needsAttention, Move: crew.MoveDone, Updated: updated}
}

// fix is #74's fix stage in run, queued, or running its address action that
// said said.
func fix(kind crew.StatusKind, run, said string) crew.Status {
	s := crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "fix", Kind: kind, Run: run, Slots: 2, Updated: updated}
	if kind != crew.StatusQueued {
		s.Actions = []crew.ActionStatus{{Name: "address", Started: updated.Add(-5 * time.Minute), Said: said}}
	}
	return s
}

func TestTheFirstStatusCreatesACommentWithOneEntry(t *testing.T) {
	tr, gh := fresh(t)
	report(t, tr, fix(crew.StatusQueued, "r2", ""))
	creates := gh.callsTo(createComment...)
	if len(creates) != 1 {
		t.Fatalf("created %d comments, want 1", len(creates))
	}
	body := statusBody(t, creates[0])
	if want := tr.renderStatus(fix(crew.StatusQueued, "r2", "")) + tail; body != want {
		t.Errorf("created body =\n%s\nwant\n%s", body, want)
	}
}

func TestQueuedThenRunningInOneRunEditsOneEntry(t *testing.T) {
	tr, gh := fresh(t)
	report(t, tr, fix(crew.StatusQueued, "r2", ""), fix(crew.StatusRunning, "r2", "Reading the review."))
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/101") {
		t.Fatalf("edits = %q, want one of comment 101", edits)
	}
	body := statusBody(t, edits[0])
	if want := tr.renderStatus(fix(crew.StatusRunning, "r2", "Reading the review.")) + tail; body != want {
		t.Errorf("edited body =\n%s\nwant\n%s", body, want)
	}
}

// Covers AE7.
func TestANewStageRunIsAppendedAfterTheEndedOne(t *testing.T) {
	tr, gh := fresh(t)
	report(t, tr, developmentEnded())
	development := strings.TrimSuffix(writes(t, gh)[0], tail)

	report(t, tr, fix(crew.StatusQueued, "r2", ""))
	want := development + separator + tr.renderStatus(fix(crew.StatusQueued, "r2", "")) + tail
	if got := writes(t, gh)[1]; got != want {
		t.Errorf("body after queuing fix =\n%s\nwant\n%s", got, want)
	}

	report(t, tr, fix(crew.StatusRunning, "r2", "Reading the review."))
	want = development + separator + tr.renderStatus(fix(crew.StatusRunning, "r2", "Reading the review.")) + tail
	if got := writes(t, gh)[2]; got != want {
		t.Errorf("body once fix runs =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(development, "**`lfg`** failed: `no open pull request closes #74`.") {
		t.Errorf("the development entry does not give lfg's check reason:\n%s", development)
	}
}

// R12: each failed action says why in crew's words; only a check's reason
// shows, in a code span.
func TestAFailedActionSaysWhyInCrewsWords(t *testing.T) {
	tr, _ := build(t, "")
	for cause, want := range map[crew.FailureCause]string{
		crew.CauseSession:   "**`lfg`** failed: its session failed. Its log is `.crew/logs/issue-74-lfg.log`.",
		crew.CauseCheck:     "**`lfg`** failed: `` `gh` found no @someone **pull request** ``. Its log is `.crew/logs/issue-74-lfg.log`.",
		crew.CauseStopped:   "**`lfg`** failed: crew stopped it. Its log is `.crew/logs/issue-74-lfg.log`.",
		crew.CauseWorkspace: "**`lfg`** failed: its workspace could not be created. Its log is `.crew/logs/issue-74-lfg.log`.",
		crew.CauseStart:     "**`lfg`** failed: its session could not start. Its log is `.crew/logs/issue-74-lfg.log`.",
		crew.CausePrompt:    "**`lfg`** failed: its prompt did not render. Its log is `.crew/logs/issue-74-lfg.log`.",
	} {
		s := developmentEnded()
		s.Actions[0].Cause = cause
		// Only a check's reason may show; any other reason must not.
		s.Actions[0].Reason = "`gh` found no @someone **pull request**"
		body := tr.renderStatus(s)
		if !slices.Contains(strings.Split(body, "\n"), want) {
			t.Errorf("cause %d: body has no line %q:\n%s", cause, want, body)
		}
		if cause != crew.CauseCheck && strings.Contains(body, "@someone") {
			t.Errorf("cause %d: body carries the reason:\n%s", cause, body)
		}
	}

	s := developmentEnded()
	s.Actions[0] = crew.ActionStatus{Name: "lfg", State: crew.ActionFailed, Cause: crew.CauseWorkspace}
	want := "**`lfg`** failed: its workspace could not be created. It failed before it had a log."
	if body := tr.renderStatus(s); !slices.Contains(strings.Split(body, "\n"), want) {
		t.Errorf("body has no line %q:\n%s", want, body)
	}
}

// Covers AE8.
func TestARestartedTrackerEditsOnlyTheLatestEntry(t *testing.T) {
	before := commentAfter(t, developmentEnded(), fix(crew.StatusQueued, "r2", ""), fix(crew.StatusRunning, "r2", "Reading the review."))
	tr, gh := restarted(t, before)
	report(t, tr, fix(crew.StatusRunning, "r2", "Pushing the fix."))

	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/12") {
		t.Fatalf("edits = %q, want one of comment 12", edits)
	}
	was, got := entries(before), entries(statusBody(t, edits[0]))
	if len(got) != 2 {
		t.Fatalf("body has %d entries, want 2:\n%s", len(got), statusBody(t, edits[0]))
	}
	if got[0] != was[0] {
		t.Errorf("development entry changed:\n%s\nwant\n%s", got[0], was[0])
	}
	if want := tr.renderStatus(fix(crew.StatusRunning, "r2", "Pushing the fix.")); got[1] != want {
		t.Errorf("fix entry =\n%s\nwant\n%s", got[1], want)
	}
}

// KTD5: crew stopped before the fix run ended, so its entry still runs.
func TestARunningEntryOfAnEarlierProcessSaysCrewStoppedFollowingIt(t *testing.T) {
	before := commentAfter(t, developmentEnded(), fix(crew.StatusRunning, "r2", "Reading the review."))
	tr, gh := restarted(t, before)
	report(t, tr, fix(crew.StatusRunning, "r3", "Starting over."))

	body := writes(t, gh)[0]
	got := entries(body)
	if len(got) != 3 {
		t.Fatalf("body has %d entries, want 3:\n%s", len(got), body)
	}
	if was := entries(before); got[0] != was[0] {
		t.Errorf("development entry changed:\n%s\nwant\n%s", got[0], was[0])
	}
	want := "<!-- crew:entry run=r2 kind=running stage=fix -->\ncrew stopped following `fix` on #74 before it ended."
	if got[1] != want {
		t.Errorf("stale fix entry =\n%s\nwant\n%s", got[1], want)
	}
	if want := tr.renderStatus(fix(crew.StatusRunning, "r3", "Starting over.")); got[2] != want {
		t.Errorf("new fix entry =\n%s\nwant\n%s", got[2], want)
	}
}

// KTD6: a marker counts only at the start of an entry.
func TestLastWordsShapedLikeAnEntryMarkerAreEntryText(t *testing.T) {
	tr, gh := fresh(t)
	said := "<!-- crew:entry run=r9 kind=queued stage=review -->"
	report(t, tr, fix(crew.StatusRunning, "r2", said), fix(crew.StatusRunning, "r2", "Pushing the fix."))
	body := writes(t, gh)[1]
	if want := tr.renderStatus(fix(crew.StatusRunning, "r2", "Pushing the fix.")) + tail; body != want {
		t.Errorf("edited body =\n%s\nwant\n%s", body, want)
	}
}

func TestARestartedTrackerReplacesTheQueuedEntryOfTheSameStage(t *testing.T) {
	before := commentAfter(t, developmentEnded(), fix(crew.StatusQueued, "r2", ""))
	tr, gh := restarted(t, before)
	report(t, tr, fix(crew.StatusQueued, "r3", ""))

	body := writes(t, gh)[0]
	want := entries(before)[0] + separator + tr.renderStatus(fix(crew.StatusQueued, "r3", "")) + tail
	if body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
}

// KTD6: a comment written before entries is one earlier entry, kept as is.
func TestACommentWithoutEntriesIsKeptAsTheFirstEntry(t *testing.T) {
	legacy := "crew: `implement` ended on #74.\n\n**`development`** failed.\n\nUpdated 2026-10-01 09:12 UTC."
	tr, gh := restarted(t, legacy+tail)
	report(t, tr, queued74())

	want := legacy + separator + tr.renderStatus(queued74()) + tail
	if body := writes(t, gh)[0]; body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}

	// The entry after the old text is the latest one: a status of its run
	// replaces it rather than adding another.
	running := queued74()
	running.Kind = crew.StatusRunning
	report(t, tr, running, running)
	want = legacy + separator + tr.renderStatus(running) + tail
	for i, body := range writes(t, gh)[1:] {
		if body != want {
			t.Errorf("write %d =\n%s\nwant\n%s", i+2, body, want)
		}
	}
}

func TestAStageNameHoldingACommentEndRoundTripsThroughItsMarker(t *testing.T) {
	queued := func(run string) crew.Status {
		s := fix(crew.StatusQueued, run, "")
		s.Stage = "fix --> now"
		return s
	}
	before := commentAfter(t, queued("r2"))
	marker, _, _ := strings.Cut(before, "\n")
	if n := strings.Count(marker, "-->"); n != 1 {
		t.Errorf("marker line %q holds %d comment ends, want 1", marker, n)
	}

	// Only the same stage's queued entry is replaced, so this reads the
	// stage back from the marker.
	tr, gh := restarted(t, before)
	report(t, tr, queued("r3"))
	if body, want := writes(t, gh)[0], tr.renderStatus(queued("r3"))+tail; body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
}

// Covers R14.
func TestAFullCommentIsContinuedInANewOne(t *testing.T) {
	full := "crew: an older status " + strings.Repeat("that went on and on, ", 3200) + "\n\nUpdated 2026-10-01 09:12 UTC."
	tr, gh := restarted(t, full+tail)
	report(t, tr, fix(crew.StatusQueued, "r2", ""))

	creates := gh.callsTo(createComment...)
	if len(creates) != 1 {
		t.Fatalf("created %d comments, want 1", len(creates))
	}
	created := statusBody(t, creates[0])
	preamble := "<!-- crew:continues -->\ncrew: this comment continues crew's earlier status comment on #74, which is full.\n\n"
	if want := preamble + tr.renderStatus(fix(crew.StatusQueued, "r2", "")) + tail; created != want {
		t.Errorf("created body =\n%s\nwant\n%s", created, want)
	}

	report(t, tr, fix(crew.StatusRunning, "r2", "Reading the review."))
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/102") {
		t.Fatalf("edits = %q, want one of comment 102 and none of the full comment 12", edits)
	}
	if body, want := statusBody(t, edits[0]), preamble+tr.renderStatus(fix(crew.StatusRunning, "r2", "Reading the review."))+tail; body != want {
		t.Errorf("edited body =\n%s\nwant\n%s", body, want)
	}
}

// A comment holding only the entry being written gains nothing from a new
// comment, so it is edited even over the limit.
func TestAnEntryOverTheLimitAloneIsEditedInPlace(t *testing.T) {
	tr, gh := fresh(t)
	long := strings.Repeat("word ", 14000)
	report(t, tr, fix(crew.StatusRunning, "r2", long), fix(crew.StatusRunning, "r2", long+"!"))
	if n := len(gh.callsTo(createComment...)); n != 1 {
		t.Errorf("created %d comments, want 1", n)
	}
	if n := len(gh.callsTo(editComment...)); n != 1 {
		t.Errorf("edited %d times, want 1", n)
	}
}

// Only a write that landed changes what the tracker holds: a fix entry that
// never reached the comment is not marked as stopped later.
func TestAFailedWriteLeavesTheCommentAsItWas(t *testing.T) {
	tr, gh := build(t, "", login,
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment, stderr: "gh: Server Error (HTTP 502)\n"},
	)
	report(t, tr, developmentEnded())
	development := strings.TrimSuffix(writes(t, gh)[0], tail)
	for _, s := range []crew.Status{fix(crew.StatusRunning, "r2", "Reading the review."), fix(crew.StatusRunning, "r3", "Starting over.")} {
		if err := tr.ReportStatus(context.Background(), s); err == nil {
			t.Fatalf("ReportStatus(%s) = nil, want the edit's error", s.Run)
		}
	}

	w := writes(t, gh)
	if want := development + separator + tr.renderStatus(fix(crew.StatusRunning, "r3", "Starting over.")) + tail; w[2] != want {
		t.Errorf("body after a failed write =\n%s\nwant\n%s", w[2], want)
	}
	if n := len(gh.callsTo(listComments...)); n != 1 {
		t.Errorf("listed the comments %d times, want 1", n)
	}
}
