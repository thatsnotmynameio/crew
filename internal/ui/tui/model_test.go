package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// zone is a fixed location three hours west of UTC, so the golden file does
// not depend on the machine's zone.
var zone = time.FixedZone("test", -3*60*60)

// start is the clock's time when a test begins.
var start = time.Date(2026, 10, 1, 14, 30, 0, 0, zone)

// testRules are the rules of the test snapshots: implement takes
// "ready", review takes "ready to review".
var testRules = []crew.Rule{
	{Name: "implement", Label: "ready", OnSuccess: "ready to review", OnFailure: "needs attention"},
	{Name: "review", Label: "ready to review", OnSuccess: "ready to merge", OnFailure: "needs attention"},
}

// harness drives a Model directly through Update and View, with a clock the
// test moves and counters for the stop and force callbacks.
type harness struct {
	t       *testing.T
	model   tea.Model
	updates chan engine.Update
	clock   time.Time
	stops   int
	forces  int
}

func newHarness(t *testing.T, width int, warnings ...string) *harness {
	t.Helper()
	return newRulesHarness(t, width, testRules, warnings...)
}

func newRulesHarness(t *testing.T, width int, rules []crew.Rule, warnings ...string) *harness {
	t.Helper()
	return newConfiguredHarness(t, width, rules, nil, warnings...)
}

// newConfiguredHarness is newRulesHarness with board's columns
// configured.
func newConfiguredHarness(
	t *testing.T, width int, rules []crew.Rule, board []crew.BoardColumn, warnings ...string,
) *harness {
	t.Helper()
	h := &harness{t: t, updates: make(chan engine.Update, 1), clock: start}
	h.model = New(Config{
		Updates: h.updates, Stop: func() { h.stops++ }, Force: func() { h.forces++ },
		Now: func() time.Time { return h.clock }, Location: zone,
		Rules: rules, Board: board, Repository: "crew", Warnings: warnings,
	})
	h.send(tea.WindowSizeMsg{Width: width, Height: 40})
	return h
}

func (h *harness) send(msg tea.Msg) tea.Cmd {
	h.t.Helper()
	m, cmd := h.model.Update(msg)
	h.model = m
	return cmd
}

// view is the view's text with its colours, styles and links stripped: the
// layout, and what tells sections and states apart without colour (AE6).
func (h *harness) view() string { return ansi.Strip(h.raw()) }

// raw is the view as the terminal gets it.
func (h *harness) raw() string { return h.model.View().Content }

var ctrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}

// quits reports whether running cmd, and the commands it batches, asks the
// program to quit. A command that blocks, such as waiting for the next
// update, is not a quit.
func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		switch msg := msg.(type) {
		case tea.QuitMsg:
			return true
		case tea.BatchMsg:
			return slices.ContainsFunc(msg, quits)
		}
		return false
	case <-time.After(100 * time.Millisecond):
		return false
	}
}

// you is the "you" entry with no bot configured: crew's writes and
// every pair act as you, and the actions in running run as it.
func you(pairs []string, running ...core.RunningAction) core.BotView {
	return core.BotView{Name: "you", You: true, Writes: true, Pairs: pairs, Running: running}
}

