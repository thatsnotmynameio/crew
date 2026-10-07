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
	// ClaimRouting: the run's sequence is over and it ends through its
	// route, one step at a time; IssueView.Route names it.
	ClaimRouting
	// ClaimOwed: the take move or a route's tracker step failed
	// transiently and waits for a retry. With an owed take, no action has
	// started yet: the first stays PhaseTaking until the retried take is
	// done. A held issue never
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
	case ClaimRouting:
		return "routing"
	case ClaimOwed:
		return "owed"
	}
	return unknownName
}

// Phase is where one action of a held issue stands.
type Phase int

// The phases of an action.
const (
	// PhaseTaking: the issue's take move is in flight or owed, and the
	// action is the first the run starts.
	PhaseTaking Phase = iota
	// PhaseAwaitingTurn: the action runs once the actions before it went
	// on to the next.
	PhaseAwaitingTurn
	// PhaseCreating: the run's workspace is being created for it.
	PhaseCreating
	// PhaseReopening: the workspace of the run this one continues is being
	// reopened for it.
	PhaseReopening
	// PhaseStarting: its session is being started.
	PhaseStarting
	// PhaseRunning: its session or its shell script runs.
	PhaseRunning
	// PhaseEnded: it ended; see its Outcome.
	PhaseEnded
	// PhaseNotRun: the run never reached it, as an action before it ended
	// the sequence through a route.
	PhaseNotRun
	// PhaseDoneInEarlierRun: it went on to the next action in the run this
	// one continues, so it does not run again.
	PhaseDoneInEarlierRun
)

// String names the phase for renderers.
func (p Phase) String() string {
	switch p {
	case PhaseTaking:
		return "taking"
	case PhaseAwaitingTurn:
		return "awaiting its turn"
	case PhaseCreating:
		return "creating workspace"
	case PhaseReopening:
		return "reopening workspace"
	case PhaseStarting:
		return "starting"
	case PhaseRunning:
		return "running"
	case PhaseEnded:
		return "ended"
	case PhaseNotRun:
		return "not run"
	case PhaseDoneInEarlierRun:
		return "done in an earlier run"
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
	// Route is the route the rule's run ended through.
	Route crew.RouteName
	// To is the state the final move of the rule's route moved the issue
	// to, or meant to when Move is MoveDropped; empty when the route closed
	// the issue, or meant to.
	To crew.State
	// Failures name the action that ended the run's sequence, with its
	// verdict, when the run ended through a route other than passed; nil
	// otherwise.
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
	// states is, since crew skips it. Each such listing decides it anew. An
	// issue the route closed is gone from the first listing requested after
	// the close landed, as no listing finds a closed issue.
	Gone bool
	// HeldBy names the rule that holds the issue again; empty while no
	// rule does (#109).
	HeldBy crew.RuleName
	// Taken is when the rule took the issue; Ended is when its run chose
	// its route.
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

// NeedsAttention reports whether you should look at the issue: its run
// ended through a route other than passed, or the route's final move or
// close did not land (R50).
func (h HandledView) NeedsAttention() bool {
	return h.Route != crew.PassedRoute || h.Move == crew.MoveDropped
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
	Queue crew.QueueName
	Claim Claim
	// Route is the route the run ends through, once it chose one; empty
	// while it takes the issue or runs its actions.
	Route   crew.RouteName
	Actions []ActionView
}

// ActionView is one action of a held issue.
type ActionView struct {
	Name      crew.ActionName
	Phase     Phase
	Workspace crew.WorkspaceName
	Branch    string
	Log       string
	// Started is when its session started or crew asked for its script;
	// zero before PhaseRunning.
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
		if p, ok := h.run.Phase().(crew.RoutingPhase); ok {
			iv.Route = p.Route
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

// claim returns h's claim, from its run: routing once it chose its route,
// stopping once a stop reached it before, and taking or running before
// that.
func (h *heldRun) claim() Claim {
	claim := ClaimRunning
	switch h.run.Phase().(type) {
	case crew.RoutingPhase:
		return ClaimRouting
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
// with the run's one workspace. The action at the cursor that has not
// started shows the run's take, or its workspace being made or reopened.
func (h *heldRun) actionViews() []ActionView {
	w, _ := h.run.Workspace().Get()
	cursor, _ := h.run.Cursor()
	actions := h.run.Actions()
	out := make([]ActionView, 0, len(actions))
	for _, a := range actions {
		v := ActionView{
			Name: a.Name(), Phase: phaseOf(a.State()), Workspace: w.Workspace.Name, Branch: w.Workspace.Branch,
			Log: w.Log, Resumed: w.Resumed,
		}
		if a.Name() == cursor.Name() && v.Phase == PhaseAwaitingTurn {
			v.Phase = h.startPhase()
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

// startPhase returns the phase that shows the action h's run is about to
// start: its take in flight, its workspace being made or reopened, or its
// start.
func (h *heldRun) startPhase() Phase {
	if _, taking := h.run.Phase().(crew.TakingPhase); taking {
		return PhaseTaking
	}
	switch h.run.WorkspaceState().(type) {
	case crew.CreatingWorkspace:
		return PhaseCreating
	case crew.ReopeningWorkspace:
		return PhaseReopening
	case crew.NoWorkspace, crew.InWorkspace:
	}
	return PhaseStarting
}

// phaseOf returns the phase that shows an action run in state.
func phaseOf(state crew.ActionRunState) Phase {
	switch state.(type) {
	case crew.StartingSession:
		return PhaseStarting
	case crew.InSession, crew.InShell:
		return PhaseRunning
	case crew.Finished:
		return PhaseEnded
	case crew.NotRun:
		return PhaseNotRun
	case crew.DoneInEarlierRun:
		return PhaseDoneInEarlierRun
	case crew.AwaitingTurn:
	}
	return PhaseAwaitingTurn
}
