package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// onRoute issues the commands e, an event about the route of h's run,
// calls for, and publishes e when the views word it: the route chosen, and
// a step that settled as a RouteStepEnded (KTD9).
func (s *step) onRoute(h *heldRun, e crew.RunEvent) {
	switch e := e.(type) {
	case crew.RouteChosen:
		s.emit(e)
		s.reportRun(h)
	case crew.RunLookupAsked:
		s.findPullRequest(h)
	case crew.StepAsked:
		s.askStep(h, e.Step)
	case crew.StepShellStopAsked:
		s.command(StopStepShell{IssueID: e.IssueID, Run: e.Run, Step: e.Step})
	case crew.StepEnded:
		s.stepEnded(h, e)
	case crew.RunTaken, crew.TakeMoved, crew.RunStopped, crew.RunOutOfTime, crew.WorkspaceAsked,
		crew.WorkspaceMissing, crew.WorkspaceOpened, crew.ActionSessionAsked, crew.ActionSessionStarted,
		crew.ActionSessionStopAsked, crew.ActionSessionEnded, crew.ActionShellAsked, crew.ActionShellStopAsked,
		crew.ActionShellEnded, crew.ActionEnded, crew.RunLookupDone, crew.RunReleased:
		// Nothing to do outside the run.
	}
}

// findPullRequest looks up the pull requests of the branch of h's run,
// opened since its new workspace was made (KTD3, KTD-S6).
func (s *step) findPullRequest(h *heldRun) {
	w, _ := h.run.Workspace().Get()
	s.command(FindPullRequest{IssueID: h.id(), Run: h.run.ID(), Branch: w.Workspace.Branch, Since: w.Since()})
}

// askStep runs the step at index i of the route of h's run: a shell step's
// script, acting as the run's bot, or a tracker step, which the outbox
// delivers in the run's lane (KTD9). A move and a close take the issue from
// the rule's running label.
func (s *step) askStep(h *heldRun, i int) {
	p, _ := h.run.Phase().(crew.RoutingPhase)
	rule := s.m.rules[h.rule]
	route, _ := rule.Route(p.Route)
	issue := h.run.Issue()
	d := &delivery{purpose: purposeStep, step: i}
	switch st := route.Steps[i].(type) {
	case crew.MoveStep:
		d.call = h.move(rule.Labels.Running, st.To)
	case crew.CloseStep:
		d.call = Call{Kind: CallClose, IssueID: issue.ID(), IssueRef: issue.Ref(), From: rule.Labels.Running}
	case crew.CommentStep:
		// The run rendered the comment already, when it asked for the step:
		// the same template and run render the same text.
		d.body, _ = st.Template.Render(h.run.CommentData())
		d.call = Call{Kind: CallComment, IssueID: issue.ID(), IssueRef: issue.Ref()}
	case crew.ReportStep:
		// The run chose its route, so it has a report.
		d.report, _ = h.run.FailureReport()
		d.call = Call{Kind: CallReport, IssueID: issue.ID(), IssueRef: issue.Ref()}
	case crew.ShellStep:
		s.command(RunStepShell{
			IssueID: issue.ID(), Run: h.run.ID(), Step: i, Script: h.script(st.Name, st.Shell.Script, h.run.Bot()),
		})
		return
	}
	s.deliver(h, d)
}

// stepEnded publishes the step that settled and reports h's run anew
// (KTD-S17). Once the route's final step settled, it applies a move or
// close that landed to the board (KTD4), reports a landed move or close on
// h's pull requests, and keeps the listing generation from which a listing
// may find the issue gone.
func (s *step) stepEnded(h *heldRun, e crew.StepEnded) {
	p, _ := h.run.Phase().(crew.RoutingPhase)
	var plan crew.StepPlan
	if e.Step < len(p.Steps) {
		plan = p.Steps[e.Step]
	}
	ended := RouteStepEnded{
		At: e.At, IssueID: e.IssueID, IssueRef: e.IssueRef, Rule: e.Rule, Route: p.Route, Step: e.Step, Plan: plan,
		Outcome: e.Outcome,
	}
	if plan.Kind == crew.StepMove || plan.Kind == crew.StepClose {
		ended.From = s.m.rules[h.rule].Labels.Running
	}
	s.emit(ended)
	_, final := p.Final()
	_, landed := e.Outcome.(crew.StepLanded)
	end, _ := p.End()
	if final && landed {
		s.m.boardMoved(h.run.Issue(), end.To)
	}
	s.reportRun(h)
	if !final {
		return
	}
	if landed {
		s.reportEnding(h)
	}
	h.landed = s.m.listings
}

// settled returns the fact that tells h's run how d, one of its
// deliveries, settled: as the tracker answered with result, for reason.
func (s *step) settled(h *heldRun, d *delivery, result Result, reason string) crew.Fact {
	head := s.head(h)
	if d.purpose == purposeTake {
		return crew.TakeSettled{FactHead: head, Landed: result == ResultDone}
	}
	return crew.StepSettled{FactHead: head, Step: d.step, Outcome: stepOutcome(result, reason)}
}

// stepOutcome returns how a tracker step whose last try ended with result
// settled: landed, dropped as the item was closed or moved meanwhile, or
// given up as the tracker refused it or its final try failed (KTD-S8).
func stepOutcome(result Result, reason string) crew.StepOutcome {
	switch result {
	case ResultDone:
		return crew.StepLanded{}
	case ResultMovedMeanwhile:
		return crew.StepDropped{Reason: reason}
	case ResultRefused, ResultFailed:
	}
	return crew.StepGivenUp{Reason: reason}
}
