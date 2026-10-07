package core

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Claim is a held issue's state inside the core, as the view derives it
// from the issue's rule run and its run lane (KTD-P14).
type Claim int

// The claim states.
const (
	// ClaimTaking: the take move is in flight.
	ClaimTaking Claim = iota
	// ClaimRunning: the issue is taken and its actions run.
	ClaimRunning
	// ClaimStopping: a stop was requested before the run chose its route;
	// the core waits for its running action to end.
	ClaimStopping
	// ClaimJudging: the run's sequence is over and it ends through its
	// route, one step at a time.
	ClaimJudging
	// ClaimOwed: the take move or a route's tracker step failed
	// transiently and waits for a retry. With an owed take, no action has started yet: they
	// stay PhaseWaiting until the retried take is done. A held issue never
	// stores it: the view shows it over any other claim from the first
	// transient failure of the call in the issue's run lane until it
	// settled.
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
	return unknownName
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
	// PhaseChecking: its shell action's script runs.
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
	return unknownName
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
	// Queues are the queues some rule runs in, in the order of the first
	// rule that runs in each.
	Queues []QueueView
	// Owed are the tracker calls waiting for a retry: the held issues'
	// moves and failure reports, then the pull request reports.
	Owed []Call
	// Handled are the issues whose rule ended this run, one entry per
	// issue holding its latest rule, in the order they were released. An
	// issue held again keeps its entry, marked HeldBy, until its new rule
	// ends (#109).
	Handled []HandledView
	// Spent sums what every session that ended this run used, including
	// those of entries Handled no longer shows (R14).
	Spent crew.Spend
	// Board is the board's items, as the last board read or listing found
	// them with crew's moves since applied, oldest first and then by id
	// (KTD4, KTD6, KTD10); nil when the model has no board (ListingBoard,
	// BoardFromListings).
	Board []crew.BoardIssue
	// BoardFailure says why the last board read, or the last listing of a
	// board filled from the listings, failed; empty once one succeeds
	// (KTD5).
	BoardFailure string
	// Bots are the configured bots, the default first, in config order,
	// then the "you" entry (KTD3).
	Bots []BotView
}

// HandledView is an issue whose rule ended this run, as that rule left it.
type HandledView struct {
	Issue crew.Issue
	Rule  crew.RuleName
	// To is the state the final move of the rule's route moved the issue
	// to, or meant to when Move is MoveDropped; empty for a close.
	To crew.State
	// Failures name the action that ended the run's sequence when the run
	// ended through a route other than passed; nil otherwise.
	Failures []crew.ActionFailure
	// Actions are the rule's actions, in its action order, with what each
	// spent and the pull request it opened (R12).
	Actions []HandledAction
	// Move is MoveDone, or MoveDropped when the route's final move or
	// close was given up or dropped.
	Move crew.MoveProgress
	// DropReason says why the final move or close did not land.
	DropReason string
	// Gone is set when a listing requested after the ending move landed, or
	// was given up, did not find the issue alone in To, and To is the label
	// of a rule: only those states are listed (KTD4). A blocked issue stays
	// in its label and stays listed, so it is not gone; an issue in two crew
	// states is, since crew skips it. Each such listing decides it anew.
	Gone bool
	// HeldBy names the rule that holds the issue again; empty while no
	// rule does (#109).
	HeldBy crew.RuleName
	// Taken is when the rule took the issue; Ended is when its last action
	// ended.
	Taken time.Time
	Ended time.Time
	// Earlier sums what the rules that ended on the issue before this one
	// spent this run, whose entries this one replaced (KTD14).
	Earlier crew.Spend
}

// HandledAction is one action of a HandledView.
type HandledAction struct {
	Name crew.ActionName
	// Spend is what its session used; it sums no session when the action
	// never had one.
	Spend crew.Spend
	// PullRequest is the pull request its lookup found.
	PullRequest crew.PullRequest
}

// Spend sums what the rule's sessions used.
func (h HandledView) Spend() crew.Spend {
	var sum crew.Spend
	for _, a := range h.Actions {
		sum = sum.Add(a.Spend)
	}
	return sum
}

// NeedsAttention reports whether you should look at the issue: an
// action failed, or crew gave the ending move up.
func (h HandledView) NeedsAttention() bool {
	return len(h.Failures) > 0 || h.Move == crew.MoveDropped
}

// Duration is the rule's time, from the take to the ending.
func (h HandledView) Duration() time.Duration { return h.Ended.Sub(h.Taken) }

// QueueView is one queue some rule runs in.
type QueueView struct {
	// Name is the queue's name; empty for the queue the rules with the
	// zero crew.Queue share.
	Name crew.QueueName
	// Slots is how many issues the queue may hold at once.
	Slots int
	// Busy is how many held issues, in any claim, run in the queue: the
	// count the core takes by (KTD3).
	Busy int
}

