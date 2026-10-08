package app_test

import (
	"context"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

// withFunctions is a config whose rule runs a session, then the function
// pull-request through the preset open-pr, and whose passed route calls
// the function again as a step before it moves the issue.
const withFunctions = `
tracker:
  name: fake
agents:
  developer:
    harness:
      name: fake
actions:
  open-pr:
    name: pull-request
    title: "Draft for {{.Issue.Ref}}"
    count: 1
rules:
  implement:
    labels:
      ready: ready
      running: in progress
    actions:
      - name: development
        prompt: "Implement development for issue {{.Issue.Ref}}"
      - open-pr:
          title: "Fixes {{.Issue.Ref}}"
    routes:
      passed:
        - pull-request:
            draft: true
        - move: ready to review
      failed: [report, move: needs attention]
`

// withFunction registers f as the function pull-request, declaring
// blocked, in r beside its tracker and harness.
func withFunction(r *crewRun, tracker port.Tracker, harness port.Harness, f *fake.Function) {
	r.opts.Registry = registry.New(
		map[string]port.TrackerFactory{"fake": fake.TrackerFactory(tracker)},
		map[string]port.HarnessFactory{"fake": fake.HarnessFactory(harness)},
		map[string]port.FunctionDefinition{"pull-request": fake.FunctionDefinition(f, "blocked")},
		nil,
	)
}

// R26 to R29: crew builds each use of a function once, from its merged
// parameters, then calls it as an action and as a route step with its
// text rendered for the issue.
func TestAFunctionRunsAsAnActionAndAsARouteStep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue("1", ready))
		h, f := fake.NewHarness(), fake.NewFunction()
		r := options(t, withFunctions, tr, h)
		withFunction(r, tr, h, f)
		r.opts.Plain = true
		r.start()

		next(t, h).End(success)
		synctest.Wait()
		r.signals <- syscall.SIGTERM

		if code := <-r.code; code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
		}
		wantBuilds := []fake.FunctionSettings{
			{Title: "Fixes #42", Count: 1},
			{Draft: true},
		}
		if got := f.Builds(); !reflect.DeepEqual(got, wantBuilds) {
			t.Errorf("builds = %+v, want one per use, %+v", got, wantBuilds)
		}
		calls := f.Calls()
		if len(calls) != 2 {
			t.Fatalf("calls = %d, want 2 (the action, then the step)", len(calls))
		}
		if got, want := calls[0].Settings, (fake.FunctionSettings{Title: "Fixes #1", Count: 1}); got != want {
			t.Errorf("the action's parameters = %+v, want %+v", got, want)
		}
		if got, want := calls[1].Settings, (fake.FunctionSettings{Draft: true}); got != want {
			t.Errorf("the step's parameters = %+v, want %+v", got, want)
		}
		if got := states(t, tr); !reflect.DeepEqual(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
	})
}

// Covers AE9 through the wiring: a use whose parameter its function does
// not take stops crew at startup, before any listing, with the config's
// exit code, naming the file, the parameter's key path and its line.
func TestAE9AnUnknownParameterExitsTwoNamingItsLine(t *testing.T) {
	body := strings.Replace(withFunctions, `title: "Fixes {{.Issue.Ref}}"`, `titel: "Fixes {{.Issue.Ref}}"`, 1)
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	h := fake.NewHarness()
	r := options(t, body, tr, h)
	withFunction(r, tr, h, fake.NewFunction())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	want := ".crew/config.yaml: rules.implement.actions[1].open-pr.titel (line 22): unknown key"
	if stderr := r.stderr.String(); !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
}

// R28, KTD-F11: a value the function refuses stops crew at startup,
// naming the line of the parameter, written in the preset.
func TestARefusedParameterExitsTwoNamingItsLine(t *testing.T) {
	tr := fake.NewTracker(issue("1", ready))
	h, f := fake.NewHarness(), fake.NewFunction()
	f.Refuse(port.RefusedParameterError{Parameter: "count", Reason: "must be at least 2"})
	r := options(t, withFunctions, tr, h)
	withFunction(r, tr, h, f)

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	want := ".crew/config.yaml: actions.open-pr.count (line 12): must be at least 2"
	if stderr := r.stderr.String(); !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
}
