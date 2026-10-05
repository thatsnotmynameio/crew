// Package core is crew's rules as a pure reducer: Model.Update takes an
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
	rules       []crew.Rule
	maxParallel int
	issues      []*heldIssue // in the order they were taken
	listing     bool         // a ListIssues is outstanding
	listings    int          // the ListIssues asked for: the generation of the last one (KTD4)
	skipped     int          // ticks that skipped their listing since the last one
	timeUp      bool         // the run time is up: take nothing new
	requested   bool         // a stop was requested
	stopping    bool         // the stop sequence runs: requested, or ending a wind-down
	stopped     bool         // the Stopped event was emitted
	lastID      CallID
	// queueOf holds the queue each rule runs in, by rule index, as an
	// index into queues (KTD2). maxParallel caps every queue together.
	queueOf []int
	// queues holds each queue some rule runs in, with the slots it has.
	queues []crew.Queue
	// slots is what the rules can use: the queues' slots summed, at most
	// maxParallel (KTD4).
	slots int
	// statuses holds each issue's status slot, by issue key; nil when
	// status reporting is off (KTD3).
	statuses map[string]*statusSlot
	// pullRequests holds each issue's pull request slot, by issue key, while
	// it has a report not settled; nil when pull request reports are off
	// (KTD3).
	pullRequests map[string]*pullRequestSlot
	// handled holds one entry per issue whose rule ended this run, in the
	// order the issues were released.
	handled []handledEntry
	// runs counts the rule runs statuses were reported for, for their ids.
	runs int
	// lastRuns holds the last run record of each issue, rule and action; nil
	// when the model records no runs (KTD1, KTD2).
	lastRuns map[runKey]RunRecord
	// reopening is set when the workspace can reopen a failed run's
	// workspace (KTD4).
	reopening bool
	// finding is set when the tracker can find the pull request an action
	// opened (KTD3).
	finding bool
	// statusUsage is set when statuses show each ended action's spend and
	// pull request (KTD11).
	statusUsage bool
	// spent sums what every session that ended this run used (R14).
	spent crew.Spend
	// otherKinds holds, by item key, the rule label of each item the last
	// listing found in the label of a rule of the other kind, which was
	// reported then or before (#92).
	otherKinds map[string]crew.State
	// board is the board the model reads; nil when it reads none (KTD4).
	board *board
	// bots is what the model knows of the identities crew acts as (KTD3).
	bots bots
}

// heldIssue is an issue the core holds, from its take until its verdict calls
// are settled.
type heldIssue struct {
	issue   crew.Issue
	rule    int // index into Model.rules
	claim   Claim
	actions []*actionRun // in the rule's action order
	calls   []*call      // the take move, then the verdict calls
	taken   time.Time    // when the rule took the issue
	// verdict is the issue's handled entry, set once every action ended and
	// completed by its verdict move's result; nil before.
	verdict *HandledView
	// landed is the listing generation when the verdict move landed or was
	// given up (KTD4).
	landed int
}

// handledEntry is a handled entry with the listing generation when its
// verdict move landed or was given up: only a later listing marks it Gone
// (KTD4).
type handledEntry struct {
	view   HandledView
	landed int
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
	agent     string            // the agent whose harness runs its session
	bot       string            // the bot its session and check act as; empty for you
	stopped   bool              // a StopCheck was sent for its check
	cause     crew.FailureCause // what made it fail, once it ended failed
	// prev is the key's run record from before this run, set when the run
	// reopens a workspace or records its start; nil when there was none.
	prev *RunRecord
	// resumed is set once the action runs in a failed run's reopened
	// workspace.
	resumed bool
	// since is when the action's new workspace was made; zero for a
	// reopened one (KTD6).
	since time.Time
	// usage is what its session reported it used, once the session ended.
	usage crew.Usage
	// finding is set while its pull request is being looked up; pr holds
	// what the lookup found once it is done. An action whose outcome is
	// known before the lookup is done waits in PhaseFinishing, holding its
	// outcome and cause.
	finding bool
	pr      crew.PullRequest
}

