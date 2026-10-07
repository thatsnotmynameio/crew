package github

import (
	"fmt"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// renderReport renders a failure report as one Markdown comment. Each failed
// action gets its name and its log's repository-relative path, or a line
// saying it failed before it had a log. A reason is a session's or a tool's
// last words, which can hold commands and their output, so the comment never
// carries it: you read it in the log or in crew's output.
func renderReport(r crew.FailureReport) string {
	var b strings.Builder
	noun := "action"
	if len(r.Failures) != 1 {
		noun = "actions"
	}
	fmt.Fprintf(&b, "crew: %d %s failed on %s.\n", len(r.Failures), noun, r.IssueRef)
	for _, f := range r.Failures {
		if f.Log == "" {
			fmt.Fprintf(&b, "\n**%s** failed before it had a log. crew's output says why.\n", codeSpan(string(f.Action)))
			continue
		}
		fmt.Fprintf(&b, "\n**%s** failed. Its log is %s.\n", codeSpan(string(f.Action)), codeSpan(f.Log))
	}
	return b.String()
}

// codeSpan renders s as inline code. Its delimiter is longer than any
// backtick run in s, so s cannot close it early; a space pads s when it
// starts or ends with a backtick, which Markdown strips again.
func codeSpan(s string) string {
	delim := strings.Repeat("`", longestBacktickRun(s)+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return delim + s + delim
}

// longestBacktickRun returns the length of the longest run of backticks in s.
func longestBacktickRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r != '`' {
			run = 0
			continue
		}
		run++
		longest = max(longest, run)
	}
	return longest
}

// renderStop renders a pull request report's stop comment, for a report
// with an end, in Markdown: how the rule ended on the issue and the label
// the issue and the pull request moved to, each failed action as the status
// comment words it, that nobody watches the pull request any more, then
// link. Like the status comment, it carries no session's words.
func renderStop(r crew.PullRequestReport, link string) string {
	var b strings.Builder
	outcome := "succeeded"
	if r.End.Failed() {
		outcome = "failed"
	}
	fmt.Fprintf(&b, "crew: %s %s on %s, which moved to %s, as did this pull request.\n",
		codeSpan(string(r.End.Rule)), outcome, r.IssueRef, codeSpan(string(r.State)))
	for _, a := range r.End.Actions {
		if a.State == crew.ActionFailed {
			fmt.Fprintf(&b, "\n%s\n", failedAction("**"+codeSpan(string(a.Name))+"**", a))
		}
	}
	b.WriteString("\nNobody watches this pull request any more: new review comments and CI failures need a person.\n")
	fmt.Fprintf(&b, "\n%s\n", link)
	return b.String()
}
