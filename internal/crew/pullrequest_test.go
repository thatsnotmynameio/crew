package crew

import (
	"reflect"
	"testing"
)

func TestPullRequestReportReturnsWhatItWasBuiltFrom(t *testing.T) {
	end := NewRuleEnd("development", "blocked", []ActionStatus{{Name: "lfg", State: ActionSucceeded{Verdict: "blocked"}}})
	r := NewPullRequestReport(PullRequestReportData{
		ID: "7", IssueID: IssueID{Key: "42"}, IssueRef: "#42", State: "crew:blocked", End: Some(end),
	})
	got, ok := r.End().Get()
	if r.ID() != "7" || r.IssueID() != (IssueID{Key: "42"}) || r.IssueRef() != "#42" || r.State() != "crew:blocked" ||
		!ok || got.Rule() != "development" || got.Route() != "blocked" || !reflect.DeepEqual(got.Actions(), end.Actions()) {
		t.Errorf("report = %+v, want the fields it was built from", r)
	}
	if _, ok := NewPullRequestReport(PullRequestReportData{ID: "1", State: "crew:in progress"}).End().Get(); ok {
		t.Error("a take report has an end")
	}
}

func TestRuleEndSharesNoActions(t *testing.T) {
	built := []ActionStatus{{Name: "lfg", State: ActionFailed{}, Shell: NewShellReason("unfinished")}}
	end := Some(NewRuleEnd("development", FailedRoute, built))
	r := NewPullRequestReport(PullRequestReportData{ID: "7", State: "crew:failed", End: end})
	built[0].Name = "built"
	got, _ := r.End().Get()
	returned := got.Actions()
	returned[0].Name = "other"

	got, _ = r.End().Get()
	if got := got.Actions(); got[0].Name != "lfg" || got[0].Shell.String() != "unfinished" {
		t.Fatalf("changing the actions it was built from or returned changed the report: %+v", got)
	}
}
