package core

import (
	"maps"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// heldRun is a rule run the core holds, from its take until it is
// released: the run, which decides its own changes (KTD2), and what only
// this crew process needs to drive it.
type heldRun struct {
	run  crew.RuleRun
	rule int // index into Model.rules
	// dir is the directory of the run's workspace, and logFromDir the
	// log's path from it, once the workspace is ready: what its sessions
	// and scripts need that is in no run event (KTD-P5).
	dir        string
	logFromDir string
	// said holds what each session of the run last said while it ran.
	said map[crew.ActionName]crew.Said
	// answers is what crew read of the answers its next session starts
	// with, when the run has open questions at its action (KTD-W6).
	answers *answers
	// landed is the listing generation when the final move or close of
	// its route settled (KTD4).
	landed int
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

// keepSaid keeps what the session of h's action named name last said,
// while it runs.
func (h *heldRun) keepSaid(name crew.ActionName, text crew.Said) {
	a, ok := h.run.Action(name)
	if !ok {
		return
	}
	if _, running := a.State().(crew.InSession); running {
		if h.said == nil {
			h.said = map[crew.ActionName]crew.Said{}
		}
		h.said[name] = text
	}
}

// sayings returns what each of h's sessions last said, by action.
func (h *heldRun) sayings() map[crew.ActionName]crew.Said {
	return maps.Clone(h.said)
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

// runInput hands an input about a rule run's workspace, an action's
// session, script or function, a route's shell or function step, the
// lookup of its pull requests or a read of its comments to the held rule
// run it names, as the fact it tells (KTD-P4, KTD7). An input naming a run the core does not hold,
// such as a late answer for a released run, changes nothing, even while a
// newer run of the same issue runs the same action.
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
		s.decide(h, crew.WorkspaceFailed{FactHead: head, Reason: in.Reason})
	case WorkspaceGone:
		s.decide(h, crew.WorkspaceGone{FactHead: head})
	case SessionStarted:
		spec, _ := s.m.rules[h.rule].Action(in.Action).Kind.(crew.SessionSpec)
		s.decide(h, crew.SessionStarted{FactHead: head, Action: in.Action, Login: s.m.bots.login(spec.Bot.Name)})
	case SessionFailedToStart:
		s.decide(h, crew.SessionFailedToStart{FactHead: head, Action: in.Action, Reason: in.Reason})
	case SessionEnded:
		s.decide(h, crew.SessionEnded{
			FactHead: head, Action: in.Action, Outcome: in.Outcome, Report: in.Report, Usage: in.Usage,
		})
	case ShellEnded:
		s.decide(h, crew.ShellEnded{FactHead: head, Action: in.Action, Outcome: in.Outcome})
	case StepShellEnded:
		s.decide(h, crew.StepShellEnded{FactHead: head, Step: in.Step, Outcome: in.Outcome})
	case FunctionEnded:
		s.decide(h, crew.FunctionEnded{FactHead: head, Action: in.Action, Outcome: in.Outcome})
	case StepFunctionEnded:
		s.decide(h, crew.StepFunctionEnded{FactHead: head, Step: in.Step, Outcome: in.Outcome})
	case PullRequestFound:
		s.decide(h, crew.PullRequestLookedUp{FactHead: head, PullRequest: in.PullRequest})
	case AnswersRead:
		s.answersRead(h, in)
	case QuestionRead:
		s.questionRead(h, in)
	}
}

// workspaceReady hands h's run its ready workspace and, once the run took
// it, keeps the workspace's directory and the log's path from it, which
// its sessions and scripts need (KTD-P5).
func (s *step) workspaceReady(h *heldRun, in WorkspaceReady) {
	events, ok := s.decisions(h, crew.WorkspaceReady{
		FactHead:  s.head(h),
		Workspace: crew.Workspace{Name: in.Workspace, Branch: in.Branch}, Log: in.Log, Resumed: in.Resumed,
	})
	if !ok {
		return
	}
	h.dir, h.logFromDir = in.Dir, in.LogFromDir
	s.apply(h, events)
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
// publishes e when the views word it: the take that landed, or a missing
// or ready workspace. A run event about one action goes to onAction, and
// one about the route to onRoute.
func (s *step) on(h *heldRun, e crew.RunEvent) {
	switch e := e.(type) {
	case crew.TakeMoved:
		s.takeMoved(h, e)
	case crew.WorkspaceAsked:
		s.workspaceAsked(h, e)
	case crew.WorkspaceMissing, crew.WorkspaceOpened:
		s.emit(e)
	case crew.RunReleased:
		s.m.release(h)
		s.freed()
	case crew.RunTaken, crew.RunStopped, crew.RunOutOfTime:
		// Nothing to do outside the run.
	case crew.ActionSessionAsked, crew.ActionSessionStarted, crew.ActionSessionStopAsked, crew.ActionSessionEnded,
		crew.ActionShellAsked, crew.ActionShellStopAsked, crew.ActionShellEnded, crew.ActionFunctionAsked,
		crew.ActionFunctionStopAsked, crew.ActionFunctionEnded, crew.ActionEnded:
		s.onAction(h, e)
	case crew.RouteChosen, crew.RunLookupAsked, crew.RunLookupDone, crew.StepAsked, crew.StepShellStopAsked,
		crew.StepFunctionStopAsked, crew.StepEnded:
		s.onRoute(h, e)
	}
}

// onAction issues the commands e, an event about the action at the cursor
// of h's run, calls for, and publishes e when the views word it: a started
// session, script or function, or an ended action.
func (s *step) onAction(h *heldRun, e crew.RunEvent) {
	switch e := e.(type) {
	case crew.ActionSessionAsked:
		s.sessionAsked(h, e.Action)
	case crew.ActionSessionStarted:
		s.emit(e)
	case crew.ActionSessionStopAsked:
		s.command(StopSession{IssueID: e.IssueID, Run: e.Run, Action: e.Action})
	case crew.ActionShellAsked:
		s.emit(e)
		s.runShell(h, e)
	case crew.ActionShellStopAsked:
		s.command(StopShell{IssueID: e.IssueID, Run: e.Run, Action: e.Action})
	case crew.ActionFunctionAsked:
		s.emit(e)
		s.runFunction(h, e)
	case crew.ActionFunctionStopAsked:
		s.command(StopFunction{IssueID: e.IssueID, Run: e.Run, Action: e.Action})
	case crew.ActionEnded:
		s.actionEnded(h, e)
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.RunOutOfTime, crew.WorkspaceAsked,
		crew.WorkspaceMissing, crew.WorkspaceOpened, crew.ActionSessionEnded, crew.ActionShellEnded,
		crew.ActionFunctionEnded, crew.RouteChosen, crew.RunLookupAsked, crew.RunLookupDone, crew.StepAsked,
		crew.StepShellStopAsked, crew.StepFunctionStopAsked, crew.StepEnded, crew.RunReleased:
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

// workspaceAsked asks for the run's one new workspace, or for the reopened
// workspace of the run it continues (R5, KTD6).
func (s *step) workspaceAsked(h *heldRun, e crew.WorkspaceAsked) {
	if w, ok := e.Reopen.Get(); ok {
		s.command(ReopenWorkspace{IssueID: e.IssueID, Run: e.Run, Workspace: w.Name, Branch: w.Branch})
		return
	}
	s.command(CreateWorkspace{Issue: h.run.Issue(), Run: e.Run, Rule: e.Rule})
}

// startSession starts the session of h's action named name, with its
// prompt rendered for the issue, then crew's paragraphs: the answers
// paragraph, or the one that says crew could not read them, when the run
// read the answers to its open questions at the action, else the resume
// paragraph when the action is where the run resumes the work of the run
// it continues, in that run's reopened workspace (R23, R48), the verdict
// paragraph when the action's on: names verdicts (R9), and the waiting
// paragraph when the session may wait for an answer (R19, KTD-W10).
func (s *step) startSession(h *heldRun, name crew.ActionName) {
	def := s.m.rules[h.rule].Action(name)
	spec, _ := def.Kind.(crew.SessionSpec)
	w, _ := h.run.Workspace().Get()
	// The run rendered the prompt already, when it asked for the session:
	// the same template and issue render the same text.
	prompt, _ := spec.Prompt.Render(h.run.Issue())
	start, resumed := h.run.Start().(crew.StartAt)
	resumed = resumed && w.Resumed && start.Action == name
	if p, ok := s.m.resumeParagraphs(h, name, start, resumed); ok {
		prompt += "\n\n" + p
	}
	if verdicts, ok := verdictParagraph(def.On); ok {
		prompt += "\n\n" + verdicts
	}
	if def.MayWait() {
		prompt += "\n\n" + waitingParagraph(s.m.waitingOf(h, name, spec))
	}
	s.command(StartSession{
		IssueID: h.id(), Run: h.run.ID(), Action: name, Dir: h.dir, Prompt: prompt, Log: w.Log, Resumed: resumed,
		Agent: spec.Agent.Name, Bot: spec.Bot.Name,
	})
}

// runShell runs the script of the shell action e asked for, acting as the
// bot e names: the run's latest session's.
func (s *step) runShell(h *heldRun, e crew.ActionShellAsked) {
	spec, _ := s.m.rules[h.rule].Action(e.Action).Kind.(crew.ShellSpec)
	s.command(RunShell{IssueID: h.id(), Run: h.run.ID(), Action: e.Action, Script: h.script(e.Action, spec.Script, e.Bot)})
}

// script returns the script of the shell action named name, command, as
// h's run runs it, acting as bot: in its workspace, when it has one, with
// its latest session's name (KTD-S12).
func (h *heldRun) script(name crew.ActionName, command string, bot crew.Bot) Script {
	w, _ := h.run.Workspace().Get()
	latest, _ := h.run.LatestSession().Get()
	issue := h.run.Issue()
	return Script{
		Dir: h.dir, Name: name, Command: command, Log: w.Log, Rule: h.run.Rule(), IssueRef: issue.Ref(),
		IssueURL: issue.URL(), Branch: w.Workspace.Branch, Session: latest.Action, Bot: bot.Name,
	}
}

// actionEnded credits what the action's session spent to the run's total
// and to the identity of the bot it acted as, the run's latest session's
// (R14, KTD4, KTD13), and publishes the action run's end.
func (s *step) actionEnded(h *heldRun, e crew.ActionEnded) {
	m := s.m
	a, _ := h.run.Action(e.Action)
	m.spent = m.spent.Add(a.Spend())
	m.bots.credit(m.bots.identity(h.run.Bot().Name), a.Spend())
	s.emit(e)
}

// release forgets h, keeping its handled entry when its route ended
// (handle).
func (m *Model) release(h *heldRun) {
	m.issues = slices.DeleteFunc(m.issues, func(x *heldRun) bool { return x == h })
	m.handle(h)
}