// spend is what a's session used, or nothing when no session started.
func (a *actionRun) spend() crew.Spend {
	if a.started.IsZero() {
		return crew.Spend{}
	}
	return a.usage.Spend()
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

// New returns a model for rules, whose rules are in config order and
// already validated, taking at most maxParallelIssues issues at once (R6),
// and for each rule at most its queue's slots (R6, KTD2).
func New(rules []crew.Rule, maxParallelIssues int, opts ...Option) *Model {
	own := make([]crew.Rule, len(rules))
	for i, s := range rules {
		s.Actions = slices.Clone(s.Actions)
		own[i] = s
	}
	m := &Model{rules: own, maxParallel: maxParallelIssues}
	m.queueOf, m.queues, m.slots = queues(own, maxParallelIssues)
	for _, o := range opts {
		o(m)
	}
	return m
}

// queues returns the queue each of rules runs in, as an index into the
// second result; each queue some rule runs in, told apart by name, with its
// slots; and what the rules can use, the sum of those slots at most
// maxParallelIssues (KTD2, KTD4). The rules with the zero Queue share one
// unnamed queue of maxParallelIssues slots, so the global cap alone limits
// them.
func queues(rules []crew.Rule, maxParallelIssues int) ([]int, []crew.Queue, int) {
	queueOf := make([]int, len(rules))
	var out []crew.Queue
	usable := 0
	index := map[string]int{}
	for i, s := range rules {
		queue := s.Queue
		if queue == (crew.Queue{}) {
			queue.Slots = maxParallelIssues
		}
		q, ok := index[queue.Name]
		if !ok {
			q = len(out)
			index[queue.Name] = q
			out = append(out, queue)
			usable += queue.Slots
		}
		queueOf[i] = q
	}
	return queueOf, out, min(usable, maxParallelIssues)
}

// Option changes a new Model.
type Option func(*Model)

// FindingPullRequests has the model look up the pull request each action
// opened, through FindPullRequest commands, once its session ended (KTD3).
func FindingPullRequests() Option {
	return func(m *Model) { m.finding = true }
}

// ReportingUsage has the statuses the model reports show each ended
// action's spend and pull request (KTD11). It takes effect only with
// ReportingStatus.
func ReportingUsage() Option {
	return func(m *Model) { m.statusUsage = true }
}

// ReportingStatus has the model report each issue's status through
// ReportStatus commands, for a tracker that keeps status comments (KTD1).
func ReportingStatus() Option {
	return func(m *Model) { m.statuses = map[string]*statusSlot{} }
}

// ReportingPullRequests has the model follow each move that landed with a
// ReportPullRequests command, for a tracker that reports on pull requests
// (KTD1, KTD2).
func ReportingPullRequests() Option {
	return func(m *Model) { m.pullRequests = map[string]*pullRequestSlot{} }
}

// Stopped reports whether a stop, requested or ending a wind-down, has
// completed: the core holds no issue, no owed call, no status write in
// flight or owed and no pull request report not settled. The engine returns
// once Stopped is true and none of its commands is still running.
func (m *Model) Stopped() bool {
	return m.stopping && len(m.issues) == 0 && !m.statusesBusy() && len(m.pullRequests) == 0
}

// unknownName is what String gives for a value outside its enumeration.
const unknownName = "unknown"

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
	// PhaseChecking: its session succeeded and its check runs. The action
	// has not ended: it is still running for you.
	PhaseChecking
	// PhaseFinishing: its outcome is known and it waits for the lookup of
	// its pull request. It is still running for you.
	PhaseFinishing
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
	case PhaseFinishing:
		return "finishing"
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
	// Board is the board's issues, as the last board read found them with
	// crew's moves since applied, oldest first and then by key (KTD4, KTD6);
	// nil when the model reads no board (ListingBoard).
	Board []crew.BoardIssue
	// BoardFailure says why the last board read failed; empty once a read
	// succeeds (KTD5).
	BoardFailure string
	// Bots are the configured bots, the default first, in config order,
	// then the "you" entry (KTD3).
	Bots []BotView
}

// HandledView is an issue whose rule ended this run, as that rule left it.
type HandledView struct {
	Issue crew.Issue
	Rule  string
	// To is the state the rule's verdict moved the issue to, or meant to
	// when Move is MoveDropped.
	To crew.State
	// Failures are the rule's failed actions, in its action order; nil
	// when every action succeeded.
	Failures []crew.ActionFailure
	// Actions are the rule's actions, in its action order, with what each
	// spent and the pull request it opened (R12).
	Actions []HandledAction
	// Move is MoveDone, or MoveDropped when crew gave the verdict move up.
	Move crew.MoveProgress
	// DropReason says why the verdict move was given up.
	DropReason string
	// Gone is set when a listing requested after the verdict move landed, or
	// was given up, did not find the issue alone in To, and To is the label
	// of a rule: only those states are listed (KTD4). A blocked issue stays
	// in its label and stays listed, so it is not gone; an issue in two crew
	// states is, since crew skips it. Each such listing decides it anew.
	Gone bool
	// HeldBy names the rule that holds the issue again; empty while no
	// rule does (#109).
	HeldBy string
	// Taken is when the rule took the issue; Ended is when its last action
	// ended.
	Taken time.Time
	Ended time.Time
}

// HandledAction is one action of a HandledView.
type HandledAction struct {
	Name string
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
// action failed, or crew gave the verdict move up.
func (h HandledView) NeedsAttention() bool {
	return len(h.Failures) > 0 || h.Move == crew.MoveDropped
}

// Duration is the rule's time, from the take to the verdict.
func (h HandledView) Duration() time.Duration { return h.Ended.Sub(h.Taken) }

// clone returns a copy of h that shares no memory with it.
func (h HandledView) clone() HandledView {
	h.Issue = h.Issue.Clone()
	h.Failures = slices.Clone(h.Failures)
	h.Actions = slices.Clone(h.Actions)
	return h
}

// QueueView is one queue some rule runs in.
type QueueView struct {
	// Name is the queue's name; empty for the queue the rules with the
	// zero crew.Queue share.
	Name string
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
	Rule  string
	// Queue is the name of the queue the issue's rule runs in.
	Queue   string
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
	v := View{Stopping: m.requested, TimeUp: m.timeUp, Spent: m.spent}
	for q, queue := range m.queues {
		v.Queues = append(v.Queues, QueueView{Name: queue.Name, Slots: queue.Slots, Busy: m.busy(q)})
	}
	for _, h := range m.issues {
		iv := IssueView{
			Issue: h.issue.Clone(), Rule: m.rules[h.rule].Name,
			Queue: m.queues[m.queueOf[h.rule]].Name, Claim: h.claim,
		}
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
	v.Owed = append(v.Owed, m.owedPullRequests()...)
	for _, e := range m.handled {
		hv := e.view.clone()
		if h := m.held(hv.Issue.Key); h != nil {
			hv.HeldBy = m.rules[h.rule].Name
		}
		v.Handled = append(v.Handled, hv)
	}
	if m.board != nil {
		v.Board, v.BoardFailure = m.board.view(), m.board.failure
	}
	v.Bots = m.botsView()
	return v
}

// describe returns c as a Call of h.
func (h *heldIssue) describe(c *call) Call {
	return Call{Kind: c.kind, IssueKey: h.issue.Key, IssueRef: h.issue.Ref, From: c.from, To: c.to}
}
