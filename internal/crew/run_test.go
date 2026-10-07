package crew

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestTheTakeStartsARun(t *testing.T) {
	e := RunTaken{
		EventHead: eh(0), Issue: testIssue(), From: labelReady, To: labelRunning, Continues: Some[RuleRunID]("run-1"),
		Actions: []ActionTaken{{Name: "development", Resume: Some(resumePoint())}, {Name: "review"}},
	}
	run, err := Apply(RuleRun{}, e)
	if err != nil {
		t.Fatal(err)
	}
	continues, _ := run.Continues().Get()
	if run.ID() != testRun || continues != "run-1" || run.Issue().Ref() != "#9" || run.Rule() != "implement" ||
		!run.Taken().Equal(at(0)) || run.Stopping() || run.Phase() != (TakingPhase{}) {
		t.Errorf("run = %#v, want run-2 continuing run-1, taking #9 for implement at minute 0", run.Snapshot())
	}
	names := make([]ActionName, 0, len(e.Actions))
	for _, a := range run.Actions() {
		names = append(names, a.Name())
		if a.State() != (AwaitingTake{}) || a.Lookup() != (LookupNotAsked{}) {
			t.Errorf("action %s = %#v, want it awaiting the take", a.Name(), a)
		}
	}
	resume, ok := run.Actions()[0].Resume().Get()
	if !slices.Equal(names, []ActionName{"development", "review"}) || !ok || resume != resumePoint() {
		t.Errorf("actions = %v, development resumes %+v, want both in order, development resuming", names, resume)
	}
}

func TestApplyRefusesAnEventOfAnotherRun(t *testing.T) {
	run := given(t, preparing())
	e := RunStopped{EventHead: eh(3)}
	e.Run = "run-1"
	got, err := Apply(run, e)
	if !errors.Is(err, ErrRefused) || !reflect.DeepEqual(got, run) {
		t.Errorf("Apply = %v, %v, want a refusal and the run unchanged", got.Snapshot(), err)
	}
}

func TestApplyBuildsARunFromAnActionStartWithoutItsTake(t *testing.T) {
	run, err := Apply(RuleRun{}, ActionOpened{
		EventHead: eh(3), Action: "development", Workspace: ws("development"), Log: logOf("development"),
	})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := run.Action("development")
	if run.ID() != testRun || run.Issue().ID() != testID || run.Rule() != "implement" || !ok ||
		a.Workspace() != opened("development") {
		t.Errorf("run = %#v, want run-2 of #9 with development's workspace", run.Snapshot())
	}
}

func TestApplyLeavesTheRunItIsGivenAsItWas(t *testing.T) {
	check := func(n int, name CheckName, passed bool) RunEvent {
		return ActionCheckEnded{EventHead: eh(n), Action: "development", Result: CheckResult{Name: name, Passed: passed}}
	}
	// Three results, so an append in place would have room for a fourth.
	base := given(t, seq(preparing(), inChecks(), []RunEvent{
		check(6, "build", true), check(6, "test", true), check(6, "lint", true),
	}))
	before := base.Snapshot()
	one, _ := Apply(base, check(7, "vet", true))
	two, _ := Apply(base, check(7, "vet", false))
	a, _ := one.Action("development")
	b, _ := two.Action("development")
	if !reflect.DeepEqual(base.Snapshot(), before) || !a.Checks()[3].Passed || b.Checks()[3].Passed {
		t.Errorf("base = %#v, one = %v, two = %v, want each run its own", base.Snapshot(), a.Checks(), b.Checks())
	}
}

// snapshots are runs in each phase, for the snapshot tests.
var snapshots = map[string][]RunEvent{
	"taking":   {taken(bothActions()...)},
	"stopping": seq(preparing(), inChecks(), stopped(5)),
	"looking up": seq(lookingUp, []RunEvent{
		ActionLookupDone{EventHead: eh(6), Action: "development", PullRequest: foundPR},
	}),
	"ending": endedFailed,
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
	ending := given(t, endedFailed).Snapshot()
	running := ending
	running.Actions = slices.Clone(running.Actions)
	running.Actions[0].State = InSession{}
	twice := given(t, preparing()).Snapshot()
	twice.Actions[1].Name = "development"
	noID := ending
	noID.ID = ""
	noPhase := ending
	noPhase.Phase = nil
	for name, s := range map[string]RuleRunSnapshot{
		"ending with an action running": running, "an action twice": twice, "no id": noID, "no phase": noPhase,
	} {
		if _, err := RestoreRuleRun(s); err == nil {
			t.Errorf("RestoreRuleRun(%s) = nil, want an error", name)
		}
	}
}

func TestARunSharesNothingWithItsSnapshotsAndCopies(t *testing.T) {
	run := given(t, endedFailed)
	before := run.Snapshot()
	s := run.Snapshot()
	s.Actions[0].Checks = append(s.Actions[0].Checks, CheckResult{Name: "extra"})
	failures(t, s.Phase)[0].Action = "changed"
	s.Issue.States[0] = "changed"
	restored, _ := RestoreRuleRun(s)
	failures(t, restored.Phase())[0].Log = "changed"
	run.Actions()[0] = ActionRun{}
	failures(t, run.Phase())[1].Log = "changed"
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
	run := given(t, seq(preparing(), inSession("development"), []RunEvent{
		ActionSessionEnded{EventHead: eh(5), Action: "development", Outcome: succeeded("done"), Usage: usage},
		ActionLookupAsked{EventHead: eh(5), Action: "development"},
		ActionFinishing{EventHead: eh(5), Action: "development", End: EndSucceeded{Reason: NewSessionText("done")}},
	}))
	dev, _ := run.Action("development")
	started, _ := dev.SessionStarted().Get()
	w, _ := dev.Workspace().Get()
	if !started.Equal(at(4)) || !reflect.DeepEqual(dev.Usage(), usage) || dev.Spend() != usage.Spend() ||
		!w.Since().Equal(at(3)) || dev.Outcome() != succeeded("done") || dev.Ended() || dev.PullRequest() != nil {
		t.Errorf("development = %#v, want its session from minute 4, its usage, and its outcome waiting", dev)
	}
	review, _ := run.Action("review")
	if review.Spend() != (Spend{}) || review.Outcome() != (Outcome{}) {
		t.Errorf("review = %#v, want no spend and no outcome before its session", review)
	}
	resumed := given(t, seq(reopening(), []RunEvent{ActionOpened{
		EventHead: eh(3), Action: "development", Workspace: ws("development"), Resumed: true,
	}}))
	a, _ := resumed.Action("development")
	if w, _ := a.Workspace().Get(); !w.Since().IsZero() {
		t.Errorf("a reopened workspace's since = %v, want none", w.Since())
	}
}

func TestApplyBuildsARunFromASessionStartWithoutItsWorkspace(t *testing.T) {
	run, err := Apply(RuleRun{}, ActionSessionStarted{
		EventHead: eh(4), Action: "review", Workspace: ws("review"), Log: logOf("review"),
	})
	a, _ := run.Action("review")
	w, _ := a.Workspace().Get()
	if err != nil || a.State() != (InSession{}) || w.Workspace != ws("review") || w.Log != logOf("review") {
		t.Errorf("run = %#v, %v, want review in its session in its workspace", run.Snapshot(), err)
	}
}
