package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// crashedReason is the reason of a run that recorded its start and never its
// end: crew did not live to see it end.
const crashedReason = "crew stopped before the run ended: it crashed or was killed"

// RunEvent tells a run's start from its end in a RunRecord.
type RunEvent int

// The events of a run record.
const (
	// RunStarted: the run's workspace is ready and its session is about to
	// start.
	RunStarted RunEvent = iota
	// RunEnded: the run's action ended; see Succeeded and Reason.
	RunEnded
)

// RunRecord is one line of the run journal: how a run of Action, in Rule,
// on the issue, started or ended (KTD1). A run's start is recorded once it
// has a workspace; its end is recorded whatever happened, and names no
// workspace when the action never got one.
type RunRecord struct {
	Event    RunEvent
	At       time.Time
	IssueID  crew.IssueID
	IssueRef string
	Rule     crew.RuleName
	Action   crew.ActionName
	// Workspace, Branch and Log are the run's workspace name, branch and
	// repository-relative log path.
	Workspace crew.WorkspaceName
	Branch    string
	Log       string
	// Succeeded and Reason are the action's outcome; set on RunEnded only.
	Succeeded bool
	Reason    crew.SessionText
	// SessionStarted is when the action's session started, Usage what it
	// used and PullRequest what its lookup found; set on RunEnded only.
	// SessionStarted is zero when no session started, and then Usage is
	// empty too.
	SessionStarted time.Time
	Usage          crew.Usage
	PullRequest    crew.PullRequest
}

// runKey identifies the runs one record replaces: those of an action, in a
// rule, on an issue (R3).
type runKey struct {
	issue  crew.IssueID
	rule   crew.RuleName
	action crew.ActionName
}

func keyOf(r RunRecord) runKey { return runKey{r.IssueID, r.Rule, r.Action} }

// failed reports whether the run r records counts as failed (R2): it ended
// and did not succeed, or it never recorded an end.
func (r RunRecord) failed() bool {
	return r.Event == RunStarted || !r.Succeeded
}

// reason is why the run r records failed: its outcome's reason, or
// crashedReason when it never recorded an end.
func (r RunRecord) reason() crew.SessionText {
	if r.Event == RunStarted {
		return crew.NewSessionText(crashedReason)
	}
	return r.Reason
}

// RecordingRuns has the model record every run through RecordRun commands,
// starting from past, the run journal's records in the order they were
// written (KTD1, KTD2).
func RecordingRuns(past []RunRecord) Option {
	return func(m *Model) {
		m.lastRuns = map[runKey]RunRecord{}
		for _, r := range past {
			m.remember(r)
		}
	}
}

// Reopening has the model reopen a failed run's workspace, through
// ReopenWorkspace commands, for a workspace that can (KTD4). It takes effect
// only with RecordingRuns, which tells the model which runs failed.
func Reopening() Option {
	return func(m *Model) { m.reopening = true }
}

// remember makes r its key's last record. A start also retires the record of
// every other key naming r's workspace: names repeat once a workspace is
// gone, so that record's workspace now holds another key's work (KTD5).
func (m *Model) remember(r RunRecord) {
	if r.Event == RunStarted {
		for k, other := range m.lastRuns {
			if k != keyOf(r) && other.Workspace == r.Workspace {
				delete(m.lastRuns, k)
			}
		}
	}
	m.lastRuns[keyOf(r)] = r
}

// resumable returns where the action named action, in rule, on the issue
// identified by id, resumes its failed last run, when the model can reopen
// that run's workspace (R1, R2): a new rule run inherits it at take.
func (m *Model) resumable(id crew.IssueID, rule crew.RuleName, action crew.ActionName) crew.Optional[crew.ResumePoint] {
	r, ok := m.lastRuns[runKey{id, rule, action}]
	if !m.reopening || !ok || !r.failed() {
		return crew.Optional[crew.ResumePoint]{}
	}
	return crew.Some(crew.ResumePoint{
		Workspace: crew.Workspace{Name: r.Workspace, Branch: r.Branch}, Log: r.Log, Reason: r.reason(),
	})
}

// recordOpened records the start of the action run e opened in its
// workspace, keeping first its key's last record from before, when the
// model records runs.
func (s *step) recordOpened(h *heldIssue, e crew.ActionOpened) {
	m := s.m
	if m.lastRuns == nil {
		return
	}
	p := h.plumb(e.Action)
	p.prev = nil
	if prev, ok := m.lastRuns[runKey{e.IssueID, e.Rule, e.Action}]; ok {
		p.prev = &prev
	}
	s.record(RunRecord{
		Event: RunStarted, At: e.At, IssueID: e.IssueID, IssueRef: e.IssueRef, Rule: e.Rule, Action: e.Action,
		Workspace: e.Workspace.Name, Branch: e.Workspace.Branch, Log: e.Log,
	})
}

// recordEnded records the end of the action run e ended, when the model
// records runs. An ended run whose session never started keeps the reason
// of the last session in its workspace, which says more than how this run
// failed to start (KTD6).
func (s *step) recordEnded(h *heldIssue, e crew.ActionEnded) {
	if s.m.lastRuns == nil {
		return
	}
	outcome := e.End.Outcome()
	r := RunRecord{
		Event: RunEnded, At: e.At, IssueID: e.IssueID, IssueRef: e.IssueRef, Rule: e.Rule, Action: e.Action,
		Succeeded: outcome.Succeeded, Reason: outcome.Reason, PullRequest: e.PullRequest,
	}
	if w, ok := e.Workspace.Get(); ok {
		r.Workspace, r.Branch, r.Log = w.Workspace.Name, w.Workspace.Branch, w.Log
	}
	if started, ok := e.SessionStarted.Get(); ok {
		r.SessionStarted, r.Usage = started, e.Usage
	} else if p := h.live[e.Action]; p != nil && p.prev != nil && p.prev.Workspace == r.Workspace && p.prev.failed() {
		r.Reason = p.prev.reason()
	}
	s.record(r)
}

// record remembers r and asks the engine to write it. The end of a run
// without a workspace is written but not remembered: it would replace the
// key's last record, and stop a failed run from resuming.
func (s *step) record(r RunRecord) {
	if r.Workspace != "" {
		s.m.remember(r)
	}
	s.command(RecordRun{Record: r})
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
