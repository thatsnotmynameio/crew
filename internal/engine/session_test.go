package engine

import (
	"context"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Covers KTD7: the loop keys sessions by rule run and action, so a
// StopSession stops the session of the run it names, never another run's
// session of the same issue and action.
func TestStopSessionStopsTheSessionOfTheRunItNames(t *testing.T) {
	e := New(Config{})
	e.model = core.New(nil, 1)
	ctx := context.Background()
	h := fake.NewHarness()
	issue := crew.IssueID{Key: "1"}
	start := func(run crew.RuleRunID) *fake.Session {
		s, err := h.Start(ctx, port.Run{})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		e.receive(ctx, message{input: core.SessionStarted{IssueID: issue, Run: run, Action: "development"}, session: s})
		return h.Sessions()[len(h.Sessions())-1]
	}
	older, newer := start("seed.1"), start("seed.2")

	e.launch(ctx, core.StopSession{IssueID: issue, Run: "seed.1", Action: "development"})
	e.wg.Wait()

	if !older.Stopped() {
		t.Error("the session of the run StopSession names was not stopped")
	}
	if newer.Stopped() {
		t.Error("the other run's session of the same issue and action was stopped")
	}
}
