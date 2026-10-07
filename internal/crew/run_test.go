package crew

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestTheTakeStartsARun(t *testing.T) {
	e, _ := taken().(RunTaken)
	e.Continues = Some[RuleRunID]("run-1")
	run, err := Apply(RuleRun{}, e)
	if err != nil {
		t.Fatal(err)
	}
	continues, _ := run.Continues().Get()
	if run.ID() != testRun || continues != "run-1" || run.Issue().Ref() != "#9" || run.Rule() != "implement" ||
		!run.Taken().Equal(at(0)) || run.Stopping() || run.Phase() != (TakingPhase{}) ||
		run.WorkspaceState() != (NoWorkspace{}) || run.Lookup() != (LookupNotAsked{}) || run.Bot() != (Bot{}) {
		t.Errorf("run = %#v, want run-2 continuing run-1, taking #9 for implement at minute 0", run.Snapshot())
	}
	if got := states(run); !reflect.DeepEqual(got, []ActionRunState{AwaitingTurn{}, AwaitingTurn{}, AwaitingTurn{}}) {
		t.Errorf("actions = %#v, want three awaiting their turn", got)
	}
	if a, ok := run.Cursor(); !ok || a.Name() != "install" {
		t.Errorf("cursor = %#v, %v, want install", a, ok)
	}
}

func TestATakeThatResumesStartsAtTheResumePointsAction(t *testing.T) {
	run := given(t, []RunEvent{resumedAt("judge")})
	resume, _ := run.Resume().Get()
	if a, _ := run.Cursor(); a.Name() != "judge" || resume != resumePoint("judge") {
		t.Errorf("cursor %s, resume %#v, want judge resuming", a.Name(), resume)
	}
	want := []ActionRunState{DoneInEarlierRun{}, DoneInEarlierRun{}, AwaitingTurn{}}
	if got := states(run); !reflect.DeepEqual(got, want) {
		t.Errorf("actions = %#v, want install and lfg done in an earlier run", got)
	}
	gone := given(t, []RunEvent{resumedAt("deploy")})
	if a, _ := gone.Cursor(); a.Name() != "install" || !reflect.DeepEqual(states(gone)[0], AwaitingTurn{}) {
		t.Errorf("a resume at an action the rule lost starts at %s, want install", a.Name())
	}
	missing := given(t, seq(reopening(), []RunEvent{WorkspaceMissing{EventHead: eh(2), Workspace: runWS()}}))
	if a, _ := missing.Cursor(); a.Name() != "install" || !reflect.DeepEqual(states(missing)[0], AwaitingTurn{}) {
		t.Errorf("after a missing workspace the cursor is on %s, want install, which runs again", a.Name())
	}
}

// states returns the states of run's actions, in order.
func states(run RuleRun) []ActionRunState {
	out := make([]ActionRunState, 0, len(run.Actions()))
	for _, a := range run.Actions() {
		out = append(out, a.State())
	}
	return out
}

func TestApplyRefusesAnEventOfAnotherRun(t *testing.T) {
	run := given(t, asking())
	e := RunStopped{EventHead: eh(3)}
	e.Run = "run-1"
	got, err := Apply(run, e)
	if !errors.Is(err, ErrRefused) || !reflect.DeepEqual(got, run) {
		t.Errorf("Apply = %v, %v, want a refusal and the run unchanged", got.Snapshot(), err)
	}
}

func TestApplyBuildsARunFromAnActionStartWithoutItsTake(t *testing.T) {
	run, err := Apply(RuleRun{}, ActionShellAsked{EventHead: eh(3), Action: "judge", Bot: developer})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := run.Cursor()
	if run.ID() != testRun || run.Issue().ID() != testID || run.Rule() != "implement" || !ok ||
		a.State() != (InShell{Started: at(3)}) || run.Phase() != (RunningPhase{}) {
		t.Errorf("run = %#v, want run-2 of #9 running judge", run.Snapshot())
	}
}

