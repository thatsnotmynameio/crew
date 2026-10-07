package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// boardRules is one agent and the rules promote triage, without actions,
// triage and development, which takes pull requests, for the board's tests.
// It ends on line 22.
const boardRules = oneAgent + `rules:
  promote triage:
    labels: {ready: "crew:triage:done", running: "crew:triage:promoting"}
    routes: {passed: "crew:development:ready"}
  triage:
    labels: {ready: "crew:triage:ready", running: "crew:triage:in progress"}
    actions:
      - {name: triage, prompt: "Triage {{.Issue.Ref}}"}
    routes: {passed: "crew:triage:done", failed: [report, move: "crew:triage:failed"]}
  development:
    takes: pull_requests
    labels:
      ready: "crew:development:ready"
      running: "crew:development:in progress"
    actions:
      - {name: lfg, prompt: "/lfg {{.Issue.Ref}}"}
    routes:
      passed: "crew:development:done"
      failed: [report, move: "crew:development:failed"]
`

// Covers AE5: without board, the board has one column per rule that has
// actions, in file order, with the rule's ready and running labels and its
// kind.
func TestAE5WithoutBoardEveryRuleWithActionsHasAColumn(t *testing.T) {
	cfg := load(t, boardRules)
	want := []crew.BoardColumn{
		{Name: "triage", Labels: []crew.State{"crew:triage:ready", "crew:triage:in progress"}},
		{
			Name: "development", Labels: []crew.State{"crew:development:ready", "crew:development:in progress"},
			Takes: crew.KindPullRequest,
		},
	}
	if !reflect.DeepEqual(cfg.Board, want) || cfg.BoardWritten {
		t.Errorf("Board = %+v (written %v)\nwant %+v, not written", cfg.Board, cfg.BoardWritten, want)
	}
}

// KTD17: without board, each label a waiting route moves to has a column of
// its own after its rule's, named after it and showing the rule's kind, so
// an issue paused there stays on screen. A label already on the board for
// that kind gets no second column.
func TestWithoutBoardEveryWaitingRouteLabelHasAColumn(t *testing.T) {
	waiting := `
  review:
    takes: pull_requests
    labels: {ready: "crew:review:ready", running: "crew:review:in progress"}
    actions:
      - {name: review, prompt: "Review {{.Issue.Ref}}", on: {waiting: asks, blocked: handed}}
      - {name: again, prompt: "Again {{.Issue.Ref}}", on: {waiting: handed}}
    routes:
      passed: "crew:review:done"
      failed: [report, move: "crew:review:failed"]
      asks: "crew:development:waiting answer"
      handed: "crew:development:ready"
`
	body := strings.Replace(boardRules, `      - {name: lfg, prompt: "/lfg {{.Issue.Ref}}"}`,
		`      - {name: lfg, prompt: "/lfg {{.Issue.Ref}}", on: {waiting: waiting-answer}}`, 1) +
		`      waiting-answer: [comment: "{{.Action}} asks", move: "crew:development:waiting answer"]` + waiting
	cfg := load(t, body)
	want := []crew.BoardColumn{
		{Name: "triage", Labels: []crew.State{"crew:triage:ready", "crew:triage:in progress"}},
		{
			Name: "development", Labels: []crew.State{"crew:development:ready", "crew:development:in progress"},
			Takes: crew.KindPullRequest,
		},
		{
			Name: "crew:development:waiting answer", Labels: []crew.State{"crew:development:waiting answer"},
			Takes: crew.KindPullRequest,
		},
		{
			Name: "review", Labels: []crew.State{"crew:review:ready", "crew:review:in progress"},
			Takes: crew.KindPullRequest,
		},
	}
	if !reflect.DeepEqual(cfg.Board, want) || cfg.BoardWritten {
		t.Errorf("Board = %+v (written %v)\nwant %+v, not written", cfg.Board, cfg.BoardWritten, want)
	}
}

