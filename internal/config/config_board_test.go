package config_test

import (
	"reflect"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// boardRules is a set of rules with a triage rule, and an extra, for the
// board's tests.
const boardRules = `extra_labels:
  - label: crew:brainstorm:ready
workflow:
  - name: triage
    label: crew:triage:ready
    moves_to: crew:triage:in progress
    on_success: crew:triage:done
    on_failure: crew:triage:failed
    actions:
      - name: triage
        prompt: "Triage {{.Issue.Ref}}"
`

// boardCase is a config with a board, and the columns Load must read from
// it.
type boardCase struct {
	name string
	body string
	want []crew.BoardColumn
}

// validBoards are boards Load reads.
var validBoards = []boardCase{
	{
		name: "no board keeps the stages' board",
		body: boardRules,
	},
	{
		name: "columns in file order with their labels",
		body: boardRules + `board:
  - name: ideas
    labels: [crew:brainstorm:ready]
  - name: bugs
    labels: [bug]
  - name: done
    labels: [crew:brainstorm:done, crew:triage:done]
`,
		want: []crew.BoardColumn{
			{Name: "ideas", Labels: []string{"crew:brainstorm:ready"}},
			{Name: "bugs", Labels: []string{"bug"}},
			{Name: "done", Labels: []string{"crew:brainstorm:done", "crew:triage:done"}},
		},
	},
	{
		// Covers AE6.
		name: "a label no issue carries",
		body: boardRules + `board:
  - name: bugs
    labels: [bgu]
`,
		want: []crew.BoardColumn{{Name: "bugs", Labels: []string{"bgu"}}},
	},
	{
		name: "labels take the workflow's, the extras' or their first spelling",
		body: boardRules + `board:
  - name: triage
    labels: [Crew:Triage:Ready, CREW:BRAINSTORM:READY]
  - name: bugs
    labels: [bug]
  - name: more bugs
    labels: [BUG]
`,
		want: []crew.BoardColumn{
			{Name: "triage", Labels: []string{"crew:triage:ready", "crew:brainstorm:ready"}},
			{Name: "bugs", Labels: []string{"bug"}},
			{Name: "more bugs", Labels: []string{"bug"}},
		},
	},
	{
		name: "a label written twice in one column counts once",
		body: boardRules + `board:
  - name: bugs
    labels: [bug, Bug, bug]
`,
		want: []crew.BoardColumn{{Name: "bugs", Labels: []string{"bug"}}},
	},
}

func TestLoadBoard(t *testing.T) {
	for _, tt := range validBoards {
		t.Run(tt.name, func(t *testing.T) {
			if got := load(t, tt.body).Board; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Board = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadRejectsInvalidBoard(t *testing.T) {
	testRejects(t, invalidBoards)
}

// invalidBoards are errors in board.
var invalidBoards = []rejectCase{
	{
		// Covers AE5.
		name: "a column with no label",
		body: oneRule + `board:
  - name: ideas
    labels: [idea]
  - name: bugs
    labels: []
`,
		wants: []string{"board[1].labels", "line 14", `column "bugs"`, "one or more labels"},
	},
	{
		name: "a column without labels",
		body: oneRule + `board:
  - name: bugs
`,
		wants: []string{"board[0].labels", "line 11", `column "bugs"`, "one or more labels"},
	},
	{
		name: "a column without a name",
		body: oneRule + `board:
  - labels: [bug]
`,
		wants: []string{"board[0].name", "line 11", "required"},
	},
	{
		name: "a column with an empty label",
		body: oneRule + `board:
  - name: bugs
    labels: [bug, ""]
`,
		wants: []string{"board[0].labels", "line 12", `column "bugs"`, "empty label"},
	},
	{
		name:  "no column",
		body:  oneRule + "board: []\n",
		wants: []string{"board", "line 10", "one or more columns"},
	},
	{
		name: "two columns share a name",
		body: oneRule + `board:
  - name: bugs
    labels: [bug]
  - name: bugs
    labels: [defect]
`,
		wants: []string{"board[1].name", "line 13", `"bugs" is already board[0].name`},
	},
	{
		name:  "a board that is a mapping",
		body:  oneRule + "board:\n  bugs: [bug]\n",
		wants: []string{"board", "line 11", "must be a list of columns"},
	},
	{
		name:  "a column that is a string",
		body:  oneRule + "board:\n  - bugs\n",
		wants: []string{"board[0]", "line 11", "must be a column with name and labels"},
	},
	{
		name: "an unknown key in a column",
		body: oneRule + `board:
  - name: bugs
    label: bug
`,
		wants: []string{"board[0].label", "line 12", "unknown key"},
	},
}

// Errors in the board and in the rules come together, so you fix
// them in one go.
func TestLoadReportsBoardAndRulesErrorsTogether(t *testing.T) {
	loadErr(t, `workflow:
  - name: implement
    label: ready
board:
  - name: bugs
    labels: []
`, "workflow[0]", "board[0].labels")
}

func TestLoadRejectsANonMappingConfigNamingTheBoard(t *testing.T) {
	loadErr(t, "- board\n", "extra_labels, prompts and board")
}
