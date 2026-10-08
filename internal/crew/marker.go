package crew

import (
	"net/url"
	"strings"
)

// MarkerPrefix starts every one of crew's markers, the hidden HTML comments
// in the comments crew and its sessions write on an issue, which GitHub's
// Markdown renders as nothing. A comment that holds it anywhere is crew's
// or a session's and never an answer (R43, R46).
const MarkerPrefix = "<!-- crew:"

// PostedMarker is crew's own marker: the tracker writes it on every comment
// crew posts or edits, its reports, route comments and status comments
// alike, so none counts as an answer, and a comment that holds it is never
// a question, even when its text renders a session's marker (R46). Shell
// actions get it as CREW_COMMENT_MARKER, to mark the comments they post.
const PostedMarker = MarkerPrefix + "posted -->"

// SessionMarker returns the marker a session writes on the comment that
// asks its question: <!-- crew:session run=<run> action=<action> -->. Its
// values are query-escaped, so none can hold a space or close the HTML
// comment.
func SessionMarker(run RuleRunID, action ActionName) string {
	return MarkerPrefix + "session run=" + url.QueryEscape(string(run)) +
		" action=" + url.QueryEscape(string(action)) + " -->"
}

// HoldsMarker reports whether body holds any of crew's markers, crew's own,
// a session's or the status comment's, anywhere: such a comment is never
// an answer.
func HoldsMarker(body string) bool {
	return strings.Contains(body, MarkerPrefix)
}

// HoldsPostedMarker reports whether body holds crew's own marker
// (PostedMarker) anywhere: crew wrote it, and it is never a question.
func HoldsPostedMarker(body string) bool {
	return strings.Contains(body, PostedMarker)
}

// HoldsSessionMarker reports whether body holds, anywhere, the marker of
// the session at action in run (SessionMarker).
func HoldsSessionMarker(body string, run RuleRunID, action ActionName) bool {
	return strings.Contains(body, SessionMarker(run, action))
}

// questionPrefix starts a question's marker, and delegatedPrefix a
// delegation's.
const (
	questionPrefix  = MarkerPrefix + "question "
	delegatedPrefix = MarkerPrefix + "delegated "
)

// markerEnd closes every one of crew's markers.
const markerEnd = " -->"

// QuestionMarker returns the marker of a question a rule asks, on the
// comment that asks it: <!-- crew:question id=<id> rule=<rule>
// return=<label> -->. Its values are query-escaped, as SessionMarker's are.
func QuestionMarker(id QuestionID, rule RuleName, ret State) string {
	return questionPrefix + "id=" + url.QueryEscape(string(id)) + " rule=" + url.QueryEscape(string(rule)) +
		" return=" + url.QueryEscape(string(ret)) + markerEnd
}

// FindQuestionMarker returns the question the last question marker in body
// names, and whether body holds one that parses. crew writes its marker
// after the question's text, which may render what anyone wrote, such as
// an issue's title, so only the last one counts.
func FindQuestionMarker(body string) (PostedQuestion, bool) {
	_, marker, found := strings.CutLast(body, questionPrefix)
	inner, _, closed := strings.Cut(marker, markerEnd)
	values, ok := markerValues(inner, "id", "rule", "return")
	if !found || !closed || !ok {
		return PostedQuestion{}, false
	}
	return PostedQuestion{ID: QuestionID(values[0]), Rule: RuleName(values[1]), Return: State(values[2])}, true
}

// markerValues returns the values of inner, a marker's key=value pairs
// separated by single spaces, which must be keys, in this order, and
// whether each pair is there and unescapes.
func markerValues(inner string, keys ...string) ([]string, bool) {
	pairs := strings.Split(inner, " ")
	if len(pairs) != len(keys) {
		return nil, false
	}
	values := make([]string, 0, len(keys))
	for i, pair := range pairs {
		escaped, found := strings.CutPrefix(pair, keys[i]+"=")
		value, err := url.QueryUnescape(escaped)
		if !found || err != nil {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

// DelegatedMarker returns the marker of the comment that delegates the
// question id, empty when crew found none: <!-- crew:delegated id=<id> -->.
// Its value is query-escaped, as SessionMarker's are.
func DelegatedMarker(id QuestionID) string {
	return delegatedPrefix + "id=" + url.QueryEscape(string(id)) + markerEnd
}

// HoldsDelegatedMarker reports whether body holds, anywhere, the marker of
// a delegation, of any question.
func HoldsDelegatedMarker(body string) bool {
	return strings.Contains(body, delegatedPrefix)
}
