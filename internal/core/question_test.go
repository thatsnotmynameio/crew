package core_test

import (
	"slices"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// askedAndWaited plays, on a model of waitingRules made with opts, a run
// of implement on issue 1 whose session acceptance starts and ends with
// waiting, its move to waiting landed, and returns the driver.
func askedAndWaited(t *testing.T, opts ...core.Option) *driver {
	t.Helper()
	opts = append([]core.Option{core.WithBots(developerBots())}, opts...)
	d := &driver{t: t, m: core.New(waitingRules("developer", 10*time.Minute), 2, opts...), now: t0}
	d.running(issue("1", 1, ready))
	cmds, _ := d.send(core.SessionEnded{
		IssueID: issueID("1"), Action: "acceptance", Outcome: crew.Outcome{Succeeded: true},
		Report: crew.VerdictReported{Verdict: crew.Waiting},
	})
	d.settle(cmds)
	wantHeld(t, d.m)
	return d
}

// Covers R23, R45, KTD-W7: a session that may ask starts as the login its
// bot acts as, and the next run of its rule on the issue takes over its
// question from the journal.
func TestTheNextRunTakesOverTheQuestionOfTheSessionThatWaited(t *testing.T) {
	first := askedAndWaited(t, core.Journaling(nil), core.Reopening())
	asker := first.run(issueID("1"))
	i := slices.IndexFunc(first.recorded, func(e crew.RunEvent) bool {
		_, ok := e.(crew.ActionSessionStarted)
		return ok
	})
	if i < 0 {
		t.Fatalf("recorded %#v, want a session's start", first.recorded)
	}
	if s, _ := first.recorded[i].(crew.ActionSessionStarted); s.Login != "crew-developer[bot]" || !s.Asks {
		t.Errorf("session started %#v, want it acting as crew-developer[bot] and asking", s)
	}

	next := &driver{t: t, now: t0, inputs: 1000, m: core.New(waitingRules("developer", 10*time.Minute), 2,
		core.Journaling(first.recorded), core.Reopening(), core.WithBots(developerBots()))}
	next.poll(issue("1", 1, ready))
	want := []crew.Question{{Run: asker, Action: "acceptance", Login: "crew-developer[bot]"}}
	if got := takenOf(t, next, "1").Questions; !slices.Equal(got, want) {
		t.Errorf("the next take carries %#v, want %#v", got, want)
	}
}

// Without the run journal, no run inherits a question.
func TestWithoutAJournalNoRunTakesOverAQuestion(t *testing.T) {
	d := askedAndWaited(t)
	asker := d.run(issueID("1"))
	d.poll(issue("1", 1, ready))
	if got := takenOf(t, d, "1"); got.Run == asker || got.Questions != nil {
		t.Errorf("the next take is %#v, want a new run without questions", got)
	}
}