// runningSnapshot is #1 running two actions in default, started 5 and 7
// minutes before start, and #2 being taken by a later rule in clerk, 12
// minutes into a one-hour run, with no bot configured.
func runningSnapshot() engine.Update {
	one := crew.Issue{Key: "1", Ref: "#1", Title: "Add login form"}
	two := crew.Issue{Key: "2", Ref: "#2", Title: "Fix the flaky stream test"}
	return engine.Update{Snapshot: engine.Snapshot{
		View: core.View{Queues: []core.QueueView{
			{Name: crew.DefaultQueue, Slots: 2, Busy: 1},
			{Name: crew.ClerkQueue, Slots: 1, Busy: 1},
		}, Issues: []core.IssueView{
			{Issue: one, Rule: "implement", Queue: crew.DefaultQueue, Claim: core.ClaimRunning, Actions: []core.ActionView{
				{Name: "code", Phase: core.PhaseRunning, Branch: "crew/1-code", Started: start.Add(-5 * time.Minute)},
				{Name: "tests", Phase: core.PhaseRunning, Branch: "crew/1-tests", Started: start.Add(-7 * time.Minute)},
			}},
			{Issue: two, Rule: "review", Queue: crew.ClerkQueue, Claim: core.ClaimTaking, Actions: []core.ActionView{
				{Name: "check", Phase: core.PhaseWaiting},
			}},
		}, Bots: []core.BotView{you([]string{"implement/code", "implement/tests", "review/check"},
			core.RunningAction{IssueRef: "#1", Rule: "implement", Action: "code"},
			core.RunningAction{IssueRef: "#1", Rule: "implement", Action: "tests"})}},
		Started: start.Add(-12 * time.Minute), RunTimeLimit: time.Hour,
		Recent: []core.Event{
			core.IssueTaken{At: start.Add(-7*time.Minute - 2*time.Second), Issue: one, Rule: "implement",
				From: "ready", To: "in progress"},
			core.ActionStarted{At: start.Add(-7 * time.Minute), IssueRef: "#1", Rule: "implement", Action: "tests",
				Branch: "crew/1-tests", Log: ".crew/logs/1-tests.log"},
			core.ActionStarted{At: start.Add(-5 * time.Minute), IssueRef: "#1", Rule: "implement", Action: "code",
				Branch: "crew/1-code", Log: ".crew/logs/1-code.log"},
			core.PollDone{At: start.Add(-10 * time.Second), Listed: 2, Taken: 1},
			core.IssueTaken{At: start.Add(-10 * time.Second), Issue: two, Rule: "review",
				From: "ready to review", To: "in review"},
		},
	}}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file (run with -update to create it): %v", err)
	}
	if got != string(want) {
		t.Errorf("view does not match %s:\n%s\nwant:\n%s", path, got, want)
	}
}

// Covers AE6 (TUI side), and AE1 of #94: each queue's size, busy and free
// slots, and each action's queue.
func TestASnapshotWithTwoRunningActionsRendersTheGoldenView(t *testing.T) {
	h := newHarness(t, 80)

	h.send(updateMsg(runningSnapshot()))

	golden(t, "running", h.view())
}

// resumingSnapshot is #9 with one action reopening a failed run's
// workspace, one resumed in its reopened workspace 3 minutes before start,
// and one fresh, started 2 minutes before start.
func resumingSnapshot() engine.Update {
	issue := crew.Issue{Key: "9", Ref: "#9", Title: "Add login form"}
	return engine.Update{Snapshot: engine.Snapshot{
		View: core.View{Queues: []core.QueueView{{Name: crew.DefaultQueue, Slots: 2, Busy: 1}}, Issues: []core.IssueView{
			{Issue: issue, Rule: "development", Queue: crew.DefaultQueue, Claim: core.ClaimRunning, Actions: []core.ActionView{
				{Name: "docs", Phase: core.PhaseReopening, Workspace: "issue-9-docs", Branch: "crew/issue-9-docs"},
				{Name: "lfg", Phase: core.PhaseRunning, Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg",
					Started: start.Add(-3 * time.Minute), Resumed: true},
				{Name: "tests", Phase: core.PhaseRunning, Workspace: "issue-9-tests", Branch: "crew/issue-9-tests",
					Started: start.Add(-2 * time.Minute)},
			}},
		}, Bots: []core.BotView{you([]string{"development/docs", "development/lfg", "development/tests"},
			core.RunningAction{IssueRef: "#9", Rule: "development", Action: "lfg"},
			core.RunningAction{IssueRef: "#9", Rule: "development", Action: "tests"})}},
		Started: start.Add(-4 * time.Minute),
		Recent: []core.Event{
			core.ActionStarted{At: start.Add(-3 * time.Minute), IssueRef: "#9", Rule: "development", Action: "lfg",
				Workspace: "issue-9-lfg", Branch: "crew/issue-9-lfg", Log: ".crew/logs/issue-9-lfg.log", Resumed: true},
			core.ActionStarted{At: start.Add(-2 * time.Minute), IssueRef: "#9", Rule: "development", Action: "tests",
				Workspace: "issue-9-tests", Branch: "crew/issue-9-tests", Log: ".crew/logs/issue-9-tests.log"},
		},
	}}
}

