package engine_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// checkUse and stepUse are the places of the config that call the function
// pr-open: the action check, and the route step check.
const (
	checkUse crew.FunctionUse = "rules.implement.actions[1]"
	stepUse  crew.FunctionUse = "rules.promote.routes.passed[0]"
)

// prOpen is a call of the function pr-open for use, whose text parameter
// title is a template over the issue.
func prOpen(use crew.FunctionUse) crew.FunctionSpec {
	title, err := crew.ParseParameterTemplate("title", "PR for {{.Issue.Ref}}")
	if err != nil {
		panic(err)
	}
	return crew.FunctionSpec{Function: "pr-open", Use: use, Texts: []crew.TextParameter{{Name: "title", Template: title}}}
}

// checkedByFunction is develop whose session, acting as developer, the
// function action check follows.
var checkedByFunction = crew.Rule{
	Name: develop.Name, Labels: develop.Labels,
	Actions: []crew.Action{
		botSession(develop.Actions[0], "developer"), {Name: "check", Kind: prOpen(checkUse)},
	},
	Routes: develop.Routes,
}

// botSession is the session action a, acting as bot.
func botSession(a crew.Action, bot crew.BotName) crew.Action {
	spec, _ := a.Kind.(crew.SessionSpec)
	spec.Bot = crew.Bot{Name: bot}
	a.Kind = spec
	return a
}

// bind is a use's binding as the config builds it: each Decode it returns
// fills the fake function's settings from texts alone.
func bind(texts map[string]string) port.Decode {
	return func(target any) error {
		settings, ok := target.(*fake.FunctionSettings)
		if !ok {
			return fmt.Errorf("cannot decode into %T", target)
		}
		settings.Title = texts["title"]
		return nil
	}
}

// functionConfig is config for rule, whose function uses call f.
func functionConfig(t *testing.T, tr port.Tracker, f port.Function, rule crew.Rule) engine.Config {
	t.Helper()
	cfg := config(t, tr, rule)
	cfg.Functions = map[crew.FunctionUse]engine.Function{
		checkUse: {Function: f, Bind: bind}, stepUse: {Function: f, Bind: bind},
	}
	cfg.Identities = map[crew.BotName]port.Identity{"developer": {Bot: "developer", Login: "crew-developer[bot]"}}
	return cfg
}

// R29, KTD-F2, KTD13: a function action that returns passed ends the run
// through passed; its call carries its text rendered for the issue, the
// run's workspace and the identity of the latest session's bot.
func TestAFunctionActionThatReturnsPassedEndsThroughPassed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, f := fake.NewTracker(issue(1, ready)), fake.NewFunction()
		cfg := functionConfig(t, tr, f, checkedByFunction)

		reports, reason := checkedRun(t, tr, cfg)

		if len(reports) != 0 || reason != `the function action check returned "passed"` {
			t.Fatalf("reports = %+v, reason %q, want none and check returning passed", reports, reason)
		}
		if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
		calls := f.Calls()
		if len(calls) != 1 {
			t.Fatalf("calls = %+v, want one", calls)
		}
		c, dir := calls[0], filepath.Join(cfg.Root, ".crew", "worktrees", "issue-1-implement")
		if calls[0].Settings.Title != "PR for #1" || c.Call.IssueRef != "#1" || c.Call.IssueID != issueID("1") ||
			c.Call.IssueURL != "https://example.test/issues/1" || c.Call.Dir != dir || c.Call.Branch != branch ||
			c.Call.Identity.Bot != "developer" || c.Call.Identity.Login != "crew-developer[bot]" {
			t.Errorf("call = %+v, want #1's, in its workspace, as developer", c)
		}
	})
}

// R49, KTD-F12: a function that returns an error fails its action with
// crew's words and the error, scrubbed and stripped; the run's log holds
// the marker line naming the function, what it wrote and its error.
func TestAFunctionActionThatReturnsAnErrorFailsWithItsErrorStripped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, f := fake.NewTracker(issue(1, ready)), fake.NewFunction()
		cfg := functionConfig(t, tr, f, checkedByFunction)
		f.Script(fake.FunctionResult{
			Print: "looking for a pull request\n", Err: errors.New("no pull \x1b[31mrequest\x1b[0m\nin " + cfg.Root),
		})
		r := start(t, cfg)
		s := r.session()
		if _, err := s.Run().Output.Write([]byte("session output\n")); err != nil {
			t.Fatal(err)
		}
		s.End(port.SessionEnd{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if want := "the function action check failed: no pull request in ."; r.lastReason() != want {
			t.Errorf("reason = %q, want %q", r.lastReason(), want)
		}
		if got := tr.Reports(); len(got) != 1 {
			t.Errorf("reports = %+v, want one", got)
		}
		checkRunLog(t, cfg.Root, "issue-1-implement", "development", "session output\n"+
			"\ncrew: calling the function action check (pr-open)\nlooking for a pull request\n"+
			"crew: the function action check failed: no pull \x1b[31mrequest\x1b[0m\nin "+cfg.Root+"\n")
	})
}

// R53: a stop cancels a running function's context, and its action ends
// stopped.
func TestAStopEndsARunningFunctionAction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, f := fake.NewTracker(issue(1, ready)), fake.NewFunction()
		f.Script(fake.FunctionResult{Block: true})

		reports, reason := checkedRun(t, tr, functionConfig(t, tr, f, checkedByFunction))

		if len(reports) != 1 || reason != "the function action check was stopped" {
			t.Fatalf("reports = %+v, reason %q, want check stopped", reports, reason)
		}
	})
}

