package crew

import (
	"slices"
	"testing"
	"time"
)

func TestTheZeroIssueAndRuleAreOfKindIssue(t *testing.T) {
	if got := (Issue{}).Kind(); got != KindIssue {
		t.Errorf("Issue{}.Kind() = %v, want %v", got, KindIssue)
	}
	if got := (Rule{}).Takes; got != KindIssue {
		t.Errorf("Rule{}.Takes = %v, want %v", got, KindIssue)
	}
}

func TestAnIssueReturnsTheFieldsItWasBuiltFrom(t *testing.T) {
	created := time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)
	i := NewIssue(IssueData{
		ID: IssueID{Repository: "R_1", Key: "42"}, Ref: "#42", Title: "Fix it", URL: "https://example.com/42",
		Created: created, Priority: 2, States: []State{"ready", "bug"}, Blocked: true, Kind: KindPullRequest,
	})
	if got := i.ID(); got != (IssueID{Repository: "R_1", Key: "42"}) {
		t.Errorf("ID() = %#v, want R_1/42", got)
	}
	if i.Ref() != "#42" || i.Title() != "Fix it" || i.URL() != "https://example.com/42" {
		t.Errorf("Ref(), Title(), URL() = %q, %q, %q, want #42, Fix it, https://example.com/42", i.Ref(), i.Title(), i.URL())
	}
	if !i.Created().Equal(created) || i.Priority() != 2 || !i.Blocked() || i.Kind() != KindPullRequest {
		t.Errorf("Created(), Priority(), Blocked(), Kind() = %v, %d, %v, %v, want %v, 2, true, %v",
			i.Created(), i.Priority(), i.Blocked(), i.Kind(), created, KindPullRequest)
	}
	if got := i.States(); !slices.Equal(got, []State{"ready", "bug"}) {
		t.Errorf("States() = %q, want [ready bug]", got)
	}
}

func TestChangingTheStatesGivenOrReturnedLeavesTheIssueUnchanged(t *testing.T) {
	states := []State{"ready"}
	i := NewIssue(IssueData{ID: IssueID{Key: "1"}, States: states})
	states[0] = "changed"
	i.States()[0] = "changed"
	d := i.Data()
	d.States[0] = "changed"
	if got := i.States(); !slices.Equal(got, []State{"ready"}) {
		t.Errorf("States() = %q after changing the given, returned and data states, want [ready]", got)
	}
}

func TestAnIssueInAnotherRepositoryKeepsItsOtherFields(t *testing.T) {
	i := NewIssue(IssueData{ID: IssueID{Key: "7"}, Ref: "#7", States: []State{"ready"}})
	moved := i.WithRepository("R_2")
	if got := moved.ID(); got != (IssueID{Repository: "R_2", Key: "7"}) {
		t.Errorf("WithRepository(R_2).ID() = %#v, want R_2/7", got)
	}
	if moved.Ref() != "#7" || !slices.Equal(moved.States(), []State{"ready"}) {
		t.Errorf("WithRepository changed the other fields: %#v", moved.Data())
	}
	if got := i.ID(); got != (IssueID{Key: "7"}) {
		t.Errorf("the original's ID() = %#v after WithRepository, want 7 without a repository", got)
	}
}
