package github

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// running74 is #74 in implement with one action, lfg, running since started
// and having said said.
func running74(started time.Time, said string) crew.Status {
	return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusRunning,
		Actions: []crew.ActionStatus{{Name: "lfg", State: crew.ActionRunning, Started: started, Said: said}},
		Updated: updated}
}

// Covers AE6: what a session said can neither render nor mention anyone.
func TestASessionsWordsAreFencedAsText(t *testing.T) {
	tr, _ := build(t)
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
	tr, _ := build(t)
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
	tr, _ := build(t)
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
	tr, _ := build(t)
	body := tr.renderStatus(queued74())
	wants := []string{"`implement`", "free slot", "at most 2 issues at once", "Updated 2026-10-02 14:03 UTC."}
	for _, want := range wants {
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
	tr, _ := build(t)
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

// AE8: an ended action whose status holds what its session spent says so,
// with its pull request; one without stays as it was.
func TestAnEndedActionShowsWhatItSpentAndItsPullRequest(t *testing.T) {
	tr, _ := build(t)
	pr45 := crew.PullRequest{Lookup: crew.PullRequestFound, Ref: "#45", URL: "https://github.com/o/r/pull/45"}
	spent := crew.Usage{Cost: 12.4, HasCost: true, Tokens: crew.Tokens{CacheRead: 17_200_000}, HasTokens: true}.Spend()
	ended := func(a crew.ActionStatus) crew.Status {
		a.Name = "lfg"
		return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusEnded,
			Actions: []crew.ActionStatus{a}, To: needsAttention, Updated: updated}
	}
	tests := []struct {
		name   string
		action crew.ActionStatus
		want   string
	}{
		{"succeeded with a pull request",
			crew.ActionStatus{State: crew.ActionSucceeded, Spend: spent, PullRequest: pr45},
			"**`lfg`** succeeded. Usage: $12.40, 17.2M tokens. Pull request: [#45](https://github.com/o/r/pull/45).\n"},
		{"failed without a pull request",
			crew.ActionStatus{
				State: crew.ActionFailed, Cause: crew.CauseSession, Log: ".crew/logs/issue-9-lfg.log",
				Spend: spent, PullRequest: crew.PullRequest{Lookup: crew.PullRequestNone},
			},
			"**`lfg`** failed: its session failed. Its log is `.crew/logs/issue-9-lfg.log`. " +
				"Usage: $12.40, 17.2M tokens. Pull request: none.\n"},
		{"nothing reported, not looked up",
			crew.ActionStatus{State: crew.ActionSucceeded, Spend: crew.Usage{}.Spend()},
			"**`lfg`** succeeded. Usage: cost and tokens not reported. Pull request: not looked up.\n"},
		{"no session", crew.ActionStatus{State: crew.ActionSucceeded}, "**`lfg`** succeeded.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _ := strings.Cut(actionLines(t, tr, ended(tt.action)), "\n#74 ")
			if got != tt.want {
				t.Errorf("action lines:\n got %q\nwant %q", got, tt.want)
			}
		})
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

// resumed is s with its first action resumed in worktree issue-9-lfg.
func resumed(s crew.Status) crew.Status {
	s.Actions[0].Workspace = "issue-9-lfg"
	return s
}

// lfgEnded is #74's implement stage, ended with its one action, lfg, in
// state.
func lfgEnded(state crew.ActionState) crew.Status {
	return crew.Status{IssueKey: "74", IssueRef: "#74", Stage: "implement", Kind: crew.StatusEnded,
		Actions: []crew.ActionStatus{{Name: "lfg", State: state}}, To: needsAttention, Updated: updated}
}

// lfgFailed is lfgEnded with lfg failed on its session, with a log.
func lfgFailed() crew.Status {
	s := lfgEnded(crew.ActionFailed)
	s.Actions[0].Cause, s.Actions[0].Log = crew.CauseSession, ".crew/logs/issue-9-lfg.log"
	return s
}

// R11: a resumed action's line names its worktree, whatever its state, and
// a fresh action's line stays as it was.
func TestAResumedActionNamesItsWorktree(t *testing.T) {
	tr, _ := build(t)
	said := "U1 committed: 168 tests pass. Starting U2."
	tests := []struct {
		name   string
		status crew.Status
		want   string
	}{
		{"resumed running with words", resumed(running74(updated.Add(-5*time.Minute), said)),
			"**`lfg`** resumed in worktree `issue-9-lfg` and has been running for 5 minutes. " +
				"It last said:\n\n```text\n" + said + "\n```\n"},
		{"resumed running without words", resumed(running74(updated.Add(-5*time.Minute), "")),
			"**`lfg`** resumed in worktree `issue-9-lfg` and has been running for 5 minutes.\n"},
		{"resumed not started", resumed(running74(time.Time{}, "")),
			"**`lfg`** resumed in worktree `issue-9-lfg` and is running.\n"},
		{"resumed failed", resumed(lfgFailed()),
			"**`lfg`** resumed in worktree `issue-9-lfg` and failed: its session failed. " +
				"Its log is `.crew/logs/issue-9-lfg.log`.\n"},
		{"resumed succeeded", resumed(lfgEnded(crew.ActionSucceeded)),
			"**`lfg`** resumed in worktree `issue-9-lfg` and succeeded.\n"},
		{"fresh running with words", running74(updated.Add(-5*time.Minute), said),
			"**`lfg`** has been running for 5 minutes. It last said:\n\n```text\n" + said + "\n```\n"},
		{"fresh running without words", running74(updated.Add(-5*time.Minute), ""),
			"**`lfg`** has been running for 5 minutes.\n"},
		{"fresh not started", running74(time.Time{}, ""),
			"**`lfg`** is running.\n"},
		{"fresh failed", lfgFailed(), "**`lfg`** failed: its session failed. Its log is `.crew/logs/issue-9-lfg.log`.\n"},
		{"fresh succeeded", lfgEnded(crew.ActionSucceeded), "**`lfg`** succeeded.\n"},
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

// R12: each failed action says why in crew's words; only a check's reason
// shows, in a code span.
func TestAFailedActionSaysWhyInCrewsWords(t *testing.T) {
	tr, _ := build(t)
	const log = " Its log is `.crew/logs/issue-74-lfg.log`."
	for cause, want := range map[crew.FailureCause]string{
		crew.CauseSession:   "**`lfg`** failed: its session failed." + log,
		crew.CauseCheck:     "**`lfg`** failed: `` `gh` found no @someone **pull request** ``." + log,
		crew.CauseStopped:   "**`lfg`** failed: crew stopped it." + log,
		crew.CauseWorkspace: "**`lfg`** failed: its workspace could not be created." + log,
		crew.CauseStart:     "**`lfg`** failed: its session could not start." + log,
		crew.CausePrompt:    "**`lfg`** failed: its prompt did not render." + log,
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

// fenced returns the content of each fenced code block in markdown, checking
// that each closes with exactly its own opening fence.
func fenced(t *testing.T, markdown string) []string {
	t.Helper()
	var blocks []string
	lines := strings.Split(markdown, "\n")
	for i := 0; i < len(lines); i++ {
		fence := strings.TrimRight(lines[i], "abcdefghijklmnopqrstuvwxyz")
		if len(fence) < 3 || strings.Trim(fence, "`") != "" {
			continue
		}
		end := slices.IndexFunc(lines[i+1:], func(l string) bool {
			return strings.Trim(l, "`") == "" && len(l) >= len(fence)
		})
		if end < 0 {
			t.Fatalf("fence on line %d never closes:\n%s", i+1, markdown)
		}
		blocks = append(blocks, strings.Join(lines[i+1:i+1+end], "\n"))
		i += end + 1
	}
	return blocks
}
