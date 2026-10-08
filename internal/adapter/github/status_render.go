package github

import (
	"fmt"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// renderStatus renders a status as its entry in the status comment, in
// Markdown: the entry's marker line, what the rule does, each action with
// its state and, when it resumed, its worktree, then, once the run chose
// its route, the route's steps that did not land and where the issue goes,
// then the update time in UTC. A session's last words go in a fenced code
// block, so nothing in them may render, link or mention anyone. A failed
// action says why in crew's words, from its cause; only a shell or function
// action's line shows, in a code span, as no session's or tool's own words
// may. A route's step shows in crew's words only (R49). An ended action
// whose status holds what it spent says so, with its pull request.
func (t *Tracker) renderStatus(s crew.Status) string {
	var b strings.Builder
	b.WriteString(markerLine(s) + "\n")
	writeHeadline(&b, s)
	for _, a := range s.Actions() {
		writeAction(&b, a, s.Updated())
	}
	if ended, ok := s.Progress().(crew.StatusEnded); ok {
		writeSteps(&b, s.Steps())
		writeMove(&b, s.IssueRef(), ended)
	}
	fmt.Fprintf(&b, "\nUpdated %s UTC.", s.Updated().UTC().Format("2006-01-02 15:04"))
	return b.String()
}

// writeHeadline writes the status entry's first line: what the rule does,
// and once it ended the route it ends through.
func writeHeadline(b *strings.Builder, s crew.Status) {
	rule := codeSpan(string(s.Rule()))
	switch p := s.Progress().(type) {
	case crew.StatusRunning:
		fmt.Fprintf(b, "crew: %s is running on %s.\n", rule, s.IssueRef())
	case crew.StatusEnded:
		fmt.Fprintf(b, "crew: %s ended on %s through %s.\n", rule, s.IssueRef(), codeSpan(string(p.Route)))
	}
}

// writeAction writes the paragraph of action a, as of updated: its state
// and, when it resumed, its worktree, then its shell or function line,
// unless its failure already gives it.
func writeAction(b *strings.Builder, a crew.ActionStatus, updated time.Time) {
	writeState(b, a, updated)
	line := a.Shell.String()
	if failed, ok := a.State.(crew.ActionFailed); line == "" || ok && givesLine(failed.Cause) {
		return
	}
	b.WriteString("\n- " + codeSpan(line) + "\n")
}

// givesLine reports whether a failure of cause gives the action's line
// itself: a script's or a function's failure.
func givesLine(cause crew.FailureCause) bool {
	return cause == crew.CauseShell || cause == crew.CauseFunction
}

// writeState writes the line of action a, as of updated: its state and,
// when it resumed, its worktree.
func writeState(b *strings.Builder, a crew.ActionStatus, updated time.Time) {
	// A resumed action's line names its worktree: "**`lfg`** resumed in
	// worktree `issue-9-lfg` and failed." A fresh one reads "**`lfg`**
	// failed."
	name, and := "**"+codeSpan(string(a.Name))+"**", ""
	if a.Workspace != "" {
		name += " resumed in worktree " + codeSpan(string(a.Workspace))
		and = " and"
	}
	switch state := a.State.(type) {
	case crew.ActionSucceeded:
		fmt.Fprintf(b, "\n%s%s %s.%s\n", name, and, verdictWords(state.Verdict), usage(state.Usage))
	case crew.ActionFailed:
		fmt.Fprintf(b, "\n%s%s\n", failedAction(name+and, state, a.Shell), usage(state.Usage))
	case crew.ActionPending:
		fmt.Fprintf(b, "\n%s%s is running.\n", name, and)
	case crew.ActionAwaitingTurn:
		fmt.Fprintf(b, "\n%s waits for its turn.\n", name)
	case crew.ActionNotRun:
		fmt.Fprintf(b, "\n%s did not run.\n", name)
	case crew.ActionDoneInEarlierRun:
		fmt.Fprintf(b, "\n%s was done in an earlier run.\n", name)
	case crew.ActionRunning:
		fmt.Fprintf(b, "\n%s%s has been running for %s.", name, and, elapsed(updated.Sub(state.Started)))
		said := state.Said.String()
		if said == "" {
			b.WriteString("\n")
			return
		}
		fence := strings.Repeat("`", max(minFence, longestBacktickRun(said)+1))
		fmt.Fprintf(b, " It last said:\n\n%stext\n%s\n%s\n", fence, said, fence)
	}
}

// verdictWords words how an action that ended with v ended: succeeded for
// Passed, which the empty Verdict counts as, and the verdict otherwise.
func verdictWords(v crew.Verdict) string {
	if v == "" || v == crew.Passed {
		return "succeeded"
	}
	return "ended with " + codeSpan(string(v))
}

// writeSteps writes, for each step of the route but its final move or
// close, which writeMove words, that did not land or run, how it settled,
// in crew's words (R16, R49).
func writeSteps(b *strings.Builder, steps []crew.StepStatus) {
	for i, s := range steps {
		if i == len(steps)-1 {
			return
		}
		if line := stepLine(s); line != "" {
			b.WriteString("\n" + line + "\n")
		}
	}
}

// stepLine words step s that did not land or run, or returns "". Only a
// failed step's reason shows, which is crew's own line: a route's step
// never carries what its script printed, nor what the tracker said.
func stepLine(s crew.StepStatus) string {
	step := "the route's " + stepName(s.Step)
	switch o := s.Outcome.(type) {
	case crew.StepFailed:
		return fmt.Sprintf("The route's %s failed: %s.", stepName(s.Step), codeSpan(o.Reason.String()))
	case crew.StepStopped:
		return "crew stopped " + step + "."
	case crew.StepSkipped:
		return "crew skipped " + step + ", as it was stopping."
	case crew.StepGivenUp:
		return "crew gave up " + step + "."
	case crew.StepDropped:
		return "crew dropped " + step + ", as the issue was closed or moved meanwhile."
	case crew.StepLanded, crew.StepRan, nil:
	}
	return ""
}

// stepName names step p of a route.
func stepName(p crew.StepPlan) string {
	switch p.Kind {
	case crew.StepMove:
		return "move to " + codeSpan(string(p.To))
	case crew.StepClose:
		return "close"
	case crew.StepComment:
		return "comment"
	case crew.StepReport:
		return "report"
	case crew.StepShell:
		return "shell step " + codeSpan(string(p.Shell))
	case crew.StepFunction:
		return "function step " + codeSpan(string(p.Function))
	case crew.StepQuestion:
		return "question"
	case crew.StepDelegate:
		return "delegation"
	}
	return "step"
}

// writeMove writes where the ended rule's issue, ref, moves, or that it is
// closed when To is empty, and how far that went.
func writeMove(b *strings.Builder, ref string, ended crew.StatusEnded) {
	if ended.To == "" {
		writeClose(b, ref, ended.Move)
		return
	}
	to := codeSpan(string(ended.To))
	switch ended.Move {
	case crew.MovePending:
		fmt.Fprintf(b, "\n%s is moving to %s.\n", ref, to)
	case crew.MoveDone:
		fmt.Fprintf(b, "\n%s moved to %s.\n", ref, to)
	case crew.MoveDropped:
		fmt.Fprintf(b, "\ncrew could not move it to %s.\n", to)
	}
}

// writeClose writes that the ended rule's route closes its issue, ref, as
// move says.
func writeClose(b *strings.Builder, ref string, move crew.MoveProgress) {
	switch move {
	case crew.MovePending:
		fmt.Fprintf(b, "\n%s is being closed.\n", ref)
	case crew.MoveDone:
		fmt.Fprintf(b, "\n%s was closed.\n", ref)
	case crew.MoveDropped:
		b.WriteString("\ncrew could not close it.\n")
	}
}

// failedAction words the action named by subject, which failed as failed
// says, as the status comment and the stop comment both give it: why it
// failed, in crew's words, then its log. line is a shell action's line,
// which the status comment gives for a script failure, and the stop comment
// passes empty (R49). A stopped lfg reads: **`lfg`** failed: crew stopped
// it. Its log is `.crew/logs/issue-42-lfg.log`.
func failedAction(subject string, failed crew.ActionFailed, line crew.ShellReason) string {
	text := fmt.Sprintf("%s failed%s.", subject, failureCause(failed.Cause, line))
	if failed.Log == "" {
		return text + " It failed before it had a log."
	}
	return text + " Its log is " + codeSpan(failed.Log) + "."
}

// failureCause words cause, what made an action fail, after a colon, or
// returns "" for a cause it does not know. A script or function failure
// gives line, the shell or function action's line, when it is not empty.
func failureCause(cause crew.FailureCause, line crew.ShellReason) string {
	switch cause {
	case crew.CauseSession:
		return ": its session failed"
	case crew.CauseShell:
		// The line already says how the script ended.
		if line.String() == "" {
			return ": its script failed"
		}
		return ": " + codeSpan(line.String())
	case crew.CauseFunction:
		// The line already says how the function ended.
		if line.String() == "" {
			return ": its function failed"
		}
		return ": " + codeSpan(line.String())
	case crew.CauseVerdict:
		return ": it ended with a verdict it may not end with"
	case crew.CauseStopped:
		return ": crew stopped it"
	case crew.CauseStoppedBeforeStart:
		return ": crew stopped before it started"
	case crew.CauseTimeUp:
		return ": crew's run time was up before it started"
	case crew.CauseWorkspace:
		return ": its workspace could not be created"
	case crew.CauseStart:
		return ": its session could not start"
	case crew.CausePrompt:
		return ": its prompt did not render"
	default:
		return ""
	}
}

// elapsed renders d in whole minutes, as "less than a minute", "42 minutes"
// or "1 hour 5 minutes".
func elapsed(d time.Duration) string {
	minutes := int(d / time.Minute)
	if minutes < 1 {
		return "less than a minute"
	}
	var parts []string
	if h := minutes / minutesPerHour; h > 0 {
		parts = append(parts, plural(h, "hour"))
	}
	if m := minutes % minutesPerHour; m > 0 {
		parts = append(parts, plural(m, "minute"))
	}
	return strings.Join(parts, " ")
}

// plural renders n of unit, as "1 hour" or "2 hours".
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
