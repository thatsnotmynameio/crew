package core

import (
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// heldRun is a rule run the core holds, from its take until it is
// released: the run, which decides its own changes (KTD2), and what only
// this crew process needs to drive it.
type heldRun struct {
	run  crew.RuleRun
	rule int // index into Model.rules
	// live holds, by action, the session plumbing of each action run that
	// got some (KTD-P5).
	live map[crew.ActionName]*plumbing
	// landed is the listing generation when the verdict move landed or was
	// given up (KTD4).
	landed int
}

// plumbing is what one live action run's session and checks need that is
// neither run state nor in any run event (KTD-P5): the workspace's
// directory and the log's path from it, the rendered prompt, the session's
// last message and what it last said.
type plumbing struct {
	dir         string
	logFromDir  string
	prompt      string
	lastMessage string
	said        crew.Said
}

// findRun returns the held run identified by id, or nil.
func (m *Model) findRun(id crew.RuleRunID) *heldRun {
	for _, h := range m.issues {
		if h.run.ID() == id {
			return h
		}
	}
	return nil
}

// held returns the held run of the issue identified by id, or nil.
func (m *Model) held(id crew.IssueID) *heldRun {
	for _, h := range m.issues {
		if h.id() == id {
			return h
		}
	}
	return nil
}

// plumb returns the plumbing of h's action named name, made on first use.
func (h *heldRun) plumb(name crew.ActionName) *plumbing {
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
func (h *heldRun) said(name crew.ActionName, text crew.Said) {
	a, ok := h.run.Action(name)
	if !ok {
		return
	}
	if _, running := a.State().(crew.InSession); running {
		h.plumb(name).said = text
	}
}

// sayings returns what each of h's sessions last said, by action.
func (h *heldRun) sayings() map[crew.ActionName]crew.Said {
	out := make(map[crew.ActionName]crew.Said, len(h.live))
	for name, p := range h.live {
		out[name] = p.said
	}
	return out
}

// id returns the id of h's issue.
func (h *heldRun) id() crew.IssueID { return h.run.Issue().ID() }

// running reports whether h's run runs its actions and no stop reached it.
func (h *heldRun) running() bool {
	_, running := h.run.Phase().(crew.RunningPhase)
	return running && !h.run.Stopping()
}

// move returns the move of h's issue from one state to another, as a Call.
func (h *heldRun) move(from, to crew.State) Call {
	issue := h.run.Issue()
	return Call{Kind: CallMove, IssueID: issue.ID(), IssueRef: issue.Ref(), From: from, To: to}
}

// definition returns what h's run decides by.
func (m *Model) definition(h *heldRun) crew.RunDefinition {
	return crew.RunDefinition{Rule: m.rules[h.rule], FindsPullRequests: m.finding}
}

// runInput hands an input about one action's workspace, session, check or
// pull request to the held rule run it names, as the fact it tells
// (KTD-P4, KTD7). An input naming a run the core does not hold, such as a
// late answer for a released run, changes nothing, even while a newer run
// of the same issue runs the same action.
func (s *step) runInput(in RunInput) {
	h := s.m.findRun(in.ruleRun())
	if h == nil {
		return
	}
	head := s.head(h)
	switch in := in.(type) {
	case WorkspaceReady:
		s.workspaceReady(h, in)
	case WorkspaceFailed:
		s.decide(h, crew.WorkspaceFailed{FactHead: head, Action: in.Action, Reason: in.Reason})
	case WorkspaceGone:
		s.decide(h, crew.WorkspaceGone{FactHead: head, Action: in.Action})
	case SessionStarted:
		s.decide(h, crew.SessionStarted{FactHead: head, Action: in.Action})
	case SessionFailedToStart:
		s.decide(h, crew.SessionFailedToStart{FactHead: head, Action: in.Action, Reason: in.Reason})
	case SessionEnded:
		s.sessionEnded(h, in)
	case CheckEnded:
		s.decide(h, crew.CheckEnded{FactHead: head, Action: in.Action, Passed: in.Passed, Reason: in.Reason})
	case PullRequestFound:
		s.decide(h, crew.PullRequestLookedUp{FactHead: head, Action: in.Action, PullRequest: in.PullRequest})
	}
}

// workspaceReady hands h's run the ready workspace and, once the run took
// it, keeps the workspace's directory and the log's path from it, which the
// action's session and checks need (KTD-P5).
func (s *step) workspaceReady(h *heldRun, in WorkspaceReady) {
	events, ok := s.decisions(h, crew.WorkspaceReady{
		FactHead: s.head(h), Action: in.Action,
		Workspace: crew.Workspace{Name: in.Workspace, Branch: in.Branch}, Log: in.Log, Resumed: in.Resumed,
	})
	if !ok {
		return
	}
	p := h.plumb(in.Action)
	p.dir, p.logFromDir = in.Dir, in.LogFromDir
	s.apply(h, events)
}

// sessionEnded hands h's run the session's end and, once the run took it,
// keeps the session's last message, which the action's checks read
// (KTD-P5).
func (s *step) sessionEnded(h *heldRun, in SessionEnded) {
	events, ok := s.decisions(h, crew.SessionEnded{
		FactHead: s.head(h), Action: in.Action, Outcome: in.Outcome, Usage: in.Usage,
	})
	if !ok {
		return
	}
	h.plumb(in.Action).lastMessage = in.LastMessage
	s.apply(h, events)
}

// settled returns the fact that tells h's run how one of its deliveries,
// of purpose p, settled: it landed, or crew gave it up for reason.
func (s *step) settled(h *heldRun, p purpose, landed bool, reason string) crew.Fact {
	head := s.head(h)
	switch p {
	case purposeTake:
		return crew.TakeSettled{FactHead: head, Landed: landed}
	case purposeVerdict:
		if landed {
			return crew.VerdictSettled{FactHead: head, Move: crew.VerdictLanded{}}
		}
		return crew.VerdictSettled{FactHead: head, Move: crew.VerdictGivenUp{Reason: reason}}
	default:
		return crew.FailureReportSettled{FactHead: head, Landed: landed}
	}
}

// head returns the head of a fact of h's run, at the input's time.
func (s *step) head(h *heldRun) crew.FactHead {
	return crew.FactHead{Run: h.run.ID(), At: s.at}
}

// decide hands fact to h's run and applies the events it decides. A fact
// the run refuses changes nothing, as an input that answers nothing the
// core waits for.
func (s *step) decide(h *heldRun, fact crew.Fact) {
	if events, ok := s.decisions(h, fact); ok {
		s.apply(h, events)
	}
}

// decisions returns the events h's run decides on fact, and false when the
// run refuses it.
func (s *step) decisions(h *heldRun, fact crew.Fact) ([]crew.RunEvent, bool) {
	events, err := crew.Decide(h.run, s.m.definition(h), fact)
	return events, err == nil
}

// apply applies events to h's run, in order, and after each records it,
// then issues the commands it calls for and publishes it when the views
// word it (KTD-P6).
func (s *step) apply(h *heldRun, events []crew.RunEvent) {
	for _, e := range events {
		run, err := crew.Apply(h.run, e)
		if err != nil {
			// Decide returns only events of h's run.
			return
		}
		h.run = run
		s.record(e)
		s.on(h, e)
	}
}

// on issues the commands e calls for, once applied to h's run, and
// publishes e when the views word it: a landed move or a posted failure
// report. A run event about one action goes to onAction.
func (s *step) on(h *heldRun, e crew.RunEvent) {
	switch e := e.(type) {
	case crew.TakeMoved:
		s.takeMoved(h, e)
	case crew.RunJudged:
		s.judged(h, e)
	case crew.VerdictMoved:
		s.emit(e)
		s.m.boardMoved(h.run.Issue(), e.To)
		s.reportRun(h)
		s.reportVerdict(h)
		h.landed = s.m.listings
	case crew.VerdictDropped:
		s.reportRun(h)
		h.landed = s.m.listings
	case crew.FailureReported:
		s.emit(e)
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

// onAction issues the commands e, an event about one action of h's run,
// calls for, and publishes e when the views word it: a missing workspace, a
// started session or an ended action.
func (s *step) onAction(h *heldRun, e crew.RunEvent) {
	switch e := e.(type) {
	case crew.ActionWorkspaceAsked:
		s.workspaceAsked(h, e)
	case crew.WorkspaceMissing:
		s.emit(e)
	case crew.ActionSessionAsked:
		s.startSession(h, e.Action)
	case crew.ActionSessionStarted:
		s.emit(e)
	case crew.ActionSessionStopAsked:
		s.command(StopSession{IssueID: e.IssueID, Run: e.Run, Action: e.Action})
	case crew.ActionLookupAsked:
		s.findPullRequest(h, e.Action)
	case crew.ActionCheckAsked:
		s.runCheck(h, e.Action)
	case crew.ActionCheckStopAsked:
		s.command(StopCheck{IssueID: e.IssueID, Run: e.Run, Action: e.Action})
	case crew.ActionEnded:
		s.actionEnded(h, e)
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.ActionOpened, crew.ActionSessionEnded,
		crew.ActionCheckEnded, crew.ActionLookupDone, crew.ActionFinishing, crew.RunJudged, crew.VerdictMoved,
		crew.VerdictDropped, crew.FailureReported, crew.FailureReportDropped, crew.RunReleased:
		// Nothing to do outside the run.
	}
}

// takeMoved publishes the take, applies it to the board (KTD4) and reports
// it on h's pull requests.
func (s *step) takeMoved(h *heldRun, e crew.TakeMoved) {
	s.emit(e)
	s.m.boardMoved(h.run.Issue(), e.To)
	s.reportPullRequests(h.run.TakeReport(e.To))
}

// workspaceAsked asks for a new workspace for the action, or for the
// reopened workspace of the failed run it resumes (R5).
func (s *step) workspaceAsked(h *heldRun, e crew.ActionWorkspaceAsked) {
	if w, ok := e.Reopen.Get(); ok {
		s.command(ReopenWorkspace{
			IssueID: e.IssueID, Run: e.Run, Action: e.Action, Workspace: w.Name, Branch: w.Branch,
		})
		return
	}
	s.command(CreateWorkspace{Issue: h.run.Issue(), Run: e.Run, Action: e.Action})
}

// startSession starts the session of h's action named name, with its
// prompt rendered for the issue and, when the action resumed a failed
// run's workspace, the resume paragraph after it (R5).
func (s *step) startSession(h *heldRun, name crew.ActionName) {
	def := s.m.rules[h.rule].Action(name)
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
		IssueID: h.id(), Run: h.run.ID(), Action: name, Dir: p.dir, Prompt: p.prompt, Log: w.Log, Resumed: w.Resumed,
		Agent: def.Agent.Name, Bot: def.Bot.Name,
	})
}

// findPullRequest looks up the pull request h's action named name opened
// since its new workspace was made (KTD3).
func (s *step) findPullRequest(h *heldRun, name crew.ActionName) {
	a, _ := h.run.Action(name)
	w, _ := a.Workspace().Get()
	s.command(FindPullRequest{
		IssueID: h.id(), Run: h.run.ID(), Action: name, Branch: w.Workspace.Branch, Since: w.Since(),
	})
}

// runCheck runs the next check of h's action named name: the first of its
// checks that has not ended.
func (s *step) runCheck(h *heldRun, name crew.ActionName) {
	def := s.m.rules[h.rule].Action(name)
	a, _ := h.run.Action(name)
	w, _ := a.Workspace().Get()
	c := def.Checks[len(a.Checks())]
	p := h.plumb(name)
	issue := h.run.Issue()
	s.command(RunCheck{
		IssueID: issue.ID(), Run: h.run.ID(), Action: name, Dir: p.dir, Name: c.Name, Command: c.Script, Log: w.Log,
		IssueRef: issue.Ref(), IssueURL: issue.URL(), Branch: w.Workspace.Branch, Bot: def.Bot.Name,
		Prompt: p.prompt, LastMessage: p.lastMessage,
	})
}

// actionEnded credits what the action's session spent to the run's total
// and its identity's (R14, KTD4) and publishes the action run's end.
func (s *step) actionEnded(h *heldRun, e crew.ActionEnded) {
	m := s.m
	a, _ := h.run.Action(e.Action)
	m.spent = m.spent.Add(a.Spend())
	m.bots.credit(m.bots.identity(m.rules[h.rule].Action(e.Action).Bot.Name), a.Spend())
	s.emit(e)
}

// judged moves h to its verdict's state, with the failure report when an
// action failed, and reports the run ended with its move pending (R7).
func (s *step) judged(h *heldRun, e crew.RunJudged) {
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

// release forgets h, keeping its handled entry when its verdict settled
// (handle).
func (m *Model) release(h *heldRun) {
	m.issues = slices.DeleteFunc(m.issues, func(x *heldRun) bool { return x == h })
	m.handle(h)
}