// windingDownSnapshot is #42 still running after a one-hour run time is up.
func windingDownSnapshot() engine.Update {
	issue := crew.Issue{Key: "42", Ref: "#42", Title: "Add login form"}
	return engine.Update{Snapshot: engine.Snapshot{
		View: core.View{TimeUp: true, Queues: []core.QueueView{
			{Name: crew.DefaultQueue, Slots: 2, Busy: 1},
		}, Issues: []core.IssueView{
			{Issue: issue, Rule: "implement", Queue: crew.DefaultQueue, Claim: core.ClaimRunning, Actions: []core.ActionView{
				{Name: "code", Phase: core.PhaseRunning, Branch: "crew/42-code", Started: start.Add(-75 * time.Minute)},
			}},
		}, Bots: []core.BotView{you([]string{"implement/code"},
			core.RunningAction{IssueRef: "#42", Rule: "implement", Action: "code"})}},
		Recent: []core.Event{
			core.WindingDown{At: start.Add(-15 * time.Minute), Limit: time.Hour},
		},
		Started: start.Add(-75 * time.Minute), RunTimeLimit: time.Hour,
	}}
}

func TestARequestedStopWhileWindingDownShowsTheStoppingHeader(t *testing.T) {
	h := newHarness(t, 80)
	u := windingDownSnapshot()
	u.Snapshot.Stopping = true

	h.send(updateMsg(u))

	if got, _, _ := strings.Cut(h.view(), "\n"); !strings.HasSuffix(got, "STOPPING ") {
		t.Errorf("header = %q, want it to end in the STOPPING pill", got)
	}
}

func TestATickOneSecondLaterAdvancesBothElapsedTimes(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	h.clock = h.clock.Add(time.Second)
	cmd := h.send(tickMsg{})

	view := h.view()
	for _, want := range []string{"5m01s", "7m01s"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks elapsed %s:\n%s", want, view)
		}
	}
	if strings.Contains(view, "5m00s") || strings.Contains(view, "7m00s") {
		t.Errorf("view still shows the old elapsed times:\n%s", view)
	}
	if cmd == nil {
		t.Error("tick scheduled no next tick")
	}
}

// Covers R9 (TUI side): an action whose check runs is still running.
func TestAnActionRunningItsCheckShowsAsCheckingWithItsElapsedTime(t *testing.T) {
	h := newHarness(t, 80)
	u := runningSnapshot()
	u.Snapshot.Issues[0].Actions[0].Phase = core.PhaseChecking

	h.send(updateMsg(u))

	if view := h.view(); !strings.Contains(view, "implement/code    default   checking 5m00s") {
		t.Errorf("view lacks the checking action with its elapsed time:\n%s", view)
	}
}

func TestCtrlCPostsOneStopAndKeepsRunningUntilTheEngineStops(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	if quits(h.send(ctrlC)) {
		t.Fatal("Ctrl-C quit the program before the engine stopped")
	}
	if h.stops != 1 {
		t.Fatalf("stop called %d times, want 1", h.stops)
	}
	contains(t, h.view(), "STOPPING", "q or ctrl+c again forces the exit")

	// The engine keeps publishing while it stops; the TUI keeps rendering.
	stopping := runningSnapshot()
	stopping.Snapshot.Stopping = true
	if quits(h.send(updateMsg(stopping))) {
		t.Fatal("an update while stopping quit the program")
	}
	if h.stops != 1 || h.forces != 0 {
		t.Fatalf("after one Ctrl-C: stop called %d times, force %d; want 1 and 0", h.stops, h.forces)
	}

	close(h.updates)
	if !quits(h.send(engineStoppedMsg{})) {
		t.Error("the engine-stopped message did not quit the program")
	}
}

