package crew

// crashedReason is the reason of an action run that recorded its start and
// never its end: crew did not live to see it end.
const crashedReason = "crew stopped before the run ended: it crashed or was killed"

// History is the past of the rule runs, folded from their events in the
// order they happened, replayed from a journal or live: for each issue and
// rule its last run, rebuilt with Apply, and for each of their actions the
// last action run that had a workspace. It retires nothing by itself:
// which workspace another action has since started in is the core's to
// know, and the core has it Forget the action run that workspace held.
//
// The zero History holds no past. Fold changes it, so the one that holds it
// is the one that folds every event; its accessors return copies.
type History struct {
	runs    map[ruleKey]RuleRun
	actions map[ruleKey]map[ActionName]pastAction
}

// ruleKey identifies the runs of a rule on an issue.
type ruleKey struct {
	issue IssueID
	rule  RuleName
}

// pastAction is the last action run of an action that had a workspace.
type pastAction struct {
	run       RuleRunID
	workspace Workspace
	log       string
	// end is how the action run ended, with the reason a resume quotes;
	// none when it never recorded its end.
	end Optional[ActionEnd]
	// carried is the reason of the failed action run, in the same
	// workspace, that this one's start replaced: an end without a session
	// takes it, which says more than how that session failed to start.
	carried Optional[SessionText]
}

// Fold adds e to the past. An event of another run than its issue and
// rule's last one starts that rule's last run again, from e alone when e
// is not its take. An action's start, and its end when it names a
// workspace, become the action's last action run; an end without a
// workspace leaves the earlier one, so a failed run still resumes.
func (h *History) Fold(e RunEvent) {
	if h.runs == nil {
		h.runs = map[ruleKey]RuleRun{}
		h.actions = map[ruleKey]map[ActionName]pastAction{}
	}
	head := e.Head()
	k := ruleKey{issue: head.IssueID, rule: head.Rule}
	run := h.runs[k]
	if run.ID() != head.Run {
		run = RuleRun{}
	}
	if next, err := Apply(run, e); err == nil {
		h.runs[k] = next
	}
	if opened, ok := e.(ActionOpened); ok {
		h.opened(k, opened)
	}
	if ended, ok := e.(ActionEnded); ok {
		h.ended(k, ended)
	}
}

// LastRun returns the last run of rule on issue, as its events rebuilt it,
// and whether there was one. A new run of the rule on the issue continues
// it.
func (h *History) LastRun(issue IssueID, rule RuleName) (RuleRun, bool) {
	run, ok := h.runs[ruleKey{issue: issue, rule: rule}]
	return run, ok
}

// ResumePoints returns where each action of rule on issue resumes: the
// workspace, log and reason of its last action run that had a workspace,
// when that run failed or never recorded its end. An action whose last
// action run succeeded, or that never had a workspace, has none.
func (h *History) ResumePoints(issue IssueID, rule RuleName) map[ActionName]ResumePoint {
	points := map[ActionName]ResumePoint{}
	for action, p := range h.actions[ruleKey{issue: issue, rule: rule}] {
		if reason, ok := p.failure(); ok {
			points[action] = ResumePoint{Workspace: p.workspace, Log: p.log, Reason: reason}
		}
	}
	return points
}

// Forget drops the last action run of action, in rule on issue: another
// action has since started in its workspace, which no longer holds its
// work. The action then has no resume point, and its next start carries no
// reason from before.
func (h *History) Forget(issue IssueID, rule RuleName, action ActionName) {
	delete(h.actions[ruleKey{issue: issue, rule: rule}], action)
}

// opened makes e's action run its action's last one. It carries the reason
// of the action's last action run when that run failed in the same
// workspace.
func (h *History) opened(k ruleKey, e ActionOpened) {
	p := pastAction{run: e.Run, workspace: e.Workspace, log: e.Log}
	if prev, ok := h.actions[k][e.Action]; ok && prev.workspace.Name == e.Workspace.Name {
		p.carried = prev.failed()
	}
	h.set(k, e.Action, p)
}

// ended makes e's action run its action's last one, when it had a
// workspace. An end without a session in the workspace of a failed run
// takes that run's reason: the reason its start carried, or, with no start
// of its run before it, the reason of the action run before it.
func (h *History) ended(k ruleKey, e ActionEnded) {
	w, ok := e.Workspace.Get()
	if !ok {
		return
	}
	p := pastAction{run: e.Run, workspace: w.Workspace, log: w.Log, end: Some(e.End)}
	if prev, ok := h.actions[k][e.Action]; ok && prev.workspace.Name == w.Workspace.Name {
		p.carried = prev.failed()
		if _, ended := prev.end.Get(); !ended && prev.run == e.Run {
			p.carried = prev.carried
		}
	}
	if reason, ok := p.carried.Get(); ok {
		if _, started := e.SessionStarted.Get(); !started {
			p.end = Some(withReason(e.End, reason))
		}
	}
	h.set(k, e.Action, p)
}

// set makes p the last action run of the action named action, in k.
func (h *History) set(k ruleKey, action ActionName, p pastAction) {
	if h.actions[k] == nil {
		h.actions[k] = map[ActionName]pastAction{}
	}
	h.actions[k][action] = p
}

// withReason returns end with reason as its reason.
func withReason(end ActionEnd, reason SessionText) ActionEnd {
	switch end := end.(type) {
	case EndFailed:
		end.Reason = reason
		return end
	case EndSucceeded:
		end.Reason = reason
		return end
	}
	return end
}

// failure returns why the action run failed, and whether it did: its end's
// reason, or crashedReason when it never recorded its end.
func (p pastAction) failure() (SessionText, bool) {
	end, ok := p.end.Get()
	if !ok {
		return NewSessionText(crashedReason), true
	}
	switch end := end.(type) {
	case EndFailed:
		return end.Reason, true
	case EndSucceeded:
	}
	return SessionText{}, false
}

// failed returns the reason failure returns, when p failed.
func (p pastAction) failed() Optional[SessionText] {
	if reason, ok := p.failure(); ok {
		return Some(reason)
	}
	return Optional[SessionText]{}
}
