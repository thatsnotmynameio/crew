package claude

import (
	"io"
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// usageOf runs one session of p to its end and returns the usage it reports.
func usageOf(t *testing.T, p *fakeProcess) crew.Usage {
	t.Helper()
	h := build(t, noSection, &fakeSpawn{process: p})
	s, err := h.Start(t.Context(), port.Run{Dir: "/work", Prompt: "Implement #4", Output: io.Discard})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Wait()
	r, ok := s.(port.UsageReporter)
	if !ok {
		t.Fatalf("session %T is not a port.UsageReporter", s)
	}
	return r.Usage()
}

func TestUsageOfACleanResultIsItsCostTokensTurnsAndModel(t *testing.T) {
	got := usageOf(t, newProcess(fixture(t, "success.jsonl"), nil))

	want := crew.Usage{
		Cost: 0.4182, HasCost: true,
		Tokens:    crew.Tokens{Input: 14, Output: 132, CacheRead: 38216, CacheWrite: 5360},
		HasTokens: true,
		Turns:     3, HasTurns: true,
		Models: []string{"claude-opus-5-5"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

// The last result's cost and modelUsage cover the whole session, while each
// result's num_turns counts only its own query, and its usage only its last
// call.
func TestUsageOfSeveralResultsIsTheLastCostAndTokensAndEveryResultsTurns(t *testing.T) {
	got := usageOf(t, newProcess(fixture(t, "multiresult.jsonl"), nil))

	want := crew.Usage{
		Cost: 19.946884799999992, HasCost: true,
		Tokens: crew.Tokens{
			Input:      450 + 92,
			Output:     161694 + 23168,
			CacheRead:  45538985 + 1277724,
			CacheWrite: 894564 + 470360,
		},
		HasTokens: true,
		Turns:     42 + 1 + 21, HasTurns: true,
		Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

func TestUsageOfAnErrorResultWithoutModelUsageIsItsCostWithoutTokens(t *testing.T) {
	got := usageOf(t, newProcess(fixture(t, "error.jsonl"), exitError{code: 1, msg: "exit status 1"}))

	want := crew.Usage{Cost: 0.0311, HasCost: true, Turns: 1, HasTurns: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

func TestUsageReportsACostOfZero(t *testing.T) {
	line := `{"type":"result","subtype":"success","is_error":false,"num_turns":1,"result":"Nothing to do.",` +
		`"total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0},"modelUsage":{}}` + "\n"

	got := usageOf(t, newProcess([]byte(line), nil))

	want := crew.Usage{HasCost: true, Turns: 1, HasTurns: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

func TestUsageOfASessionWithoutAResultIsNothing(t *testing.T) {
	got := usageOf(t, newProcess(fixture(t, "noresult.jsonl"), exitError{code: 1, msg: "exit status 1"}))

	if !reflect.DeepEqual(got, crew.Usage{}) {
		t.Errorf("usage = %+v, want nothing", got)
	}
}

func TestUsageOfAProcessEndedByASignalAfterAResultIsNothing(t *testing.T) {
	got := usageOf(t, newProcess(fixture(t, "success.jsonl"), exitError{code: -1, msg: "signal: killed"}))

	if !reflect.DeepEqual(got, crew.Usage{}) {
		t.Errorf("usage = %+v, want nothing", got)
	}
}

func TestUsageOfASessionCrewStoppedAfterAResultIsNothing(t *testing.T) {
	p := newProcess(lines(t, "multiresult.jsonl", 3), nil) // up to the first result
	p.hang = true
	h := build(t, noSection, &fakeSpawn{process: p})
	s, err := h.Start(t.Context(), port.Run{Dir: "/work", Prompt: "Implement #4", Output: io.Discard})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	s.Wait()
	r, ok := s.(port.UsageReporter)
	if !ok {
		t.Fatalf("session %T is not a port.UsageReporter", s)
	}

	if got := r.Usage(); !reflect.DeepEqual(got, crew.Usage{}) {
		t.Errorf("usage = %+v, want nothing", got)
	}
}
