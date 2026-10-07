package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// journal is what the model knows of the rule runs' past, when it journals
// them (KTD12): their History, replayed from the run journal and folded
// live, and which action last worked in each workspace (KTD9).
type journal struct {
	history crew.History
	// claims holds, by workspace name, the issue, rule and action whose
	// action run last started or ended in it, across issues (KTD9).
	claims map[crew.WorkspaceName]actionKey
}

// actionKey identifies the runs of an action, in a rule, on an issue.
type actionKey struct {
	issue  crew.IssueID
	rule   crew.RuleName
	action crew.ActionName
}

// Journaling has the model journal every run event through Record
// commands, starting from past, the run journal's events in the order they
// were written (KTD12). Replaying past folds the History and the
// workspaces' claims only: it takes no slot and makes no command, event,
// status, handled entry or spend.
func Journaling(past []crew.RunEvent) Option {
	return func(m *Model) {
		m.journal = &journal{claims: map[crew.WorkspaceName]actionKey{}}
		for _, e := range past {
			m.journal.fold(e)
		}
	}
}

// Reopening has the model reopen a failed run's workspace, through
// ReopenWorkspace commands, for a workspace that can (KTD4). It takes effect
// only with Journaling, which tells the model which runs failed.
func Reopening() Option {
	return func(m *Model) { m.reopening = true }
}

// fold adds e to the past. An action run's start in a workspace another
// action last worked in retires that action's last action run, which the
// workspace no longer holds (KTD5, KTD9); the start and an end in a
// workspace claim it.
func (j *journal) fold(e crew.RunEvent) {
	if opened, ok := e.(crew.ActionOpened); ok {
		key := actionKey{opened.IssueID, opened.Rule, opened.Action}
		if other, ok := j.claims[opened.Workspace.Name]; ok && other != key {
			j.history.Forget(other.issue, other.rule, other.action)
		}
		j.claims[opened.Workspace.Name] = key
	}
	if ended, ok := e.(crew.ActionEnded); ok {
		if w, ok := ended.Workspace.Get(); ok {
			j.claims[w.Workspace.Name] = actionKey{ended.IssueID, ended.Rule, ended.Action}
		}
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

// continued returns the id of the last run of rule on the issue identified
// by id, which a new run of it continues, and the resume points of its
// actions when the model can reopen their workspaces (R1, R2, KTD12).
func (m *Model) continued(
	id crew.IssueID, rule crew.RuleName,
) (crew.Optional[crew.RuleRunID], map[crew.ActionName]crew.ResumePoint) {
	if m.journal == nil {
		return crew.Optional[crew.RuleRunID]{}, nil
	}
	var continues crew.Optional[crew.RuleRunID]
	if last, ok := m.journal.history.LastRun(id, rule); ok {
		continues = crew.Some(last.ID())
	}
	if !m.reopening {
		return continues, nil
	}
	return continues, m.journal.history.ResumePoints(id, rule)
}

// notRecorded returns the event that says e, a run event the engine could
// not append, was not recorded, and false for an event of which today's
// journal wrote no line: only an action's start and end say so (KTD-P6).
func notRecorded(e crew.RunEvent, at time.Time, reason string) (RunNotRecorded, bool) {
	action, ok := recordedAction(e)
	if !ok {
		return RunNotRecorded{}, false
	}
	h := e.Head()
	return RunNotRecorded{
		At: at, IssueID: h.IssueID, IssueRef: h.IssueRef, Rule: h.Rule, Action: action, Reason: reason,
	}, true
}

// recordedAction returns the action whose start or end e is, and false when
// e is neither.
func recordedAction(e crew.RunEvent) (crew.ActionName, bool) {
	if opened, ok := e.(crew.ActionOpened); ok {
		return opened.Action, true
	}
	if ended, ok := e.(crew.ActionEnded); ok {
		return ended.Action, true
	}
	return "", false
}

// resumeParagraph is what crew appends to a resumed session's prompt (R5,
// KTD7): that the session continues a failed run in this workspace, why
// that run failed (reason), and where its output is. log is the
// repository-relative path of the log, and logFromDir the same path from
// the workspace; branch is the workspace's branch.
func resumeParagraph(reason crew.SessionText, branch, log, logFromDir string) string {
	var b strings.Builder
	b.WriteString("crew: this session continues the work of an earlier session on this action, in this worktree")
	if branch != "" {
		fmt.Fprintf(&b, ", on branch `%s`", branch)
	}
	fmt.Fprintf(&b, ". That run failed: %q.", oneLine(reason.String()))
	fmt.Fprintf(&b, " Its output is in the log `%s` of the repository's main checkout", log)
	if logFromDir != "" {
		fmt.Fprintf(&b, " (`%s` from this worktree)", logFromDir)
	}
	b.WriteString(", above the line crew wrote there when this session started. ")
	b.WriteString("Check the worktree's state with `git status` and `git log` before you go on, ")
	b.WriteString("and continue from where it stopped instead of starting over.")
	return b.String()
}

// oneLine joins the words of s with single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
