// Package core is crew's workflow as a pure reducer: Model.Update takes an
// Input and returns the Commands to run and the domain Events to publish.
// It performs no I/O, reads no clock, starts no goroutine and builds no path;
// times and paths arrive as input data (R11, KTD2, KTD12). The engine's loop
// is its only caller, from one goroutine.
//
// Each issue the core holds moves through claim states kept apart from the
// tracker's states: Taking, then Running (or Stopping), then Judging, and
// Owed while its take or a verdict call waits for a retry. An issue is
// released when its verdict calls are settled, or when its take is given up.
package core

import (
	"slices"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Model is the core's state. Its zero value is not usable; use New. A Model
// is not safe for concurrent use: one goroutine owns it (KTD2).
type Model struct {
	stages      []crew.Stage
	maxParallel int
	issues      []*heldIssue // in the order they were taken
	listing     bool         // a ListIssues is outstanding
	timeUp      bool         // the run time is up: take nothing new
	requested   bool         // a stop was requested
	stopping    bool         // the stop sequence runs: requested, or ending a wind-down
	stopped     bool         // the Stopped event was emitted
	lastID      CallID
	// statuses holds each issue's status slot, by issue key; nil when
	// status reporting is off (KTD3).
	statuses map[string]*statusSlot
	// handled holds one entry per issue whose stage ended this run, in the
	// order the issues were released.
	handled []HandledView
	// runs counts the stage runs statuses were reported for, for their ids.
	runs int
	// lastRuns holds the last run record of each issue, stage and action; nil
	// when the model records no runs (KTD1, KTD2).
	lastRuns map[runKey]RunRecord
	// reopening is set when the workspace can reopen a failed run's
	// workspace (KTD4).
	reopening bool
}

// heldIssue is an issue the core holds, from its take until its verdict calls
// are settled.
type heldIssue struct {
	issue   crew.Issue
	stage   int // index into Model.stages
	claim   Claim
	actions []*actionRun // in the stage's action order
	calls   []*call      // the take move, then the verdict calls
	taken   time.Time    // when the stage took the issue
	// verdict is the issue's handled entry, set once every action ended and
	// completed by its verdict move's result; nil before.
	verdict *HandledView
}

// actionRun is one action of a held issue.
type actionRun struct {
	name      string
	prompt    string
	phase     Phase
	workspace string
	dir       string
	branch    string
	log       string // set once a session is asked to start
	started   time.Time
	said      string // what its running session last said
	outcome   crew.Outcome
	check     string            // its check command; empty when it has none
	stopped   bool              // a StopCheck was sent for its check
	cause     crew.FailureCause // what made it fail, once it ended failed
	// prev is the key's run record from before this run, set when the run
	// reopens a workspace or records its start; nil when there was none.
	prev *RunRecord
	// resumed is set once the action runs in a failed run's reopened
	// workspace.
	resumed bool
}

// call is a tracker call the core made and has not settled.
type call struct {
	id       CallID
	kind     CallKind
	take     bool
	from, to crew.State
	report   crew.FailureReport
	inFlight bool
	owed     bool // failed transiently; retried at the next tick
	final    bool // its current or last attempt is its one try after stop
}

// New returns a model for workflow, whose stages are in config order and
// already validated, taking at most maxParallelIssues issues at once (R6).
func New(workflow []crew.Stage, maxParallelIssues int, opts ...Option) *Model {
	stages := make([]crew.Stage, len(workflow))
	for i, s := range workflow {
		s.Actions = slices.Clone(s.Actions)
		stages[i] = s
	}
	m := &Model{stages: stages, maxParallel: maxParallelIssues}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Option changes a new Model.
type Option func(*Model)

// ReportingStatus has the model report each issue's status through
// ReportStatus commands, for a tracker that keeps status comments (KTD1).
func ReportingStatus() Option {
	return func(m *Model) { m.statuses = map[string]*statusSlot{} }
}

// Stopped reports whether a stop, requested or ending a wind-down, has
// completed: the core holds no issue, no owed call and no status write in
// flight or owed. The engine returns once Stopped is true and none of its
// commands is still running.
func (m *Model) Stopped() bool {
	return m.stopping && len(m.issues) == 0 && !m.statusesBusy()
}

// Claim is a held issue's state inside the core.
type Claim int

// The claim states.
const (
	// ClaimTaking: the take move is in flight.
	ClaimTaking Claim = iota
	// ClaimRunning: the issue is taken and its actions run.
	ClaimRunning
	// ClaimStopping: a stop was requested before every action ended; the
	// core waits for them to end.
	ClaimStopping
	// ClaimJudging: every action ended and the verdict calls are in flight.
	ClaimJudging
	// ClaimOwed: the take move or a verdict call failed transiently and
	// waits for a retry. With an owed take, no action has started yet: they
	// stay PhaseWaiting until the retried take is done.
	ClaimOwed
)

// String names the claim for renderers.
func (c Claim) String() string {
	switch c {
	case ClaimTaking:
		return "taking"
	case ClaimRunning:
		return "running"
	case ClaimStopping:
		return "stopping"
	case ClaimJudging:
		return "judging"
	case ClaimOwed:
		return "owed"
	}
	return "unknown"
}

// Phase is where one action of a held issue stands.
type Phase int

// The phases of an action.
const (
	// PhaseWaiting: the issue's take move is in flight or owed.
	PhaseWaiting Phase = iota
	// PhaseCreating: its workspace is being created.
	PhaseCreating
	// PhaseReopening: a failed run's workspace is being reopened.
	PhaseReopening
	// PhaseStarting: its session is being started.
	PhaseStarting
	// PhaseRunning: its session runs.
	PhaseRunning
	// PhaseChecking: its session succeeded and its check runs. The action
	// has not ended: it is still running for the boss.
	PhaseChecking
	// PhaseEnded: it ended; see its Outcome.
	PhaseEnded
)

// String names the phase for renderers.
func (p Phase) String() string {
	switch p {
	case PhaseWaiting:
		return "waiting"
	case PhaseCreating:
		return "creating workspace"
	case PhaseReopening:
		return "reopening workspace"
	case PhaseStarting:
		return "starting"
	case PhaseRunning:
		return "running"
	case PhaseChecking:
		return "checking"
	case PhaseEnded:
		return "ended"
	}
	return "unknown"
}

// View is a snapshot of what the core holds, for subscribers (KTD6). It
// shares no memory with the Model, so it may be kept and changed freely.
type View struct {
	// Stopping is true once a stop was requested. A wind-down ending in the
	// stop sequence by itself does not set it.
	Stopping bool
	// TimeUp is true once the run time is up and crew winds down.
	TimeUp bool
	// Issues are the held issues, in the order they were taken.
	Issues []IssueView
	// Owed are the tracker calls waiting for a retry.
	Owed []Call
	// Handled are the issues whose stage ended this run, one entry per
	// issue holding its latest stage, in the order they were released. An
	// issue held again is left out until its new stage ends.
	Handled []HandledView
}

// HandledView is an issue whose stage ended this run, as that stage left it.
type HandledView struct {
	Issue crew.Issue
	Stage string
	// To is the state the stage's verdict moved the issue to, or meant to
	// when Move is MoveDropped.
	To crew.State
	// Failures are the stage's failed actions, in its action order; nil
	// when every action succeeded.
	Failures []crew.ActionFailure
	// Move is MoveDone, or MoveDropped when crew gave the verdict move up.
	Move crew.MoveProgress
	// DropReason says why the verdict move was given up.
	DropReason string
	// Taken is when the stage took the issue; Ended is when its last action
	// ended.
	Taken time.Time
	Ended time.Time
}

// NeedsAttention reports whether the boss should look at the issue: an
// action failed, or crew gave the verdict move up.
func (h HandledView) NeedsAttention() bool {
	return len(h.Failures) > 0 || h.Move == crew.MoveDropped
}

// Duration is the stage's time, from the take to the verdict.
func (h HandledView) Duration() time.Duration { return h.Ended.Sub(h.Taken) }

// clone returns a copy of h that shares no memory with it.
func (h HandledView) clone() HandledView {
	h.Issue = h.Issue.Clone()
	h.Failures = slices.Clone(h.Failures)
	return h
}

// IssueView is one held issue.
type IssueView struct {
	Issue   crew.Issue
	Stage   string
	Claim   Claim
	Actions []ActionView
}

// ActionView is one action of a held issue.
type ActionView struct {
	Name      string
	Phase     Phase
	Workspace string
	Branch    string
	Log       string
	// Started is when its session started; zero before PhaseRunning.
	Started time.Time
	// Outcome is set once Phase is PhaseEnded.
	Outcome crew.Outcome
	// Resumed is set once the action runs in a failed run's reopened
	// workspace.
	Resumed bool
}

// View returns a snapshot of what the core holds.
func (m *Model) View() View {
	v := View{Stopping: m.requested, TimeUp: m.timeUp}
	for _, h := range m.issues {
		iv := IssueView{Issue: h.issue.Clone(), Stage: m.stages[h.stage].Name, Claim: h.claim}
		for _, a := range h.actions {
			iv.Actions = append(iv.Actions, ActionView{
				Name: a.name, Phase: a.phase, Workspace: a.workspace, Branch: a.branch,
				Log: a.log, Started: a.started, Outcome: a.outcome, Resumed: a.resumed,
			})
		}
		v.Issues = append(v.Issues, iv)
		for _, c := range h.calls {
			if c.owed {
				v.Owed = append(v.Owed, h.describe(c))
			}
		}
	}
	for _, e := range m.handled {
		if m.held(e.Issue.Key) == nil {
			v.Handled = append(v.Handled, e.clone())
		}
	}
	return v
}

// describe returns c as a Call of h.
func (h *heldIssue) describe(c *call) Call {
	return Call{Kind: c.kind, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, From: c.from, To: c.to}
}