func TestLoadBoard(t *testing.T) {
	tests := []struct {
		name  string
		board string
		want  []crew.BoardColumn
	}{
		{
			name:  "columns in file order, of one label or a list",
			board: "board:\n  ideas: crew:brainstorm:ready\n  bugs: [bug]\n  done: [crew:brainstorm:done, crew:triage:done]\n",
			want: []crew.BoardColumn{
				{Name: "ideas", Labels: []crew.State{"crew:brainstorm:ready"}},
				{Name: "bugs", Labels: []crew.State{"bug"}},
				{Name: "done", Labels: []crew.State{"crew:brainstorm:done", "crew:triage:done"}},
			},
		},
		{
			name:  "a label no rule names and no issue carries",
			board: "board:\n  bugs: bgu\n",
			want:  []crew.BoardColumn{{Name: "bugs", Labels: []crew.State{"bgu"}}},
		},
		{
			name:  "labels take the rules' or their first spelling",
			board: "board:\n  triage: [Crew:Triage:Ready, CREW:TRIAGE:DONE]\n  bugs: bug\n  more bugs: [BUG]\n",
			want: []crew.BoardColumn{
				{Name: "triage", Labels: []crew.State{"crew:triage:ready", "crew:triage:done"}},
				{Name: "bugs", Labels: []crew.State{"bug"}},
				{Name: "more bugs", Labels: []crew.State{"bug"}},
			},
		},
		{
			name:  "a label written twice in one column counts once",
			board: "board:\n  bugs: [bug, Bug, bug]\n",
			want:  []crew.BoardColumn{{Name: "bugs", Labels: []crew.State{"bug"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, boardRules+tt.board)
			if !reflect.DeepEqual(cfg.Board, tt.want) || !cfg.BoardWritten {
				t.Errorf("Board = %+v (written %v)\nwant %+v, written", cfg.Board, cfg.BoardWritten, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidBoard(t *testing.T) {
	testRejects(t, []rejectCase{
		{
			name:  "a column with no label",
			body:  boardRules + "board:\n  ideas: idea\n  bugs: []\n",
			wants: []string{"board.bugs", "line 25", `column "bugs"`, "one or more labels"},
		},
		{
			name:  "a column without a value",
			body:  boardRules + "board:\n  bugs:\n",
			wants: []string{"board.bugs", "line 24", `column "bugs"`, "one or more labels"},
		},
		{
			name:  "a column with an empty label",
			body:  boardRules + "board:\n  bugs: [bug, \"\"]\n",
			wants: []string{"board.bugs", "line 24", `column "bugs"`, "empty label"},
		},
		{
			name:  "a column of one empty label",
			body:  boardRules + "board:\n  bugs: \"\"\n",
			wants: []string{"board.bugs", "line 24", "empty label"},
		},
		{
			name:  "no column",
			body:  boardRules + "board: {}\n",
			wants: []string{"board", "line 23", "one or more columns"},
		},
		{
			name:  "two columns share a name",
			body:  boardRules + "board:\n  bugs: bug\n  bugs: defect\n",
			wants: []string{"board.bugs", "line 25", "duplicate key, first set on line 24"},
		},
		{
			name:  "a board written as a list",
			body:  boardRules + "board:\n  - bugs\n",
			wants: []string{"board", "line 24", "must be a mapping"},
		},
		{
			name:  "a column that is a mapping",
			body:  boardRules + "board:\n  bugs: {labels: [bug]}\n",
			wants: []string{"board.bugs", "line 24"},
		},
	})
}

// Errors in the board and in the rules come together, so you fix them in
// one go.
func TestLoadReportsBoardAndRulesErrorsTogether(t *testing.T) {
	loadErr(t, "rules:\n  implement:\n    labels: {ready: ready}\nboard:\n  bugs: []\n",
		"rules.implement.labels.running", "board.bugs")
}
