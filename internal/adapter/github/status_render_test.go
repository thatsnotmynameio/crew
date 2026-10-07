package github

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// changed returns s with change made to its data.
func changed(s crew.Status, change func(*crew.StatusData)) crew.Status {
	d := s.Data()
	change(&d)
	return crew.NewStatus(d)
}

// isEnded reports whether s is an ended status.
func isEnded(s crew.Status) bool {
	_, ended := s.Progress().(crew.StatusEnded)
	return ended
}

// running74 is #74 in implement with one action, lfg, running since started
// and having said said, or pending when started is zero.
func running74(started time.Time, said string) crew.Status {
	var state crew.ActionState = crew.ActionRunning{Started: started, Said: crew.NewSaid(said)}
	if started.IsZero() {
		state = crew.ActionPending{}
	}
	return crew.NewStatus(crew.StatusData{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement", Progress: crew.StatusRunning{},
		Actions: []crew.ActionStatus{{Name: "lfg", State: state}}, Updated: updated,
	})
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
func TestARunningStatusShowsTheRuleElapsedTimeAndLastWords(t *testing.T) {
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

// Covers AE3 and AE4: an ended status names the route, then says where the
// issue moves or that it is closed, and how far that went.
func TestAnEndedStatusShowsEachActionTheRouteAndTheMoveOrClose(t *testing.T) {
	tr, _ := build(t)
	for _, tt := range []struct {
		to   crew.State
		move crew.MoveProgress
		want string
	}{
		{needsAttention, crew.MovePending, "\n#74 is moving to `needs attention`.\n"},
		{needsAttention, crew.MoveDone, "\n#74 moved to `needs attention`.\n"},
		{needsAttention, crew.MoveDropped, "\ncrew could not move it to `needs attention`.\n"},
		{"", crew.MovePending, "\n#74 is being closed.\n"},
		{"", crew.MoveDone, "\n#74 was closed.\n"},
		{"", crew.MoveDropped, "\ncrew could not close it.\n"},
	} {
		move, want := tt.move, tt.want
		body := tr.renderStatus(crew.NewStatus(crew.StatusData{
			IssueID: issueID("74"), IssueRef: "#74", Rule: "implement",
			Progress: crew.StatusEnded{Route: crew.FailedRoute, To: tt.to, Move: move},
			Actions: []crew.ActionStatus{
				{Name: "development", State: crew.ActionFailed{}},
				{Name: "acceptance", State: crew.ActionSucceeded{}},
			},
			Updated: updated,
		}))
		for _, want := range []string{
			"crew: `implement` ended on #74 through `failed`.\n", "**`development`** failed", "**`acceptance`** succeeded", want,
		} {
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
	pr45 := crew.PullRequestFound{Ref: "#45", URL: "https://github.com/o/r/pull/45"}
	spent := crew.Usage{Cost: crew.Some(12.4), Tokens: crew.Some(crew.Tokens{CacheRead: 17_200_000})}.Spend()
	tests := []struct {
		name  string
		state crew.ActionState
		want  string
	}{
		{"succeeded with a pull request",
			crew.ActionSucceeded{Usage: crew.Some(crew.ShownUsage{Spend: spent, PullRequest: pr45})},
			"**`lfg`** succeeded. Usage: $12.40, 17.2M tokens. Pull request: [#45](https://github.com/o/r/pull/45).\n"},
		{"failed without a pull request",
			crew.ActionFailed{
				Cause: crew.CauseSession, Log: ".crew/logs/issue-9-lfg.log",
				Usage: crew.Some(crew.ShownUsage{Spend: spent, PullRequest: crew.PullRequestNone{}}),
			},
			"**`lfg`** failed: its session failed. Its log is `.crew/logs/issue-9-lfg.log`. " +
				"Usage: $12.40, 17.2M tokens. Pull request: none.\n"},
		{"nothing reported, not looked up",
			crew.ActionSucceeded{Usage: crew.Some(crew.ShownUsage{Spend: crew.Usage{}.Spend()})},
			"**`lfg`** succeeded. Usage: cost and tokens not reported. Pull request: not looked up.\n"},
		{"no session", crew.ActionSucceeded{}, "**`lfg`** succeeded.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _ := strings.Cut(actionLines(t, tr, lfgEnded(tt.state)), "\n#74 ")
			if got != tt.want {
				t.Errorf("action lines:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// actionLines renders s and returns the body between the rule's line and
// the update line, which holds the actions' lines.
func actionLines(t *testing.T, tr *Tracker, s crew.Status) string {
	t.Helper()
	body := tr.renderStatus(s)
	_, rest, ok := strings.Cut(body, ".\n\n")
	if !ok {
		t.Fatalf("no rule line:\n%s", body)
	}
	actions, _, ok := strings.Cut(rest, "\nUpdated ")
	if !ok {
		t.Fatalf("no update line:\n%s", body)
	}
	return actions
}

// resumed is s with its first action resumed in worktree issue-9-lfg.
func resumed(s crew.Status) crew.Status {
	return changed(s, func(d *crew.StatusData) { d.Actions[0].Workspace = "issue-9-lfg" })
}

// lfgEnded is #74's implement rule, ended with its one action, lfg, in
// state.
func lfgEnded(state crew.ActionState) crew.Status {
	return crew.NewStatus(crew.StatusData{
		IssueID: issueID("74"), IssueRef: "#74", Rule: "implement",
		Progress: crew.StatusEnded{Route: crew.FailedRoute, To: needsAttention},
		Actions:  []crew.ActionStatus{{Name: "lfg", State: state}}, Updated: updated,
	})
}

// lfgFailed is lfgEnded with lfg failed by cause, with a log.
func lfgFailed(cause crew.FailureCause) crew.Status {
	return lfgEnded(crew.ActionFailed{Cause: cause, Log: ".crew/logs/issue-9-lfg.log"})
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
		{"resumed failed", resumed(lfgFailed(crew.CauseSession)),
			"**`lfg`** resumed in worktree `issue-9-lfg` and failed: its session failed. " +
				"Its log is `.crew/logs/issue-9-lfg.log`.\n"},
		{"resumed succeeded", resumed(lfgEnded(crew.ActionSucceeded{})),
			"**`lfg`** resumed in worktree `issue-9-lfg` and succeeded.\n"},
		{"fresh running with words", running74(updated.Add(-5*time.Minute), said),
			"**`lfg`** has been running for 5 minutes. It last said:\n\n```text\n" + said + "\n```\n"},
		{"fresh running without words", running74(updated.Add(-5*time.Minute), ""),
			"**`lfg`** has been running for 5 minutes.\n"},
		{"fresh not started", running74(time.Time{}, ""),
			"**`lfg`** is running.\n"},
		{"fresh failed", lfgFailed(crew.CauseSession),
			"**`lfg`** failed: its session failed. Its log is `.crew/logs/issue-9-lfg.log`.\n"},
		{"fresh succeeded", lfgEnded(crew.ActionSucceeded{}), "**`lfg`** succeeded.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionLines(t, tr, tt.status)
			if isEnded(tt.status) {
				got, _, _ = strings.Cut(got, "\n#74 ")
			}
			if got != tt.want {
				t.Errorf("action lines = %q, want %q", got, tt.want)
			}
		})
	}
}

// R12: each failed action says why in crew's words; only a shell action's
// line shows, in a code span, as no session's or tool's own words may.
func TestAFailedActionSaysWhyInCrewsWords(t *testing.T) {
	tr, _ := build(t)
	const log = " Its log is `.crew/logs/issue-74-lfg.log`."
	for cause, want := range map[crew.FailureCause]string{
		crew.CauseSession:            "**`lfg`** failed: its session failed." + log,
		crew.CauseShell:              "**`lfg`** failed: `` `gh` found no @someone **pull request** ``." + log,
		crew.CauseVerdict:            "**`lfg`** failed: it ended with a verdict it may not end with." + log,
		crew.CauseStopped:            "**`lfg`** failed: crew stopped it." + log,
		crew.CauseStoppedBeforeStart: "**`lfg`** failed: crew stopped before it started." + log,
		crew.CauseTimeUp:             "**`lfg`** failed: crew's run time was up before it started." + log,
		crew.CauseWorkspace:          "**`lfg`** failed: its workspace could not be created." + log,
		crew.CauseStart:              "**`lfg`** failed: its session could not start." + log,
		crew.CausePrompt:             "**`lfg`** failed: its prompt did not render." + log,
	} {
		s := changed(developmentEnded(), func(d *crew.StatusData) {
			d.Actions[0].State = crew.ActionFailed{Cause: cause, Log: ".crew/logs/issue-74-lfg.log"}
			d.Actions[0].Shell = crew.NewShellReason("`gh` found no @someone **pull request**")
		})
		body := tr.renderStatus(s)
		if !slices.Contains(strings.Split(body, "\n"), want) {
			t.Errorf("cause %d: body has no line %q:\n%s", cause, want, body)
		}
	}

	s := changed(developmentEnded(), func(d *crew.StatusData) {
		d.Actions[0] = crew.ActionStatus{Name: "lfg", State: crew.ActionFailed{Cause: crew.CauseWorkspace}}
	})
	want := "**`lfg`** failed: its workspace could not be created. It failed before it had a log."
	if body := tr.renderStatus(s); !slices.Contains(strings.Split(body, "\n"), want) {
		t.Errorf("body has no line %q:\n%s", want, body)
	}
	s = changed(developmentEnded(), func(d *crew.StatusData) { d.Actions[0].Shell = crew.ShellReason{} })
	want = "**`lfg`** failed: its script failed. Its log is `.crew/logs/issue-74-lfg.log`."
	if body := tr.renderStatus(s); !slices.Contains(strings.Split(body, "\n"), want) {
		t.Errorf("a script failure without a line: body has no line %q:\n%s", want, body)
	}
}

// KTD23: an action that ended says its verdict, and one that has not run
// says whether it waits for its turn, never ran or ran in an earlier run.
func TestAnActionSaysItsVerdictOrWhyItHasNotRun(t *testing.T) {
	tr, _ := build(t)
	tests := []struct {
		state crew.ActionState
		want  string
	}{
		{crew.ActionSucceeded{Verdict: crew.Passed}, "**`lfg`** succeeded.\n"},
		{crew.ActionSucceeded{Verdict: "blocked"}, "**`lfg`** ended with `blocked`.\n"},
		{crew.ActionAwaitingTurn{}, "**`lfg`** waits for its turn.\n"},
		{crew.ActionNotRun{}, "**`lfg`** did not run.\n"},
		{crew.ActionDoneInEarlierRun{}, "**`lfg`** was done in an earlier run.\n"},
	}
	for _, tt := range tests {
		got, _, _ := strings.Cut(actionLines(t, tr, lfgEnded(tt.state)), "\n#74 ")
		if got != tt.want {
			t.Errorf("%#v: action lines = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// R16, R49: a route's step that did not land shows in crew's words; the
// steps that landed or ran, and the final move, say nothing of their own.
func TestARoutesStepsThatDidNotLandShowInCrewsWords(t *testing.T) {
	tr, _ := build(t)
	notify := crew.StepPlan{Kind: crew.StepShell, Shell: "notify"}
	const exitedOne = "the route's shell step notify exited with status 1"
	s := changed(developmentEnded(), func(d *crew.StatusData) {
		d.Steps = []crew.StepStatus{
			{Step: crew.StepPlan{Kind: crew.StepComment}, Outcome: crew.StepFailed{
				Reason: crew.NewShellReason("the comment did not render: no .Foo"),
			}},
			{Step: notify, Outcome: crew.StepFailed{Reason: crew.NewShellReason(exitedOne)}},
			{Step: notify, Outcome: crew.StepStopped{Reason: crew.NewShellReason("the route's shell step notify was stopped")}},
			{Step: notify, Outcome: crew.StepSkipped{}},
			{Step: crew.StepPlan{Kind: crew.StepReport}, Outcome: crew.StepGivenUp{Reason: "gh: HTTP 403 @someone"}},
			{Step: crew.StepPlan{Kind: crew.StepComment}, Outcome: crew.StepDropped{Reason: "gh: HTTP 404"}},
			{Step: crew.StepPlan{Kind: crew.StepReport}, Outcome: crew.StepLanded{}},
			{Step: notify, Outcome: crew.StepRan{Reason: crew.NewShellReason("the route's shell step notify exited 0")}},
			{Step: crew.StepPlan{Kind: crew.StepMove, To: needsAttention}, Outcome: crew.StepGivenUp{Reason: "gh: HTTP 403"}},
		}
	})
	body := tr.renderStatus(s)
	want := "\nThe route's comment failed: `the comment did not render: no .Foo`.\n" +
		"\nThe route's shell step `notify` failed: `the route's shell step notify exited with status 1`.\n" +
		"\ncrew stopped the route's shell step `notify`.\n" +
		"\ncrew skipped the route's shell step `notify`, as it was stopping.\n" +
		"\ncrew gave up the route's report.\n" +
		"\ncrew dropped the route's comment, as the issue was closed or moved meanwhile.\n" +
		"\n#74 moved to `needs attention`.\n"
	if !strings.Contains(body, want) {
		t.Errorf("body does not contain the steps:\n%s\nwant\n%s", body, want)
	}
	for _, never := range []string{"@someone", "HTTP", "exited 0"} {
		if strings.Contains(body, never) {
			t.Errorf("body carries %q:\n%s", never, body)
		}
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

// R49: a shell action's line is followed by the last line its script
// printed, stripped, as crew's line gives it; a script failure's line is on
// the action's line instead.
func TestAShellActionShowsItsLastLine(t *testing.T) {
	tr, _ := build(t)
	withLine := func(s crew.Status, line string) crew.Status {
		return changed(s, func(d *crew.StatusData) { d.Actions[0].Shell = crew.NewShellReason(line) })
	}
	const log = " Its log is `.crew/logs/issue-9-lfg.log`.\n"
	tests := []struct {
		name   string
		status crew.Status
		want   string
	}{
		{
			"passed", withLine(lfgEnded(crew.ActionSucceeded{}), "judge exited with status 0: done (0.97)"),
			"**`lfg`** succeeded.\n\n- `judge exited with status 0: done (0.97)`\n",
		},
		{
			"needs a person", withLine(lfgEnded(crew.ActionSucceeded{Verdict: "needs_person"}),
				"judge exited with status 3: \x1b[31mneeds a person\x1b[0m (0.95)"),
			"**`lfg`** ended with `needs_person`.\n\n- `judge exited with status 3: needs a person (0.95)`\n",
		},
		{
			"failed", withLine(lfgFailed(crew.CauseShell), "judge exited with status 1: unfinished (1.00)"),
			"**`lfg`** failed: `judge exited with status 1: unfinished (1.00)`." + log,
		},
		{
			"stopped", withLine(lfgFailed(crew.CauseStopped), "the shell action judge was stopped"),
			"**`lfg`** failed: crew stopped it." + log + "\n- `the shell action judge was stopped`\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, _ := strings.Cut(actionLines(t, tr, tt.status), "\n#74 ")
			if got != tt.want {
				t.Errorf("action lines:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}
