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

// harnessRows is the height of a harness's window.
const harnessRows = 48

// start is the clock's time when a test begins.
var start = time.Date(2026, 10, 1, 14, 30, 0, 0, zone)

// testNotify is which rules of the test snapshots notify: implement takes
// "ready", review takes "ready to review", and neither notifies.
var testNotify = map[crew.RuleName]bool{"implement": false, "review": false}

// testBoard is the default board of the test snapshots' rules: each
// rule's ready and running labels.
var testBoard = []crew.BoardColumn{
	{Name: "implement", Labels: []crew.State{"ready", "in progress"}},
	{Name: "review", Labels: []crew.State{"ready to review", "in review"}},
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
	pauses  int
}

// newHarness is newBoardHarness of testNotify and testBoard.
func newHarness(t *testing.T, width int, warnings ...string) *harness {
	t.Helper()
	return newBoardHarness(t, width, testNotify, testBoard, warnings...)
}

// newBoardHarness returns a harness of the rules' notify and board's
// columns, in a window width wide and 48 rows high: room for every section
// of the test snapshots, whose columns hold up to two cards.
func newBoardHarness(
	t *testing.T, width int, notify map[crew.RuleName]bool, board []crew.BoardColumn, warnings ...string,
) *harness {
	t.Helper()
	h := &harness{t: t, updates: make(chan engine.Update, 1), clock: start}
	h.model = New(Config{
		Updates: h.updates, Stop: func() { h.stops++ }, Force: func() { h.forces++ }, Pause: func() { h.pauses++ },
		Now: func() time.Time { return h.clock }, Location: zone,
		Notify: notify, Board: board, Repository: "crew", Warnings: warnings,
	})
	h.send(tea.WindowSizeMsg{Width: width, Height: harnessRows})
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

// runningSnapshot is #1 running code, the first of its two actions, in
// default, started 5 minutes before start, and #2 being taken by a later
// rule in clerk, each on the board in its rule's column, 12 minutes into a
// one-hour run, with no bot configured.
func runningSnapshot() engine.Update {
	one := crew.NewIssue(crew.IssueData{ID: issueID("1"), Ref: "#1", Title: "Add login form"})
	two := crew.NewIssue(crew.IssueData{ID: issueID("2"), Ref: "#2", Title: "Fix the flaky stream test"})
	return engine.Update{Snapshot: engine.Snapshot{
		View: core.View{Queues: []core.QueueView{
			{Name: crew.DefaultQueue, Slots: 2, Busy: 1},
			{Name: "clerk", Slots: 1, Busy: 1},
		}, Issues: []core.IssueView{
			{Issue: one, Rule: "implement", Queue: crew.DefaultQueue, Claim: core.ClaimRunning, Actions: []core.ActionView{
				{Name: "code", Phase: core.PhaseRunning, Branch: "crew/1-code", Started: start.Add(-5 * time.Minute)},
				{Name: "tests", Phase: core.PhaseAwaitingTurn, Branch: "crew/1-code"},
			}},
			{Issue: two, Rule: "review", Queue: "clerk", Claim: core.ClaimTaking, Actions: []core.ActionView{
				{Name: "check", Phase: core.PhaseTaking},
			}},
		}, Bots: []core.BotView{you([]string{"implement/code", "implement/tests", "review/check"},
			core.RunningAction{IssueRef: "#1", Rule: "implement", Action: "code"})},
			Board: []crew.BoardIssue{
				crew.NewBoardIssue(one, []crew.State{"in progress"}), crew.NewBoardIssue(two, []crew.State{"ready to review"}),
			}},
		Started: start.Add(-12 * time.Minute), RunTimeLimit: time.Hour,
		Recent: []core.Published{
			taken(start.Add(-7*time.Minute-2*time.Second), one, "implement", "ready", "in progress"),
			crew.WorkspaceOpened{
				At: start.Add(-7 * time.Minute), IssueRef: "#1", Rule: "implement",
				Workspace: crew.Workspace{Name: "1-code", Branch: "crew/1-code"}, Log: ".crew/logs/1-code.log",
			},
			crew.ActionSessionStarted{
				At: start.Add(-5 * time.Minute), IssueRef: "#1", Rule: "implement", Action: "code",
			},
			core.PollDone{At: start.Add(-10 * time.Second), Listed: 2, Taken: 1},
			taken(start.Add(-10*time.Second), two, "review", "ready to review", "in review"),
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
func TestASnapshotWithARunningSequenceRendersTheGoldenView(t *testing.T) {
	h := newHarness(t, 80)

	h.send(updateMsg(runningSnapshot()))

	golden(t, "running", h.view())
}

// resumingSnapshot is #9's run resumed in the reopened workspace of a run
// that failed: docs done in that run, lfg running since 3 minutes before
// start, and tests awaiting its turn.
func resumingSnapshot() engine.Update {
	issue := crew.NewIssue(crew.IssueData{ID: issueID("9"), Ref: "#9", Title: "Add login form"})
	return engine.Update{Snapshot: engine.Snapshot{
		View: core.View{Queues: []core.QueueView{{Name: crew.DefaultQueue, Slots: 2, Busy: 1}}, Issues: []core.IssueView{
			{Issue: issue, Rule: "development", Queue: crew.DefaultQueue, Claim: core.ClaimRunning, Actions: []core.ActionView{
				{Name: "docs", Phase: core.PhaseDoneInEarlierRun, Workspace: "issue-9-development",
					Branch: "crew/issue-9-development", Resumed: true},
				{Name: "lfg", Phase: core.PhaseRunning, Workspace: "issue-9-development", Branch: "crew/issue-9-development",
					Started: start.Add(-3 * time.Minute), Resumed: true},
				{Name: "tests", Phase: core.PhaseAwaitingTurn, Workspace: "issue-9-development",
					Branch: "crew/issue-9-development", Resumed: true},
			}},
		}, Bots: []core.BotView{you([]string{"development/docs", "development/lfg", "development/tests"},
			core.RunningAction{IssueRef: "#9", Rule: "development", Action: "lfg"})}},
		Started: start.Add(-4 * time.Minute),
		Recent: []core.Published{
			crew.WorkspaceOpened{
				At: start.Add(-3*time.Minute - time.Second), IssueRef: "#9", Rule: "development",
				Workspace: crew.Workspace{Name: "issue-9-development", Branch: "crew/issue-9-development"},
				Log:       ".crew/logs/issue-9-development.log", Resumed: true,
			},
			crew.ActionSessionStarted{
				At: start.Add(-3 * time.Minute), IssueRef: "#9", Rule: "development", Action: "lfg",
			},
		},
	}}
}

// Covers R16 and KTD-S17: a resumed run's card shows the action it runs
// and how many are left, and its reopened worktree shows in its event.
func TestAResumedRunShowsItsRunningActionAndItsWorktree(t *testing.T) {
	h := newHarness(t, 120)

	h.send(updateMsg(resumingSnapshot()))

	golden(t, "resuming", h.view())
	contains(t, h.view(), "⠋ lfg 3m · 1 left", "development resumed in worktree issue-9-development")
}

// windingDownSnapshot is #42 still running after a one-hour run time is up.
func windingDownSnapshot() engine.Update {
	issue := crew.NewIssue(crew.IssueData{ID: issueID("42"), Ref: "#42", Title: "Add login form"})
	return engine.Update{Snapshot: engine.Snapshot{
		View: core.View{TimeUp: true, Queues: []core.QueueView{
			{Name: crew.DefaultQueue, Slots: 2, Busy: 1},
		}, Issues: []core.IssueView{
			{Issue: issue, Rule: "implement", Queue: crew.DefaultQueue, Claim: core.ClaimRunning, Actions: []core.ActionView{
				{Name: "code", Phase: core.PhaseRunning, Branch: "crew/42-code", Started: start.Add(-75 * time.Minute)},
			}},
		}, Bots: []core.BotView{you([]string{"implement/code"},
			core.RunningAction{IssueRef: "#42", Rule: "implement", Action: "code"})},
			Board: []crew.BoardIssue{crew.NewBoardIssue(issue, []crew.State{"in progress"})}},
		Recent: []core.Published{
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

func TestATickAMinuteLaterAdvancesBothElapsedTimes(t *testing.T) {
	h := newHarness(t, 120)
	h.send(updateMsg(runningSnapshot()))

	h.clock = h.clock.Add(time.Minute)
	cmd := h.send(tickMsg{})

	view := h.view()
	contains(t, view, "⠋ code 6m · 1 left")
	if strings.Contains(view, "code 5m") {
		t.Errorf("view still shows the old elapsed times:\n%s", view)
	}
	if cmd == nil {
		t.Error("tick scheduled no next tick")
	}
}

// Covers KTD-S17 (TUI side): a run in its routing phase shows the route it
// ends through, after a stop or an owed step's claim.
func TestARoutingRunShowsTheRouteItEndsThrough(t *testing.T) {
	for _, tt := range []struct {
		claim core.Claim
		want  string
	}{
		{core.ClaimRouting, "run  ⠋ through stale"},
		{core.ClaimOwed, "run  ! owed · ⠋ through stale"},
	} {
		h := newHarness(t, 80)
		u := runningSnapshot()
		iv := &u.Snapshot.Issues[0]
		iv.Claim, iv.Route = tt.claim, "stale"
		iv.Actions[0].Phase, iv.Actions[1].Phase = core.PhaseEnded, core.PhaseNotRun

		h.send(updateMsg(u))

		if got := faceOf(t, boardOf(t, h.view()), "#1")[1]; got != tt.want {
			t.Errorf("%s: #1's run row = %q, want %q", tt.claim, got, tt.want)
		}
	}
}

func TestTwoCtrlCsPostOneStopAndKeepRunningUntilTheEngineStops(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	h.send(ctrlC)
	if quits(h.send(ctrlC)) {
		t.Fatal("Ctrl-C quit the program before the engine stopped")
	}
	if h.stops != 1 {
		t.Fatalf("stop called %d times, want 1", h.stops)
	}
	contains(t, h.view(), "STOPPING", forceNotice)

	// The engine keeps publishing while it stops; the TUI keeps rendering.
	stopping := runningSnapshot()
	stopping.Snapshot.Stopping = true
	if quits(h.send(updateMsg(stopping))) {
		t.Fatal("an update while stopping quit the program")
	}
	if h.stops != 1 || h.forces != 0 {
		t.Fatalf("after two Ctrl-Cs: stop called %d times, force %d; want 1 and 0", h.stops, h.forces)
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

// The live view's own notices in its footer (R1, R6 of #266), and the stop
// key as its help names it (R9 of #266).
const (
	armedNotice = "q or ctrl+c again within 3s stops crew"
	forceNotice = "q or ctrl+c forces the exit"
	stopKeys    = "q q"
)

var qKey = tea.KeyPressMsg{Code: 'q', Text: "q"}

// footer is the view's last row.
func (h *harness) footer() string {
	rows := rowsOf(h.view())
	return rows[len(rows)-1]
}

// armExpiry is the message that ends the stop armed at at.
func armExpiry(at time.Time) tea.Msg { return armExpiredMsg{until: at.Add(armWindow)} }

// Covers AE1 of #266: one Ctrl-C only arms the stop, and the footer goes
// back to its key help when 3 seconds pass.
func TestAStrayCtrlCOnlyArmsTheStop(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	if quits(h.send(ctrlC)) {
		t.Fatal("one Ctrl-C quit the program")
	}
	if h.stops != 0 {
		t.Fatalf("one Ctrl-C called Stop %d times, want none", h.stops)
	}
	if got := h.footer(); got != armedNotice {
		t.Errorf("footer = %q, want the armed notice", got)
	}
	if strings.Contains(h.view(), "STOPPING") {
		t.Errorf("one Ctrl-C shows STOPPING:\n%s", h.view())
	}

	h.clock = h.clock.Add(armWindow)
	h.send(armExpiry(start))

	if got := h.footer(); !strings.Contains(got, "tab focus") {
		t.Errorf("footer = %q 3s later, want the key help", got)
	}
	if h.stops != 0 || h.forces != 0 {
		t.Errorf("Stop called %d times and Force %d, want neither", h.stops, h.forces)
	}
}

// Covers AE2 of #266: q and Ctrl-C count as one key, and the second press
// within 3 seconds stops crew.
func TestTwoPressesWithinTheWindowStopCrew(t *testing.T) {
	for _, keys := range [][2]tea.KeyPressMsg{{qKey, ctrlC}, {ctrlC, qKey}, {qKey, qKey}, {ctrlC, ctrlC}} {
		t.Run(keys[0].String()+" then "+keys[1].String(), func(t *testing.T) {
			h := newHarness(t, 80)
			h.send(updateMsg(runningSnapshot()))

			h.send(keys[0])
			h.clock = h.clock.Add(2 * time.Second)
			if quits(h.send(keys[1])) {
				t.Fatal("the confirmed stop quit the program before the engine stopped")
			}

			if h.stops != 1 || h.forces != 0 {
				t.Errorf("Stop called %d times and Force %d, want 1 and 0", h.stops, h.forces)
			}
			contains(t, h.view(), "STOPPING")
			if got := h.footer(); got != forceNotice {
				t.Errorf("footer = %q, want the forcing notice", got)
			}
		})
	}
}

// Covers AE5 of #266: a press after the window arms the stop again, even
// before the earlier arm's expiry arrives, and the expiry of that earlier
// arm leaves the new one armed.
func TestAPressAfterTheWindowArmsAgain(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	h.send(qKey)
	h.clock = h.clock.Add(4 * time.Second)
	h.send(qKey)
	if h.stops != 0 {
		t.Fatalf("two presses 4s apart called Stop %d times, want none", h.stops)
	}

	h.send(armExpiry(start))
	if got := h.footer(); got != armedNotice {
		t.Errorf("footer = %q after the first arm's expiry, want the second arm's notice", got)
	}

	h.clock = h.clock.Add(2 * time.Second)
	h.send(qKey)
	if h.stops != 1 {
		t.Errorf("a press 2s after the second arm called Stop %d times, want once", h.stops)
	}
}

// Covers R5 of #266: other keys work while the stop is armed, and neither
// cancel nor extend the window.
func TestOtherKeysNeitherCancelNorExtendTheArmedStop(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after time.Duration
		stops int
	}{
		{"a press inside the window confirms", 2500 * time.Millisecond, 1},
		{"a press past the window arms again", 3500 * time.Millisecond, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, 80)
			h.send(updateMsg(runningSnapshot()))

			h.send(qKey)
			h.clock = h.clock.Add(2 * time.Second)
			h.send(tab)
			if h.current().focus != focusBots {
				t.Errorf("tab while armed left the focus on %v", h.current().focus)
			}
			if got := h.footer(); got != armedNotice {
				t.Errorf("footer = %q after tab, want the armed notice", got)
			}
			h.clock = start.Add(tc.after)
			h.send(qKey)

			if h.stops != tc.stops {
				t.Errorf("Stop called %d times, want %d", h.stops, tc.stops)
			}
		})
	}
}

// Covers AE3 of #266: once a confirmed stop runs, one more press forces
// the exit.
func TestOnePressWhileStoppingForcesTheExit(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{qKey, ctrlC} {
		t.Run(key.String(), func(t *testing.T) {
			h := newHarness(t, 80)
			h.send(qKey)
			h.send(qKey)
			if h.stops != 1 || h.forces != 0 {
				t.Fatalf("after two presses: stop %d, force %d; want 1 and 0", h.stops, h.forces)
			}

			cmd := h.send(key)

			if h.stops != 1 || h.forces != 1 {
				t.Errorf("after the third press: stop %d, force %d; want 1 and 1", h.stops, h.forces)
			}
			if !quits(cmd) {
				t.Error("the forced exit did not quit the program")
			}
		})
	}
}

// Covers AE4 of #266: when a signal stopped crew, the first press forces
// the exit, and the footer says so.
func TestOnePressAfterAnOutsideStopForcesTheExit(t *testing.T) {
	h := newHarness(t, 80)
	stopping := runningSnapshot()
	stopping.Snapshot.Stopping = true
	h.send(updateMsg(stopping))
	if got := h.footer(); got != forceNotice {
		t.Errorf("footer = %q while the engine stops, want the forcing notice", got)
	}

	cmd := h.send(qKey)

	if h.stops != 0 || h.forces != 1 {
		t.Errorf("Stop called %d times and Force %d, want 0 and 1", h.stops, h.forces)
	}
	if !quits(cmd) {
		t.Error("the forced exit did not quit the program")
	}
}

// Covers R6 of #266: a signal that stops crew while the stop is armed shows
// the forcing notice, not the armed one.
func TestAnOutsideStopWhileArmedShowsTheForcingNotice(t *testing.T) {
	h := newHarness(t, 80)
	h.send(qKey)
	stopping := runningSnapshot()
	stopping.Snapshot.Stopping = true

	h.send(updateMsg(stopping))

	if got := h.footer(); got != forceNotice {
		t.Errorf("footer = %q, want the forcing notice", got)
	}
}

// Covers AE6 of #266: a wind-down still takes two presses to stop.
func TestAWindDownStillTakesTwoPressesToStop(t *testing.T) {
	h := newHarness(t, 80)
	h.send(windingDown())

	h.send(qKey)
	if h.stops != 0 {
		t.Fatalf("one press during a wind-down called Stop %d times, want none", h.stops)
	}
	if got := h.footer(); got != armedNotice {
		t.Errorf("footer = %q, want the armed notice", got)
	}

	h.clock = h.clock.Add(time.Second)
	h.send(qKey)
	if h.stops != 1 {
		t.Errorf("a second press during a wind-down called Stop %d times, want once", h.stops)
	}
}

func TestANarrowWindowRendersWithoutPanickingAndTruncatesTitles(t *testing.T) {
	for _, width := range []int{59, 40, 12, 1} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			h := newHarness(t, width)
			snap := runningSnapshot()
			title := strings.Repeat("A very long issue title ", 8)
			snap.Snapshot.Issues[0].Issue = titled(snap.Snapshot.Issues[0].Issue, title)
			snap.Snapshot.Board[0] = titledOnBoard(snap.Snapshot.Board[0], title)

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
			if strings.Contains(view, snap.Snapshot.Issues[0].Issue.Title()) {
				t.Errorf("view at width %d shows the whole long title:\n%s", width, view)
			}
		})
	}
}

// Covers R2: a startup warning shows under the header.
func TestAStartupWarningShowsUnderTheHeader(t *testing.T) {
	h := newHarness(t, 120,
		"bot ops has no key on this machine for thatsnotmynameio; run `crew bots create ops` in this repository")

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
	if strings.Contains(view, "⠋ code 5m") || !strings.Contains(view, "⠙ code 5m") {
		t.Errorf("the spinner did not turn to its next frame:\n%s", view)
	}
}

// Covers AE8: a record the statistics store lost shows in Events as the
// line renderer's warning.
func TestALostStatisticShowsInEvents(t *testing.T) {
	u := engine.Update{Snapshot: engine.Snapshot{
		Started: start.Add(-time.Minute),
		Recent: []core.Published{core.StatisticNotRecorded{
			At: start, Statistic: crew.Process{ID: "p"}, Reason: "no data folder: set XDG_DATA_HOME or HOME",
		}},
	}}

	view := fitted(t, 160, 40, u)

	contains(t, view, "warning: could not record this crew process in the statistics store: no data folder")
}
