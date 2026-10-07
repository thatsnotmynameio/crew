package core_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The labels of the fix review rule, which takes pull requests.
const (
	fixReviewReady crew.State = "fix review ready"
	fixing         crew.State = "fixing review"
)

// withFixReview is the draft rules plus a fix review rule that takes
// pull requests.
func withFixReview() []crew.Rule {
	return append(draft(), crew.Rule{
		Name:    "fix review",
		Labels:  crew.Labels{Ready: fixReviewReady, Running: fixing, Success: readyToReview, Failure: needsAttention},
		Takes:   crew.KindPullRequest,
		Actions: []crew.Action{{Name: "fix", Prompt: parsedPrompt("fix", "Fix the review comments on {{.Issue.Ref}}")}},
	})
}

// pr90 returns pull request #90, opened minute minutes after t0.
func pr90(minute int, states ...crew.State) crew.Issue {
	return pullRequest(issue("90", minute, states...))
}

// otherKinds returns the IssueOfOtherKind events in events.
func otherKinds(events []core.Published) []core.Published {
	var out []core.Published
	for _, e := range events {
		if _, ok := e.(core.IssueOfOtherKind); ok {
			out = append(out, e)
		}
	}
	return out
}

// Covers AE1 of #92.
func TestAPullRequestInTheLabelOfARuleThatTakesIssuesIsLeftAloneWithANotice(t *testing.T) {
	d := newDriver(t, draft(), 2)

	cmds, events := d.poll(pr90(1, ready))
	wantCommands(t, cmds)
	wantEvents(t, otherKinds(events), core.IssueOfOtherKind{
		At: d.now, IssueID: issueID("90"), IssueRef: "#90", Kind: crew.KindPullRequest,
		Label: ready, Rule: "implement", Takes: crew.KindIssue,
	})
	wantHeld(t, d.m)
}

// Covers AE2 of #92.
func TestARuleThatTakesPullRequestsTakesAPullRequestInItsLabel(t *testing.T) {
	d := newDriver(t, withFixReview(), 2)

	cmds, events := d.poll(pr90(1, fixReviewReady))
	wantCommands(t, cmds, core.Move{IssueID: issueID("90"), From: fixReviewReady, To: fixing})
	hasEvent(t, events, d.taken(1, pr90(1, fixReviewReady), "fix review", fixReviewReady, fixing, "fix"))
	if n := otherKinds(events); n != nil {
		t.Fatalf("notices for a pull request of the rule's kind: %#v", n)
	}
	wantHeld(t, d.m, "90")
}

// Covers AE3 of #92.
func TestAnIssueInTheLabelOfARuleThatTakesPullRequestsGetsTheNoticeOnce(t *testing.T) {
	d := newDriver(t, withFixReview(), 2)

	cmds, events := d.poll(issue("42", 1, fixReviewReady))
	wantCommands(t, cmds)
	wantEvents(t, otherKinds(events), core.IssueOfOtherKind{
		At: d.now, IssueID: issueID("42"), IssueRef: "#42", Kind: crew.KindIssue,
		Label: fixReviewReady, Rule: "fix review", Takes: crew.KindPullRequest,
	})

	cmds, events = d.poll(issue("42", 1, fixReviewReady))
	wantCommands(t, cmds)
	wantEvents(t, otherKinds(events))
	wantHeld(t, d.m)
}

func TestTheNoticeShowsAgainOnceAListingFoundTheItemWithoutTheLabel(t *testing.T) {
	tests := []struct {
		name    string
		between []crew.Issue
	}{
		{name: "the item was not listed", between: nil},
		{name: "the item was in another state", between: []crew.Issue{pr90(1, needsAttention)}},
		{name: "the item carried two crew labels", between: []crew.Issue{pr90(1, ready, needsAttention)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, draft(), 2)
			_, events := d.poll(pr90(1, ready))
			if len(otherKinds(events)) != 1 {
				t.Fatalf("first listing: notices %#v, want one", otherKinds(events))
			}

			_, events = d.poll(tt.between...)
			wantEvents(t, otherKinds(events))

			_, events = d.poll(pr90(1, ready))
			wantEvents(t, otherKinds(events), core.IssueOfOtherKind{
				At: d.now, IssueID: issueID("90"), IssueRef: "#90", Kind: crew.KindPullRequest,
				Label: ready, Rule: "implement", Takes: crew.KindIssue,
			})
		})
	}
}

