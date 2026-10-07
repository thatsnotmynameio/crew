package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// runInput hands an input about one action's workspace, session, check or
// pull request to the held rule run it names, as the fact it tells
// (KTD-P4, KTD7). An input naming a run the core does not hold, such as a
// late answer for a released run, changes nothing, even while a newer run
// of the same issue runs the same action.
func (s *step) runInput(in RunInput) {
	h := s.m.heldRun(in.ruleRun())
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
func (s *step) workspaceReady(h *heldIssue, in WorkspaceReady) {
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
func (s *step) sessionEnded(h *heldIssue, in SessionEnded) {
	events, ok := s.decisions(h, crew.SessionEnded{
		FactHead: s.head(h), Action: in.Action, Outcome: in.Outcome, Usage: in.Usage,
	})
	if !ok {
		return
	}
	h.plumb(in.Action).lastMessage = in.LastMessage
	s.apply(h, events)
}
