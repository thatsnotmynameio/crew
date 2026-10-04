package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// crewWorkflow is this repository's workflow, with both promote stages
// hidden (AE1).
var crewWorkflow = []crew.Stage{
	{Name: "promote brainstorm", Label: "crew:brainstorm:done", OnSuccess: "crew:triage:ready", OffBoard: true},
	{Name: "triage", Label: "crew:triage:ready", OnSuccess: "crew:triage:done", OnFailure: "crew:triage:failed"},
	{Name: "promote triage", Label: "crew:triage:done", OnSuccess: "crew:development:ready", OffBoard: true},
	{Name: "development", Label: "crew:development:ready", OnFailure: "crew:development:failed"},
	{Name: "fix", Label: "crew:fix:ready", OnFailure: "crew:fix:failed"},
}

var twelve = crew.Issue{Key: "12", Ref: "#12", Title: "Stage labels", URL: "https://github.com/o/r/issues/12"}

// held is a snapshot of issue held by stage in claim, with one running
// action.
func held(issue crew.Issue, stage, action string, claim core.Claim) engine.Update {
	return engine.Update{Snapshot: engine.Snapshot{View: core.View{Issues: []core.IssueView{{
		Issue: issue, Stage: stage, Queue: "clerk", Claim: claim,
		Actions: []core.ActionView{{Name: action, Phase: core.PhaseRunning, Started: start.Add(-time.Minute)}},
	}}}}}
}

// handledBy is a snapshot of issue handled by stage, moved to to.
func handledBy(issue crew.Issue, stage string, to crew.State) engine.Update {
	e := entry(issue.Key, issue.Title, stage, to, 10, 1)
	e.Issue = issue
	return engine.Update{Snapshot: engine.Snapshot{View: core.View{Handled: []core.HandledView{e}}}}
}

// boardOf returns the Workflow section of view: its rule up to the blank
// line before Actions.
func boardOf(t *testing.T, view string) string {
	t.Helper()
	i := strings.Index(view, "Workflow ")
	j := strings.Index(view, "\n\nActions ")
	if i < 0 || j < i {
		t.Fatalf("view lacks the Workflow section:\n%s", view)
	}
	return view[i:j]
}

// Covers AE1.
func TestAE1HiddenStagesHaveNoColumnAndTheirIssuesNoCard(t *testing.T) {
	h := newWorkflowHarness(t, 120, crewWorkflow)

	h.send(updateMsg(held(twelve, "promote triage", "promote", core.ClaimRunning)))
	board := boardOf(t, h.view())

	names := strings.Fields(strings.Split(board, "\n")[1])
	if got, want := strings.Join(names, " "), "triage development fix"; got != want {
		t.Errorf("columns = %q, want %q:\n%s", got, want, board)
	}
	if strings.Contains(board, "#12") {
		t.Errorf("the board shows #12 while a hidden stage holds it:\n%s", board)
	}
	contains(t, h.view(), "promote triage/promote")
}

// Covers AE2.
func TestAE2ACardWaitsInItsColumnThenSlidesToTheNextStage(t *testing.T) {
	h := newWorkflowHarness(t, 120, crewWorkflow)
	h.send(updateMsg(held(twelve, "triage", "triage", core.ClaimRunning)))

	h.send(updateMsg(handledBy(twelve, "triage", "crew:triage:done")))
	board := boardOf(t, h.view())
	contains(t, board, "▌ #12 Stage labels", "▌ → crew:triage:done")
	if col := cardColumn(t, board, "#12"); col != 0 {
		t.Errorf("waiting card is in column %d, want triage's 0:\n%s", col, board)
	}

	cmd := h.send(updateMsg(held(twelve, "development", "lfg", core.ClaimRunning)))
	board = boardOf(t, h.view())
	if col := cardColumn(t, board, "#12"); col != 1 {
		t.Errorf("card is in column %d, want development's 1 at once:\n%s", col, board)
	}
	if got := h.current().memory.slides; len(got) != 1 || got[0].from != 1 || got[0].to != 3 {
		t.Errorf("slides = %+v, want one from triage (1) to development (3)", got)
	}
	if !schedulesSlideTick(cmd) {
		t.Error("the move scheduled no slide frame")
	}
}

// cardColumn returns which drawn column holds ref's card, by its x on the
// board's rows.
func cardColumn(t *testing.T, board, ref string) int {
	t.Helper()
	for l := range strings.SplitSeq(board, "\n") {
		if i := strings.Index(l, "▌ "+ref+" "); i >= 0 {
			return len([]rune(l[:i])) / (maxColumn + columnGap)
		}
	}
	t.Fatalf("board has no card for %s:\n%s", ref, board)
	return -1
}

// schedulesSlideTick reports whether cmd, or a command it batches, sends a
// slide frame.
func schedulesSlideTick(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	select {
	case msg := <-got:
		switch msg := msg.(type) {
		case slideTickMsg:
			return true
		case tea.BatchMsg:
			return slices.ContainsFunc(msg, schedulesSlideTick)
		}
	case <-time.After(200 * time.Millisecond):
	}
	return false
}

// Covers AE3.
func TestAE3AFailedStageTakesItsCardOffTheBoard(t *testing.T) {
	h := newWorkflowHarness(t, 120, crewWorkflow)
	five := crew.Issue{Key: "5", Ref: "#5", Title: "Parse the config"}
	h.send(updateMsg(held(five, "development", "lfg", core.ClaimRunning)))

	u := handledBy(five, "development", "crew:development:failed")
	u.Snapshot.Handled[0].Failures = []crew.ActionFailure{{Action: "lfg", Reason: "tests failed"}}
	h.send(updateMsg(u))

	view := h.view()
	if strings.Contains(boardOf(t, view), "#5") {
		t.Errorf("the board still shows #5:\n%s", view)
	}
	contains(t, view, "NEEDS ATTENTION   #5")
}

