package core

import (
	"time"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// ReadReturn asks the tracker for every comment on the issue of the rule
// run Run, for its action Action, the answered rule's check, which finds
// in them where the issue returns (KTD2). Its result is ReturnRead,
// carrying Run and Action.
type ReadReturn struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
}

// ReturnRead is a ReadReturn that ended: every comment on the issue,
// oldest first, or Failed when the tracker could not list them. The
// comments reach only the check, never a run event, the journal, a comment
// or the status (KTD2).
type ReturnRead struct {
	At       time.Time
	IssueID  crew.IssueID
	Run      crew.RuleRunID
	Action   crew.ActionName
	Comments []crew.Comment
	Failed   bool
}

func (ReadReturn) command()    {}
func (ReadReturn) runCommand() {}

// Stamped implements Input.
func (i ReturnRead) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

func (i ReturnRead) arrival() time.Time      { return i.At }
func (i ReturnRead) ruleRun() crew.RuleRunID { return i.Run }

// returnRead ends the answered rule's check of h's run with what
// crew.CheckReturn finds in in's comments: the questions the rules declare,
// asked by one of crew's writers, and an answer that counts after the
// question (KTD3, KTD5). Only that label or the verdict reaches the run
// (KTD2). A read the run no longer waits for, as its check is not at its
// cursor or ended already, changes nothing: the run refuses the fact.
func (s *step) returnRead(h *heldRun, in ReturnRead) {
	check := crew.CheckReturn(
		in.Comments, in.Failed, crew.DeclaredQuestions(s.m.rules), s.m.bots.writers(), s.m.answerers,
	)
	s.decide(h, crew.ReturnChecked{FactHead: s.head(h), Action: in.Action, Check: check})
}
