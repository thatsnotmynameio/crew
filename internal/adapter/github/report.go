package github

import (
	"fmt"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// renderReport renders a route's report as one Markdown comment: the rule,
// the route it ends through and the issue, then the action whose verdict
// ended the sequence, that verdict and its log's repository-relative path,
// or a line saying it ended before it had a log (R17, KTD23). The report
// carries no reason: a reason is a session's or a tool's last words, which
// can hold commands and their output, so you read it in the log or in
// crew's output.
func renderReport(r crew.FailureReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "crew: %s ended through %s on %s.\n", codeSpan(string(r.Rule)), codeSpan(string(r.Route)), r.IssueRef)
	for _, f := range r.Failures {
		action := fmt.Sprintf("**%s** ended with %s", codeSpan(string(f.Action)), codeSpan(string(f.Verdict)))
		if f.Log == "" {
			fmt.Fprintf(&b, "\n%s before it had a log. crew's output says why.\n", action)
			continue
		}
		fmt.Fprintf(&b, "\n%s. Its log is %s.\n", action, codeSpan(f.Log))
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

// renderStop renders the stop comment of a pull request report whose rule
// ended as end, in Markdown: the route the rule ended through on the issue,
// and the label the issue and the pull request moved to, or that the issue
// was closed and the pull request lost crew's labels (R51); then each action
// that failed, worded as the status comment words it, or that ended with a
// verdict other than passed; that nobody watches the pull request any more;
// then link. Like the status comment, it carries no session's words, and
// unlike it no shell action's line (R49).
func renderStop(r crew.PullRequestReport, end crew.RuleEnd, link string) string {
	var b strings.Builder
	ended := fmt.Sprintf("crew: %s ended through %s on %s", codeSpan(string(end.Rule())), codeSpan(string(end.Route())),
		r.IssueRef())
	if r.State() == "" {
		fmt.Fprintf(&b, "%s, which crew closed. crew took its labels off this pull request, which stays open.\n", ended)
	} else {
		fmt.Fprintf(&b, "%s, which moved to %s, as did this pull request.\n", ended, codeSpan(string(r.State())))
	}
	for _, a := range end.Actions() {
		name := "**" + codeSpan(string(a.Name)) + "**"
		switch state := a.State.(type) {
		case crew.ActionFailed:
			fmt.Fprintf(&b, "\n%s\n", failedAction(name, state, crew.CheckReason{}))
		case crew.ActionSucceeded:
			if state.Verdict != "" && state.Verdict != crew.Passed {
				fmt.Fprintf(&b, "\n%s %s.\n", name, verdictWords(state.Verdict))
			}
		case crew.ActionPending, crew.ActionAwaitingTurn, crew.ActionRunning, crew.ActionNotRun,
			crew.ActionDoneInEarlierRun:
		}
	}
	b.WriteString("\nNobody watches this pull request any more: new review comments and CI failures need a person.\n")
	fmt.Fprintf(&b, "\n%s\n", link)
	return b.String()
}
