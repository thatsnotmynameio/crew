package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// actionInput hands an input about one action's workspace, session, check
// or pull request to the rule run of its issue, as the fact it tells
// (KTD-P4). An input for an issue the core does not hold changes nothing.
func (s *step) actionInput(in Input) {
	switch in := in.(type) {
	case WorkspaceReady:
		s.workspaceReady(in)
	case WorkspaceFailed:
		s.tell(in.IssueID, func(head crew.FactHead) crew.Fact {
			return crew.WorkspaceFailed{FactHead: head, Action: in.Action, Reason: in.Reason}
		})
	case WorkspaceGone:
		s.tell(in.IssueID, func(head crew.FactHead) crew.Fact {
			return crew.WorkspaceGone{FactHead: head, Action: in.Action}
		})
	case SessionStarted:
		s.tell(in.IssueID, func(head crew.FactHead) crew.Fact {
			return crew.SessionStarted{FactHead: head, Action: in.Action}
		})
	case SessionFailedToStart:
		s.tell(in.IssueID, func(head crew.FactHead) crew.Fact {
			return crew.SessionFailedToStart{FactHead: head, Action: in.Action, Reason: in.Reason}
		})
	case SessionEnded:
		s.sessionEnded(in)
	case CheckEnded:
		s.tell(in.IssueID, func(head crew.FactHead) crew.Fact {
			return crew.CheckEnded{FactHead: head, Action: in.Action, Passed: in.Passed, Reason: in.Reason}
		})
	case PullRequestFound:
		s.tell(in.IssueID, func(head crew.FactHead) crew.Fact {
			return crew.PullRequestLookedUp{FactHead: head, Action: in.Action, PullRequest: in.PullRequest}
		})
	}
}

// tell hands the run of the issue identified by id the fact that build
// returns, when the core holds the issue.
func (s *step) tell(id crew.IssueID, build func(crew.FactHead) crew.Fact) {
	if h := s.m.held(id); h != nil {
		s.decide(h, build(s.head(h)))
	}
}

// workspaceReady hands the run the ready workspace and, once the run took
// it, keeps the workspace's directory and the log's path from it, which the
// action's session and checks need (KTD-P5).
func (s *step) workspaceReady(in WorkspaceReady) {
	h := s.m.held(in.IssueID)
	if h == nil {
		return
	}
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

// sessionEnded hands the run the session's end and, once the run took it,
// keeps the session's last message, which the action's checks read
// (KTD-P5).
func (s *step) sessionEnded(in SessionEnded) {
	h := s.m.held(in.IssueID)
	if h == nil {
		return
	}
	events, ok := s.decisions(h, crew.SessionEnded{
		FactHead: s.head(h), Action: in.Action, Outcome: in.Outcome, Usage: in.Usage,
	})
	if !ok {
		return
	}
	h.plumb(in.Action).lastMessage = in.LastMessage
	s.apply(h, events)
}
