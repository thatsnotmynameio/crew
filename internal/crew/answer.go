package crew

import (
	"slices"
	"strings"
)

// Answerers are who may answer a question a session asks on its issue: the
// code owners, and the Apps on the answering list. Collaborators do not
// answer, and neither does any other App (R37, R38).
type Answerers struct {
	// CodeOwners are the code owners' logins, those sessions get as
	// CREW_CODE_OWNERS.
	CodeOwners []string
	// Apps are the logins of the Apps that answer, each <slug>[bot]: the
	// config's answering_apps, or crew's own bots without it. It never
	// holds github-actions[bot], which can post anyone's text (R39).
	Apps []string
}

// AnswersCap is the most bytes the answers a session's prompt carries take,
// each counted as quoted (Answer.Quoted): the prompt reaches the harness
// as one command-line argument (R47).
const AnswersCap = 32 << 10

// Answer is one comment that answers an open question: the login of its
// author and its body, stripped by StripControlsKeepingLines.
type Answer struct {
	Author string
	Body   string
}

// Quoted returns the answer as a session's prompt carries it, on lines of
// its own: its body between a marker that names its author and an end
// marker. An answer holds no marker (MarkerPrefix), so no text in its body
// can end the quote or pose as crew's words.
func (a Answer) Quoted() string {
	return MarkerPrefix + "answer by " + a.Author + " -->\n" + a.Body + "\n" + MarkerPrefix + "answer end -->\n"
}

// Answered is what Answers found on an issue: whether it found the
// question, the answers that count, newest first, and how many older ones
// it left out to keep within AnswersCap.
type Answered struct {
	Found   bool
	Answers []Answer
	LeftOut int
}

// Answers returns the answers to the open questions among comments, which
// are oldest first, with who may answer (R37, R39, R43, R44, R45, R47,
// KTD-W9). The question is the latest comment that holds the marker of one
// of questions, by that question's login, and not crew's own marker. An
// answer is a comment after it that holds none of crew's markers, by a
// code owner who is not an App, or by an App on the answering list that
// asked none of questions. Logins compare ignoring case, and an empty one
// matches nothing. The answers go newest first, whole, until the next one
// would take the quoted answers past AnswersCap: that one and every older
// one are left out and counted.
func Answers(comments []Comment, questions []Question, who Answerers) Answered {
	asked := -1
	for i, c := range slices.Backward(comments) {
		if isQuestion(c, questions) {
			asked = i
			break
		}
	}
	if asked < 0 {
		return Answered{}
	}
	var counted []Answer
	for _, c := range comments[asked+1:] {
		c.Body = StripControlsKeepingLines(c.Body)
		if answers(c, questions, who) {
			counted = append(counted, Answer{Author: c.Author, Body: c.Body})
		}
	}
	slices.Reverse(counted)
	out, size := Answered{Found: true}, 0
	for i, a := range counted {
		if size += len(a.Quoted()); size > AnswersCap {
			out.LeftOut = len(counted) - i
			break
		}
		out.Answers = append(out.Answers, a)
	}
	return out
}

// isQuestion reports whether c asks one of questions: it holds that
// question's marker, its author is that question's login, and it does not
// hold crew's own marker.
func isQuestion(c Comment, questions []Question) bool {
	if HoldsPostedMarker(c.Body) {
		return false
	}
	return slices.ContainsFunc(questions, func(q Question) bool {
		return q.Login != "" && strings.EqualFold(c.Author, q.Login) && HoldsSessionMarker(c.Body, q.Run, q.Action)
	})
}

// answers reports whether c, a comment after the question whose body is
// already stripped, counts as an answer: it holds none of crew's markers,
// also none that stripping its control characters revealed, and a code
// owner who is not an App, or an App on the answering list that asked none
// of questions, wrote it.
func answers(c Comment, questions []Question, who Answerers) bool {
	if HoldsMarker(c.Body) || c.Author == "" {
		return false
	}
	if !c.App {
		return containsFold(who.CodeOwners, c.Author)
	}
	return containsFold(who.Apps, c.Author) && !slices.ContainsFunc(questions, func(q Question) bool {
		return strings.EqualFold(q.Login, c.Author)
	})
}

// containsFold reports whether logins holds login, ignoring case.
func containsFold(logins []string, login string) bool {
	return slices.ContainsFunc(logins, func(l string) bool { return strings.EqualFold(l, login) })
}
