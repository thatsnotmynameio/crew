package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// raws runs cmd and returns the raw sequences it, or the commands it
// batches or sequences, writes to the terminal. A command that blocks, such
// as waiting for the next update, writes nothing.
func raws(cmd tea.Cmd) []string {
	if cmd == nil {
		return nil
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-got:
	case <-time.After(100 * time.Millisecond):
		return nil
	}
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.RawMsg:
		seq, _ := msg.Msg.(string)
		return []string{seq}
	case tea.BatchMsg:
		cmds = msg
	default:
		cmds = sequenced(msg)
	}
	var out []string
	for _, c := range cmds {
		out = append(out, raws(c)...)
	}
	return out
}

// ended is a snapshot where rule ended on #12, moved to to, at the minute
// ended before start.
func ended(rule crew.RuleName, to crew.State, endedAt int) engine.Update {
	u := handledBy(twelve, rule, to)
	u.Snapshot.Handled[0].Ended = start.Add(-time.Duration(endedAt) * time.Minute)
	return u
}

// Covers AE5.
func TestAE5ARuleEndNotifiesWhileUnfocusedButAMutedRulesDoesNot(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(tea.BlurMsg{})

	notes := raws(h.send(updateMsg(ended("triage", "crew:triage:done", 3))))
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "\x1b]9;") ||
		!strings.Contains(notes[0], "triage ended through passed on #12 Rule labels; moved to crew:triage:done") {
		t.Errorf("notifications = %q, want one OSC 9 for triage on #12", notes)
	}

	if notes := raws(h.send(updateMsg(ended("promote triage", "crew:development:ready", 1)))); len(notes) != 0 {
		t.Errorf("the muted promote triage notified: %q", notes)
	}
}

func TestARuleEndNotifiesNothingWhileFocused(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(tea.BlurMsg{})
	h.send(tea.FocusMsg{})

	if notes := raws(h.send(updateMsg(ended("triage", "crew:triage:done", 3)))); len(notes) != 0 {
		t.Errorf("a focused terminal got %q", notes)
	}
}

func TestWithoutAnyFocusReportARuleEndNotifiesNothing(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)

	if notes := raws(h.send(updateMsg(ended("triage", "crew:triage:done", 3)))); len(notes) != 0 {
		t.Errorf("a terminal that never reported focus got %q", notes)
	}
}

func TestTheSameRuleEndNotifiesOnce(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(tea.BlurMsg{})

	first := raws(h.send(updateMsg(ended("triage", "crew:triage:done", 3))))
	second := raws(h.send(updateMsg(ended("triage", "crew:triage:done", 3))))

	if len(first) != 1 || len(second) != 0 {
		t.Errorf("notified %d then %d times, want once", len(first), len(second))
	}
}

func TestOnceAStopIsAskedForNothingNotifies(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(tea.BlurMsg{})
	u := ended("triage", "crew:triage:failed", 3)
	u.Snapshot.Stopping = true

	if notes := raws(h.send(updateMsg(u))); len(notes) != 0 {
		t.Errorf("a stopping crew notified %q", notes)
	}
}

// The last update's notification is written before the model reads the
// closed channel and quits (KTD6).
func TestTheLastUpdatesNotificationComesBeforeTheQuit(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(tea.BlurMsg{})
	cmd := h.send(updateMsg(ended("triage", "crew:triage:done", 3)))
	close(h.updates)

	seq := sequenced(cmd())
	if len(seq) != 2 {
		t.Fatalf("the update returned no sequence of the notification then the wait: %#v", seq)
	}
	if notes := raws(seq[0]); len(notes) != 1 {
		t.Errorf("the sequence's first step wrote %q, want the notification", notes)
	}
	if got := seq[1](); got != (engineStoppedMsg{}) {
		t.Errorf("the sequence's second step returned %#v, want the engine-stopped message", got)
	}
}

func TestANotificationIsCleanedOfControlCharacters(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, crewBoard)
	h.send(tea.BlurMsg{})
	u := ended("triage", "crew:triage:done", 3)
	u.Snapshot.Handled[0].Issue = titled(u.Snapshot.Handled[0].Issue, "Rule\x07 labels\x1b]0;evil\x07")

	notes := raws(h.send(updateMsg(u)))

	if len(notes) != 1 || strings.Count(notes[0], "\x07") != 1 || strings.Count(notes[0], "\x1b") != 1 {
		t.Errorf("notification = %q, want only its own OSC 9 framing", notes)
	}
}

// KTD23: a note names the route the rule ended through, and the label it
// moved the issue to or its close.
func TestANoteSaysTheRouteAndWhereTheIssueWent(t *testing.T) {
	closed := entry("7", "Dupe", "triage", "", 10, 1)
	closed.Route = "duplicate"
	for _, tt := range []struct {
		entry core.HandledView
		want  string
	}{
		{
			failedEntry("5", "Parse", 10, 1, "lfg"),
			"crew: implement ended through failed on #5 Parse; moved to needs attention",
		},
		{
			givenUpEntry(entry("6", "Drop", "implement", "ready to review", 10, 1), "closed"),
			"crew: implement ended through passed on #6 Drop; its move to ready to review was given up",
		},
		{closed, "crew: triage ended through duplicate on #7 Dupe; closed it"},
		{givenUpEntry(closed, "refused"), "crew: triage ended through duplicate on #7 Dupe; its close was given up"},
	} {
		if got := noteText(tt.entry); got != tt.want {
			t.Errorf("note = %q, want %q", got, tt.want)
		}
	}
}

