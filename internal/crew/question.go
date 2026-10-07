package crew

import "slices"

// Question is a session that may have asked a question on the issue, and
// that no later session at its action ended on since (R23, KTD-W7): the
// run it ran in, its action, and the login it acted as, which crew finds
// its question by (R45). An empty login finds no question.
type Question struct {
	Run    RuleRunID
	Action ActionName
	Login  string
}

// Questions returns a copy of the run's open questions at the action named
// action, oldest first: those it inherited from the run it continues, then
// its own session's, when it may have asked one.
func (r RuleRun) Questions(action ActionName) []Question {
	var out []Question
	for _, q := range r.questions {
		if q.Action == action {
			out = append(out, q)
		}
	}
	return out
}

// asked returns r with the question of its session at the action named
// action, which acted as login, added.
func (r RuleRun) asked(action ActionName, login string) RuleRun {
	r.questions = append(slices.Clone(r.questions), Question{Run: r.id, Action: action, Login: login})
	return r
}

// endedOn returns r once its session at the action named action ended
// well with verdict: it ends on every question at that action, except its
// own when it ended with Waiting, since it asked again (KTD-W7).
func (r RuleRun) endedOn(action ActionName, verdict Verdict) RuleRun {
	r.questions = slices.DeleteFunc(slices.Clone(r.questions), func(q Question) bool {
		return q.Action == action && (q.Run != r.id || verdict != Waiting)
	})
	return r
}

// Questions returns the open questions a new run of rule on issue
// inherits, oldest first: those of the last run of rule on issue, retired
// or not, and none when it ended through PassedRoute and finished its
// route, or without a last run.
func (h *History) Questions(issue IssueID, rule RuleName) []Question {
	past, ok := h.runs[ruleKey{issue: issue, rule: rule}]
	if !ok || passedFinished(past.run) {
		return nil
	}
	return slices.Clone(past.run.questions)
}
