package crew

import (
	"reflect"
	"slices"
	"testing"
)

// The history tests rebuild a past run of the test rule by deciding its
// facts, then read the start of the run after it.

// past is a run of the test rule on issue #9: its take, then the facts it
// decided, with def changed as change says when change is set. Crew
// crashed before it recorded its last drop events.
type past struct {
	take   RunEvent
	change func(RunDefinition) RunDefinition
	facts  []Fact
	drop   int
}

// events returns the events p recorded.
func (p past) events(t *testing.T) []RunEvent {
	t.Helper()
	def := sequence()
	if p.change != nil {
		def = p.change(def)
	}
	take := p.take
	if take == nil {
		take = taken()
	}
	events := []RunEvent{take}
	run := given(t, events)
	for _, f := range p.facts {
		got, err := Decide(run, def, f)
		if err != nil {
			t.Fatalf("Decide(%#v) = %v", f, err)
		}
		events = append(events, got...)
		run = given(t, events)
	}
	return events[:len(events)-p.drop]
}

// folded returns the history of events.
func folded(events ...[]RunEvent) *History {
	var h History
	for _, e := range seq(events...) {
		h.Fold(e)
	}
	return &h
}

// landed is the run's take that landed at minute 1.
func landed() Fact { return TakeSettled{FactHead: fh(1), Landed: true} }

// toJudge are the facts of a run whose install passed and whose lfg
// session passed, so judge runs.
func toJudge() []Fact {
	return []Fact{
		landed(), ready(false), shellEnded(3, "install", exited(0, "install passed")),
		SessionStarted{FactHead: fh(4), Action: "lfg"}, lfgEnded(succeeded("done"), nil),
	}
}

// judgeEnds are toJudge's facts, and judge's end after its exit status.
func judgeEnds(status int, reason string, more ...Fact) []Fact {
	return slices.Concat(toJudge(), []Fact{shellEnded(6, "judge", exited(status, reason))}, more)
}

// atAction is the start at action, in the run's workspace, after a run
// that ended through route for reason, whose latest session was lfg's.
func atAction(action ActionName, route RouteName, reason string) StartAt {
	return StartAt{
		Workspace: runWS(), Log: runLog, Action: action, Route: route, Reason: NewSessionText(reason),
		Session: Some(lfgLatest),
	}
}

func crashedAt(action ActionName) StartAt { return atAction(action, "", crashedReason) }

// passedAlone is the passed route alone, in the run's workspace.
var passedAlone = StartPassedRoute{Workspace: Some(runWS()), Log: runLog, Session: Some(lfgLatest)}

// judgeResumesSelf makes judge resume at itself.
func judgeResumesSelf(d RunDefinition) RunDefinition {
	spec, _ := d.Rule.Actions[2].Kind.(ShellSpec)
	spec.ResumeSelf = true
	d.Rule.Actions = slices.Clone(d.Rule.Actions)
	d.Rule.Actions[2].Kind = spec
	return d
}

// withoutJudge drops judge from the test rule.
func withoutJudge(d RunDefinition) RunDefinition {
	d.Rule.Actions = d.Rule.Actions[:2]
	return d
}

// startCase is a past run and the start of the run after it, for the
// test rule as rule changes it when rule is set.
type startCase struct {
	name string
	past past
	rule func(RunDefinition) RunDefinition
	want Start
}

// passedStarts are the branches of a run that chose passed.
func passedStarts() []startCase {
	return []startCase{
		{
			name: "ended through passed: fresh",
			past: past{facts: judgeEnds(0, "judge passed", settled(7, 0, StepLanded{}))},
			want: StartFresh{},
		},
		{
			name: "passed, its move dropped as the item moved meanwhile: fresh (KTD-S8)",
			past: past{facts: judgeEnds(0, "judge passed", settled(7, 0, StepDropped{Reason: "moved"}))},
			want: StartFresh{},
		},
		{
			name: "AE19: passed, its move given up after its final try: the passed route alone",
			past: past{facts: judgeEnds(0, "judge passed", settled(7, 0, StepGivenUp{Reason: "refused"}))},
			want: passedAlone,
		},
		{
			name: "passed, crashed before its move settled: the passed route alone",
			past: past{facts: judgeEnds(0, "judge passed")},
			want: passedAlone,
		},
		{
			name: "the last action went next and crew crashed before the route: the passed route alone",
			past: past{facts: judgeEnds(0, "judge passed"), drop: 2},
			want: passedAlone,
		},
	}
}

