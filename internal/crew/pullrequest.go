package crew

import "slices"

// PullRequestReport is what crew shows on the open pull requests that close
// an issue, once it moved the issue: the state the issue moved to, mirrored
// as the pull requests' own state, and, when the move ended a rule, how the
// rule ended. The tracker adapter finds the pull requests and formats the
// report in its own markup.
type PullRequestReport struct {
	// ID identifies the report across its retries: it is unique within one
	// crew process and stays the same each time the report is sent again.
	ID string
	// IssueID and IssueRef identify the issue, as ID and Ref in Issue.
	IssueID  IssueID
	IssueRef string
	// State is the crew state the issue moved to. Each pull request is put
	// in it, and in no other crew state.
	State State
	// End is how the rule ended when the move ended one; nil for the move
	// that takes the issue.
	End *RuleEnd
}

// RuleEnd is how a rule ended on an issue, for a pull request report.
type RuleEnd struct {
	// Rule is the rule's name.
	Rule RuleName
	// Actions are the rule's actions, in its action order, as an ended
	// Status carries them: each succeeded or failed, with its checks'
	// reasons, and a failed one with its cause and its log. The rule failed
	// when any action failed.
	Actions []ActionStatus
}

// Failed reports whether any action of the rule failed.
func (e RuleEnd) Failed() bool {
	return slices.ContainsFunc(e.Actions, func(a ActionStatus) bool { return a.State == ActionFailed })
}

// Clone returns a copy of r with its own End and Actions, so the copy shares
// no memory with r.
func (r PullRequestReport) Clone() PullRequestReport {
	if r.End != nil {
		end := *r.End
		end.Actions = cloneActions(end.Actions)
		r.End = &end
	}
	return r
}
