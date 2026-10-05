package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// actionsOf returns the Actions section of view.
func actionsOf(t *testing.T, view string) string {
	t.Helper()
	i := strings.Index(view, "Actions ")
	j := strings.Index(view, "\n\nQueues ")
	if i < 0 || j < i {
		t.Fatalf("view lacks the Actions section:\n%s", view)
	}
	return view[i:j]
}

// Covers R5 and R17: each action with its icon, reference and title, rule
// and action, queue, state with elapsed time, and branch.
func TestEachActionShowsItsIconQueueStateAndBranch(t *testing.T) {
	view := fitted(t, 120, 0, runningSnapshot())

	contains(t, actionsOf(t, view),
		"2 running · 1 waiting",
		" ⠋ #1  Add login form             implement/code    default   running 5m00s  crew/1-code",
		" ⠋ #1  Add login form             implement/tests   default   running 7m00s  crew/1-tests",
		" ○ #2  Fix the flaky stream test  review/check      clerk     waiting",
	)
}

// withSaid is runningSnapshot with #1's code session having said text.
func withSaid(text string) engine.Update {
	u := runningSnapshot()
	u.Snapshot.Said = []core.Said{{IssueKey: "1", Action: "code", Text: text}}
	return u
}

// Covers R18: a running action shows what its session last said, cut to
// the width; checking, it shows nothing.
func TestARunningActionShowsWhatItsSessionLastSaid(t *testing.T) {
	text := "Running go test -race ./internal/core; 2 failures left in model_test.go " + strings.Repeat("and more ", 20)
	view := fitted(t, 80, 0, withSaid(text))

	contains(t, actionsOf(t, view), "crew/1-code\n   └ Running go test -race ./internal/core; 2 failures left")
	if strings.Contains(view, "and more and more and more and more and more and more and more and more and more") {
		t.Errorf("the said line was not cut to the width:\n%s", view)
	}

	u := withSaid(text)
	u.Snapshot.Issues[0].Actions[0].Phase = core.PhaseChecking
	if view := fitted(t, 80, 0, u); strings.Contains(view, "└") {
		t.Errorf("an action running its check shows a said line:\n%s", view)
	}
}

func TestASaidLineIsCleanedOfEscapes(t *testing.T) {
	view := fitted(t, 120, 0, withSaid("done\x1b[31m red\x07 bell\nnext"))

	contains(t, view, "└ done red bell next")
}

// Covers R16: a resumed action names its workspace, a reopening one its
// phase.
func TestAResumedActionShowsItsWorkspaceAndAReopeningOneItsPhase(t *testing.T) {
	h := newHarness(t, 120)

	h.send(updateMsg(resumingSnapshot()))

	golden(t, "resuming", h.view())
	contains(t, h.view(), "reopening workspace", "resumed in issue-9-lfg, running 3m00s")
}

func TestWithNoActionTheSectionSaysNone(t *testing.T) {
	h := newHarness(t, 80)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 0})

	contains(t, actionsOf(t, h.view()), "0 running\n none")
}
