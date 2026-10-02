package github

import (
	"fmt"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// renderReport renders a failure report as one Markdown comment. Each failed
// action gets its name, workspace and log, then its reason in a fenced code
// block: a reason is a session's or a tool's last words, so nothing in it may
// render, link or mention anyone.
func renderReport(r crew.FailureReport) string {
	var b strings.Builder
	noun := "action"
	if len(r.Failures) != 1 {
		noun = "actions"
	}
	fmt.Fprintf(&b, "crew: %d %s failed on %s.\n", len(r.Failures), noun, r.IssueRef)
	for _, f := range r.Failures {
		fmt.Fprintf(&b, "\n**%s** failed in workspace %s. Its log is %s.\n\n",
			codeSpan(f.Action), codeSpan(f.Workspace), codeSpan(f.Log))
		fence := strings.Repeat("`", max(3, longestBacktickRun(f.Reason)+1))
		fmt.Fprintf(&b, "%stext\n%s\n%s\n", fence, f.Reason, fence)
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
