package crew

import (
	"cmp"
	"fmt"
	"testing"
	"uuid"
)

// seed returns a seed whose last byte is n, as a test stamps its inputs.
func seed(n byte) uuid.UUID {
	var s uuid.UUID
	s[15] = n
	return s
}

func TestIssueIDsOfTwoRepositoriesDiffer(t *testing.T) {
	a := IssueID{Repository: "R_one", Key: "42"}
	b := IssueID{Repository: "R_two", Key: "42"}
	if a == b {
		t.Fatalf("issue 42 of two repositories has one identity: %v", a)
	}
	runs := map[IssueID]string{a: "first", b: "second"}
	if len(runs) != 2 || runs[a] != "first" || runs[b] != "second" {
		t.Fatalf("issue 42 of two repositories shares a map key: %v", runs)
	}
}

func TestIssueIDPrintsItsKey(t *testing.T) {
	id := IssueID{Repository: "R_one", Key: "42"}
	if got := fmt.Sprintf("#%s", id); got != "#42" {
		t.Fatalf("issue id prints %q, want %q", got, "#42")
	}
}

func TestNewRuleRunID(t *testing.T) {
	first := NewRuleRunID(seed(1), 1)
	if again := NewRuleRunID(seed(1), 1); again != first {
		t.Fatalf("the same seed and index give %q and %q", first, again)
	}
	ids := map[RuleRunID]string{}
	for _, c := range []struct {
		seed  byte
		index int
	}{{1, 1}, {1, 2}, {2, 1}, {2, 2}} {
		id := NewRuleRunID(seed(c.seed), c.index)
		if id == "" {
			t.Fatalf("seed %d index %d gives an empty id", c.seed, c.index)
		}
		label := fmt.Sprintf("seed %d index %d", c.seed, c.index)
		if other, ok := ids[id]; ok {
			t.Fatalf("%s and %s give the same id %q", label, other, id)
		}
		ids[id] = label
	}
}

func TestPullRequestReportIDs(t *testing.T) {
	run := NewRuleRunID(seed(1), 1)
	take, verdict := run.TakeReport(), run.VerdictReport()
	if take == verdict {
		t.Fatalf("a run's take and verdict reports share the id %q", take)
	}
	if again := NewRuleRunID(seed(1), 1).TakeReport(); again != take {
		t.Fatalf("one run's take report has ids %q and %q", take, again)
	}
	if other := NewRuleRunID(seed(2), 1).TakeReport(); other == take {
		t.Fatalf("two runs' take reports share the id %q", take)
	}
}

func TestIssueIDCompare(t *testing.T) {
	ordered := []IssueID{{Repository: "R_a", Key: "10"}, {Repository: "R_a", Key: "9"}, {Repository: "R_b", Key: "1"}}
	for i, a := range ordered {
		for j, b := range ordered {
			if got, want := a.Compare(b), cmp.Compare(i, j); got != want {
				t.Fatalf("%#v.Compare(%#v) = %d, want %d", a, b, got, want)
			}
		}
	}
}
