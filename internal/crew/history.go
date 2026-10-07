package crew

// crashedReason is the reason of an action run that recorded its start and
// never its end: crew did not live to see it end.
const crashedReason = "crew stopped before the run ended: it crashed or was killed"

// History is the past of the rule runs, folded from their events in the
// order they happened, replayed from a journal or live: for each issue and
// rule its last run, rebuilt with Apply, and for each of their actions the
// last action run that started or ended in its rule run's workspace. It
// retires nothing by itself: the core has it Retire the action runs a
// workspace held once another run starts in it.
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

// pastAction is the last action run of an action that started or ended in
// its rule run's workspace.
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
// is not its take. An action's start, and its end, become the action's
// last action run when the run's workspace was ready; an end in a run
// without one leaves the earlier one, so a failed run still resumes.
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
		run = next
		h.runs[k] = next
	}
	w, ok := run.Workspace().Get()
	if !ok {
		return
	}
	if asked, ok := e.(ActionSessionAsked); ok {
		h.opened(k, asked.Run, asked.Action, w)
	}
	if asked, ok := e.(ActionShellAsked); ok {
		h.opened(k, asked.Run, asked.Action, w)
	}
	if ended, ok := e.(ActionEnded); ok {
		h.ended(k, ended, w)
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
			points[action] = ResumePoint{Workspace: p.workspace, Log: p.log, Reason: reason, Action: action}
		}
	}
	return points
}

// Retire drops the last action run of every action, in any rule on any
// issue, whose last action run had the workspace named w, except action's
// own in rule on issue: that action is starting in w, which no longer holds
// their work, since names repeat once a workspace is gone. A retired action
// then has no resume point, and its next start carries no reason from
// before. An action whose last run moved to another workspace keeps it.
func (h *History) Retire(w WorkspaceName, issue IssueID, rule RuleName, action ActionName) {
	own := ruleKey{issue: issue, rule: rule}
	for k, actions := range h.actions {
		for name, p := range actions {
			if p.workspace.Name == w && (k != own || name != action) {
				delete(actions, name)
			}
		}
	}
}

// opened makes the start of action in run, in the run's workspace w, its
// action's last action run. It carries the reason of the action's last
// action run when that run failed in the same workspace.
func (h *History) opened(k ruleKey, run RuleRunID, action ActionName, w OpenedWorkspace) {
	p := pastAction{run: run, workspace: w.Workspace, log: w.Log}
	if prev, ok := h.actions[k][action]; ok && prev.workspace.Name == w.Workspace.Name {
		p.carried = prev.failed()
	}
	h.set(k, action, p)
}

// ended makes e's action run, in its run's workspace w, its action's last
// one. An end without a session in the workspace of a failed run takes
// that run's reason: the reason its start carried, or, with no start of its
// run before it, the reason of the action run before it.
func (h *History) ended(k ruleKey, e ActionEnded, w OpenedWorkspace) {
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
