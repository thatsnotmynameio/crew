package crew

import "slices"

// PullRequestReport is what crew shows on the open pull requests that close
// an issue, once it moved the issue: the state the issue moved to, mirrored
// as the pull requests' own state, and, when the move ended a rule, how the
// rule ended. The tracker adapter finds the pull requests and formats the
// report in its own markup.
//
// A PullRequestReport cannot be changed once built: its fields are read
// through accessors, and its RuleEnd keeps its own actions.
type PullRequestReport struct {
	data PullRequestReportData
}

// PullRequestReportData is a pull request report's fields as plain data, to
// build a PullRequestReport from (NewPullRequestReport).
type PullRequestReportData struct {
	// ID identifies the report: it is derived from the rule run and the move
	// it reports, so it stays the same across the report's retries and
	// across crew processes.
	ID PullRequestReportID
	// IssueID and IssueRef identify the issue, as ID and Ref in Issue.
	IssueID  IssueID
	IssueRef string
	// State is the crew state the issue moved to. Each pull request is put
	// in it, and in no other crew state.
	State State
	// End is how the rule ended, when the move ended one; none for the move
	// that takes the issue.
	End Optional[RuleEnd]
}

// NewPullRequestReport returns the report d describes.
func NewPullRequestReport(d PullRequestReportData) PullRequestReport {
	return PullRequestReport{data: d}
}

// ID returns the report's identity, the same across its retries.
func (r PullRequestReport) ID() PullRequestReportID { return r.data.ID }

// IssueID returns the identity of the issue.
func (r PullRequestReport) IssueID() IssueID { return r.data.IssueID }

// IssueRef returns how humans write the issue, such as "#42".
func (r PullRequestReport) IssueRef() string { return r.data.IssueRef }

// State returns the crew state the issue moved to.
func (r PullRequestReport) State() State { return r.data.State }

// End returns how the rule ended, or none for the move that takes the
// issue.
func (r PullRequestReport) End() Optional[RuleEnd] { return r.data.End }

// RuleEnd is how a rule ended on an issue, for a pull request report. It
// cannot be changed once built: NewRuleEnd copies the actions it is given,
// and Actions returns a copy.
type RuleEnd struct {
	rule    RuleName
	actions []ActionStatus
}

// NewRuleEnd returns how rule ended, with its actions in its action order,
// as an ended Status carries them: each succeeded or failed, with its
// checks' reasons, and a failed one with its cause and its log. The rule
// failed when any action failed.
func NewRuleEnd(rule RuleName, actions []ActionStatus) RuleEnd {
	return RuleEnd{rule: rule, actions: cloneActions(actions)}
}

// Rule returns the rule's name.
func (e RuleEnd) Rule() RuleName { return e.rule }

// Actions returns a copy of the rule's actions, in its action order, each
// with its own Checks.
func (e RuleEnd) Actions() []ActionStatus { return cloneActions(e.actions) }

// Failed reports whether any action of the rule failed.
func (e RuleEnd) Failed() bool {
	return slices.ContainsFunc(e.actions, func(a ActionStatus) bool {
		_, failed := a.State.(ActionFailed)
		return failed
	})
}