// restartStarts are the branches of a run that chose another route, or
// none: the action at its cursor decides where the next one restarts.
func restartStarts() []startCase {
	install := atAction("install", FailedRoute, "install failed")
	install.Session = Optional[LatestSession]{}
	return []startCase{
		{
			name: "AE11: needs-person at judge, a shell action after a session: at lfg",
			past: past{facts: judgeEnds(3, "needs a person", settled(7, 0, StepLanded{}))},
			want: atAction("lfg", "needs-person", "needs a person"),
		},
		{
			name: "AE6: failed at judge, after install and lfg: at lfg, skipping install",
			past: past{facts: judgeEnds(1, "no pull request", settled(7, 0, StepLanded{}), settled(8, 1, StepLanded{}))},
			want: atAction("lfg", FailedRoute, "no pull request"),
		},
		{
			name: "AE23: judge resumes at itself",
			past: past{change: judgeResumesSelf, facts: judgeEnds(3, "needs a person")},
			rule: judgeResumesSelf, want: atAction("judge", "needs-person", "needs a person"),
		},
		{
			name: "a shell action with no session before it: at itself",
			past: past{facts: []Fact{landed(), ready(false), shellEnded(3, "install", exited(1, "install failed"))}},
			want: install,
		},
		{
			name: "chose failed at lfg and crashed before its move: at lfg",
			past: past{facts: slices.Concat(toJudge()[:4], []Fact{lfgEnded(failedOutcome("gave up"), nil)})},
			want: atAction("lfg", FailedRoute, "gave up"),
		},
		{
			name: "crashed during judge, after a session: at judge",
			past: past{facts: toJudge()},
			want: crashedAt("judge"),
		},
		{
			name: "time up between lfg and judge: at judge, which never started (KTD-S7)",
			past: past{facts: slices.Concat(toJudge()[:4], []Fact{TimeUp{FactHead: fh(4)}, lfgEnded(succeeded("done"), nil)})},
			want: atAction("judge", FailedRoute, timeUpReason),
		},
		{
			name: "stopped while judge ran: at judge (KTD-S7)",
			past: past{facts: slices.Concat(toJudge(), []Fact{
				StopReached{FactHead: fh(6)}, shellEnded(7, "judge", ShellOutcome{Reason: NewCheckReason("stopped")}),
			})},
			want: atAction("judge", FailedRoute, "stopped"),
		},
	}
}

// crashStarts are the branches of a run crew did not live to see end.
func crashStarts() []startCase {
	lfg := crashedAt("lfg")
	lfg.Session = Optional[LatestSession]{}
	return []startCase{
		{
			name: "its last event is lfg's start: at lfg",
			past: past{facts: toJudge()[:3]},
			want: lfg,
		},
		{
			name: "install went next and crew crashed before lfg started: at lfg",
			past: past{facts: toJudge()[:3], drop: 1},
			want: lfg,
		},
		{
			name: "resumed at judge and crashed during it: at judge, acting as lfg's bot (KTD-S11)",
			past: past{take: resumedAt("judge"), facts: []Fact{landed(), ready(true)}},
			want: crashedAt("judge"),
		},
		{
			name: "crashed during judge, which the rule no longer has: fresh, naming judge",
			past: past{facts: toJudge()},
			rule: withoutJudge, want: StartWithoutAction{Action: "judge"},
		},
	}
}

