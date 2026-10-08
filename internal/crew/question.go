package crew

import "slices"

// Question is an open question on the issue. It is a session's when its ID
// is empty: a session that may have asked a question, and that no later
// session at its action ended on since (R23, KTD-W7), by the run it ran in,
// its action, and the login it acted as, which crew finds its question by
// (R45); an empty login finds no question. Otherwise it is a rule's: the
// question ID the rule named Rule asked, which crew finds by its question
// marker (QuestionMarker), posted by one of crew's writers (KTD5). A rule's
// question is tied to no action: the first session a run starts gets it
// (KTD8).
type Question struct {
	Run    RuleRunID
	Action ActionName
	Login  string
	ID     QuestionID
	Rule   RuleName
}

// Questions returns a copy of the open questions the session of the run's
// action named action gets, oldest first: the session questions at that
// action, those it inherited from the run it continues, then its own
// session's, when it may have asked one; and, when it is the run's first
// session, every rule's question the run holds (KTD8).
func (r RuleRun) Questions(action ActionName) []Question {
	first := r.firstSession(action)
	var out []Question
	for _, q := range r.questions {
		if q.gets(action, first) {
			out = append(out, q)
		}
	}
	return out
}

// gets reports whether the session at the action named action gets q: a
// session question at that action, or a rule's question when the session
// is its run's first.
func (q Question) gets(action ActionName, first bool) bool {
	if q.ID != "" {
		return first
	}
	return q.Action == action
}

// firstSession reports whether the session at the action named action is
// the run's first: no session of another action started in the run.
func (r RuleRun) firstSession(action ActionName) bool {
	return !slices.ContainsFunc(r.actions, func(a ActionRun) bool {
		_, started := a.session.Get()
		return started && a.name != action
	})
}

// asked returns r with the question of its session at the action named
// action, which acted as login, added.
func (r RuleRun) asked(action ActionName, login string) RuleRun {
	r.questions = append(slices.Clone(r.questions), Question{Run: r.id, Action: action, Login: login})
	return r
}

// ruleAsked returns r with its rule's question id, which a question step
// posted, open, unless it already is. A plan a journal recorded before crew
// kept its question's id names none, and opens none.
func (r RuleRun) ruleAsked(id QuestionID) RuleRun {
	q := Question{ID: id, Rule: r.rule}
	if id == "" || slices.Contains(r.questions, q) {
		return r
	}
	r.questions = append(slices.Clone(r.questions), q)
	return r
}

// endedOn returns r once its session at the action named action ended
// well with verdict: it ends on every question that session got
// (Questions), unless it ended with Waiting. A session that waits again
// keeps them all, its own and the earlier ones, since it may have waited
// for answers to an earlier question without asking a new one; the latest
// question asked wins when crew reads the answers (KTD-W7).
func (r RuleRun) endedOn(action ActionName, verdict Verdict) RuleRun {
	if verdict == Waiting {
		return r
	}
	first := r.firstSession(action)
	r.questions = slices.DeleteFunc(slices.Clone(r.questions), func(q Question) bool {
		return q.gets(action, first)
	})
	return r
}

// Questions returns the open questions a new run of rule on issue
// inherits, oldest first: those of the last run of rule on issue, retired
// or not; only the rules' questions when it ended through PassedRoute and
// finished its route, as a question that route asked is still open (KTD8);
// and none without a last run.
func (h *History) Questions(issue IssueID, rule RuleName) []Question {
	past, ok := h.runs[ruleKey{issue: issue, rule: rule}]
	if !ok {
		return nil
	}
	questions := slices.Clone(past.run.questions)
	if passedFinished(past.run) {
		return slices.DeleteFunc(questions, func(q Question) bool { return q.ID == "" })
	}
	return questions
}
