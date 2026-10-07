package fake_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// params returns a strict port.Decode over body, a YAML mapping, as the
// engine hands a function its rendered parameters.
func params(body string) port.Decode {
	return func(target any) error {
		dec := yaml.NewDecoder(strings.NewReader(body))
		dec.KnownFields(true)
		return dec.Decode(target)
	}
}

func TestFunctionReturnsItsScriptedVerdictAndRecordsTheCall(t *testing.T) {
	f := fake.NewFunction()
	f.Script(fake.FunctionResult{Verdict: "blocked"})
	call := port.FunctionCall{
		Params:   params(`{title: "Fix #7", count: 3}`),
		IssueRef: "#7",
		Dir:      "/tmp/worktree",
		Branch:   "crew/issue-7",
		Identity: port.Identity{Bot: "developer"},
	}

	verdict, err := f.Run(context.Background(), call)
	if err != nil || verdict != "blocked" {
		t.Fatalf("Run = %q, %v; want blocked", verdict, err)
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("recorded %d calls, want 1", len(calls))
	}
	got := calls[0]
	if want := (fake.FunctionSettings{Title: "Fix #7", Count: 3}); got.Settings != want {
		t.Errorf("recorded the parameters %+v, want %+v", got.Settings, want)
	}
	if got.Call.IssueRef != "#7" || got.Call.Dir != "/tmp/worktree" || got.Call.Branch != "crew/issue-7" ||
		got.Call.Identity.Bot != "developer" {
		t.Errorf("recorded the call %+v, want the one made", got.Call)
	}
}

func TestFunctionEndsEachCallAsScriptedInOrderThenPasses(t *testing.T) {
	f := fake.NewFunction()
	boom := errors.New("the CI API is down")
	f.Script(fake.FunctionResult{Err: boom}, fake.FunctionResult{Verdict: crew.Failed})

	if _, err := f.Run(context.Background(), port.FunctionCall{}); !errors.Is(err, boom) {
		t.Errorf("first Run error = %v, want %v", err, boom)
	}
	for _, want := range []crew.Verdict{crew.Failed, crew.Passed} {
		if got, err := f.Run(context.Background(), port.FunctionCall{}); err != nil || got != want {
			t.Errorf("Run = %q, %v; want %q", got, err, want)
		}
	}
}

func TestFunctionThatCannotDecodeItsParametersFails(t *testing.T) {
	f := fake.NewFunction()

	_, err := f.Run(context.Background(), port.FunctionCall{Params: params("{titel: Fix it}")})
	if err == nil || !strings.Contains(err.Error(), "titel") {
		t.Errorf("Run error = %v, want one naming titel", err)
	}
}

func TestFunctionPrintsToTheRunsLog(t *testing.T) {
	f := fake.NewFunction()
	f.Script(fake.FunctionResult{Print: "checked CI\n"})
	var log strings.Builder

	if _, err := f.Run(context.Background(), port.FunctionCall{Log: &log}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if log.String() != "checked CI\n" {
		t.Errorf("the log got %q, want the scripted line", log.String())
	}
}

func TestBlockingFunctionRunsUntilItsContextEnds(t *testing.T) {
	f := fake.NewFunction()
	f.Script(fake.FunctionResult{Block: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.Run(ctx, port.FunctionCall{})
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Run returned %v before its context ended", err)
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run error = %v, want one wrapping context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not end with its context")
	}
}

func TestFunctionRecordsConcurrentCalls(t *testing.T) {
	f := fake.NewFunction()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _, _ = f.Run(context.Background(), port.FunctionCall{IssueRef: "#1"}) })
	}
	wg.Wait()

	if got := len(f.Calls()); got != 8 {
		t.Errorf("recorded %d calls, want 8", got)
	}
}

func TestFunctionDefinitionDeclaresItsVerdictsAndBuildsTheFunction(t *testing.T) {
	f := fake.NewFunction()
	def := fake.FunctionDefinition(f, crew.Passed, "blocked")

	if want := []crew.Verdict{crew.Passed, "blocked"}; !slices.Equal(def.Verdicts, want) {
		t.Errorf("Verdicts = %v, want %v", def.Verdicts, want)
	}
	got, err := def.New(params("{title: Fix it, draft: true}"))
	if err != nil || got != port.Function(f) {
		t.Fatalf("New = %v, %v; want the fake", got, err)
	}
	if want := []fake.FunctionSettings{{Title: "Fix it", Draft: true}}; !slices.Equal(f.Builds(), want) {
		t.Errorf("Builds = %+v, want %+v", f.Builds(), want)
	}
	if _, err := def.New(params("{titel: Fix it}")); err == nil || !strings.Contains(err.Error(), "titel") {
		t.Errorf("New error = %v, want one naming titel", err)
	}
}

func TestFunctionDefinitionRefusesAParameterWhenScripted(t *testing.T) {
	f := fake.NewFunction()
	refusal := port.RefusedParameterError{Parameter: "count", Reason: "must be at least 1"}
	f.Refuse(refusal)

	got, err := fake.FunctionDefinition(f).New(params("{count: 0}"))
	var refused port.RefusedParameterError
	if !errors.As(err, &refused) || refused != refusal {
		t.Errorf("New error = %v, want %+v", err, refusal)
	}
	if got != nil {
		t.Errorf("New = %v, want none", got)
	}
}