func TestTheClosedUpdateChannelBecomesTheEngineStoppedMessage(t *testing.T) {
	h := newHarness(t, 80)
	cmd := h.send(updateMsg(runningSnapshot()))

	close(h.updates)

	if msg := cmd(); msg != (engineStoppedMsg{}) {
		t.Errorf("waiting on a closed channel returned %#v, want engineStoppedMsg", msg)
	}
}

func TestASecondCtrlCOrQWhileStoppingForcesTheExit(t *testing.T) {
	for _, keys := range [][2]tea.KeyPressMsg{
		{ctrlC, ctrlC},
		{{Code: 'q', Text: "q"}, {Code: 'q', Text: "q"}},
		{{Code: 'q', Text: "q"}, ctrlC},
	} {
		t.Run(keys[0].String()+" then "+keys[1].String(), func(t *testing.T) {
			h := newHarness(t, 80)
			h.send(keys[0])
			if h.stops != 1 || h.forces != 0 {
				t.Fatalf("after the first key: stop %d, force %d; want 1 and 0", h.stops, h.forces)
			}

			cmd := h.send(keys[1])

			if h.stops != 1 || h.forces != 1 {
				t.Errorf("after the second key: stop %d, force %d; want 1 and 1", h.stops, h.forces)
			}
			if !quits(cmd) {
				t.Error("the forced exit did not quit the program")
			}
		})
	}
}

func TestANarrowWindowRendersWithoutPanickingAndTruncatesTitles(t *testing.T) {
	for _, width := range []int{59, 40, 12, 1} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			h := newHarness(t, width)
			snap := runningSnapshot()
			snap.Snapshot.Issues[0].Issue.Title = strings.Repeat("A very long issue title ", 8)

			h.send(updateMsg(snap))
			view := h.view()

			for l := range strings.SplitSeq(view, "\n") {
				if n := lipgloss.Width(l); n > width {
					t.Errorf("line is %d columns wide, over %d: %q", n, width, l)
				}
			}
			if width >= 40 && !strings.Contains(view, "#1 A very long") {
				t.Errorf("view at width %d lacks the start of #1's title:\n%s", width, view)
			}
			if strings.Contains(view, snap.Snapshot.Issues[0].Issue.Title) {
				t.Errorf("view at width %d shows the whole long title:\n%s", width, view)
			}
		})
	}
}

// Covers R2: a startup warning shows under the header.
func TestAStartupWarningShowsUnderTheHeader(t *testing.T) {
	h := newHarness(t, 120,
		"mate ops has no key on this machine for thatsnotmynameio; run `crew mates create ops` in this repository")

	h.send(updateMsg(runningSnapshot()))

	golden(t, "warning", h.view())
}

func TestInitStartsWaitingTickingAndSpinning(t *testing.T) {
	h := newHarness(t, 80)

	if h.model.Init() == nil {
		t.Error("Init returned no command")
	}
}

// Covers R5: the spinner of the running actions turns on its ticks.
func TestASpinnerTickTurnsTheRunningSpinners(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	tick, ok := h.current().spinner.Tick().(spinner.TickMsg)
	if !ok {
		t.Fatal("the spinner's tick is not a spinner.TickMsg")
	}
	if h.send(tick) == nil {
		t.Error("the spinner scheduled no next frame")
	}

	view := h.view()
	if strings.Contains(view, "⠋ running") || !strings.Contains(view, "⠙ running") {
		t.Errorf("the spinner did not turn to its next frame:\n%s", view)
	}
}
