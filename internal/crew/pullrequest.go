package crew

import "slices"

// PullRequestReport is what crew shows on the open pull requests that close
// an issue, once it moved or closed the issue: the state the issue moved
// to, mirrored as the pull requests' own state, and, when the move or close
// ended a rule, how the rule ended. The tracker adapter finds the pull requests and formats the
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
	// in it, and in no other crew state. It is empty when the rule's route
	// closed the issue: the close took crew's labels off the issue and its
	// pull requests, so no label is put back (R51).
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

// State returns the crew state the issue moved to, or the empty State when
// the issue was closed.
func (r PullRequestReport) State() State { return r.data.State }

// End returns how the rule ended, or none for the move that takes the
// issue.
func (r PullRequestReport) End() Optional[RuleEnd] { return r.data.End }

// RuleEnd is how a rule ended on an issue, for a pull request report: the
// route it ended through, and its actions. It cannot be changed once built:
// NewRuleEnd copies the actions it is given, and Actions returns a copy.
type RuleEnd struct {
	rule    RuleName
	route   RouteName
	actions []ActionStatus
}

// NewRuleEnd returns how rule ended through route, with its actions in its
// action order, as an ended Status carries them.
func NewRuleEnd(rule RuleName, route RouteName, actions []ActionStatus) RuleEnd {
	return RuleEnd{rule: rule, route: route, actions: slices.Clone(actions)}
}

// Rule returns the rule's name.
func (e RuleEnd) Rule() RuleName { return e.rule }

// Route returns the route the rule ended through.
func (e RuleEnd) Route() RouteName { return e.route }

// Actions returns a copy of the rule's actions, in its action order.
func (e RuleEnd) Actions() []ActionStatus { return slices.Clone(e.actions) }
