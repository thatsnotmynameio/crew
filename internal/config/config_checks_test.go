package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// checkedRule is oneRule whose action names the check name, on line 13,
// after checks, which is written as is.
func checkedRule(checks, name string) string {
	return checks + strings.Replace(oneRule, "        prompt:", "        check: "+name+"\n        prompt:", 1)
}

// Covers AE7: an action's check must name a check of checks.
func TestAE7AnActionNamingACheckChecksLacksIsRefused(t *testing.T) {
	loadErr(t, checkedRule("", "pr-closes-issue"),
		"rules.implement.actions.development.check", "line 13", `check "pr-closes-issue" does not exist`,
		"checks declares none")
	loadErr(t, checkedRule("checks:\n  lint: make lint\n  test: make test\n", "pr-closes-issue"),
		"rules.implement.actions.development.check", "line 16", `check "pr-closes-issue" does not exist`,
		"the checks are lint, test")
}

// A check is defined once and runs for every action that names it; a check
// no action names is fine.
func TestLoadGivesEveryActionTheScriptOfItsCheck(t *testing.T) {
	cfg := load(t, checkedRule("checks:\n  unused: \"false\"\n  pr-closes-issue: |-\n    gh pr list\n    exit 0\n",
		"pr-closes-issue"))
	want := []crew.Check{{Name: "pr-closes-issue", Script: "gh pr list\nexit 0"}}
	if got := cfg.Rules[0].Actions[0].Checks; !reflect.DeepEqual(got, want) {
		t.Errorf("Checks = %q, want %q", got, want)
	}
}

// R3: an action's check may be a list of checks, which run in the order the
// list gives; the same check may come twice.
func TestLoadGivesAnActionItsListOfChecksInOrder(t *testing.T) {
	checks := "checks:\n  judge: ./judge\n  pr-closes-issue: gh pr list\n"
	cfg := load(t, checkedRule(checks, "[judge, pr-closes-issue, judge]"))
	want := []crew.Check{
		{Name: "judge", Script: "./judge"},
		{Name: "pr-closes-issue", Script: "gh pr list"},
		{Name: "judge", Script: "./judge"},
	}
	if got := cfg.Rules[0].Actions[0].Checks; !reflect.DeepEqual(got, want) {
		t.Errorf("Checks = %q, want %q", got, want)
	}
	block := strings.Replace(oneRule, "        prompt:", "        check:\n          - pr-closes-issue\n        prompt:", 1)
	cfg = load(t, checks+block)
	if got := cfg.Rules[0].Actions[0].Checks; len(got) != 1 || got[0].Name != "pr-closes-issue" {
		t.Errorf("Checks from a block list = %q, want pr-closes-issue alone", got)
	}
}

// An action without a check has none.
func TestLoadLeavesAnActionWithoutACheckWithNone(t *testing.T) {
	if got := load(t, oneRule).Rules[0].Actions[0].Checks; got != nil {
		t.Errorf("Checks = %q, want none", got)
	}
}

func TestLoadRejectsInvalidCheckLists(t *testing.T) {
	checks := "checks:\n  lint: make lint\n  test: make test\n"
	testRejects(t, []rejectCase{
		{
			name:  "an empty list",
			body:  checkedRule(checks, "[]"),
			wants: []string{"rules.implement.actions.development.check", "line 16", "must name at least one check"},
		},
		{
			name: "a list with two names that are not checks",
			body: checkedRule(checks, "[lint, vet, fmt]"),
			wants: []string{
				"rules.implement.actions.development.check[1]", `check "vet" does not exist; the checks are lint, test`,
				"rules.implement.actions.development.check[2]", `check "fmt" does not exist`,
			},
		},
		{
			name:  "a mapping",
			body:  checkedRule(checks, "{lint: test}"),
			wants: []string{"rules.implement.actions.development.check", "line 16", "must be a check's name or a list of checks' names"},
		},
		{
			name:  "a list holding a list",
			body:  checkedRule(checks, "[lint, [test]]"),
			wants: []string{"rules.implement.actions.development.check[1]", "must be a check's name or a list of checks' names"},
		},
	})
}

func TestLoadRejectsInvalidChecks(t *testing.T) {
	testRejects(t, []rejectCase{
		{
			name:  "checks written as a list",
			body:  "checks:\n  - make test\n" + oneRule,
			wants: []string{"checks", "line 2", "must be a mapping"},
		},
		{
			name:  "an empty check",
			body:  "checks:\n  test: \"\"\n" + oneRule,
			wants: []string{"checks.test", "line 2", "must not be empty"},
		},
		{
			// A blank check would run nothing and pass every time.
			name:  "a blank check",
			body:  "checks:\n  test: \"  \\n \"\n" + oneRule,
			wants: []string{"checks.test", "line 2", "must not be empty"},
		},
		{
			name:  "a check of the wrong type",
			body:  "checks:\n  test: [make, test]\n" + oneRule,
			wants: []string{"checks.test", "line 2"},
		},
		{
			name:  "two checks share a name",
			body:  "checks:\n  test: make test\n  test: go test\n" + oneRule,
			wants: []string{"checks.test", "line 3", "duplicate key, first set on line 2"},
		},
		{
			// The old variables would reach the check empty (KTD14).
			name:  "a check that reads CREW_MATES",
			body:  "checks:\n  bots: |-\n    for b in $CREW_MATES; do echo \"$b\"; done\n" + oneRule,
			wants: []string{"checks.bots", "line 2", "CREW_MATES is now CREW_BOTS"},
		},
		{
			name:  "a check that reads both old variables",
			body:  "checks:\n  people: echo $CREW_BOSS $CREW_MATES\n" + oneRule,
			wants: []string{"checks.people", "line 2", "CREW_BOSS is now CREW_CODE_OWNERS; CREW_MATES is now CREW_BOTS"},
		},
	})
}
