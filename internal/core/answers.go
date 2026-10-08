package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// answers is what crew read of the answers to the open questions at the
// action whose session starts (KTD-W6). The held run keeps it, from the
// ReadAnswers to the next session it asks for: it is in no run event, so
// no answer reaches the journal, a comment or the status.
type answers struct {
	// action is the action whose session the answers are for.
	action crew.ActionName
	// read reports whether the read ended, failed or not.
	read bool
	// failed reports that the tracker could not list the comments, and
	// reason why.
	failed bool
	reason crew.SessionText
	// answered is what crew.Answers found in the comments.
	answered crew.Answered
}

// sessionAsked starts the session of h's action named name, once crew read
// the answers to the open questions at that action, when the run has any:
// it asks for the issue's comments first, and the session starts on their
// answers (KTD-W6). Without open questions, it starts at once.
func (s *step) sessionAsked(h *heldRun, name crew.ActionName) {
	h.answers = nil
	if len(h.run.Questions(name)) == 0 {
		s.startSession(h, name)
		return
	}
	h.answers = &answers{action: name}
	s.command(ReadAnswers{IssueID: h.id(), Run: h.run.ID(), Action: name})
}

// answersRead keeps the answers in, which h's run read for the session of
// in's action, and starts that session. A read the run no longer waits
// for, as its action's session no longer starts or started already,
// changes nothing.
func (s *step) answersRead(h *heldRun, in AnswersRead) {
	a := h.answers
	if a == nil || a.read || a.action != in.Action {
		return
	}
	at, ok := h.run.Cursor()
	if !ok || at.Name() != in.Action {
		return
	}
	if _, starting := at.State().(crew.StartingSession); !starting {
		return
	}
	a.read, a.failed, a.reason = true, in.Failed, in.Reason
	if !in.Failed {
		a.answered = crew.Answers(in.Comments, h.run.Questions(in.Action), s.m.bots.writers(), s.m.answerers)
	}
	s.startSession(h, in.Action)
}

// resumeParagraphs returns what crew appends to the prompt of the session
// of h's action named name before its verdict paragraph, and false when
// nothing (R23, R48, KTD-W10). After a failed read, it is the paragraph
// that says so; once a read found the question, the answers paragraph;
// either after the worktree's guidance when the session resumes, which
// resumed reports, in the worktree of the run start resumes. Otherwise it
// is the resume paragraph when the session resumes.
func (m *Model) resumeParagraphs(h *heldRun, name crew.ActionName, start crew.StartAt, resumed bool) (string, bool) {
	w, _ := h.run.Workspace().Get()
	var continues string
	if resumed {
		continues = continuesParagraph(w.Workspace.Branch, "", w.Log, h.logFromDir) + "\n\n"
	}
	issue := h.run.Issue().ID().Key
	if a := h.answers; a != nil && a.read && a.action == name {
		switch {
		case a.failed:
			return continues + failedReadParagraph(m.unread(h, name), a.reason), true
		case a.answered.Found:
			return continues + answersParagraph(issue, a.answered), true
		}
	}
	if resumed {
		return resumeParagraph(start, w.Workspace.Branch, w.Log, h.logFromDir), true
	}
	return "", false
}

// unread returns what the read command of a session whose answers crew
// could not read reads: the open questions of h's run at its action named
// name, and who may answer them, the Apps that asked one left out, crew's
// writers among them for a rule's question (R48, KTD5).
func (m *Model) unread(h *heldRun, name crew.ActionName) reader {
	asks, logins := m.askedAt(h.run.Questions(name))
	return reader{
		issue: h.run.Issue().ID().Key, questions: asks, owners: m.answerers.CodeOwners, apps: m.appsExcept(logins...),
	}
}