// eightStages are eight shown stages, s1 to s8, each taking "sN".
func eightStages() []crew.Stage {
	var out []crew.Stage
	for i := 1; i <= 8; i++ {
		out = append(out, crew.Stage{Name: fmt.Sprintf("s%d", i), Label: crew.State(fmt.Sprintf("s%d", i))})
	}
	return out
}

// Covers AE4.
func TestAE4EmptyColumnsDropThenTheBoardScrollsSideways(t *testing.T) {
	u := engine.Update{Snapshot: engine.Snapshot{View: core.View{Issues: []core.IssueView{
		{Issue: crew.Issue{Key: "1", Ref: "#1", Title: "One"}, Stage: "s2", Claim: core.ClaimRunning},
		{Issue: crew.Issue{Key: "2", Ref: "#2", Title: "Two"}, Stage: "s6", Claim: core.ClaimRunning},
	}}}}

	h := newWorkflowHarness(t, 80, eightStages())
	h.send(updateMsg(u))
	board := boardOf(t, h.view())
	contains(t, board, "6 empty stages not shown")
	if got := strings.Join(strings.Fields(strings.Split(board, "\n")[1]), " "); got != "s2 s6" {
		t.Errorf("columns = %q, want s2 s6:\n%s", got, board)
	}

	h = newWorkflowHarness(t, 30, eightStages())
	h.send(updateMsg(u))
	board = boardOf(t, h.view())
	contains(t, board, "s2", "1 ▸", "#1")
	if strings.Contains(board, "#2") {
		t.Errorf("a 30-column board shows both columns:\n%s", board)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	board = boardOf(t, h.view())
	contains(t, board, "◂ 1", "s6", "#2")
	h.send(tea.KeyPressMsg{Code: tea.KeyRight})
	contains(t, boardOf(t, h.view()), "◂ 1", "#2")
	h.send(tea.KeyPressMsg{Code: tea.KeyLeft})
	contains(t, boardOf(t, h.view()), "#1", "1 ▸")
}

func TestAWaitingCardWhoseIssueIsGoneLeavesTheBoard(t *testing.T) {
	u := handledBy(twelve, "triage", "crew:triage:done")
	u.Snapshot.Handled[0].Gone = true

	h := newWorkflowHarness(t, 120, crewWorkflow)
	h.send(updateMsg(u))

	if board := boardOf(t, h.view()); strings.Contains(board, "#12") {
		t.Errorf("the board shows a gone issue:\n%s", board)
	}
}

func TestAGivenUpMoveLeavesNoCard(t *testing.T) {
	u := handledBy(twelve, "triage", "crew:triage:done")
	u.Snapshot.Handled[0].Move = crew.MoveDropped

	h := newWorkflowHarness(t, 120, crewWorkflow)
	h.send(updateMsg(u))

	if board := boardOf(t, h.view()); strings.Contains(board, "#12") {
		t.Errorf("the board shows an issue whose move was given up:\n%s", board)
	}
}

func TestAnItemTheNextStageWouldNotTakeHasNoWaitingCard(t *testing.T) {
	pr := twelve
	pr.Kind = crew.KindPullRequest

	h := newWorkflowHarness(t, 120, crewWorkflow)
	h.send(updateMsg(handledBy(pr, "triage", "crew:triage:done")))

	if board := boardOf(t, h.view()); strings.Contains(board, "#12") {
		t.Errorf("the board shows a pull request no stage of its kind takes:\n%s", board)
	}
}

func TestWithEveryStageHiddenTheBoardSaysSo(t *testing.T) {
	h := newWorkflowHarness(t, 80, []crew.Stage{{Name: "only", Label: "ready", OffBoard: true}})

	contains(t, boardOf(t, h.view()), "every stage is hidden")
}

// Covers R11 and KTD13: each claim reads through its icon.
func TestEachClaimReadsThroughItsIcon(t *testing.T) {
	for claim, want := range map[core.Claim]string{
		core.ClaimRunning: "⠋ running", core.ClaimJudging: "⠋ judging", core.ClaimTaking: "◌ taking",
		core.ClaimOwed: "! owed", core.ClaimStopping: "■ stopping",
	} {
		h := newWorkflowHarness(t, 120, crewWorkflow)
		h.send(updateMsg(held(twelve, "triage", "triage", claim)))
		contains(t, boardOf(t, h.view()), "▌ "+want)
	}
}

func TestAColumnCapsItsCardsWhenTheWindowIsShort(t *testing.T) {
	issues := make([]core.IssueView, 0, 8)
	for n := range 8 {
		issues = append(issues, core.IssueView{
			Issue: crew.Issue{Key: strconv.Itoa(n), Ref: fmt.Sprintf("#%d", n), Title: "Card"}, Stage: "triage",
			Claim: core.ClaimRunning,
		})
	}
	h := newWorkflowHarness(t, 80, crewWorkflow)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	h.send(updateMsg(engine.Update{Snapshot: engine.Snapshot{View: core.View{Issues: issues}}}))

	view := checkFits(t, h, 80, 24)
	contains(t, boardOf(t, view), "more")
	if !strings.HasSuffix(view, "? help") {
		t.Errorf("view does not end with the key-help line:\n%s", view)
	}
}
