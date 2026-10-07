package engine_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// otherKinds returns the IssueOfOtherKind events among the updates r
// published. Call it once Run has returned.
func (r *rig) otherKinds() []core.IssueOfOtherKind {
	r.t.Helper()
	var out []core.IssueOfOtherKind
	for u := range r.queue.Updates() {
		for _, e := range u.Events {
			if n, ok := e.(core.IssueOfOtherKind); ok {
				out = append(out, n)
			}
		}
	}
	return out
}

// Covers AE1 of #92 through the engine: a pull request in the label of a
// rule that takes issues gets one notice over several polls, and no session.
func TestAPullRequestInAnIssueRulesLabelIsNoticedOnceAcrossPolls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := issue(90, ready).Data()
		d.Kind = crew.KindPullRequest
		pr := crew.NewIssue(d)
		r := start(t, config(t, fake.NewTracker(pr), develop))

		time.Sleep(2*poll + time.Second) // the polls at 0s, 300s and 600s
		r.engine.Stop()
		final, err := r.wait()
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if held := final.Snapshot.Issues; len(held) != 0 {
			t.Errorf("held = %#v, want none", held)
		}
		notices := r.otherKinds()
		if len(notices) != 1 {
			t.Fatalf("notices = %#v, want one", notices)
		}
		if n := notices[0]; n.IssueRef != "#90" || n.Label != ready || n.Rule != "implement" || n.Takes != crew.KindIssue {
			t.Errorf("notice = %#v, want #90 in ready, whose rule implement takes issues", n)
		}
	})
}
