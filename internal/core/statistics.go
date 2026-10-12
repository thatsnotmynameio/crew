package core

import (
	"slices"
	"time"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// statistics is what the model knows of the crew process it records in
// the statistics store, when it records statistics (KTD4).
type statistics struct {
	version string
	folder  string
	tracker crew.TrackerName
	// process is the id of the recorded process, which the work it runs
	// points to; empty until Started.
	process crew.ProcessID
	// states holds, by kind of item, the states the rules taking that kind
	// name: the labels that count an item of that kind (KTD1).
	states map[crew.Kind][]crew.State
	// issues holds, by item id, each item recorded in this process.
	issues map[crew.IssueID]recordedIssue
}

// recordedIssue is what the model knows of an item it recorded (KTD4).
type recordedIssue struct {
	// state is the last crew state the item was known at; empty when it
	// was first seen in two or more.
	state crew.State
	// landed is the listing generation when a move crew made of the item
	// last landed: a listing of that generation may predate it (KTD5).
	landed int
}

// newStatistics returns what a model of rules knows of the process of crew
// version, working in the repository at folder on tracker, before it
// starts.
func newStatistics(rules []crew.Rule, version, folder string, tracker crew.TrackerName) *statistics {
	states := map[crew.Kind][]crew.State{}
	for _, kind := range []crew.Kind{crew.KindIssue, crew.KindPullRequest} {
		takers := slices.DeleteFunc(slices.Clone(rules), func(r crew.Rule) bool { return r.Takes != kind })
		states[kind] = crew.RuleStates(takers)
	}
	return &statistics{
		version: version, folder: folder, tracker: tracker, states: states, issues: map[crew.IssueID]recordedIssue{},
	}
}

// statisticsInput applies in, an input about the statistics store.
func (s *step) statisticsInput(in statisticsInput) {
	switch in := in.(type) {
	case Started:
		s.started(in.Seed, in.Repository)
	case StatisticFailed:
		s.emit(StatisticNotRecorded{At: s.at, Statistic: in.Statistic, Reason: in.Reason})
	}
}

// started records the process, with an id minted from seed, then
// repository, the one it works in, once, when the model records statistics
// (KTD3).
func (s *step) started(seed uuid.UUID, repository crew.Repository) {
	st := s.m.statistics
	if st == nil || st.process != "" {
		return
	}
	st.process = crew.ProcessID(seed.String())
	s.command(RecordStatistic{Statistic: crew.Process{
		ID: st.process, Version: st.version, Folder: st.folder, Start: s.at,
	}})
	s.command(RecordStatistic{Statistic: crew.RepositoryRecord{Tracker: st.tracker, Repository: repository}})
}

// sighted records each counted item of a listing's answer the first time
// this process finds it, and each change of its one crew state since,
// made outside crew at the listing's time (KTD4). It compares no item crew
// holds, nor one whose move crew made landed while this listing was
// outstanding, which may predate it: their moves are the runs' to record
// (KTD5). Only one listing is outstanding at a time, so this one is
// generation m.listings.
func (s *step) sighted(issues []crew.Issue) {
	m := s.m
	st := m.statistics
	if st == nil {
		return
	}
	for _, issue := range issues {
		if !st.counts(issue) {
			continue
		}
		known, ok := st.issues[issue.ID()]
		if !ok {
			s.sight(issue)
			continue
		}
		state, alone := issue.OnlyState()
		if !alone || m.held(issue.ID()) != nil || known.landed >= m.listings {
			continue
		}
		if known.state == "" {
			known.state = state
			st.issues[issue.ID()] = known
			continue
		}
		s.moved(issue.ID(), known.state, state, s.at, crew.Optional[crew.RuleRunID]{})
	}
}

// counts reports whether issue is in a state a rule of its kind names
// (KTD1).
func (st *statistics) counts(issue crew.Issue) bool {
	return slices.ContainsFunc(issue.States(), func(state crew.State) bool {
		return slices.Contains(st.states[issue.Kind()], state)
	})
}

// sight records issue, which this process finds for the first time, with
// the tracker's creation date when it gives one and its one state when it
// is in one, which becomes its last state (R7).
func (s *step) sight(issue crew.Issue) {
	st := s.m.statistics
	sighting := crew.IssueSighting{
		Tracker: st.tracker, Issue: issue.ID(), Ref: issue.Ref(), Kind: issue.Kind(), Seen: s.at,
	}
	if created := issue.Created(); !created.IsZero() {
		sighting.Created = crew.Some(created)
	}
	state, alone := issue.OnlyState()
	if alone {
		sighting.State = crew.Some(state)
	}
	st.issues[issue.ID()] = recordedIssue{state: state}
	s.command(RecordStatistic{Statistic: sighting})
}

// moved records the move of the item identified by id from one crew state
// to another, which crew made or saw at seen, made by run or outside crew
// when run is none, and keeps to as the item's last state (KTD4). It
// records no move that leaves or reaches no state, or stays in one. A move
// crew made keeps the listing generation it landed at (KTD5).
func (s *step) moved(id crew.IssueID, from, to crew.State, seen time.Time, run crew.Optional[crew.RuleRunID]) {
	st := s.m.statistics
	if st == nil || from == "" || to == "" || from == to {
		return
	}
	known := st.issues[id]
	known.state = to
	if _, byRun := run.Get(); byRun {
		known.landed = s.m.listings
	}
	st.issues[id] = known
	s.command(RecordStatistic{Statistic: crew.LabelMove{
		Tracker: st.tracker, Issue: id, From: from, To: to, Seen: seen, Run: run,
	}})
}

// spanOpened records the open of the span of h's run, which the rule just
// took: under its issue, by this process, in its rule's queue, with the run
// it continues, at the take's time, before the take's move that names the
// run (KTD2, KTD6).
func (s *step) spanOpened(h *heldRun) {
	if s.m.statistics == nil {
		return
	}
	s.command(RecordStatistic{Statistic: s.m.span(h)})
}

// spanEnded records the end of the span of h's run, released at at: how
// it ended, the route it ended through and the halt that chose that route
// (KTD2, KTD3). The record carries the whole span, so an end whose open was
// lost still records it (KTD5).
func (s *step) spanEnded(h *heldRun, at time.Time) {
	if s.m.statistics == nil {
		return
	}
	sp := s.m.span(h)
	end := crew.RuleRunEnd{At: at, Outcome: crew.OutcomeNotTaken, Halt: h.halt}
	released, _ := h.run.Phase().(crew.ReleasedPhase)
	if p, routed := released.Route.Get(); routed {
		end.Outcome, end.Route = outcome(p), crew.Some(p.Route)
	}
	sp.End = crew.Some(end)
	s.command(RecordStatistic{Statistic: sp})
}

// span returns the span of h's run, with no end.
func (m *Model) span(h *heldRun) crew.RuleRunSpan {
	st := m.statistics
	sp := crew.RuleRunSpan{
		Tracker: st.tracker, Issue: h.id(), Run: h.run.ID(), Process: st.process, Rule: h.run.Rule(),
		Continues: h.run.Continues(), Start: h.run.Taken(),
	}
	if queue := m.queues[m.queueOf[h.rule]].Name; queue != "" {
		sp.Queue = crew.Some(queue)
	}
	return sp
}

// outcome returns how a run that ended through p ended, from how its final
// move or close settled: dropped as the item moved meanwhile, given up, or
// landed, as a route with no steps counts (KTD3).
func outcome(p crew.RoutingPhase) crew.RunOutcome {
	switch final, _ := p.Final(); final.(type) {
	case crew.StepDropped:
		return crew.OutcomeRouteDropped
	case crew.StepGivenUp:
		return crew.OutcomeRouteGivenUp
	case crew.StepLanded, crew.StepRan, crew.StepFailed, crew.StepSkipped, crew.StepStopped, nil:
	}
	return crew.OutcomeRouted
}

// keepHalt keeps on h the halt that chose route, the route h's run just
// chose, for its span's end (KTD3): crew's stop, when it reached the run
// before it chose FailedRoute, or the run time limit, when it kept the
// action at the run's cursor from starting. A stop that reaches a run
// already going through its route, and a session that ends on its own
// during the wind-down, chose nothing.
func (s *step) keepHalt(h *heldRun, route crew.RouteName) {
	if s.m.statistics == nil || route != crew.FailedRoute {
		return
	}
	if h.run.Stopping() {
		h.halt = crew.Some(crew.HaltStop)
		return
	}
	a, _ := h.run.Cursor()
	if finished, ok := a.State().(crew.Finished); ok {
		if failed, ok := finished.End.(crew.EndFailed); ok && failed.Cause == crew.CauseTimeUp {
			h.halt = crew.Some(crew.HaltRunTimeLimit)
		}
	}
}

// actionAsked keeps on h when crew asked for its action named name, at,
// which the action's span starts at (KTD3).
func (s *step) actionAsked(h *heldRun, name crew.ActionName, at time.Time) {
	if s.m.statistics == nil {
		return
	}
	if h.starts == nil {
		h.starts = map[crew.ActionName]time.Time{}
	}
	h.starts[name] = at
}

// actionSpanEnded records the span of the action e ended in h's run, once
// applied: from when crew asked for it, or from its end when crew never
// did, to its end, by this process, with its outcome, its verdict and what
// it did (KTD2, KTD3, KTD10).
func (s *step) actionSpanEnded(h *heldRun, e crew.ActionEnded) {
	st := s.m.statistics
	if st == nil {
		return
	}
	start, asked := h.starts[e.Action]
	if !asked {
		start = e.At
	}
	s.command(RecordStatistic{Statistic: crew.ActionSpan{
		Issue: h.id(), Rule: h.run.Rule(), Run: h.run.ID(), Action: e.Action, Process: st.process,
		Start: start, End: e.At, Outcome: workOutcome(e.End), Verdict: e.Verdict, Work: s.m.work(h, e),
	}})
}

// workOutcome returns how an action that ended as end ended: stopped,
// stopped before its start or kept from running by the run time limit
// when a halt chose its end, else succeeded or failed (KTD5).
func workOutcome(end crew.ActionEnd) crew.WorkOutcome {
	failed, ok := end.(crew.EndFailed)
	if !ok {
		return crew.WorkSucceeded
	}
	switch failed.Cause {
	case crew.CauseStopped:
		return crew.WorkStopped
	case crew.CauseStoppedBeforeStart:
		return crew.WorkStoppedBeforeStart
	case crew.CauseTimeUp:
		return crew.WorkRunTimeLimit
	case crew.CauseSession, crew.CauseWorkspace, crew.CauseStart, crew.CausePrompt, crew.CauseShell,
		crew.CauseVerdict, crew.CauseFunction:
	}
	return crew.WorkFailed
}

// work returns what the action e ended in h's run did, with its kind's
// detail: a session's agent, harness, bot, prompt template's hash, start
// and the turns and tokens its harness reported, none when it reported
// none (R14); a script's exit status; a function's name and the verdict it
// returned (KTD7).
func (m *Model) work(h *heldRun, e crew.ActionEnded) crew.Work {
	a, _ := h.run.Action(e.Action)
	switch k := m.rules[h.rule].Action(e.Action).Kind.(type) {
	case crew.SessionSpec:
		return crew.SessionWork{
			Agent: k.Agent.Name, Harness: k.Agent.Harness, Bot: k.Bot.Name, PromptHash: k.Prompt.Hash(),
			Started: e.SessionStarted, Turns: e.Usage.Turns, Tokens: slices.Clone(e.Usage.TokenRecords()),
		}
	case crew.ShellSpec:
		shell, _ := a.Shell().Get()
		return crew.ShellWork{ExitStatus: shell.Status}
	case crew.FunctionSpec:
		function, _ := a.Function().Get()
		return crew.FunctionWork{Function: k.Function, Verdict: function.Verdict}
	case crew.QuestionSpec:
		return crew.QuestionWork{}
	case crew.ReturnSpec:
	}
	return crew.CheckWork{}
}

// stepWork is what the span of a route's shell or function step needs that
// no run event keeps: when crew asked for the step, and how its script or
// function ended, as the engine's input said (KTD3, KTD4).
type stepWork struct {
	asked   crew.Optional[time.Time]
	status  crew.Optional[int]
	verdict crew.Optional[crew.Verdict]
}

// keepStep changes what h keeps of its route's step at index i with
// change, when the model records statistics. A later input for the same
// step replaces what an earlier one kept (KTD4).
func (s *step) keepStep(h *heldRun, i int, change func(*stepWork)) {
	if s.m.statistics == nil {
		return
	}
	if h.steps == nil {
		h.steps = map[int]stepWork{}
	}
	w := h.steps[i]
	change(&w)
	h.steps[i] = w
}

// routeOpened records the open of the span of the route h's run just
// chose, before any of its steps (KTD2).
func (s *step) routeOpened(h *heldRun) {
	if s.m.statistics == nil {
		return
	}
	p, _ := h.run.Phase().(crew.RoutingPhase)
	s.command(RecordStatistic{Statistic: s.m.routeSpan(h, p)})
}

// routeEnded records the end of the span of the route h's run, released at
// at, ended through, with the run's outcome, when it chose one (KTD2,
// KTD5). The record carries the whole span.
func (s *step) routeEnded(h *heldRun, at time.Time) {
	if s.m.statistics == nil {
		return
	}
	released, _ := h.run.Phase().(crew.ReleasedPhase)
	p, routed := released.Route.Get()
	if !routed {
		return
	}
	sp := s.m.routeSpan(h, p)
	sp.End = crew.Some(crew.RouteEnd{At: at, Outcome: outcome(p)})
	s.command(RecordStatistic{Statistic: sp})
}

// routeSpan returns the span of p, the route of h's run, with no end:
// under the action at the run's cursor, with its verdict, when that action
// ended in this run, and under the run otherwise, as for a rule without
// actions or a run of its continued run's passed route alone (KTD9).
func (m *Model) routeSpan(h *heldRun, p crew.RoutingPhase) crew.RouteSpan {
	sp := crew.RouteSpan{
		Issue: h.id(), Rule: h.run.Rule(), Run: h.run.ID(), Process: m.statistics.process, Route: p.Route,
		Start: p.Chosen,
	}
	if a, ok := h.run.Cursor(); ok {
		if finished, ended := a.State().(crew.Finished); ended {
			sp.ChosenBy = crew.Some(crew.RouteChoice{Action: a.Name(), Verdict: finished.Verdict})
		}
	}
	return sp
}

// stepSpanEnded records the span of the shell or function step e ended in
// the route of h's run, with its exit status or verdict as the engine said:
// from when crew asked for it, or from its end when it ended unasked
// (KTD3, KTD4). A tracker step records nothing.
func (s *step) stepSpanEnded(h *heldRun, e crew.StepEnded) {
	p, routing := h.run.Phase().(crew.RoutingPhase)
	if s.m.statistics == nil || !routing || e.Step >= len(p.Steps) {
		return
	}
	kept := h.steps[e.Step]
	start, asked := kept.asked.Get()
	if !asked {
		start = e.At
	}
	plan := p.Steps[e.Step]
	sp := crew.StepSpan{
		Issue: h.id(), Rule: h.run.Rule(), Run: h.run.ID(), Index: e.Step, Process: s.m.statistics.process,
		Start: start, End: e.At, Outcome: stepWorkOutcome(e.Outcome),
	}
	switch plan.Kind {
	case crew.StepShell:
		sp.Name, sp.Work = plan.Shell, crew.ShellWork{ExitStatus: kept.status}
	case crew.StepFunction:
		route, _ := s.m.rules[h.rule].Route(p.Route)
		step, _ := route.Steps[e.Step].(crew.FunctionStep)
		sp.Name, sp.Work = plan.Function, crew.FunctionWork{Function: step.Function.Function, Verdict: kept.verdict}
	case crew.StepMove, crew.StepClose, crew.StepComment, crew.StepReport, crew.StepQuestion, crew.StepDelegate:
		return
	}
	s.command(RecordStatistic{Statistic: sp})
}

// stepWorkOutcome returns how a route's step that settled as o ended: ran,
// stopped while it ran, skipped as a stop kept it from starting, or failed
// (KTD5).
func stepWorkOutcome(o crew.StepOutcome) crew.WorkOutcome {
	switch o.(type) {
	case crew.StepRan:
		return crew.WorkSucceeded
	case crew.StepStopped:
		return crew.WorkStopped
	case crew.StepSkipped:
		return crew.WorkStoppedBeforeStart
	case crew.StepFailed, crew.StepLanded, crew.StepGivenUp, crew.StepDropped, nil:
	}
	return crew.WorkFailed
}
