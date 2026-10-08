package crew

import (
	"slices"
	"strings"
)

// QuestionID names a question a rule asks. It follows the verdicts'
// grammar.
type QuestionID string

// Asked is the verdict of a question action, which ends as soon as the run
// reaches it: its On sends it to the route of the action's name, which
// posts the question.
const Asked Verdict = "asked"

// Ask is a question a rule asks on an item: its id, its text and the label
// the item returns to once it is answered (R2). It names no answerer: the
// config's answerer answers every question.
type Ask struct {
	// ID identifies the question.
	ID QuestionID
	// Text is the question, a template over what crew knows of the run
	// that asks it (CommentData).
	Text CommentTemplate
	// Return is the ready label of the rule the item returns to.
	Return State
}

// Body returns the comment that asks q in the run d describes: its text
// rendered for d, then its marker, naming d's rule, on a line of its own.
// An error is the text's, which did not render.
func (q Ask) Body(d CommentData) (string, error) {
	text, err := q.Text.Render(d)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text + "\n" + QuestionMarker(q.ID, d.Rule, q.Return), nil
}

// QuestionStep posts a question on the item, as a comment of crew's
// tracker writer. The config makes it the last step before its route's
// move to the question's label.
type QuestionStep struct {
	Question Ask
}

// DelegateStep reads the item's open question and posts its delegation,
// which mentions the config's answerer. Only crew's own question rule holds
// it.
type DelegateStep struct{}

// QuestionSpec is an action that asks a question: it ends at once with
// Asked, which its On sends to the route of its name, and that route posts
// the question.
type QuestionSpec struct {
	Question Ask
}

// PostedQuestion is a question crew posted on an item, as its marker
// names it: its id, the rule that asked it and its return label.
type PostedQuestion struct {
	ID     QuestionID
	Rule   RuleName
	Return State
}

// OpenQuestion returns the item's open question among comments, which are
// oldest first, and whether it has one: the latest comment that holds a
// question's marker and crew's own marker, written by one of writers, the
// logins crew posts as, compared ignoring case. A delegation after it,
// a comment of one of writers that holds crew's own marker and a
// delegation's, closes it: crew delegated it already.
func OpenQuestion(comments []Comment, writers []string) (PostedQuestion, bool) {
	for _, c := range slices.Backward(comments) {
		if c.Author == "" || !containsFold(writers, c.Author) || !HoldsPostedMarker(c.Body) {
			continue
		}
		if q, ok := FindQuestionMarker(c.Body); ok {
			return q, true
		}
		if HoldsDelegatedMarker(c.Body) {
			break
		}
	}
	return PostedQuestion{}, false
}

// QuestionSearch is what the read of an item's comments found for a
// delegation: its open question, none, or nothing as the read failed.
type QuestionSearch int

// The searches of an item's open question.
const (
	// QuestionNotFound is a read that found no open question.
	QuestionNotFound QuestionSearch = iota
	// QuestionFound is a read that found the open question.
	QuestionFound
	// QuestionUnread is a read of the comments that failed.
	QuestionUnread
)

// Delegation is what a delegation step asks a tracker to post on an item:
// the answerer to mention, and the question it is asked to answer, by its
// id and the rule that asked it. The tracker adapter formats it in its own
// markup. It never carries the question's text, which the item already
// shows.
type Delegation struct {
	// IssueID and IssueRef identify the item, as ID and Ref in Issue.
	IssueID  IssueID
	IssueRef string
	// Answerer is the login of the person or App the config names to
	// answer every question.
	Answerer string
	// Search is what the read of the item's comments found. Every
	// delegation mentions the answerer, also one that found no question.
	Search QuestionSearch
	// ID and Rule name the question found; empty unless Search is
	// QuestionFound.
	ID   QuestionID
	Rule RuleName
}
