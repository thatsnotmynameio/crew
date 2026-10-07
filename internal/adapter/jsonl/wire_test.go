package jsonl_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The wire names of version 3 are spelled out here, not taken from the
// adapter's constants, so a rename of the events, the causes, the starts
// or the step outcomes cannot change the journal's lines unnoticed.

func TestEveryEventKeepsItsWireType(t *testing.T) {
	j, root := journal(t)
	appendAll(t, j, everyEvent()...)

	want := []any{
		"run_taken", "take_moved", "run_stopped", "run_out_of_time", "workspace_asked", "workspace_missing",
		"workspace_asked", "workspace_opened", "action_session_asked", "action_session_started",
		"action_session_stop_asked", "action_session_ended", "action_ended", "action_function_asked",
		"action_function_stop_asked", "action_function_ended", "action_ended", "action_shell_asked",
		"action_shell_stop_asked", "action_shell_ended", "action_ended", "route_chosen", "lookup_asked",
		"lookup_done", "step_asked", "step_ended", "step_asked", "step_shell_stop_asked", "step_ended",
		"step_asked", "step_function_stop_asked", "step_ended", "run_released",
	}
	got := make([]any, 0, len(want))
	for _, l := range lines(t, root) {
		got = append(got, l["type"])
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("types =\n%v\nwant\n%v", got, want)
	}
}

func TestATakeKeepsItsWireKeys(t *testing.T) {
	j, root := journal(t)
	appendAll(t, j, takeEvents()[0])

	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"v":3,"type":"run_taken","time":"2026-10-07T09:00:00Z","run":"2026-10-07T09:00:00Z",` +
		`"rule_run":"development-1","issue":"9","ref":"#9","stage":"development","from":"ready",` +
		`"to":"in progress","title":"Fix the login","url":"https://example.test/issues/9",` +
		`"created":"2026-10-07T08:00:00Z","priority":2,"states":["ready"],"blocked":true,"kind":"pull_request",` +
		`"continues":"development-0","actions":[{"name":"install"},{"name":"lfg"},{"name":"judge"}],` +
		`"start":{"kind":"at","workspace":"issue-9-development","branch":"crew/issue-9-development",` +
		`"log":".crew/logs/issue-9-development.log","action":"lfg","route":"no-pr",` +
		`"reason":"no pull request was found","session":"lfg","bot":"crew-developer"}}` + "\n"
	if string(data) != want {
		t.Fatalf("journal:\n got %s\nwant %s", data, want)
	}
}

func TestARouteKeepsItsWireKeys(t *testing.T) {
	j, root := journal(t)
	appendAll(t, j, routeEvents()[0], routeEvents()[7])

	got := lines(t, root)
	steps, _ := got[0]["steps"].([]any)
	want := []any{
		map[string]any{"kind": "comment"}, map[string]any{"kind": "report"},
		map[string]any{"kind": "shell", "shell": "notify"}, map[string]any{"kind": "function", "function": "label"},
		map[string]any{"kind": "move", "to": "needs attention"}, map[string]any{"kind": "close"},
	}
	if got[0]["route"] != "failed" || got[0]["action"] != "judge" || !reflect.DeepEqual(steps, want) {
		t.Errorf("route_chosen line = %v, want route failed at judge with its six steps", got[0])
	}
	if got[1]["step"] != 2.0 || got[1]["outcome"] != "stopped" || got[1]["reason"] != "stopped" {
		t.Errorf("step_ended line = %v, want step 2 stopped", got[1])
	}
}

// wireValues appends events and returns the value of key on each line.
func wireValues(t *testing.T, key string, events []crew.RunEvent) []any {
	t.Helper()
	j, root := journal(t)
	appendAll(t, j, events...)
	out := make([]any, 0, len(events))
	for _, l := range lines(t, root) {
		out = append(out, l[key])
	}
	return out
}

func TestEveryFailureCauseKeepsItsWireName(t *testing.T) {
	causes := map[crew.FailureCause]string{
		crew.CauseSession: "session", crew.CauseStopped: "stopped",
		crew.CauseWorkspace: "workspace", crew.CauseStart: "start", crew.CausePrompt: "prompt",
		crew.CauseShell: "shell", crew.CauseVerdict: "verdict", crew.CauseStoppedBeforeStart: "stopped_before_start",
		crew.CauseTimeUp: "time_up", crew.CauseFunction: "function",
	}
	for cause, name := range causes {
		got := wireValues(t, "cause", []crew.RunEvent{crew.ActionEnded{
			EventHead: head(1), Action: "lfg", End: crew.EndFailed{Cause: cause},
		}})
		if got[0] != name {
			t.Errorf("cause %d is written %v, want %s", cause, got[0], name)
		}
	}
}

func TestEveryStartAndStepOutcomeKeepsItsWireName(t *testing.T) {
	starts := []crew.Start{
		crew.StartFresh{}, resumed, crew.StartPassedRoute{}, crew.StartWithoutAction{Action: "deploy"}, nil,
	}
	takes := make([]crew.RunEvent, 0, len(starts))
	for _, s := range starts {
		takes = append(takes, crew.RunTaken{EventHead: head(0), Start: s})
	}
	kinds := make([]any, 0, len(takes))
	for _, start := range wireValues(t, "start", takes) {
		s, _ := start.(map[string]any)
		kinds = append(kinds, s["kind"])
	}
	if want := []any{"fresh", "at", "passed_route", "without_action", "fresh"}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("start kinds = %v, want %v", kinds, want)
	}

	outcomes := []crew.StepOutcome{
		crew.StepLanded{}, crew.StepRan{}, crew.StepFailed{}, crew.StepGivenUp{}, crew.StepDropped{},
		crew.StepSkipped{}, crew.StepStopped{},
	}
	ends := make([]crew.RunEvent, 0, len(outcomes))
	for _, o := range outcomes {
		ends = append(ends, crew.StepEnded{EventHead: head(0), Outcome: o})
	}
	want := []any{"landed", "ran", "failed", "given_up", "dropped", "skipped", "stopped"}
	if got := wireValues(t, "outcome", ends); !reflect.DeepEqual(got, want) {
		t.Errorf("step outcomes = %v, want %v", got, want)
	}
}

func TestAFunctionsEndWritesTheVerdictItReturnedOrNone(t *testing.T) {
	got := wireValues(t, "verdict", []crew.RunEvent{
		crew.ActionFunctionEnded{EventHead: head(1), Action: "label", Outcome: crew.FunctionOutcome{
			Verdict: crew.Some(crew.Passed), Reason: crew.NewShellReason("label: passed"),
		}},
		crew.ActionFunctionEnded{EventHead: head(2), Action: "label", Outcome: crew.FunctionOutcome{
			Reason: crew.NewShellReason("label: timed out"),
		}},
	})
	if want := []any{"passed", nil}; !reflect.DeepEqual(got, want) {
		t.Errorf("verdicts = %v, want %v", got, want)
	}
}