func TestApplyLeavesTheRunItIsGivenAsItWas(t *testing.T) {
	base := given(t, installing())
	before := base.Snapshot()
	one, _ := Apply(base, ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "passed")})
	two, _ := Apply(base, ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(2, "failed")})
	a, _ := one.Action("install")
	b, _ := two.Action("install")
	if !reflect.DeepEqual(base.Snapshot(), before) || a.Shell() != Some(exited(0, "passed")) ||
		b.Shell() != Some(exited(2, "failed")) {
		t.Errorf("base = %#v, one = %v, two = %v, want each run its own", base.Snapshot(), a.Shell(), b.Shell())
	}
}

// snapshots are runs in each phase, for the snapshot tests.
var snapshots = map[string][]RunEvent{
	"taking":     {taken()},
	"resuming":   reopening(),
	"stopping":   seq(inSession(), stopped(5)),
	"in a shell": judging(),
	"looking up": seq(lookingUp, []RunEvent{RunLookupDone{EventHead: eh(7), PullRequest: foundPR}}),
	"ending":     endedFailed,
	"released": seq(endedFailed, []RunEvent{
		EndingDropped{EventHead: eh(8), To: labelFailed, Reason: "closed"}, FailureReported{EventHead: eh(8)},
		RunReleased{EventHead: eh(8)},
	}),
}

func TestASnapshotRestoresToAnEqualRun(t *testing.T) {
	for name, events := range snapshots {
		t.Run(name, func(t *testing.T) {
			run := given(t, events)
			restored, err := RestoreRuleRun(run.Snapshot())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored, run) {
				t.Errorf("restored %#v, want %#v", restored.Snapshot(), run.Snapshot())
			}
			if path, ok := plainData(reflect.ValueOf(run.Snapshot()), "snapshot"); !ok {
				t.Errorf("%s is not plain data", path)
			}
		})
	}
}

// plainData reports whether v holds no pointer, function, channel, map or
// unsafe pointer anywhere, and the path of the first it found. A time.Time
// is a value: its location pointer only names a zone.
func plainData(v reflect.Value, path string) (string, bool) {
	if v.Type() == reflect.TypeFor[time.Time]() {
		return "", true
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Chan, reflect.UnsafePointer, reflect.Map:
		return path, false
	case reflect.Interface:
		if v.IsNil() {
			return "", true
		}
		return plainData(v.Elem(), path)
	case reflect.Struct:
		return plainFields(v, path)
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			if p, ok := plainData(v.Index(i), path+"[]"); !ok {
				return p, false
			}
		}
	default:
	}
	return "", true
}

// plainFields reports whether every field of the struct v is plainData.
func plainFields(v reflect.Value, path string) (string, bool) {
	for i := range v.NumField() {
		if p, ok := plainData(v.Field(i), path+"."+v.Type().Field(i).Name); !ok {
			return p, false
		}
	}
	return "", true
}

func TestRestoreRejectsWhatIsNotARun(t *testing.T) {
	routing := given(t, passedAll()).Snapshot()
	running := routing
	running.Actions = slices.Clone(running.Actions)
	running.Actions[2].State = InShell{}
	twice := given(t, asking()).Snapshot()
	twice.Actions[1].Name = "install"
	noID := routing
	noID.ID = ""
	noPhase := routing
	noPhase.Phase = nil
	noWorkspace := routing
	noWorkspace.Workspace = nil
	noLookup := routing
	noLookup.Lookup = nil
	noState := given(t, asking()).Snapshot()
	noState.Actions[0].State = nil
	pastTheEnd := routing
	pastTheEnd.Cursor = 3
	for name, s := range map[string]RuleRunSnapshot{
		"routing with an action running": running, "an action twice": twice, "no id": noID, "no phase": noPhase,
		"no workspace state": noWorkspace, "no lookup": noLookup, "an action without a state": noState,
		"a cursor past the actions": pastTheEnd,
	} {
		if _, err := RestoreRuleRun(s); err == nil {
			t.Errorf("RestoreRuleRun(%s) = nil, want an error", name)
		}
	}
	if _, err := RestoreRuleRun(given(t, []RunEvent{takenWithoutActions()}).Snapshot()); err != nil {
		t.Errorf("RestoreRuleRun(a run without actions) = %v, want it restored", err)
	}
}

