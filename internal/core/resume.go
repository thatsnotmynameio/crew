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

// RunRecord is one line of the run journal: how a run of Action, in Stage,
// on the issue, started or ended (KTD1). A run's start is recorded once it
// has a workspace; its end is recorded whatever happened, and names no
// workspace when the action never got one.
type RunRecord struct {
	Event    RunEvent
	At       time.Time
	IssueKey string
	IssueRef string
	Stage    string
	Action   string
	// Workspace, Branch and Log are the run's workspace name, branch and
	// repository-relative log path.
	Workspace string
	Branch    string
	Log       string
	// Succeeded and Reason are the action's outcome; set on RunEnded only.
	Succeeded bool
	Reason    string
	// SessionStarted is when the action's session started, Usage what it
	// used and PullRequest what its lookup found; set on RunEnded only.
	// SessionStarted is zero when no session started, and then Usage is
	// empty too.
	SessionStarted time.Time
	Usage          crew.Usage
	PullRequest    crew.PullRequest
}

// runKey identifies the runs one record replaces: those of an action, in a
// stage, on an issue (R3).
type runKey struct {
	issue, stage, action string
}

func keyOf(r RunRecord) runKey { return runKey{r.IssueKey, r.Stage, r.Action} }

// failed reports whether the run r records counts as failed (R2): it ended
// and did not succeed, or it never recorded an end.
func (r RunRecord) failed() bool {
	return r.Event == RunStarted || !r.Succeeded
}

// reason is why the run r records failed: its outcome's reason, or
// crashedReason when it never recorded an end.
func (r RunRecord) reason() string {
	if r.Event == RunStarted {
		return crashedReason
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

// lastRun returns the last run record of a, in h's stage, on h's issue
// (R3).
func (m *Model) lastRun(h *heldIssue, a *actionRun) (RunRecord, bool) {
	r, ok := m.lastRuns[runKey{h.issue.Key, m.stages[h.stage].Name, a.name}]
	return r, ok
}

// resumable returns the failed last run of a when the model can reopen its
// workspace (R1, R2).
func (m *Model) resumable(h *heldIssue, a *actionRun) (RunRecord, bool) {
	if !m.reopening {
		return RunRecord{}, false
	}
	r, ok := m.lastRun(h, a)
	if !ok || !r.failed() {
		return RunRecord{}, false
	}
	return r, true
}

// record remembers a's run event and asks the engine to write it, when the
// model records runs. An ended run whose session never started keeps the
// reason of the last session in its workspace, which says more than how
// this run failed to start (KTD6). The end of a run without a workspace is
// written but not remembered: it would replace the key's last record, and
// stop a failed run from resuming.
func (s *step) record(h *heldIssue, a *actionRun, event RunEvent) {
	m := s.m
	if m.lastRuns == nil {
		return
	}
	r := RunRecord{
		Event: event, At: s.at, IssueKey: h.issue.Key, IssueRef: h.issue.Ref,
		Stage: m.stages[h.stage].Name, Action: a.name,
		Workspace: a.workspace, Branch: a.branch, Log: a.log,
	}
	if event == RunEnded {
		r.Succeeded, r.Reason = a.outcome.Succeeded, a.outcome.Reason
		if p := a.prev; a.started.IsZero() && p != nil && p.Workspace == a.workspace && p.failed() {
			r.Reason = p.reason()
		}
		r.SessionStarted, r.PullRequest = a.started, a.pr
		if !a.started.IsZero() {
			r.Usage = a.usage
		}
	}
	if r.Workspace != "" {
		m.remember(r)
	}
	s.command(RecordRun{Record: r})
}

// resumeParagraph is what crew appends to a resumed session's prompt (R5,
// KTD7): that the session continues prev's failed run in this workspace,
// why that run failed, and where its output is. log is the repository-
// relative path of the log, and logFromDir the same path from the
// workspace; branch is the workspace's branch.
func resumeParagraph(prev RunRecord, branch, log, logFromDir string) string {
	var b strings.Builder
	b.WriteString("crew: this session continues the work of an earlier session on this action, in this worktree")
	if branch != "" {
		fmt.Fprintf(&b, ", on branch `%s`", branch)
	}
	fmt.Fprintf(&b, ". That run failed: %q. Its output is in the log `%s` of the repository's main checkout", oneLine(prev.reason()), log)
	if logFromDir != "" {
		fmt.Fprintf(&b, " (`%s` from this worktree)", logFromDir)
	}
	b.WriteString(", above the line crew wrote there when this session started. ")
	b.WriteString("Check the worktree's state with `git status` and `git log` before you go on, and continue from where it stopped instead of starting over.")
	return b.String()
}

// oneLine joins the words of s with single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