// Free is how many of the queue's slots are not busy.
func (q QueueView) Free() int { return max(q.Slots-q.Busy, 0) }

// IssueView is one held issue.
type IssueView struct {
	Issue crew.Issue
	Rule  crew.RuleName
	// Queue is the name of the queue the issue's rule runs in.
	Queue   crew.QueueName
	Claim   Claim
	Actions []ActionView
}

// ActionView is one action of a held issue.
type ActionView struct {
	Name      crew.ActionName
	Phase     Phase
	Workspace crew.WorkspaceName
	Branch    string
	Log       string
	// Started is when its session or script started; zero before
	// PhaseRunning or PhaseChecking.
	Started time.Time
	// Outcome is set once Phase is PhaseEnded.
	Outcome crew.Outcome
	// Resumed is set once the run works in the reopened workspace of the
	// run it continues.
	Resumed bool
}

// View returns a snapshot of what the core holds.
func (m *Model) View() View {
	v := View{Stopping: m.requested, TimeUp: m.timeUp, Spent: m.spent}
	for q, queue := range m.queues {
		v.Queues = append(v.Queues, QueueView{Name: queue.Name, Slots: queue.Slots, Busy: m.busy(q)})
	}
	for _, h := range m.issues {
		iv := IssueView{
			Issue: h.run.Issue(), Rule: h.run.Rule(), Queue: m.queues[m.queueOf[h.rule]].Name, Claim: h.claim(),
		}
		if m.outbox.owing(h.id()) {
			iv.Claim = ClaimOwed
		}
		iv.Actions = h.actionViews()
		v.Issues = append(v.Issues, iv)
		v.Owed = append(v.Owed, m.outbox.owedRun(h.id())...)
	}
	v.Owed = append(v.Owed, m.outbox.owedPullRequests()...)
	for _, e := range m.handled {
		hv, _ := e.view()
		if h := m.held(hv.Issue.ID()); h != nil {
			hv.HeldBy = h.run.Rule()
		}
		v.Handled = append(v.Handled, hv)
	}
	if m.board != nil {
		v.Board, v.BoardFailure = m.board.view(), m.board.failure
	}
	v.Bots = m.botsView()
	return v
}

// claim returns h's claim, from its run: judging once it chose its route,
// stopping once a stop reached it before, and taking or running before
// that.
func (h *heldRun) claim() Claim {
	claim := ClaimRunning
	switch h.run.Phase().(type) {
	case crew.RoutingPhase:
		return ClaimJudging
	case crew.TakingPhase:
		claim = ClaimTaking
	case crew.RunningPhase, crew.ReleasedPhase:
	}
	if h.run.Stopping() {
		return ClaimStopping
	}
	return claim
}

// actionViews returns the actions of h's run as the view shows them, each
// with the run's one workspace. The action at the cursor shows the run's
// workspace being made or reopened before it starts.
func (h *heldRun) actionViews() []ActionView {
	w, _ := h.run.Workspace().Get()
	cursor, _ := h.run.Cursor()
	out := make([]ActionView, 0, len(h.run.Actions()))
	for _, a := range h.run.Actions() {
		v := ActionView{
			Name: a.Name(), Phase: phaseOf(a.State()), Workspace: w.Workspace.Name, Branch: w.Workspace.Branch,
			Log: w.Log, Resumed: w.Resumed,
		}
		if a.Name() == cursor.Name() && v.Phase == PhaseWaiting {
			v.Phase = workspacePhase(h.run.WorkspaceState())
		}
		v.Started, _ = a.SessionStarted().Get()
		switch s := a.State().(type) {
		case crew.InShell:
			v.Started = s.Started
		case crew.Finished:
			v.Outcome = s.End.Outcome()
		case crew.AwaitingTurn, crew.DoneInEarlierRun, crew.StartingSession, crew.InSession, crew.NotRun:
		}
		out = append(out, v)
	}
	return out
}

// workspacePhase returns the phase that shows the action a run is about to
// start while its workspace stands at state.
func workspacePhase(state crew.WorkspaceState) Phase {
	switch state.(type) {
	case crew.CreatingWorkspace:
		return PhaseCreating
	case crew.ReopeningWorkspace:
		return PhaseReopening
	case crew.NoWorkspace, crew.InWorkspace:
	}
	return PhaseWaiting
}

// phaseOf returns the phase that shows an action run in state.
func phaseOf(state crew.ActionRunState) Phase {
	switch state.(type) {
	case crew.StartingSession:
		return PhaseStarting
	case crew.InSession:
		return PhaseRunning
	case crew.InShell:
		return PhaseChecking
	case crew.Finished:
		return PhaseEnded
	case crew.AwaitingTurn, crew.DoneInEarlierRun, crew.NotRun:
	}
	return PhaseWaiting
}
