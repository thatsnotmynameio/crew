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
// carries it: the boss reads it in the log or in crew's output.
func renderReport(r crew.FailureReport) string {
	var b strings.Builder
	noun := "action"
	if len(r.Failures) != 1 {
		noun = "actions"
	}
	fmt.Fprintf(&b, "crew: %d %s failed on %s.\n", len(r.Failures), noun, r.IssueRef)
	for _, f := range r.Failures {
		if f.Log == "" {
			fmt.Fprintf(&b, "\n**%s** failed before it had a log. crew's output says why.\n", codeSpan(f.Action))
			continue
		}
		fmt.Fprintf(&b, "\n**%s** failed. Its log is %s.\n", codeSpan(f.Action), codeSpan(f.Log))
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
