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
// question, which one, the answers that count, newest first, and how many
// older ones it left out to keep within AnswersCap.
type Answered struct {
	Found   bool
	Answers []Answer
	LeftOut int
	// RuleQuestion is the id of the rule's question found; empty when the
	// question found is a session's.
	RuleQuestion QuestionID
}

// Answers returns the answers to the open questions among comments, which
// are oldest first, with writers, the logins crew posts as, and who may
// answer (R37, R39, R43, R44, R45, R47, KTD-W9, KTD5). The question is the
// latest comment that asks one of questions (askedIn). An answer is a
// comment after it that answerOf keeps, the Apps that asked any of
// questions left out: each session question's login, and crew's writers
// when one of questions is a rule's. Logins compare ignoring case, and an
// empty one matches nothing. The answers go newest first, whole, until the
// next one would take the quoted answers past AnswersCap: that one and
// every older one are left out and counted.
func Answers(comments []Comment, questions []Question, writers []string, who Answerers) Answered {
	asked, found := -1, Question{}
	for i, c := range slices.Backward(comments) {
		if q, ok := askedIn(c, questions, writers); ok {
			asked, found = i, q
			break
		}
	}
	if asked < 0 {
		return Answered{}
	}
	askers := askingLogins(questions, writers)
	var counted []Answer
	for _, c := range comments[asked+1:] {
		if a, ok := answerOf(c, who, askers); ok {
			counted = append(counted, a)
		}
	}
	slices.Reverse(counted)
	out, size := Answered{Found: true, RuleQuestion: found.ID}, 0
	for i, a := range counted {
		if size += len(a.Quoted()); size > AnswersCap {
			out.LeftOut = len(counted) - i
			break
		}
		out.Answers = append(out.Answers, a)
	}
	return out
}

// askedIn returns the one of questions c asks, and whether it asks one. A
// session's question is asked by a comment that holds its marker, written
// by its login, that does not hold crew's own marker. A rule's is asked by
// a comment one of writers posted through crew (postedBy) whose question
// marker names its id and its rule.
func askedIn(c Comment, questions []Question, writers []string) (Question, bool) {
	posted, marked := FindQuestionMarker(c.Body)
	byCrew := postedBy(c, writers)
	i := slices.IndexFunc(questions, func(q Question) bool {
		if q.ID != "" {
			return byCrew && marked && posted.ID == q.ID && posted.Rule == q.Rule
		}
		return q.Login != "" && strings.EqualFold(c.Author, q.Login) && !HoldsPostedMarker(c.Body) &&
			HoldsSessionMarker(c.Body, q.Run, q.Action)
	})
	if i < 0 {
		return Question{}, false
	}
	return questions[i], true
}

// askingLogins returns the logins that asked one of questions, which never
// answer as Apps: each session question's login, and writers when one of
// questions is a rule's, which crew's writers asked (KTD5).
func askingLogins(questions []Question, writers []string) []string {
	var logins []string
	for _, q := range questions {
		logins = append(logins, q.Login)
	}
	if slices.ContainsFunc(questions, func(q Question) bool { return q.ID != "" }) {
		logins = append(logins, writers...)
	}
	return logins
}

// answerOf returns c, a comment after the question, as an answer, and
// whether it counts as one: the one answer rule of a session's answers and
// of the return's check (KTD5). Its body is stripped of control characters
// (StripControlsKeepingLines), then of the answer markers that parse
// (StripAnswerMarkers). It counts when that body holds none of crew's
// markers, also none that stripping revealed, and a code owner who is not
// an App wrote it, or an App on the answering list that is none of asked,
// the logins that asked.
func answerOf(c Comment, who Answerers, asked []string) (Answer, bool) {
	a := Answer{Author: c.Author, Body: StripAnswerMarkers(StripControlsKeepingLines(c.Body))}
	if HoldsMarker(a.Body) || c.Author == "" {
		return Answer{}, false
	}
	if !c.App {
		return a, containsFold(who.CodeOwners, c.Author)
	}
	return a, containsFold(who.Apps, c.Author) && !containsFold(asked, c.Author)
}

// containsFold reports whether logins holds login, ignoring case.
func containsFold(logins []string, login string) bool {
	return slices.ContainsFunc(logins, func(l string) bool { return strings.EqualFold(l, login) })
}
