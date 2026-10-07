package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// reviewClosed is the draft rules with review in a queue of no slots, so
// a listing that finds an issue in ready to review leaves it there.
func reviewClosed() []crew.Rule {
	return inQueues(draft(), defaultQueue(2), crew.Queue{Name: "review", Slots: 0})
}

// implemented runs #1 through implement with development's outcome and
// returns the ending commands, in flight.
func implemented(d *driver, development crew.Outcome) []core.Command {
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: development})
	return cmds
}

// in returns #1 in states.
func in(states ...crew.State) crew.Issue { return issue("1", 1, states...) }

// goneCase is a rule of #1 ending with outcome, then the next listing.
type goneCase struct {
	name     string
	rules    []crew.Rule
	outcome  crew.Outcome
	dropped  bool // the ending move is given up
	listed   []crew.Issue
	wantTo   crew.State
	wantGone bool
}

// run ends implement on #1 as c says, lists c.listed and checks the entry.
func (c goneCase) run(t *testing.T) {
	t.Helper()
	d := newDriver(t, c.rules, 2)
	ending := implemented(d, c.outcome)
	if c.dropped {
		d.send(core.CallResult{ID: moveID(t, ending, "1"), Result: core.ResultRefused, Reason: "nope"})
	} else {
		d.settle(ending)
	}
	if got := onlyEntry(t, d); got.Gone {
		t.Fatalf("entry gone before any listing: %#v", got)
	}

	d.poll(c.listed...)

	got := onlyEntry(t, d)
	if got.To != c.wantTo || got.Gone != c.wantGone {
		t.Fatalf("entry: got to %q, gone %v; want to %q, gone %v", got.To, got.Gone, c.wantTo, c.wantGone)
	}
}

func TestAHandledEntryIsGoneWhenTheNextListingDoesNotFindItAloneInARulesLabel(t *testing.T) {
	blocked := blockedIssue(in(readyToReview))
	blockedInReady := blockedIssue(in(ready))
	tests := []goneCase{
		{
			name: "found alone in the next rule's label", rules: reviewClosed(), outcome: succeeded,
			listed: []crew.Issue{in(readyToReview)}, wantTo: readyToReview,
		},
		{
			name: "missing from the listing", rules: draft(), outcome: succeeded,
			wantTo: readyToReview, wantGone: true,
		},
		{
			name: "blocked in the next rule's label", rules: draft(), outcome: succeeded,
			listed: []crew.Issue{blocked}, wantTo: readyToReview,
		},
		{
			name: "in the next rule's label and another crew state", rules: draft(), outcome: succeeded,
			listed: []crew.Issue{in(readyToReview, ready)}, wantTo: readyToReview, wantGone: true,
		},
		{
			name: "found alone in another rule's label", rules: draft(), outcome: succeeded,
			listed: []crew.Issue{blockedInReady}, wantTo: readyToReview, wantGone: true,
		},
		{
			name: "moved to a state that is no rule's label", rules: draft(), outcome: failed("tests fail"),
			wantTo: needsAttention,
		},
		{
			name: "its move to the next rule's label given up", rules: draft(), outcome: succeeded,
			dropped: true, wantTo: readyToReview, wantGone: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

func TestAListingRequestedBeforeTheEndingMoveLandedMarksNothing(t *testing.T) {
	d := newDriver(t, draft(), 2)
	ending := implemented(d, succeeded)
	if cmds, _ := d.send(core.Tick{}); len(cmds) == 0 {
		t.Fatal("tick issued no listing")
	}
	d.settle(ending)

	d.send(core.IssuesListed{})
	if got := onlyEntry(t, d); got.Gone {
		t.Fatalf("entry gone after a listing requested before its move landed: %#v", got)
	}

	d.poll()
	if got := onlyEntry(t, d); !got.Gone {
		t.Fatalf("entry not gone after a listing requested after its move landed: %#v", got)
	}
}

func TestAGoneEntryFoundAloneInItsLabelAgainIsNoLongerGone(t *testing.T) {
	d := newDriver(t, reviewClosed(), 2)
	d.settle(implemented(d, succeeded))
	d.poll()
	if got := onlyEntry(t, d); !got.Gone {
		t.Fatalf("entry not gone after a listing that missed it: %#v", got)
	}

	d.poll(in(readyToReview))
	if got := onlyEntry(t, d); got.Gone {
		t.Fatalf("entry still gone after a listing found it in its label: %#v", got)
	}
}

func TestAnIssueTakenAgainKeepsItsEntryAndItsNextEntryStartsNotGone(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.settle(implemented(d, succeeded))
	d.poll()
	if got := onlyEntry(t, d); !got.Gone {
		t.Fatalf("entry not gone after a listing that missed it: %#v", got)
	}

	take, _ := d.poll(in(ready))
	if got := onlyEntry(t, d); got.HeldBy != "implement" {
		t.Fatalf("entry while #1 is held again: got held by %q, want implement", got.HeldBy)
	}
	d.settle(take)
	d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: succeeded})
	ending, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "development", Outcome: succeeded})
	if cmds, _ := d.send(core.Tick{}); len(cmds) == 0 {
		t.Fatal("tick issued no listing")
	}
	d.settle(ending)
	if got := onlyEntry(t, d); got.Gone {
		t.Fatalf("new entry gone before any listing: %#v", got)
	}

	d.send(core.IssuesListed{})
	if got := onlyEntry(t, d); got.Gone {
		t.Fatalf("new entry gone after a listing requested before its move landed: %#v", got)
	}
}

// implementClosing is the draft rules with implement's passed route
// closing the issue.
func implementClosing() []crew.Rule {
	rules := draft()
	rules[0].Routes = append([]crew.Route(nil), rules[0].Routes...)
	rules[0].Routes[0] = crew.Route{Name: crew.PassedRoute, Steps: []crew.Step{crew.CloseStep{}}}
	return rules
}

// Covers R50, KTD23: a closed entry goes gone at the next listing, which
// cannot find a closed issue; one whose close was given up stays.
func TestAClosedEntryGoesGoneAtTheNextListing(t *testing.T) {
	for _, result := range []core.Result{core.ResultDone, core.ResultRefused} {
		t.Run(result.String(), func(t *testing.T) {
			d := newDriver(t, implementClosing(), 2)
			ending := implemented(d, succeeded)
			d.send(core.CallResult{ID: closeID(t, ending), Result: result, Reason: "nope"})
			if got := onlyEntry(t, d); got.Gone || got.To != "" {
				t.Fatalf("entry before any listing: %#v, want to no state, not gone", got)
			}

			d.poll()

			if got := onlyEntry(t, d); got.Gone != (result == core.ResultDone) {
				t.Fatalf("entry after the next listing: gone %v, want %v", got.Gone, result == core.ResultDone)
			}
		})
	}
}
