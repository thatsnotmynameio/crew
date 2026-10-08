// Package lines is crew's headless renderer (R18): it prints each event
// of the engine's ordered subscription as one timestamped line,
// "HH:MM:SS crew: <text>". It never imports bubbletea, so the headless mode
// stays free of the TUI.
package lines

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// Source is an ordered subscription to the engine's updates, such as an
// *engine.Queue: Updates delivers them in order and is closed once the
// engine has stopped, and Dropped counts the events lost while it was full.
type Source interface {
	Updates() <-chan engine.Update
	Dropped() int
}

// Run prints each of warnings, crew's startup warnings, as one line stamped
// with now, then every event of src to w, one line per event, stamped with
// the event's own time in loc. Each time it has drained the queue and Dropped
// has grown, it prints one line, stamped with now, saying how many events
// were dropped. It returns nil once src's channel is closed, or the first
// write error.
func Run(src Source, w io.Writer, loc *time.Location, now func() time.Time, warnings ...string) error {
	if err := warn(w, now().In(loc), warnings); err != nil {
		return err
	}
	ch := src.Updates()
	reported := 0
	// Dropped events are newer than the ones still queued, so the count is
	// printed only once the queue is drained, where the gap actually is.
	reportDrops := func() error {
		dropped := src.Dropped()
		if dropped <= reported {
			return nil
		}
		n := dropped - reported
		reported = dropped
		text := fmt.Sprintf("%d %s dropped because this output fell behind", n, Plural(n, "event was", "events were"))
		return Line(w, now().In(loc), text)
	}
	for u := range ch {
		for _, e := range u.Events {
			if err := Line(w, e.Time().In(loc), Text(e)); err != nil {
				return err
			}
		}
		if len(ch) == 0 {
			if err := reportDrops(); err != nil {
				return err
			}
		}
	}
	return reportDrops()
}

// warn prints each of warnings as one line stamped with at.
func warn(w io.Writer, at time.Time, warnings []string) error {
	for _, warning := range warnings {
		if err := Line(w, at, "warning: "+warning); err != nil {
			return err
		}
	}
	return nil
}

// Line prints text as one event line, "HH:MM:SS crew: <text>", stamped with
// at in its own location. The boot log uses it too, so both share one format.
func Line(w io.Writer, at time.Time, text string) error {
	if _, err := fmt.Fprintf(w, "%s crew: %s\n", at.Format(time.TimeOnly), text); err != nil {
		return fmt.Errorf("print event line: %w", err)
	}
	return nil
}

// Text describes e as one English sentence, without a timestamp. The TUI
// uses it for its recent events too.
func Text(e core.Published) string {
	switch e := e.(type) {
	case crew.RunEvent:
		return runText(e)
	case core.Event:
		return coreText(e)
	}
	return fmt.Sprintf("%T", e)
}

// runText describes the run events the core publishes. The core publishes
// none of the others, which have no line.
func runText(e crew.RunEvent) string {
	switch e := e.(type) {
	case crew.RunTaken:
		return fmt.Sprintf("%s took %s %q (%s -> %s)", e.Rule, e.IssueRef, e.Issue.Title, e.From, e.To)
	case crew.WorkspaceOpened:
		return workspaceOpened(e)
	case crew.WorkspaceMissing:
		return fmt.Sprintf("%s %s: worktree %s is gone, so it does not resume there", e.IssueRef, e.Rule, e.Workspace.Name)
	case crew.ActionSessionStarted:
		return fmt.Sprintf("%s %s/%s started its session", e.IssueRef, e.Rule, e.Action)
	case crew.ActionShellAsked:
		return fmt.Sprintf("%s %s/%s started its script", e.IssueRef, e.Rule, e.Action)
	case crew.ActionFunctionAsked:
		return fmt.Sprintf("%s %s/%s started its function", e.IssueRef, e.Rule, e.Action)
	case crew.ActionReturnAsked:
		return fmt.Sprintf("%s %s/%s started checking the answer", e.IssueRef, e.Rule, e.Action)
	case crew.ActionEnded:
		return actionEnded(e)
	case crew.TakeMoved:
		return moved(e.IssueRef, e.From, e.To)
	case crew.RouteChosen:
		return fmt.Sprintf("%s %s ends through %s", e.IssueRef, e.Rule, e.Route)
	case crew.RunStopped, crew.RunOutOfTime, crew.WorkspaceAsked, crew.ActionSessionAsked,
		crew.ActionSessionStopAsked, crew.ActionSessionEnded, crew.ActionShellStopAsked, crew.ActionShellEnded,
		crew.ActionFunctionStopAsked, crew.ActionFunctionEnded, crew.RunLookupAsked,
		crew.RunLookupDone, crew.StepAsked, crew.StepShellStopAsked, crew.StepFunctionStopAsked, crew.StepEnded,
		crew.RunReleased:
	}
	return ""
}