func TestARunSharesNothingWithItsSnapshotsAndCopies(t *testing.T) {
	run := given(t, endedFailed)
	before := run.Snapshot()
	s := run.Snapshot()
	s.Actions[1].Usage.Models = append(s.Actions[1].Usage.Models, "extra")
	failures(t, s.Phase)[0].Action = "changed"
	s.Issue.States[0] = "changed"
	restored, _ := RestoreRuleRun(s)
	failures(t, restored.Phase())[0].Log = "changed"
	run.Actions()[0] = ActionRun{}
	failures(t, run.Phase())[0].Log = "changed"
	if !reflect.DeepEqual(run.Snapshot(), before) {
		t.Errorf("run = %#v, want it unchanged: %#v", run.Snapshot(), before)
	}
	if failures(t, s.Phase)[0].Log == "changed" {
		t.Error("the restored run shares its failures with the snapshot")
	}
}

// failures returns the failures of an ending phase.
func failures(t *testing.T, p RunPhase) []ActionFailure {
	t.Helper()
	j, ok := p.(EndingPhase)
	if !ok {
		t.Fatalf("phase = %#v, want ending", p)
	}
	return j.Ending.Failures
}

func TestAnActionRunTellsWhatItRecorded(t *testing.T) {
	run := given(t, seq(judging(), []RunEvent{
		ActionShellEnded{EventHead: eh(6), Action: "judge", Outcome: exited(3, "judge: ask")},
		ActionEnded{
			EventHead: eh(6), Action: "judge", Verdict: "needs_person", Target: ToRoute{Route: "needs-person"},
			End: EndSucceeded{Reason: NewSessionText("judge: ask")},
		},
		chose(6, "needs-person", "judge"),
	}))
	lfg, _ := run.Action("lfg")
	started, _ := lfg.SessionStarted().Get()
	if !started.Equal(at(4)) || !reflect.DeepEqual(lfg.Usage(), usage) || lfg.Spend() != usage.Spend() ||
		!lfg.Ended() || run.Bot() != developer {
		t.Errorf("lfg = %#v, want its session from minute 4, its usage, ended, and its bot the run's", lfg)
	}
	judge, _ := run.Cursor()
	want := Finished{
		End: EndSucceeded{Reason: NewSessionText("judge: ask")}, Verdict: "needs_person",
		Target: ToRoute{Route: "needs-person"},
	}
	if judge.Name() != "judge" || judge.State() != want || judge.Shell() != Some(exited(3, "judge: ask")) ||
		judge.Spend() != (Spend{}) {
		t.Errorf("judge = %#v, want its verdict, target and exit status, and no spend", judge)
	}
	if w, _ := run.Workspace().Get(); w != opened() || !w.Since().Equal(at(2)) {
		t.Errorf("workspace = %#v, want the run's, made at minute 2", w)
	}
	resumed := given(t, seq(reopening(), []RunEvent{WorkspaceOpened{
		EventHead: eh(2), Workspace: runWS(), Log: runLog, Resumed: true,
	}}))
	if w, _ := resumed.Workspace().Get(); !w.Since().IsZero() {
		t.Errorf("a reopened workspace's since = %v, want none", w.Since())
	}
}

func TestARouteChosenLeavesTheActionsItDidNotReachNotRun(t *testing.T) {
	run := given(t, seq(installing(), []RunEvent{
		ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(2, "install failed")},
		ActionEnded{
			EventHead: eh(3), Action: "install", Verdict: Failed, Target: toFailed,
			End: EndFailed{Reason: NewSessionText("install failed"), Cause: CauseShell},
		},
		chose(3, FailedRoute, "install"),
	}))
	got := states(run)
	if !reflect.DeepEqual(got[1:], []ActionRunState{NotRun{}, NotRun{}}) || !run.ActionsEnded() {
		t.Errorf("actions = %#v, want lfg and judge not run, and the sequence over", got)
	}
	if a, _ := run.Cursor(); a.Name() != "install" || run.Phase() != (RoutingPhase{Route: FailedRoute, Chosen: at(3)}) {
		t.Errorf("cursor %s, phase %#v, want install and failed", a.Name(), run.Phase())
	}
	if given(t, inSession()).ActionsEnded() {
		t.Error("a run whose session runs has ended its actions")
	}
}
