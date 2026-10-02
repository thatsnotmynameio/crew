// Package lines is crew's headless renderer (R18): it prints each domain
// event of the engine's ordered subscription as one timestamped line,
// "HH:MM:SS crew: <text>". It never imports bubbletea, so the headless mode
// stays free of the TUI.
package lines

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// Source is an ordered subscription to the engine's updates, such as an
// *engine.Queue: Updates delivers them in order and is closed once the
// engine has stopped, and Dropped counts the events lost while it was full.
type Source interface {
	Updates() <-chan engine.Update
	Dropped() int
}

// Run prints every event of src to w, one line per event, stamped with the
// event's own time in loc. Each time it has drained the queue and Dropped
// has grown, it prints one line, stamped with now, saying how many events
// were dropped. It returns nil once src's channel is closed, or the first
// write error.
func Run(src Source, w io.Writer, loc *time.Location, now func() time.Time) error {
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
		return line(w, now().In(loc), fmt.Sprintf("%d %s dropped because this output fell behind", n, Plural(n, "event was", "events were")))
	}
	for u := range ch {
		for _, e := range u.Events {
			if err := line(w, e.Time().In(loc), Text(e)); err != nil {
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

func line(w io.Writer, at time.Time, text string) error {
	if _, err := fmt.Fprintf(w, "%s crew: %s\n", at.Format(time.TimeOnly), text); err != nil {
		return fmt.Errorf("print event line: %w", err)
	}
	return nil
}

// Text describes e as one English sentence, without a timestamp. The TUI
// uses it for its recent events too.
func Text(e core.Event) string {
	switch e := e.(type) {
	case core.IssueTaken:
		return fmt.Sprintf("%s took %s %q (%s -> %s)", e.Stage, e.Issue.Ref, e.Issue.Title, e.From, e.To)
	case core.ActionStarted:
		return fmt.Sprintf("%s %s/%s started on branch %s, log %s", e.IssueRef, e.Stage, e.Action, e.Branch, e.Log)
	case core.ActionEnded:
		// The reason is shown for successes too: a clean end is the only
		// success signal, so its last message is what tells the boss whether
		// the work was done.
		verdict := "failed"
		if e.Outcome.Succeeded {
			verdict = "succeeded"
		}
		return withReason(fmt.Sprintf("%s %s/%s %s", e.IssueRef, e.Stage, e.Action, verdict), e.Outcome.Reason)
	case core.IssueMoved:
		return fmt.Sprintf("%s moved from %s to %s", e.IssueRef, e.From, e.To)
	case core.FailureReported:
		return "reported the failure on " + e.IssueRef
	case core.IssueSkipped:
		states := make([]string, len(e.States))
		for i, s := range e.States {
			states[i] = string(s)
		}
		return fmt.Sprintf("skipped %s: it is in %d crew states (%s)", e.IssueRef, len(e.States), strings.Join(states, ", "))
	case core.PollDone:
		return fmt.Sprintf("poll: listed %d %s, took %d", e.Listed, Plural(e.Listed, "issue", "issues"), e.Taken)
	case core.ListingFailed:
		return withReason("listing issues failed", e.Reason)
	case core.CallOwed:
		return withReason(call(e.Call)+" failed, retrying at the next tick", e.Reason)
	case core.CallDropped:
		return withReason("gave up "+call(e.Call)+": "+result(e.Result), e.Reason)
	case core.StatusFailed:
		return withReason("could not update the status comment on "+e.IssueRef+": "+result(e.Result), e.Reason)
	case core.Stopped:
		return "stopped"
	}
	return fmt.Sprintf("%T", e)
}

func call(c core.Call) string {
	if c.Kind == core.CallReport {
		return "reporting the failure on " + c.IssueRef
	}
	return fmt.Sprintf("moving %s from %s to %s", c.IssueRef, c.From, c.To)
}

func result(r core.Result) string {
	switch r {
	case core.ResultMovedMeanwhile:
		return "the issue moved meanwhile"
	case core.ResultRefused:
		return "the tracker refused"
	case core.ResultFailed:
		return "it failed"
	}
	return r.String()
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
