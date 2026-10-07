package crew

import (
	"reflect"
	"testing"
)

func TestPullRequestReportReturnsWhatItWasBuiltFrom(t *testing.T) {
	end := NewRuleEnd("development", []ActionStatus{{Name: "lfg", State: ActionSucceeded{}}})
	r := NewPullRequestReport(PullRequestReportData{
		ID: "7", IssueID: IssueID{Key: "42"}, IssueRef: "#42", State: "crew:done", End: Some(end),
	})
	got, ok := r.End().Get()
	if r.ID() != "7" || r.IssueID() != (IssueID{Key: "42"}) || r.IssueRef() != "#42" || r.State() != "crew:done" ||
		!ok || got.Rule() != "development" || !reflect.DeepEqual(got.Actions(), end.Actions()) {
		t.Errorf("report = %+v, want the fields it was built from", r)
	}
	if _, ok := NewPullRequestReport(PullRequestReportData{ID: "1", State: "crew:in progress"}).End().Get(); ok {
		t.Error("a take report has an end")
	}
}

func TestRuleEndSharesNoActions(t *testing.T) {
	built := []ActionStatus{
		{Name: "lfg", State: ActionFailed{}, Checks: []CheckResult{{Name: "judge", Reason: NewCheckReason("unfinished")}}},
	}
	end := Some(NewRuleEnd("development", built))
	r := NewPullRequestReport(PullRequestReportData{ID: "7", State: "crew:failed", End: end})
	built[0].Name = "built"
	got, _ := r.End().Get()
	returned := got.Actions()
	returned[0].Name = "other"
	returned[0].Checks[0].Reason = NewCheckReason("changed")

	got, _ = r.End().Get()
	if got := got.Actions(); got[0].Name != "lfg" || got[0].Checks[0].Reason.String() != "unfinished" {
		t.Fatalf("changing the actions it was built from or returned changed the report: %+v", got)
	}
}

func TestRuleEndFailedWhenAnyActionFailed(t *testing.T) {
	ok := NewRuleEnd("development", []ActionStatus{{State: ActionSucceeded{}}, {State: ActionSucceeded{}}})
	failed := NewRuleEnd("development", []ActionStatus{{State: ActionSucceeded{}}, {State: ActionFailed{}}})
	if ok.Failed() || !failed.Failed() {
		t.Fatalf("Failed() = %v, %v, want false, true", ok.Failed(), failed.Failed())
	}
}