// coreText describes the core's own events: those about an issue, a bot or
// a tracker call here, those about crew as a whole in crewText.
func coreText(e core.Event) string {
	switch e := e.(type) {
	case core.RunNotRecorded:
		return withReason(fmt.Sprintf("%s %s: could not record %s, so a restart may not resume it",
			e.IssueRef, e.Rule, e.What), e.Reason)
	case core.RouteStepEnded:
		return stepEnded(e)
	case core.IssueSkipped:
		return issueSkipped(e)
	case core.IssueOfOtherKind:
		return issueOfOtherKind(e)
	case core.BotStopped:
		return fmt.Sprintf("bot %s stopped acting: %s", e.Bot, e.Warning)
	case core.BotActsAgain:
		return fmt.Sprintf("bot %s acts again: its token renewed", e.Bot)
	case core.CallOwed:
		return withReason(call(e.Call)+" failed, retrying at the next tick", e.Reason)
	case core.CallDropped:
		return withReason("gave up "+call(e.Call)+": "+result(e.Result), e.Reason)
	case core.StatusFailed:
		return withReason("could not update the status comment on "+e.IssueRef+": "+result(e.Result), e.Reason)
	case core.PollDone, core.PollSkipped, core.ListingFailed, core.WindingDown, core.Paused, core.Resumed,
		core.Stopped, core.StatisticNotRecorded:
		return crewText(e)
	}
	return fmt.Sprintf("%T", e)
}

// crewText describes the core's events about crew as a whole: its polls,
// its wind-down, its pause and resume (R7 of #282), its stop, and a record
// its statistics store lost. coreText describes the others.
func crewText(e core.Event) string {
	switch e := e.(type) {
	case core.PollDone:
		return fmt.Sprintf("poll: listed %d %s, took %d", e.Listed, Plural(e.Listed, "issue", "issues"), e.Taken)
	case core.PollSkipped:
		return fmt.Sprintf("poll: skipped, %d of %d %s busy", e.Busy, e.Slots, Plural(e.Slots, "slot", "slots"))
	case core.ListingFailed:
		return withReason("listing issues failed", e.Reason)
	case core.WindingDown:
		return fmt.Sprintf("run time of %v is up: taking no new issues, winding down", e.Limit)
	case core.Paused:
		return "paused: taking no new issues until resumed; held issues run to their end"
	case core.Resumed:
		return "resumed: taking new issues again"
	case core.Stopped:
		return "stopped"
	case core.StatisticNotRecorded:
		return withReason("warning: could not record "+statistic(e.Statistic)+" in the statistics store", e.Reason)
	case core.RunNotRecorded, core.RouteStepEnded, core.IssueSkipped, core.IssueOfOtherKind, core.BotStopped,
		core.BotActsAgain, core.CallOwed, core.CallDropped, core.StatusFailed:
	}
	return fmt.Sprintf("%T", e)
}

