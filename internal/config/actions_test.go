package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// shellRule is a config whose top-level actions are defs, written as is,
// and whose one rule runs the session of oneRule and then the action ref.
func shellRule(defs, ref string) string {
	return defs + strings.Replace(oneRule, "    routes:\n", "      - "+ref+"\n    routes:\n", 1)
}

// shellOf returns the shell action a runs, and fails t when a is not one.
func shellOf(t *testing.T, a crew.Action) crew.ShellSpec {
	t.Helper()
	s, ok := a.Kind.(crew.ShellSpec)
	if !ok {
		t.Fatalf("action %s runs %T, want a shell action", a.Name, a.Kind)
	}
	return s
}

// A shell action is defined once at the top, as a script or as a mapping
// with its script, its exit codes' verdicts and where a resume starts (R5,
// R6, R8, R54).
func TestLoadReadsEveryShapeOfAShellAction(t *testing.T) {
	tests := []struct {
		name string
		defs string
		want crew.ShellSpec
	}{
		{
			name: "a string is its script",
			defs: "actions:\n  lint: make lint\n",
			want: crew.ShellSpec{Script: "make lint"},
		},
		{
			// A script is a shell command, never a template: braces stay as
			// written.
			name: "not a template",
			defs: "actions:\n  lint: \"echo '{{.Issue.Title}}'\"\n",
			want: crew.ShellSpec{Script: "echo '{{.Issue.Title}}'"},
		},
		{
			name: "a mapping with its script alone",
			defs: "actions:\n  lint:\n    script: make lint\n",
			want: crew.ShellSpec{Script: "make lint"},
		},
		{
			name: "verdicts by exit code",
			defs: "actions:\n  lint:\n    script: ./judge\n    verdicts:\n      3: needs_person\n      4: blocked\n",
			want: crew.ShellSpec{Script: "./judge", Verdicts: map[int]crew.Verdict{3: "needs_person", 4: "blocked"}},
		},
		{
			name: "a resume that starts at itself",
			defs: "actions:\n  lint:\n    script: gh pr checks\n    resume: self\n",
			want: crew.ShellSpec{Script: "gh pr checks", ResumeSelf: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions := load(t, shellRule(tt.defs, "lint")).Rules[0].Actions
			if got := shellOf(t, actions[1]); actions[1].Name != "lint" || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("action %s = %+v, want lint, %+v", actions[1].Name, got, tt.want)
			}
		})
	}
}

// A shell action no rule names is fine, and an alias names one.
func TestLoadResolvesAnAliasToAShellAction(t *testing.T) {
	cfg := load(t, shellRule("actions:\n  unused: \"false\"\n  &n lint: make lint\n", "*n"))
	if a := cfg.Rules[0].Actions[1]; a.Name != "lint" || shellOf(t, a).Script != "make lint" {
		t.Errorf("action = %+v, want lint running make lint", a)
	}
}

func TestLoadRejectsInvalidShellActions(t *testing.T) {
	testRejects(t, invalidShellActions)
}

// invalidShellActions are errors in the top-level actions.
var invalidShellActions = []rejectCase{
	{
		name:  "actions written as a list",
		body:  "actions:\n  - make test\n" + oneRule,
		wants: []string{"actions", "line 2", "must be a mapping"},
	},
	{
		name:  "an empty script",
		body:  "actions:\n  test: \"\"\n" + oneRule,
		wants: []string{"actions.test", "line 2", "must not be empty"},
	},
	{
		// A blank script would run nothing and pass every time.
		name:  "a blank script",
		body:  "actions:\n  test: \"  \\n \"\n" + oneRule,
		wants: []string{"actions.test", "line 2", "must not be empty"},
	},
	{
		name:  "a mapping without script",
		body:  "actions:\n  test:\n    resume: self\n" + oneRule,
		wants: []string{"actions.test.script", "line 3", "required"},
	},
	{
		name:  "a list",
		body:  "actions:\n  test: [make, test]\n" + oneRule,
		wants: []string{"actions.test", "line 2", "must be a shell script, or a mapping with script"},
	},
	{
		// A preset is of a registered function, and Load here registers none.
		name:  "a preset of a function not registered",
		body:  "actions:\n  greet:\n    name: comment\n    text: hi\n" + oneRule,
		wants: []string{"actions.greet.name", "line 3", `no function is named "comment"; the registered functions are none`},
	},
	{
		name:  "two shell actions share a name",
		body:  "actions:\n  test: make test\n  test: go test\n" + oneRule,
		wants: []string{"actions.test", "line 3", "duplicate key, first set on line 2"},
	},
	{
		name:  "a name of the grammar",
		body:  "actions:\n  report: ./report\n" + oneRule,
		wants: []string{"actions.report", "line 2", `"report" is a word of the rules' grammar`},
	},
	{
		name:  "next as a name",
		body:  "actions:\n  next: ./next\n" + oneRule,
		wants: []string{"actions.next", "line 2", `"next" is a word of the rules' grammar`},
	},
	{
		name:  "a verdict that is not a verdict's name",
		body:  "actions:\n  judge:\n    script: ./judge\n    verdicts:\n      3: Needs Person\n" + oneRule,
		wants: []string{"actions.judge.verdicts.3", "line 5", `verdict "Needs Person"`},
	},
	{
		name:  "an exit code that is not a number",
		body:  "actions:\n  judge:\n    script: ./judge\n    verdicts:\n      three: blocked\n" + oneRule,
		wants: []string{"actions.judge.verdicts.three", "line 5", "must be an exit status, from 0 to 255"},
	},
	{
		name:  "an exit code no process returns",
		body:  "actions:\n  judge:\n    script: ./judge\n    verdicts:\n      256: blocked\n" + oneRule,
		wants: []string{"actions.judge.verdicts.256", "line 5", "must be an exit status, from 0 to 255"},
	},
	{
		name:  "a resume start other than self",
		body:  "actions:\n  ci:\n    script: gh pr checks\n    resume: session\n" + oneRule,
		wants: []string{"actions.ci.resume", "line 4", `"session" must be self`},
	},
	{
		// The old variables would reach the script empty (KTD14).
		name:  "a script that reads CREW_MATES",
		body:  "actions:\n  bots: |-\n    for b in $CREW_MATES; do echo \"$b\"; done\n" + oneRule,
		wants: []string{"actions.bots", "line 2", "CREW_MATES is now CREW_BOTS"},
	},
	{
		name:  "a mapping's script that reads both old variables",
		body:  "actions:\n  people:\n    script: echo $CREW_BOSS $CREW_MATES\n" + oneRule,
		wants: []string{"actions.people.script", "line 3", "CREW_BOSS is now CREW_CODE_OWNERS; CREW_MATES is now CREW_BOTS"},
	},
}
