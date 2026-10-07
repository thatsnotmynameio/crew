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
