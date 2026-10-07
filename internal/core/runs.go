package core

import (
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// plumbing is what one live action run's session and checks need that is
// neither run state nor in any run event (KTD-P5): the workspace's
// directory and the log's path from it, the rendered prompt, the session's
// last message and what it last said, and its key's run record from before
// its workspace was ready.
type plumbing struct {
	dir         string
	logFromDir  string
	prompt      string
	lastMessage string
	said        crew.Said
	// prev is the key's last run record when the action's workspace was
	// ready; nil when there was none.
	prev *RunRecord
}

// plumb returns the plumbing of h's action named name, made on first use.
func (h *heldIssue) plumb(name crew.ActionName) *plumbing {
	if h.live == nil {
		h.live = map[crew.ActionName]*plumbing{}
	}
	p := h.live[name]
	if p == nil {
		p = &plumbing{}
		h.live[name] = p
	}
	return p
}

// said keeps what the session of h's action named name last said, while
// it runs.
func (h *heldIssue) said(name crew.ActionName, text crew.Said) {
	if a, ok := h.run.Action(name); ok && phaseOf(a.State()) == PhaseRunning {
		h.plumb(name).said = text
	}
}

// sayings returns what each of h's sessions last said, by action.
func (h *heldIssue) sayings() map[crew.ActionName]crew.Said {
	out := make(map[crew.ActionName]crew.Said, len(h.live))
	for name, p := range h.live {
		out[name] = p.said
	}
	return out
}

// id returns the id of h's issue.
func (h *heldIssue) id() crew.IssueID { return h.run.Issue().ID() }

// move returns the move of h's issue from one state to another, as a Call.
func (h *heldIssue) move(from, to crew.State) Call {
	issue := h.run.Issue()
	return Call{Kind: CallMove, IssueID: issue.ID(), IssueRef: issue.Ref(), From: from, To: to}
}

// claim returns h's claim, from its run: judging once every action ended,
// stopping once a stop reached it before, and taking or running before
// that.
func (h *heldIssue) claim() Claim {
	claim := ClaimRunning
	switch h.run.Phase().(type) {
	case crew.JudgingPhase:
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

// phaseOf returns the phase that shows an action run in state.
func phaseOf(state crew.ActionRunState) Phase {
	switch state.(type) {
	case crew.AwaitingTake:
		return PhaseWaiting
	case crew.CreatingWorkspace:
		return PhaseCreating
	case crew.ReopeningWorkspace:
		return PhaseReopening
	case crew.StartingSession:
		return PhaseStarting
	case crew.InSession:
		return PhaseRunning
	case crew.InChecks:
		return PhaseChecking
	case crew.Finishing:
		return PhaseFinishing
	case crew.Finished:
		return PhaseEnded
	}
	return PhaseWaiting
}

// actionView returns a as the view shows it.
func actionView(a crew.ActionRun) ActionView {
	w, _ := a.Workspace().Get()
	started, _ := a.SessionStarted().Get()
	return ActionView{
		Name: a.Name(), Phase: phaseOf(a.State()), Workspace: w.Workspace.Name, Branch: w.Workspace.Branch,
		Log: w.Log, Started: started, Outcome: a.Outcome(), Resumed: w.Resumed,
	}
}

// definition returns what h's run decides by.
func (m *Model) definition(h *heldIssue) crew.RunDefinition {
	return crew.RunDefinition{Rule: m.rules[h.rule], FindsPullRequests: m.finding}
}

// action returns the definition of the action named name in h's rule.
func (m *Model) action(h *heldIssue, name crew.ActionName) crew.Action {
	actions := m.rules[h.rule].Actions
	if i := slices.IndexFunc(actions, func(a crew.Action) bool { return a.Name == name }); i >= 0 {
		return actions[i]
	}
	return crew.Action{}
}

// head returns the head of a fact of h's run, at the input's time.
func (s *step) head(h *heldIssue) crew.FactHead {
	return crew.FactHead{Run: h.run.ID(), At: s.at}
}

// decide hands fact to h's run and applies the events it decides. A fact
// the run refuses changes nothing, as an input that answers nothing the
// core waits for.
func (s *step) decide(h *heldIssue, fact crew.Fact) {
	if events, ok := s.decisions(h, fact); ok {
		s.apply(h, events)
	}
}

// decisions returns the events h's run decides on fact, and false when the
// run refuses it.
func (s *step) decisions(h *heldIssue, fact crew.Fact) ([]crew.RunEvent, bool) {
	events, err := crew.Decide(h.run, s.m.definition(h), fact)
	return events, err == nil
}

// apply applies events to h's run, in order, and after each issues the
// commands and emits the events it calls for.
func (s *step) apply(h *heldIssue, events []crew.RunEvent) {
	for _, e := range events {
		run, err := crew.Apply(h.run, e)
		if err != nil {
			// Decide returns only events of h's run.
			return
		}
		h.run = run
		s.on(h, e)
	}
}

// on issues the commands and emits the events e calls for, once applied to
// h's run. A run event about one action goes to onAction.
func (s *step) on(h *heldIssue, e crew.RunEvent) {
	switch e := e.(type) {
	case crew.TakeMoved:
		s.takeMoved(h, e)
	case crew.RunJudged:
		s.judged(h, e)
	case crew.VerdictMoved:
		s.emit(IssueMoved{At: s.at, IssueID: e.IssueID, IssueRef: e.IssueRef, From: e.From, To: e.To})
		s.m.boardMoved(h.run.Issue(), e.To)
		s.reportRun(h)
		s.reportVerdict(h)
		h.landed = s.m.listings
	case crew.VerdictDropped:
		s.reportRun(h)
		h.landed = s.m.listings
	case crew.FailureReported:
		s.emit(FailureReported{At: s.at, IssueID: e.IssueID, IssueRef: e.IssueRef})
	case crew.RunReleased:
		s.m.release(h)
		s.freed()
	case crew.RunTaken, crew.RunStopped, crew.FailureReportDropped:
		// Nothing to do outside the run.
	case crew.ActionWorkspaceAsked, crew.WorkspaceMissing, crew.ActionOpened, crew.ActionSessionAsked,
		crew.ActionSessionStarted, crew.ActionSessionStopAsked, crew.ActionSessionEnded, crew.ActionLookupAsked,
		crew.ActionCheckAsked, crew.ActionCheckStopAsked, crew.ActionCheckEnded, crew.ActionLookupDone,
		crew.ActionFinishing, crew.ActionEnded:
		s.onAction(h, e)
	}
}

// onAction issues the commands and emits the events e, an event about one
// action of h's run, calls for.
func (s *step) onAction(h *heldIssue, e crew.RunEvent) {
	switch e := e.(type) {
	case crew.ActionWorkspaceAsked:
		s.workspaceAsked(h, e)
	case crew.WorkspaceMissing:
		s.emit(WorkspaceMissing{
			At: s.at, IssueID: e.IssueID, IssueRef: e.IssueRef, Rule: e.Rule, Action: e.Action,
			Workspace: e.Workspace.Name,
		})
	case crew.ActionOpened:
		s.recordOpened(h, e)
	case crew.ActionSessionAsked:
		s.startSession(h, e.Action)
	case crew.ActionSessionStarted:
		s.emit(ActionStarted{
			At: s.at, IssueID: e.IssueID, IssueRef: e.IssueRef, Rule: e.Rule, Action: e.Action,
			Workspace: e.Workspace.Name, Branch: e.Workspace.Branch, Log: e.Log, Resumed: e.Resumed,
		})
	case crew.ActionSessionStopAsked:
		s.command(StopSession{IssueID: e.IssueID, Action: e.Action})
	case crew.ActionLookupAsked:
		s.findPullRequest(h, e.Action)
	case crew.ActionCheckAsked:
		s.runCheck(h, e.Action)
	case crew.ActionCheckStopAsked:
		s.command(StopCheck{IssueID: e.IssueID, Action: e.Action})
	case crew.ActionEnded:
		s.actionEnded(h, e)
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.ActionSessionEnded, crew.ActionCheckEnded,
		crew.ActionLookupDone, crew.ActionFinishing, crew.RunJudged, crew.VerdictMoved, crew.VerdictDropped,
		crew.FailureReported, crew.FailureReportDropped, crew.RunReleased:
		// Nothing to do outside the run.
	}
}

// takeMoved applies the take to the board (KTD4) and reports it on h's pull
// requests.
func (s *step) takeMoved(h *heldIssue, e crew.TakeMoved) {
	s.emit(IssueMoved{At: s.at, IssueID: e.IssueID, IssueRef: e.IssueRef, From: e.From, To: e.To})
	s.m.boardMoved(h.run.Issue(), e.To)
	s.reportPullRequests(h.run.TakeReport(e.To))
}

// workspaceAsked asks for a new workspace for the action, or for the
// reopened workspace of the failed run it resumes (R5).
func (s *step) workspaceAsked(h *heldIssue, e crew.ActionWorkspaceAsked) {
	if w, ok := e.Reopen.Get(); ok {
		s.command(ReopenWorkspace{IssueID: e.IssueID, Action: e.Action, Workspace: w.Name, Branch: w.Branch})
		return
	}
	s.command(CreateWorkspace{Issue: h.run.Issue(), Action: e.Action})
}

// startSession starts the session of h's action named name, with its
// prompt rendered for the issue and, when the action resumed a failed
// run's workspace, the resume paragraph after it (R5).
func (s *step) startSession(h *heldIssue, name crew.ActionName) {
	def := s.m.action(h, name)
	a, _ := h.run.Action(name)
	w, _ := a.Workspace().Get()
	p := h.plumb(name)
	// The run rendered the prompt already, when its take landed: the same
	// template and issue render the same text.
	p.prompt, _ = def.Prompt.Render(h.run.Issue())
	if resume, ok := a.Resume().Get(); ok && w.Resumed {
		p.prompt += "\n\n" + resumeParagraph(resume.Reason, w.Workspace.Branch, w.Log, p.logFromDir)
	}
	s.command(StartSession{
		IssueID: h.id(), Action: name, Dir: p.dir, Prompt: p.prompt, Log: w.Log, Resumed: w.Resumed,
		Agent: def.Agent.Name, Bot: def.Bot.Name,
	})
}

// findPullRequest looks up the pull request h's action named name opened
// since its new workspace was made (KTD3).
func (s *step) findPullRequest(h *heldIssue, name crew.ActionName) {
	a, _ := h.run.Action(name)
	w, _ := a.Workspace().Get()
	s.command(FindPullRequest{IssueID: h.id(), Action: name, Branch: w.Workspace.Branch, Since: w.Since()})
}

// runCheck runs the next check of h's action named name: the first of its
// checks that has not ended.
func (s *step) runCheck(h *heldIssue, name crew.ActionName) {
	def := s.m.action(h, name)
	a, _ := h.run.Action(name)
	w, _ := a.Workspace().Get()
	c := def.Checks[len(a.Checks())]
	p := h.plumb(name)
	issue := h.run.Issue()
	s.command(RunCheck{
		IssueID: issue.ID(), Action: name, Dir: p.dir, Name: c.Name, Command: c.Script, Log: w.Log,
		IssueRef: issue.Ref(), IssueURL: issue.URL(), Branch: w.Workspace.Branch, Bot: def.Bot.Name,
		Prompt: p.prompt, LastMessage: p.lastMessage,
	})
}

// actionEnded credits what the action's session spent to the run's total
// and its identity's (R14, KTD4), records the action run's end and reports
// it.
func (s *step) actionEnded(h *heldIssue, e crew.ActionEnded) {
	m := s.m
	a, _ := h.run.Action(e.Action)
	m.spent = m.spent.Add(a.Spend())
	m.bots.credit(m.bots.identity(m.action(h, e.Action).Bot.Name), a.Spend())
	s.recordEnded(h, e)
	w, _ := e.Workspace.Get()
	s.emit(ActionEnded{
		At: s.at, IssueID: e.IssueID, IssueRef: e.IssueRef, Rule: e.Rule, Action: e.Action,
		Outcome: e.End.Outcome(), Workspace: w.Workspace.Name, Log: w.Log,
	})
}

// judged moves h to its verdict's state, with the failure report when an
// action failed, and reports the run ended with its move pending (R7).
func (s *step) judged(h *heldIssue, e crew.RunJudged) {
	running := s.m.rules[h.rule].Labels.Running
	s.deliver(h, &delivery{purpose: purposeVerdict, call: h.move(running, e.Verdict.To)})
	if report, ok := h.run.FailureReport(); ok {
		s.deliver(h, &delivery{
			purpose: purposeReport, report: report,
			call: Call{Kind: CallReport, IssueID: e.IssueID, IssueRef: e.IssueRef},
		})
	}
	s.reportRun(h)
}

// fact returns the fact o tells the run of its delivery.
func (o outcome) fact(head crew.FactHead) crew.Fact {
	switch o.purpose {
	case purposeTake:
		return crew.TakeSettled{FactHead: head, Landed: o.landed}
	case purposeVerdict:
		if o.landed {
			return crew.VerdictSettled{FactHead: head, Move: crew.VerdictLanded{}}
		}
		return crew.VerdictSettled{FactHead: head, Move: crew.VerdictGivenUp{Reason: o.reason}}
	default:
		return crew.FailureReportSettled{FactHead: head, Landed: o.landed}
	}
}

// release forgets h, keeping its handled entry, which replaces the issue's
// earlier one, when its verdict settled. A rule without actions that ended
// well keeps an earlier entry that ended well too, marked Gone: its move
// took the issue out of the entry's To (#109, R10, KTD6).
func (m *Model) release(h *heldIssue) {
	m.issues = slices.DeleteFunc(m.issues, func(x *heldIssue) bool { return x == h })
	released, _ := h.run.Phase().(crew.ReleasedPhase)
	verdict, ok := released.Verdict.Get()
	if !ok {
		return
	}
	view := handledView(h.run, verdict)
	i := slices.IndexFunc(m.handled, func(e handledEntry) bool { return e.view.Issue.ID() == h.id() })
	if i >= 0 {
		old := m.handled[i].view
		if len(m.rules[h.rule].Actions) == 0 && !view.NeedsAttention() && !old.NeedsAttention() {
			m.handled[i].view.Gone = true
			return
		}
		view.Earlier = old.Spend().Add(old.Earlier)
		m.handled = slices.Delete(m.handled, i, i+1)
	}
	m.handled = append(m.handled, handledEntry{view: view, landed: h.landed})
}

// handledView returns the handled entry of run, released with verdict.
func handledView(run crew.RuleRun, verdict crew.SettledVerdict) HandledView {
	view := HandledView{
		Issue: run.Issue(), Rule: run.Rule(), To: verdict.Verdict.To, Failures: verdict.Verdict.Failures,
		Move: crew.MoveDone, Taken: run.Taken(), Ended: verdict.Judged,
	}
	if givenUp, ok := verdict.Move.(crew.VerdictGivenUp); ok {
		view.Move, view.DropReason = crew.MoveDropped, givenUp.Reason
	}
	for _, a := range run.Actions() {
		view.Actions = append(view.Actions, HandledAction{Name: a.Name(), Spend: a.Spend(), PullRequest: a.PullRequest()})
	}
	return view
}
