package config_test

import (
	"strings"
	"testing"
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
	if got, want := cfg.Rules[0].Actions[0].Check, "gh pr list\nexit 0"; got != want {
		t.Errorf("Check = %q, want %q", got, want)
	}
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