func TestAFunctionActionThatNeverEndsRunsOutOfTimeAfterTenMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, f := fake.NewTracker(issue(1, ready)), fake.NewFunction()
		f.Script(fake.FunctionResult{Block: true})
		r := start(t, functionConfig(t, tr, f, checkedByFunction))
		r.session().End(port.SessionEnd{Succeeded: true, Reason: "done"})
		synctest.Wait()

		time.Sleep(10*time.Minute + time.Second)
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if reports := tr.Reports(); len(reports) != 1 {
			t.Fatalf("reports = %+v, want one", reports)
		}
		if got, want := r.lastReason(), "the function action check ran out of time after 10m0s"; got != want {
			t.Errorf("reason = %q, want %q", got, want)
		}
	})
}

// KTD-F12: a use the engine has no function for ends its call as not
// started.
func TestAFunctionActionWhoseUseHasNoFunctionCannotStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := functionConfig(t, tr, fake.NewFunction(), checkedByFunction)
		cfg.Functions = nil

		reports, reason := checkedRun(t, tr, cfg)

		want := "the function action check could not start: crew has no function for rules.implement.actions[1]"
		if len(reports) != 1 || reason != want {
			t.Fatalf("reports = %+v, reason %q, want %q", reports, reason, want)
		}
	})
}

// checkedPromote is promote, a rule without actions, whose passed route
// calls the function step check before it moves the issue to ready to
// review.
var checkedPromote = crew.Rule{
	Name:   "promote",
	Labels: promote.Labels,
	Routes: []crew.Route{{Name: crew.PassedRoute, Steps: []crew.Step{
		crew.FunctionStep{Name: "check", Function: prOpen(stepUse)},
		crew.MoveStep{To: readyToReview},
	}}},
}

// promotedWith runs tr's issues under checkedPromote with f until the
// engine is idle, stops it and returns the rig.
func promotedWith(t *testing.T, tr *fake.Tracker, f port.Function) *rig {
	t.Helper()
	cfg := functionConfig(t, tr, f, checkedPromote)
	r := start(t, cfg)
	synctest.Wait()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return r
}

// R16, R49: a route's function step that returns anything but passed is a
// failed step in crew's words, and the route's final move still lands; a
// step's error never shows. A run without a workspace calls its function
// with no directory, and logs it in the log its workspace would have.
func TestARouteFunctionStepThatDoesNotPassFailsInCrewsWords(t *testing.T) {
	for name, tt := range map[string]struct {
		result fake.FunctionResult
		reason string
	}{
		"blocked": {fake.FunctionResult{Verdict: "blocked"}, `the route's function step check returned "blocked"`},
		"error":   {fake.FunctionResult{Err: errors.New("HTTP 403 @someone")}, "the route's function step check failed"},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr, f := fake.NewTracker(issue(1, ready)), fake.NewFunction()
				f.Script(tt.result)

				r := promotedWith(t, tr, f)

				checkStepFailed(t, r, tr, tt.reason)
				if calls := f.Calls(); len(calls) != 1 || calls[0].Call.Dir != "" || calls[0].Call.Branch != "" {
					t.Errorf("calls = %+v, want one without a workspace", calls)
				}
			})
		})
	}
}

// checkStepFailed fails unless r's route step check failed for reason, the
// route's move landed, and the log of the workspace #1's run would have
// holds the step's marker line.
func checkStepFailed(t *testing.T, r *rig, tr *fake.Tracker, reason string) {
	t.Helper()
	want := []crew.StepOutcome{crew.StepFailed{Reason: crew.NewShellReason(reason)}, crew.StepLanded{}}
	if got := r.stepOutcomes(); !reflect.DeepEqual(got, want) {
		t.Errorf("step outcomes = %#v, want %#v", got, want)
	}
	if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
		t.Errorf("#1 is in %v, want ready to review", got)
	}
	log, err := os.ReadFile(filepath.Join(r.root, ".crew", "logs", "issue-1-promote.log"))
	if err != nil || !strings.HasPrefix(string(log), "\ncrew: calling the route's function step check (pr-open)\n") {
		t.Errorf("log = %q, %v, want the step's marker line", log, err)
	}
}

// R53: a stop cancels a route's function step that runs, which is recorded
// as stopped, and the route's final move still happens.
func TestAStopCancelsARunningRouteFunctionStep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, f := fake.NewTracker(issue(1, ready)), fake.NewFunction()
		f.Script(fake.FunctionResult{Block: true})

		r := promotedWith(t, tr, f)

		want := []crew.StepOutcome{
			crew.StepStopped{Reason: crew.NewShellReason("the route's function step check was stopped")},
			crew.StepLanded{},
		}
		if got := r.stepOutcomes(); !reflect.DeepEqual(got, want) {
			t.Errorf("step outcomes = %#v, want %#v", got, want)
		}
		if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
	})
}

// meeting is a function whose calls each wait until as many calls as
// arrived counts are running at once, then run as f's.
type meeting struct {
	f       *fake.Function
	arrived sync.WaitGroup
}

func (m *meeting) Run(ctx context.Context, call port.FunctionCall) (crew.Verdict, error) {
	m.arrived.Done()
	m.arrived.Wait()
	return m.f.Run(ctx, call)
}

// KTD-F7: two calls of one use for different issues, at the same time,
// each decode their own text.
func TestTwoCallsOfOneUseAtOnceEachDecodeTheirOwnText(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, f := fake.NewTracker(issue(1, ready), issue(2, ready)), fake.NewFunction()
		m := &meeting{f: f}
		m.arrived.Add(2)

		promotedWith(t, tr, m)

		calls := f.Calls()
		if len(calls) != 2 {
			t.Fatalf("calls = %+v, want two", calls)
		}
		for _, c := range calls {
			if want := "PR for " + c.Call.IssueRef; c.Settings.Title != want {
				t.Errorf("the call for %s decoded the title %q, want %q", c.Call.IssueRef, c.Settings.Title, want)
			}
		}
	})
}
