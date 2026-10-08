package core

import (
	"time"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// ReadQuestion asks the tracker for every comment on the issue of the rule
// run Run, for the delegation step at index Step of its route, which finds
// the issue's open question in them (KTD8). Its result is QuestionRead,
// carrying Run and Step.
type ReadQuestion struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Step    int
}

// Delegate asks the tracker to post Delegation on its issue (KTD9). Its
// result is a CallResult carrying ID.
type Delegate struct {
	ID         CallID
	Delegation crew.Delegation
}

// QuestionRead is a ReadQuestion that ended: every comment on the issue,
// oldest first, or Failed when the tracker could not list them. The comments reach only the search for the open
// question, never a run event, the journal, a comment or the status.
type QuestionRead struct {
	At       time.Time
	IssueID  crew.IssueID
	Run      crew.RuleRunID
	Step     int
	Comments []crew.Comment
	Failed   bool
}

func (ReadQuestion) command() {}
func (Delegate) command()     {}

func (ReadQuestion) runCommand() {}
func (Delegate) trackerCommand() {}

// Stamped implements Input.
func (i QuestionRead) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

func (i QuestionRead) arrival() time.Time      { return i.At }
func (i QuestionRead) ruleRun() crew.RuleRunID { return i.Run }

// Delegating gives the model the answerer, the login of the person or App
// every delegation mentions (KTD11).
func Delegating(answerer string) Option {
	return func(m *Model) { m.answerer = answerer }
}

// questionRead delegates the open question of the issue of h's run, which
// the delegation step in.Step asked crew to read: a delegation to the
// answerer naming the question crew.OpenQuestion found among in's
// comments, written as one of crew's writers, or saying none was found or
// the read failed. The outbox delivers it in the run's lane (KTD8, KTD9).
// A read the run no longer waits for, as its step is not the one in
// flight or already delivers, changes nothing.
func (s *step) questionRead(h *heldRun, in QuestionRead) {
	p, routing := h.run.Phase().(crew.RoutingPhase)
	i, asked := p.InFlight()
	if !routing || !asked || i != in.Step || p.Steps[i].Kind != crew.StepDelegate || s.m.outbox.runs[h.id()] != nil {
		return
	}
	issue := h.run.Issue()
	d := crew.Delegation{IssueID: issue.ID(), IssueRef: issue.Ref(), Answerer: s.m.answerer, Search: crew.QuestionUnread}
	if !in.Failed {
		d.Search = crew.QuestionNotFound
		if q, ok := crew.OpenQuestion(in.Comments, s.m.bots.writers()); ok {
			d.Search, d.ID, d.Rule = crew.QuestionFound, q.ID, q.Rule
		}
	}
	s.deliver(h, &delivery{
		purpose: purposeStep, step: i, delegation: d,
		call: Call{Kind: CallDelegate, IssueID: issue.ID(), IssueRef: issue.Ref()},
	})
}