// passedOnStarts are the branches of a run that started no action and
// opened no worktree of its own (KTD-S9).
func passedOnStarts() []startCase {
	return []startCase{
		{
			name: "stopped at its take: its start",
			past: past{take: resumedAt("judge"), facts: []Fact{StopReached{FactHead: fh(0)}, landed()}},
			want: startAt("judge"),
		},
		{
			name: "its take given up: its start",
			past: past{take: resumedAt("judge"), facts: []Fact{TakeSettled{FactHead: fh(1)}}},
			want: startAt("judge"),
		},
		{
			name: "its take landed after time-up: its start",
			past: past{take: resumedAt("judge"), facts: []Fact{TimeUp{FactHead: fh(0)}, landed()}},
			want: startAt("judge"),
		},
		{
			name: "crashed after its take: its start",
			past: past{take: resumedAt("judge")},
			want: startAt("judge"),
		},
		{
			name: "stopped while it reopened its start's worktree: its start",
			past: past{take: resumedAt("judge"), facts: []Fact{landed(), StopReached{FactHead: fh(1)}, ready(true)}},
			want: startAt("judge"),
		},
	}
}

// passedOnEdges are the passed-on starts that change on the way, and the
// run that does not pass its start on as it opened its own worktree.
func passedOnEdges() []startCase {
	other := Workspace{Name: "issue-9-implement-2", Branch: "crew/issue-9-implement-2"}
	fresh := StartAt{
		Workspace: other, Log: ".crew/logs/issue-9-implement-2.log", Action: "install",
		Reason: NewSessionText(crashedReason),
	}
	return []startCase{
		{
			name: "crashed after its take, its start's action no longer in the rule: fresh, naming it",
			past: past{take: resumedAt("judge")},
			rule: withoutJudge, want: StartWithoutAction{Action: "judge"},
		},
		{
			name: "crashed after a take that named a lost action: fresh",
			past: past{take: takenAs(StartWithoutAction{Action: "deploy"})},
			want: StartFresh{},
		},
		{
			name: "the passed route alone, its take given up: the passed route alone",
			past: past{take: takenAs(passedAlone), facts: []Fact{TakeSettled{FactHead: fh(1)}}},
			want: passedAlone,
		},
		{
			name: "a new worktree after its start's was missing is its own: at install in it",
			past: past{take: resumedAt("judge"), facts: []Fact{
				landed(), WorkspaceGone{FactHead: fh(2)},
				WorkspaceReady{FactHead: fh(3), Workspace: other, Log: ".crew/logs/issue-9-implement-2.log"},
			}},
			want: fresh,
		},
	}
}

// takenAs is the take of the test rule's three actions, starting as start
// says.
func takenAs(start Start) RunEvent {
	e, _ := taken().(RunTaken)
	e.Start = start
	return e
}

func TestTheStartOfARunFollowsItsLastRun(t *testing.T) {
	cases := slices.Concat(passedStarts(), restartStarts(), crashStarts(), passedOnStarts(), passedOnEdges())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule := sequence()
			if tc.rule != nil {
				rule = tc.rule(rule)
			}
			h := folded(tc.past.events(t))
			if got := h.Start(testID, rule.Rule); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Start =\n%#v\nwant\n%#v", got, tc.want)
			}
		})
	}
}

func TestWithoutALastRunARunStartsFresh(t *testing.T) {
	var h History
	if got := h.Start(testID, sequence().Rule); got != (StartFresh{}) {
		t.Errorf("Start = %#v, want fresh", got)
	}
}

