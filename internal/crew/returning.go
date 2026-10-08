package crew

import "slices"

// The verdicts the answered rule's check fails with, each naming why the
// item does not return (R10, KTD3).
const (
	// NoQuestion is a check that found no question the config declares, or
	// whose question a later delegation closed.
	NoQuestion Verdict = "no-question"
	// Unanswered is a check that found no answer after the question.
	Unanswered Verdict = "unanswered"
	// Unread is a check that could not read the item's comments.
	Unread Verdict = "unread"
)

// ReturnCheck is what CheckReturn found: the label the item returns to, or
// the verdict that says why it does not.
type ReturnCheck struct {
	// To is the question's return label; empty when the check failed.
	To State
	// Failure is NoQuestion, Unanswered or Unread; empty when the check
	// found To.
	Failure Verdict
}

// DeclaredQuestions returns the questions rules declare, each once, in the
// rules' order: every question a rule asks (asks), by its id, the rule's
// name and its return label. Only these may return an item (KTD3).
func DeclaredQuestions(rules []Rule) []PostedQuestion {
	var out []PostedQuestion
	for _, r := range rules {
		for _, q := range r.asks() {
			if p := (PostedQuestion{ID: q.ID, Rule: r.Name, Return: q.Return}); !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// asks returns the questions r asks: those of its question actions, then
// those of its routes' question steps.
func (r Rule) asks() []Ask {
	var out []Ask
	for _, a := range r.Actions {
		if spec, ok := a.Kind.(QuestionSpec); ok {
			out = append(out, spec.Question)
		}
	}
	for _, route := range r.Routes {
		for _, step := range route.Steps {
			if s, ok := step.(QuestionStep); ok {
				out = append(out, s.Question)
			}
		}
	}
	return out
}

// CheckReturn returns where an item in the answered rule's ready state
// returns to, from its comments, oldest first, unless failed reports that
// crew could not read them (Unread): declared are the questions the config
// declares (DeclaredQuestions), writers the logins crew posts as, and who
// the people and Apps that may answer (R9, R10, KTD3, KTD5). The question
// is the latest comment one of writers posted through crew (postedBy)
// whose question marker names one of declared; another marker is skipped.
// Without one, or when the latest delegation one of writers posted after
// it found no question or names another, the check fails with NoQuestion.
// It returns to the question's return label once a comment after it
// counts as an answer (answerOf), the Apps among writers left out, as they
// asked it; otherwise it fails with Unanswered.
func CheckReturn(
	comments []Comment, failed bool, declared []PostedQuestion, writers []string, who Answerers,
) ReturnCheck {
	if failed {
		return ReturnCheck{Failure: Unread}
	}
	asked, question := -1, PostedQuestion{}
	for i, c := range slices.Backward(comments) {
		if q, ok := FindQuestionMarker(c.Body); ok && postedBy(c, writers) && slices.Contains(declared, q) {
			asked, question = i, q
			break
		}
	}
	after := comments[asked+1:]
	if asked < 0 || closedBy(after, question.ID, writers) {
		return ReturnCheck{Failure: NoQuestion}
	}
	for _, c := range after {
		if _, ok := answerOf(c, who, writers); ok {
			return ReturnCheck{To: question.Return}
		}
	}
	return ReturnCheck{Failure: Unanswered}
}

// closedBy reports whether the latest delegation among comments, oldest
// first, that one of writers posted through crew (postedBy) closes the
// question id: it found no question, or found another. One that could not
// read, or none at all, lets it stand.
func closedBy(comments []Comment, id QuestionID, writers []string) bool {
	for _, c := range slices.Backward(comments) {
		if !postedBy(c, writers) {
			continue
		}
		if d, ok := FindDelegatedMarker(c.Body); ok {
			return d.Search == QuestionNotFound || d.Search == QuestionFound && d.ID != id
		}
	}
	return false
}