func TestAnItemMovedToTheLabelOfAnotherRuleOfTheOtherKindGetsANewNotice(t *testing.T) {
	d := newDriver(t, draft(), 2)
	d.poll(pr90(1, ready))

	_, events := d.poll(pr90(1, readyToReview))
	wantEvents(t, otherKinds(events), core.IssueOfOtherKind{
		At: d.now, IssueID: issueID("90"), IssueRef: "#90", Kind: crew.KindPullRequest,
		Label: readyToReview, Rule: "review", Takes: crew.KindIssue,
	})
}

func TestAFailedOrSkippedListingDoesNotRepeatTheNotice(t *testing.T) {
	d := newDriver(t, draft(), 1)
	cmds, events := d.poll(issue("1", 1, ready), pr90(2, ready))
	if len(otherKinds(events)) != 1 {
		t.Fatalf("first listing: notices %#v, want one", otherKinds(events))
	}
	d.settle(cmds)

	// Every slot is busy: the tick lists nothing.
	cmds, _ = d.send(core.Tick{})
	wantListings(t, cmds, 0)
	// Releasing #1 lists at once; that listing fails.
	cmds, _ = d.release("1")
	wantListings(t, cmds, 1)
	d.send(core.ListFailed{Reason: "timeout"})

	_, events = d.poll(pr90(2, ready))
	wantEvents(t, otherKinds(events))
}

// Covers AE5 of #92: the mirror gave #90 the label of the issue it closes,
// whose rule takes issues.
func TestAMirroredPullRequestGetsTheNoticeWhileItsIssueIsTaken(t *testing.T) {
	d := newDriver(t, draft(), 2)

	cmds, events := d.poll(issue("42", 1, ready), pr90(2, ready))
	wantCommands(t, cmds, core.Move{IssueID: issueID("42"), From: ready, To: inProgress})
	wantEvents(t, otherKinds(events), core.IssueOfOtherKind{
		At: d.now, IssueID: issueID("90"), IssueRef: "#90", Kind: crew.KindPullRequest,
		Label: ready, Rule: "implement", Takes: crew.KindIssue,
	})
	wantHeld(t, d.m, "42")
}

func TestAnItemWithTwoCrewLabelsGetsOnlyTheTwoLabelSkip(t *testing.T) {
	d := newDriver(t, withFixReview(), 2)

	cmds, events := d.poll(issue("42", 1, ready, fixReviewReady))
	wantCommands(t, cmds)
	hasEvent(t, events, core.IssueSkipped{
		At: d.now, IssueID: issueID("42"), IssueRef: "#42", States: []crew.State{ready, fixReviewReady},
	})
	wantEvents(t, otherKinds(events))
}

func TestABlockedIssueInTheLabelOfARuleThatTakesPullRequestsGetsTheNotice(t *testing.T) {
	d := newDriver(t, withFixReview(), 2)
	blocked := blockedIssue(issue("42", 1, fixReviewReady))

	_, events := d.poll(blocked)
	wantEvents(t, otherKinds(events), core.IssueOfOtherKind{
		At: d.now, IssueID: issueID("42"), IssueRef: "#42", Kind: crew.KindIssue,
		Label: fixReviewReady, Rule: "fix review", Takes: crew.KindPullRequest,
	})
}

func TestAnItemOfTheOtherKindTakesNoSlot(t *testing.T) {
	d := newDriver(t, draft(), 1)
	urgent := prioritized(pr90(1, ready), 1)
	later := prioritized(issue("42", 2, ready), 2)

	cmds, _ := d.poll(urgent, later)
	wantCommands(t, cmds, core.Move{IssueID: issueID("42"), From: ready, To: inProgress})
	wantHeld(t, d.m, "42")
}
