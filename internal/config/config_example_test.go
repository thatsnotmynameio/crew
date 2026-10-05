package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// exampleConfig is the repository's committed example of .crew/config.yaml;
// your own copy is ignored by git.
var exampleConfig = filepath.Join("..", "..", ".crew", "config.example.yaml")

// loadExample loads exampleConfig as a repository's .crew/config.yaml, linked
// into a new repository root.
func loadExample(t *testing.T) *config.Config {
	t.Helper()
	example, err := filepath.Abs(exampleConfig)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(example, filepath.Join(root, ".crew", "config.yaml")); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load(%s) = %v", exampleConfig, err)
	}
	return cfg
}

// exampleRule is how a rule of the example config loads, its actions
// summed up as "action: agent A, bot B, check C", where C is whether it has
// one.
type exampleRule struct {
	name    string
	labels  crew.Labels
	queue   crew.Queue
	notify  bool
	actions []string
}

// crew runs on its own repository, so its example config must stay valid,
// and keep the rules, actions, queues, bots and labels it had in the old
// keys (KTD8): the same names, so failed runs still resume. refinement
// replaced triage in #160, while no issue was in a triage state.
func TestTheRepositorysOwnConfigLoads(t *testing.T) {
	cfg := loadExample(t)
	if got, want := exampleRules(cfg.Rules), wantExampleRules(); !reflect.DeepEqual(got, want) {
		t.Errorf("rules = %+v\nwant %+v", got, want)
	}
	wantBots := []string{"clerk", "product-manager", "developer"}
	if cfg.Bot != "clerk" || !reflect.DeepEqual(cfg.Bots, wantBots) {
		t.Errorf("Bot = %q, Bots = %q; want clerk, %q", cfg.Bot, cfg.Bots, wantBots)
	}
	columns := make([]string, 0, len(cfg.Board))
	for _, c := range cfg.Board {
		columns = append(columns, c.Name)
	}
	if want := []string{"refinement", "development", "fix"}; !reflect.DeepEqual(columns, want) || cfg.BoardWritten {
		t.Errorf("board columns = %q (written %v), want %q", columns, cfg.BoardWritten, want)
	}
	if script := checkScript(cfg.Rules[3].Actions[0], "pr-closes-issue"); !strings.Contains(script, `"Closes " + env.CREW_ISSUE_REF`) {
		t.Errorf("development's check = %q, want the script of pr-closes-issue", script)
	}
}

// splitOutcomes are the outcomes /cw-split-plan reports and the refine
// prompt acts on (#160).
var splitOutcomes = []string{"`not split`", "`kept whole`", "`split`", "`earlier split did not finish`"}

// The markers /cw-split-plan writes: partMarker, followed by the parent's
// reference and " -->", on each part, and recordMarker on the split record.
const (
	partMarker   = "<!-- cw-split-plan: part of "
	recordMarker = "<!-- cw-split-plan: split record -->"
)

// The refine action splits a large plan before it finds blockers, finishes
// the split by labelling the parts and taking the parent out of crew, and
// its check fails a split that stopped before that (#160).
func TestTheRefineActionSplitsBeforeFindingBlockers(t *testing.T) {
	refine := loadExample(t).Rules[1].Actions[0]
	prompt := refine.Prompt
	split := strings.Index(prompt, "/cw-split-plan {{.Issue.Ref}}")
	if split < 0 || split > strings.Index(prompt, "dependencies/blocked_by") {
		t.Errorf("the prompt does not run /cw-split-plan before it reads dependencies:\n%s", prompt)
	}
	for _, want := range []string{
		`--add-label "crew:refinement:done"`,
		`--remove-label "crew:refinement:in progress"`,
		recordMarker,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q", want)
		}
	}
	for _, want := range []string{`"crew:refinement:in progress"`, partMarker + "$CREW_ISSUE_REF -->", "/sub_issues"} {
		if script := checkScript(refine, "split-finished"); !strings.Contains(script, want) {
			t.Errorf("refine's check = %q, want the script of split-finished, with %q", script, want)
		}
	}
}

// The refine prompt branches on the outcomes /cw-split-plan reports, so both
// name the same outcomes and markers.
func TestTheRefinePromptAndTheSplitSkillAgree(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "cw-split-plan", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	prompt := loadExample(t).Rules[1].Actions[0].Prompt
	for _, want := range splitOutcomes {
		if !strings.Contains(string(skill), want) || !strings.Contains(prompt, want) {
			t.Errorf("the skill and the refine prompt do not both name the outcome %s", want)
		}
	}
	for _, want := range []string{partMarker + "#", recordMarker} {
		if !strings.Contains(string(skill), want) {
			t.Errorf("the skill does not write the marker %s", want)
		}
	}
}

// wantExampleRules are the example config's rules, as in the old keys: the
// promote rules, now without actions, notify nothing and need no failure.
func wantExampleRules() []exampleRule {
	clerk, developer := crew.Queue{Name: "clerk", Slots: 1}, crew.Queue{Name: "developer", Slots: 2}
	productManager := crew.Queue{Name: "product-manager", Slots: 1}
	labels := func(rule, success string) crew.Labels {
		return crew.Labels{
			Ready: crew.State("crew:" + rule + ":ready"), Running: crew.State("crew:" + rule + ":in progress"),
			Success: crew.State(success), Failure: crew.State("crew:" + rule + ":failed"),
		}
	}
	want := []exampleRule{
		{
			name: "promote brainstorm", queue: clerk,
			labels: crew.Labels{
				Ready: "crew:brainstorm:done", Running: "crew:brainstorm:promoting", Success: "crew:refinement:ready",
			},
		},
		{
			name: "refinement", queue: productManager, notify: true, labels: labels("refinement", "crew:refinement:done"),
			actions: []string{"refine: agent product-manager, bot product-manager, checks [split-finished]"},
		},
		{
			name: "promote refinement", queue: clerk,
			labels: crew.Labels{
				Ready: "crew:refinement:done", Running: "crew:refinement:promoting", Success: "crew:development:ready",
			},
		},
		{
			name: "development", queue: developer, notify: true,
			labels:  labels("development", "crew:development:waiting review"),
			actions: []string{"lfg: agent developer, bot developer, checks [pr-closes-issue]"},
		},
		{
			name: "fix", queue: developer, notify: true, labels: labels("fix", "crew:fix:waiting review"),
			actions: []string{"lfg: agent developer, bot developer, checks [pr-closes-issue]"},
		},
	}
	return want
}

// checkScript returns the script of a's check called name, or "" when a
// names no such check.
func checkScript(a crew.Action, name string) string {
	for _, c := range a.Checks {
		if c.Name == name {
			return c.Script
		}
	}
	return ""
}

// checkNames returns the names of checks, in order, as [a b].
func checkNames(checks []crew.Check) string {
	names := make([]string, len(checks))
	for i, c := range checks {
		names[i] = c.Name
	}
	return fmt.Sprint(names)
}

// exampleRules sums rules up as exampleRule.
func exampleRules(rules []crew.Rule) []exampleRule {
	out := make([]exampleRule, len(rules))
	for i, r := range rules {
		out[i] = exampleRule{name: r.Name, labels: r.Labels, queue: r.Queue, notify: r.Notify}
		for _, a := range r.Actions {
			out[i].actions = append(out[i].actions,
				fmt.Sprintf("%s: agent %s, bot %s, checks %s", a.Name, a.Agent, a.Bot, checkNames(a.Checks)))
		}
	}
	return out
}