// statistic names s, a record of the statistics store, in crew's words.
func statistic(s crew.Statistic) string {
	if _, ok := s.(crew.Process); ok {
		return "this crew process"
	}
	return fmt.Sprintf("%T", s)
}

// moved is the line for a move the tracker made, a take or a route's.
func moved(ref string, from, to crew.State) string {
	return fmt.Sprintf("%s moved from %s to %s", ref, from, to)
}

// workspaceOpened is the line for the run's workspace that is ready, new
// or reopened, with its log when an action will write it.
func workspaceOpened(e crew.WorkspaceOpened) string {
	verb := "works"
	if e.Resumed {
		verb = "resumed"
	}
	line := fmt.Sprintf("%s %s %s in worktree %s on branch %s", e.IssueRef, e.Rule, verb, e.Workspace.Name,
		e.Workspace.Branch)
	if e.Log == "" {
		return line
	}
	return line + ", log " + e.Log
}

// actionEnded is the line for an action that ended, with its verdict and
// its reason.
func actionEnded(e crew.ActionEnded) string {
	// The reason is shown for every verdict: without a script after it, a
	// session's clean end is the only sign it passed, so its last message is
	// what tells you whether the work was done.
	outcome := e.End.Outcome()
	verdict := "ended with " + string(e.Verdict)
	switch {
	case e.Verdict == crew.Failed || e.Verdict == "" && !outcome.Succeeded:
		verdict = "failed"
	case e.Verdict == crew.Passed || e.Verdict == "":
		verdict = "passed"
	}
	return withReason(fmt.Sprintf("%s %s/%s %s", e.IssueRef, e.Rule, e.Action, verdict), outcome.Reason.String())
}

// issueSkipped is the line for an issue crew left alone, and why.
func issueSkipped(e core.IssueSkipped) string {
	states := make([]string, len(e.States))
	for i, s := range e.States {
		states[i] = string(s)
	}
	return fmt.Sprintf("skipped %s: it carries %d crew labels (%s)",
		e.IssueRef, len(e.States), strings.Join(states, ", "))
}

// issueOfOtherKind is the line for an item crew left alone because its
// label is a rule's that takes the other kind of item.
func issueOfOtherKind(e core.IssueOfOtherKind) string {
	article, takes := "an", "issues"
	if e.Kind == crew.KindPullRequest {
		article = "a"
	}
	if e.Takes == crew.KindPullRequest {
		takes = "pull requests"
	}
	return fmt.Sprintf("left %s alone: it is %s %s, and %s is the label of %s, which takes %s",
		e.IssueRef, article, KindName(e.Kind), e.Label, e.Rule, takes)
}

// call describes c, a tracker call crew owes or gave up.
func call(c core.Call) string {
	switch c.Kind {
	case core.CallReport:
		return "posting the report on " + c.IssueRef
	case core.CallComment:
		return "commenting on " + c.IssueRef
	case core.CallClose:
		return "closing " + c.IssueRef
	case core.CallDelegate:
		return "asking the answerer on " + c.IssueRef
	case core.CallPullRequests:
		if c.To == "" {
			return fmt.Sprintf("telling the pull requests of %s it was closed", c.IssueRef)
		}
		return fmt.Sprintf("updating the pull requests of %s to %s", c.IssueRef, c.To)
	default: // core.CallMove
		return fmt.Sprintf("moving %s from %s to %s", c.IssueRef, c.From, c.To)
	}
}

func result(r core.Result) string {
	switch r {
	case core.ResultMovedMeanwhile:
		return "the issue moved meanwhile"
	case core.ResultRefused:
		return "the tracker refused"
	case core.ResultFailed:
		return "it failed"
	default: // core.ResultDone: a success never reaches these lines
		return r.String()
	}
}

func withReason(text, reason string) string {
	if reason == "" {
		return text
	}
	return text + ": " + reason
}

// Plural returns one when n is 1 and many otherwise.
func Plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