// Covers R23 and KTD7.
func TestTheWindowTitleSaysCrewsState(t *testing.T) {
	h := newHarness(t, 80)
	if got := h.model.View().WindowTitle; got != "crew · idle" {
		t.Errorf("idle title = %q", got)
	}

	u := handledSnapshot()
	h.send(updateMsg(u))
	if got, want := h.model.View().WindowTitle, "crew · 1 running · 1 taking · 2 needs attention"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}

	h.send(windingDown())
	if got := h.model.View().WindowTitle; got != "crew · winding down" {
		t.Errorf("winding-down title = %q", got)
	}

	h.send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if got := h.model.View().WindowTitle; got != "crew · stopping" {
		t.Errorf("stopping title = %q", got)
	}
}

func windingDown() updateMsg { return updateMsg(windingDownSnapshot()) }

func TestAMutedRulesFailureCountsAsNeedingAttention(t *testing.T) {
	h := newBoardHarness(t, 80, crewNotify, crewBoard)
	u := ended("promote triage", "crew:triage:failed", 1)
	u.Snapshot.Handled[0] = failing(u.Snapshot.Handled[0], "promote")

	h.send(updateMsg(u))

	if got := h.model.View().WindowTitle; got != "crew · 1 needs attention" {
		t.Errorf("title = %q, want the muted rule's failure counted", got)
	}
}

// Covers R24 and KTD7.
func TestTheTabProgressFollowsCrewsState(t *testing.T) {
	h := newHarness(t, 80)
	if p := h.model.View().ProgressBar; p != nil {
		t.Errorf("idle progress = %+v, want none", p)
	}

	h.send(updateMsg(runningSnapshot()))
	if p := h.model.View().ProgressBar; p == nil || p.State != tea.ProgressBarIndeterminate {
		t.Errorf("progress while actions run = %+v, want indeterminate", p)
	}

	h.send(updateMsg(handledSnapshot()))
	if p := h.model.View().ProgressBar; p == nil || p.State != tea.ProgressBarError {
		t.Errorf("progress while an entry needs attention = %+v, want error", p)
	}
	if !h.model.View().ReportFocus {
		t.Error("the view does not ask for focus reports")
	}
}

// sequenced returns the commands of the message tea.Sequence sends, whose
// type Bubble Tea does not export, or nil for any other message.
func sequenced(msg tea.Msg) []tea.Cmd {
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.Type().Name() != "sequenceMsg" {
		return nil
	}
	out := make([]tea.Cmd, v.Len())
	for i := range out {
		out[i], _ = reflect.TypeAssert[tea.Cmd](v.Index(i))
	}
	return out
}

// Covers AE9: a written board leaves the notifications to notify.
func TestAE9WithABoardAMutedRulesEndStillNotifiesNothing(t *testing.T) {
	h := newBoardHarness(t, 120, crewNotify, ideasBugsDone)
	h.send(tea.BlurMsg{})

	if notes := raws(h.send(updateMsg(ended("promote triage", "crew:development:ready", 3)))); len(notes) != 0 {
		t.Errorf("the muted promote triage notified: %q", notes)
	}
	if notes := raws(h.send(updateMsg(ended("triage", "crew:triage:done", 1)))); len(notes) != 1 {
		t.Errorf("triage's end sent %q, want one notification", notes)
	}
}

func TestAFailureHeldAgainDoesNotCountAsNeedingAttention(t *testing.T) {
	h := newBoardHarness(t, 80, crewNotify, crewBoard)
	u := held(twelve, "development", "lfg", core.ClaimRunning)
	e := handledBy(twelve, "fix", "crew:fix:failed").Snapshot.Handled[0]
	u.Snapshot.Handled = []core.HandledView{failing(e, "lfg")}

	h.send(updateMsg(u))
	if got := h.model.View().WindowTitle; got != "crew · 1 running · 1 needs attention" {
		t.Errorf("title before the retry = %q, want the failure counted", got)
	}

	u.Snapshot.Handled[0].HeldBy = "development"
	h.send(updateMsg(u))
	if got := h.model.View().WindowTitle; got != "crew · 1 running" {
		t.Errorf("title during the retry = %q, want no needs attention", got)
	}
	if p := h.model.View().ProgressBar; p == nil || p.State != tea.ProgressBarIndeterminate {
		t.Errorf("progress during the retry = %+v, want indeterminate", p)
	}
}

// notifyByRule turns off review, a rule with actions, and turns on
// promote, a rule without actions (R9).
var notifyByRule = map[crew.RuleName]bool{"review": false, "promote": true}

// Covers R9.
func TestNotifyDecidesWhetherARulesEndNotifies(t *testing.T) {
	h := newBoardHarness(t, 120, notifyByRule, nil)
	h.send(tea.BlurMsg{})

	u := ended("review", "needs attention", 3)
	u.Snapshot.Handled[0] = failing(u.Snapshot.Handled[0], "review")
	if notes := raws(h.send(updateMsg(u))); len(notes) != 0 {
		t.Errorf("review's failure notified with notify off: %q", notes)
	}
	if notes := raws(h.send(updateMsg(ended("promote", "ready to merge", 1)))); len(notes) != 1 {
		t.Errorf("promote's end sent %q, want one notification", notes)
	}
}

func TestAnEntryOfARuleNoLongerConfiguredIsMuted(t *testing.T) {
	h := newBoardHarness(t, 120, notifyByRule, nil)
	h.send(tea.BlurMsg{})

	if notes := raws(h.send(updateMsg(ended("gone", "ready to merge", 1)))); len(notes) != 0 {
		t.Errorf("an entry of a rule not configured notified: %q", notes)
	}
}