func TestTheStartOfTheNextRunPassesTheLatestSessionsBotToJudge(t *testing.T) {
	h := folded(past{take: resumedAt("judge"), facts: []Fact{landed(), ready(true)}}.events(t))
	start, _ := h.Start(testID, sequence().Rule).(StartAt)
	next, _ := taken().(RunTaken)
	next.Run, next.Start = "run-3", start
	run := given(t, []RunEvent{next})
	head := func(n int) FactHead { return FactHead{Run: "run-3", At: at(n)} }
	events, err := Decide(run, sequence(), TakeSettled{FactHead: head(1), Landed: true})
	if err != nil {
		t.Fatal(err)
	}
	run = given(t, append([]RunEvent{next}, events...))
	events, err = Decide(run, sequence(), WorkspaceReady{
		FactHead: head(2), Workspace: runWS(), Log: runLog, Resumed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	asked, ok := events[len(events)-1].(ActionShellAsked)
	if !ok || asked.Action != "judge" || asked.Bot != developer {
		t.Errorf("events = %#v, want judge asked as lfg's bot", events)
	}
}

func TestTheLastRunIsTheOneWhoseEventsCameLast(t *testing.T) {
	h := folded([]RunEvent{
		RunTaken{EventHead: hh("run-1", 0), Issue: testIssue(), From: labelReady, To: labelRunning},
		RunReleased{EventHead: hh("run-1", 1)},
		taken(), takeMoved(),
	})
	run, ok := h.LastRun(testID, "implement")
	if !ok || run.ID() != testRun || run.Phase() != (RunningPhase{}) || len(run.Actions()) != 3 {
		t.Errorf("LastRun = %#v, %v, want run-2 running its three actions", run.Snapshot(), ok)
	}
	if _, ok := h.LastRun(testID, "fix"); ok {
		t.Errorf("fix has a last run, want none")
	}
}

func TestARunWithGapsFolds(t *testing.T) {
	h := folded([]RunEvent{
		WorkspaceOpened{EventHead: eh(2), Workspace: runWS(), Log: runLog},
		ActionShellAsked{EventHead: eh(5), Action: "judge", Bot: developer},
	})
	run, ok := h.LastRun(testID, "implement")
	if a, _ := run.Action("judge"); !ok || run.ID() != testRun || a.State() != (InShell{Started: at(5)}) {
		t.Errorf("LastRun = %#v, %v, want run-2 rebuilt with judge running", run.Snapshot(), ok)
	}
	want := StartAt{Workspace: runWS(), Log: runLog, Action: "judge", Reason: NewSessionText(crashedReason)}
	if got := h.Start(testID, sequence().Rule); !reflect.DeepEqual(got, want) {
		t.Errorf("Start = %#v, want %#v", got, want)
	}
	h = folded([]RunEvent{ActionShellAsked{EventHead: eh(5), Action: "judge"}})
	if got := h.Start(testID, sequence().Rule); got != (StartFresh{}) {
		t.Errorf("Start after a run without a worktree to reopen = %#v, want fresh", got)
	}
}

// hh returns the head of an event of run at minute n, of the rule
// implement on issue #9.
func hh(run RuleRunID, n int) EventHead {
	h := eh(n)
	h.Run = run
	return h
}

func TestARunThatOpensAWorktreeNameRetiresAnotherRulesRunThatHeldIt(t *testing.T) {
	failed := past{facts: judgeEnds(1, "no pull request")}.events(t)
	h := folded(failed)
	h.Retire(runWS().Name, testID, "implement")
	if got := h.Start(testID, sequence().Rule); got == (StartFresh{}) {
		t.Errorf("Start = fresh after the rule's own run opened its worktree, want the restart at lfg")
	}
	h.Retire(runWS().Name, testID, "fix")
	if got := h.Start(testID, sequence().Rule); got != (StartFresh{}) {
		t.Errorf("Start after fix opened issue-9-implement = %#v, want fresh", got)
	}
	if last, ok := h.LastRun(testID, "implement"); !ok || last.ID() != testRun {
		t.Errorf("LastRun after the retire = %v, want run-2, which the next run still continues", ok)
	}
	h.Fold(RunTaken{EventHead: hh("run-3", 9), Issue: testIssue(), Actions: []ActionName{"install"}})
	h.Fold(WorkspaceOpened{EventHead: hh("run-3", 9), Workspace: runWS(), Log: runLog})
	h.Fold(ActionShellAsked{EventHead: hh("run-3", 9), Action: "install"})
	if got, ok := h.Start(testID, sequence().Rule).(StartAt); !ok || got.Action != "install" {
		t.Errorf("Start after a new run = %#v, want the new run's restart at install", got)
	}
}

func TestARetiredPassedRouteRunsAloneWithoutAWorktree(t *testing.T) {
	h := folded(past{facts: judgeEnds(0, "judge passed", settled(7, 0, StepGivenUp{}))}.events(t))
	h.Retire(runWS().Name, IssueID{Repository: "R_1", Key: "10"}, "implement")
	want := StartPassedRoute{Session: Some(lfgLatest)}
	if got := h.Start(testID, sequence().Rule); !reflect.DeepEqual(got, want) {
		t.Errorf("Start = %#v, want %#v", got, want)
	}
}
