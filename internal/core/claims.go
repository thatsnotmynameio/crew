package core

import (
	"fmt"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// journal is what the model knows of the rule runs' past, when it journals
// them (KTD12): their History, replayed from the run journal and folded
// live, with each past run whose worktree's name another run opens retired
// (KTD18).
type journal struct {
	history crew.History
}

// Journaling has the model journal every run event through Record
// commands, starting from past, the run journal's events in the order they
// were written (KTD12). Replaying past folds the History only: it takes no
// slot and makes no command, event, status, handled entry or spend.
func Journaling(past []crew.RunEvent) Option {
	return func(m *Model) {
		m.journal = &journal{}
		for _, e := range past {
			m.journal.fold(e)
		}
	}
}

// Reopening has the model reopen the worktree of the run a new run
// continues, through ReopenWorkspace commands, for a workspace that can
// (KTD4, KTD19). It takes effect only with Journaling, which tells the
// model where each run stopped. Without it, a run that would resume starts
// fresh, and the passed route alone runs without a worktree.
func Reopening() Option {
	return func(m *Model) { m.reopening = true }
}

// fold adds e to the past. A run's worktree that opens retires every other
// past run that would reopen a worktree of its name, which no longer holds
// that run's work (KTD18).
func (j *journal) fold(e crew.RunEvent) {
	if opened, ok := e.(crew.WorkspaceOpened); ok {
		j.history.Retire(opened.Workspace.Name, opened.IssueID, opened.Rule)
	}
	j.history.Fold(e)
}

// record folds e, an event of a live run, into the past and asks the engine
// to append it to the run journal, before the commands e calls for, when
// the model journals.
func (s *step) record(e crew.RunEvent) {
	if s.m.journal == nil {
		return
	}
	s.m.journal.fold(e)
	s.command(Record{Event: e})
}

// continued returns take, of a new run of rule, with what it gets from the
// last run of rule on its issue, which it continues: that run's id, how
// the new run starts from it (R22, KTD19), and the open questions it
// inherits (KTD-W7). Without a journal it continues nothing and starts
// fresh, and without reopening it starts without the worktree to reopen.
func (m *Model) continued(take crew.RunTaken, rule crew.Rule) crew.RunTaken {
	take.Start = crew.StartFresh{}
	if m.journal == nil {
		return take
	}
	id := take.IssueID
	if last, ok := m.journal.history.LastRun(id, rule.Name); ok {
		take.Continues = crew.Some(last.ID())
	}
	take.Start = m.journal.history.Start(id, rule)
	if !m.reopening {
		take.Start = crew.WithoutWorktree(take.Start)
	}
	take.Questions = m.journal.history.Questions(id, rule.Name)
	return take
}

// notRecorded returns the event that says e, a run event the engine could
// not append, was not recorded, and false for an event no resume depends
// on (KTD18).
func notRecorded(e crew.RunEvent, at time.Time, reason string) (RunNotRecorded, bool) {
	action, what, ok := unrecorded(e)
	if !ok {
		return RunNotRecorded{}, false
	}
	h := e.Head()
	return RunNotRecorded{
		At: at, IssueID: h.IssueID, IssueRef: h.IssueRef, Rule: h.Rule, Action: action, What: what, Reason: reason,
	}, true
}

// unrecorded returns, for e, an event a resume depends on, the action it is
// about and what it is in crew's words: the run's worktree, an action's
// start or end, its session's start, the route it chose, a step's outcome
// or its release. It returns false for any other event.
func unrecorded(e crew.RunEvent) (crew.ActionName, string, bool) {
	switch e := e.(type) {
	case crew.WorkspaceOpened:
		return "", "its worktree " + string(e.Workspace.Name), true
	case crew.ActionSessionAsked:
		return e.Action, "the start of " + string(e.Action), true
	case crew.ActionShellAsked:
		return e.Action, "the start of " + string(e.Action), true
	case crew.ActionFunctionAsked:
		return e.Action, "the start of " + string(e.Action), true
	case crew.ActionReturnAsked:
		return e.Action, "the start of " + string(e.Action), true
	case crew.ActionSessionStarted:
		return e.Action, "the start of " + string(e.Action) + "'s session", true
	case crew.ActionEnded:
		return e.Action, "the end of " + string(e.Action), true
	case crew.RouteChosen:
		return e.Action, "the route " + string(e.Route) + " it chose", true
	case crew.StepEnded:
		return "", fmt.Sprintf("the outcome of step %d of its route", e.Step+1), true
	case crew.RunReleased:
		return "", "its release", true
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.RunOutOfTime, crew.WorkspaceAsked,
		crew.WorkspaceMissing, crew.ActionSessionStopAsked, crew.ActionSessionEnded, crew.ActionShellStopAsked,
		crew.ActionShellEnded, crew.ActionFunctionStopAsked, crew.ActionFunctionEnded, crew.RunLookupAsked,
		crew.RunLookupDone, crew.StepAsked, crew.StepShellStopAsked, crew.StepFunctionStopAsked:
	}
	return "", "", false
}
