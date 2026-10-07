package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// ownConfig is crew's own config, committed as this repository's
// .crew/config.yaml.
var ownConfig = filepath.Join("..", "..", ".crew", "config.yaml")

// loadOwn loads ownConfig alone, linked into a new repository root, so a
// developer's .crew/config.local.yaml never reaches it.
func loadOwn(t *testing.T) *config.Config {
	t.Helper()
	own, err := filepath.Abs(ownConfig)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(own, filepath.Join(root, ".crew", "config.yaml")); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatalf("Load(%s) = %v", ownConfig, err)
	}
	return cfg
}

// ownRule is how a rule of crew's own config loads, its actions summed up
// as "session S: agent A, bot B" or "shell S", each followed by its on as
// "; verdict to target", and its routes as "route: step, step".
type ownRule struct {
	name    crew.RuleName
	labels  crew.Labels
	queue   crew.Queue
	notify  bool
	actions []string
	routes  []string
}

// crew runs on its own repository, so its own config must stay valid,
// and keep the rules, sessions, queues, bots and labels it had in the old
// format (KTD8, KTD-S3): the same names, each check a shell action after
// its session, each success label the passed route and each failure label
// a failed route that reports, then moves (N1). development and fix send
// the judge's needs_person to a needs-person route that comments, then
// moves (AE2). refinement replaced triage in #160, while no issue was in a
// triage state.
func TestTheRepositorysOwnConfigLoads(t *testing.T) {
	cfg := loadOwn(t)
	if got, want := ownRules(cfg.Rules, cfg.Notify), wantOwnRules(); !reflect.DeepEqual(got, want) {
		t.Errorf("rules = %+v\nwant %+v", got, want)
	}
	wantBots := []crew.BotName{"clerk", "product-manager", "developer"}
	if cfg.Bot.Name != "clerk" || !reflect.DeepEqual(cfg.BotNames(), wantBots) {
		t.Errorf("Bot = %q, Bots = %q; want clerk, %q", cfg.Bot, cfg.Bots, wantBots)
	}
	columns := make([]string, 0, len(cfg.Board))
	for _, c := range cfg.Board {
		columns = append(columns, c.Name)
	}
	if want := []string{"refinement", "development", "fix"}; !reflect.DeepEqual(columns, want) || cfg.BoardWritten {
		t.Errorf("board columns = %q (written %v), want %q", columns, cfg.BoardWritten, want)
	}
	script := shellScript(t, ownRuleNamed(t, cfg, "development"), "pr-closes-issue")
	if !strings.Contains(script, `"Closes " + env.CREW_ISSUE_REF`) {
		t.Errorf("development's pr-closes-issue = %q, want the script of pr-closes-issue", script)
	}
	judge, ok := ownRuleNamed(t, cfg, "development").Action("session-finished").Kind.(crew.ShellSpec)
	if want := map[int]crew.Verdict{3: "needs_person"}; !ok || !reflect.DeepEqual(judge.Verdicts, want) {
		t.Errorf("session-finished's verdicts = %v, want %v", judge.Verdicts, want)
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

// The refine session splits a large plan before it finds blockers,
// finishes the split by labelling the parts and taking the parent out of
// crew, and the shell action after it fails a split that stopped before
// that (#160).
func TestTheRefineActionSplitsBeforeFindingBlockers(t *testing.T) {
	refinement := ownRuleNamed(t, loadOwn(t), "refinement")
	prompt := ownSession(t, refinement, "refine").Prompt.Text()
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
		if script := shellScript(t, refinement, "split-finished"); !strings.Contains(script, want) {
			t.Errorf("split-finished = %q, want its script, with %q", script, want)
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
	prompt := ownSession(t, ownRuleNamed(t, loadOwn(t), "refinement"), "refine").Prompt.Text()
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

// The refine prompt reads in full only the candidates /cw-rank-blockers
// shortlists for each issue it refines, after the split and before it
// records a dependency, and falls back to reading every candidate when the
// script fails (#166).
func TestTheRefinePromptReadsTheShortlist(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join("..", "..", ".agents", "skills", "cw-rank-blockers", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(skill), "name: cw-rank-blockers") {
		t.Errorf("the skill the refine prompt runs is not cw-rank-blockers")
	}
	prompt := ownSession(t, ownRuleNamed(t, loadOwn(t), "refinement"), "refine").Prompt.Text()
	split := strings.Index(prompt, "/cw-split-plan {{.Issue.Ref}}")
	rank := strings.Index(prompt, "/cw-rank-blockers")
	record := strings.Index(prompt, "dependencies/blocked_by -F")
	if split < 0 || rank < split || record < rank {
		t.Errorf("the prompt does not run /cw-rank-blockers between /cw-split-plan and recording:\n%s", prompt)
	}
	for _, want := range []string{"--json number,title,body,labels", "unavailable"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks the full read when the shortlist is unavailable: %q", want)
		}
	}
}

// needsPerson is the comment of the needs-person route of development and
// fix: it names only what crew knows of the run, never what the session or
// the judge said (R18).
const needsPerson = "{{.Issue.Ref}} needs a person: `{{.Action}}` ended with `{{.Verdict}}`. " +
	"See the session's comments above."

// wantOwnRules are crew's own config's rules, as in the old format, with
// the needs-person route of development and fix: the promote rules,
// without actions, notify nothing and end only through passed.
func wantOwnRules() []ownRule {
	clerk, developer := crew.Queue{Name: "clerk", Slots: 1}, crew.Queue{Name: "developer", Slots: 2}
	productManager := crew.Queue{Name: "product-manager", Slots: 1}
	labels := func(rule string) crew.Labels {
		return crew.Labels{Ready: crew.State("crew:" + rule + ":ready"), Running: crew.State("crew:" + rule + ":in progress")}
	}
	routes := func(rule, passed string) []string {
		return []string{"passed: move " + passed, "failed: report, move crew:" + rule + ":failed"}
	}
	lfgRoutes := func(rule, passed string) []string {
		return append(routes(rule, passed), "needs-person: comment "+needsPerson+", move crew:"+rule+":needs person")
	}
	lfg := []string{
		"session lfg: agent developer, bot developer",
		"shell session-finished; needs_person to needs-person",
		"shell pr-closes-issue",
	}
	return []ownRule{
		{
			name: "promote brainstorm", queue: clerk,
			labels: crew.Labels{Ready: "crew:brainstorm:done", Running: "crew:brainstorm:promoting"},
			routes: []string{"passed: move crew:refinement:ready"},
		},
		{
			name: "refinement", queue: productManager, notify: true, labels: labels("refinement"),
			actions: []string{"session refine: agent product-manager, bot product-manager", "shell split-finished"},
			routes:  routes("refinement", "crew:refinement:done"),
		},
		{
			name: "promote refinement", queue: clerk,
			labels: crew.Labels{Ready: "crew:refinement:done", Running: "crew:refinement:promoting"},
			routes: []string{"passed: move crew:development:ready"},
		},
		{
			name: "development", queue: developer, notify: true, labels: labels("development"),
			actions: lfg, routes: lfgRoutes("development", "crew:development:waiting review"),
		},
		{
			name: "fix", queue: developer, notify: true, labels: labels("fix"),
			actions: lfg, routes: lfgRoutes("fix", "crew:fix:waiting review"),
		},
	}
}

// ownRuleNamed returns the rule of cfg called name, and fails t without
// one.
func ownRuleNamed(t *testing.T, cfg *config.Config, name crew.RuleName) crew.Rule {
	t.Helper()
	for _, r := range cfg.Rules {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("crew's own config has no rule %q", name)
	return crew.Rule{}
}

// ownSession returns the session r's action called name runs, and fails t
// when r has no such session.
func ownSession(t *testing.T, r crew.Rule, name crew.ActionName) crew.SessionSpec {
	t.Helper()
	s, ok := r.Action(name).Kind.(crew.SessionSpec)
	if !ok {
		t.Fatalf("rule %s has no session %s", r.Name, name)
	}
	return s
}

// shellScript returns the script of r's shell action called name, which
// the config's top-level actions define, and fails t when r has no such
// shell action.
func shellScript(t *testing.T, r crew.Rule, name crew.ActionName) string {
	t.Helper()
	s, ok := r.Action(name).Kind.(crew.ShellSpec)
	if !ok {
		t.Fatalf("rule %s has no shell action %s", r.Name, name)
	}
	return s.Script
}

// ownRules sums rules up as ownRule, with each rule's notify.
func ownRules(rules []crew.Rule, notify map[crew.RuleName]bool) []ownRule {
	out := make([]ownRule, len(rules))
	for i, r := range rules {
		out[i] = ownRule{name: r.Name, labels: r.Labels, queue: r.Queue, notify: notify[r.Name]}
		for _, a := range r.Actions {
			switch k := a.Kind.(type) {
			case crew.SessionSpec:
				out[i].actions = append(out[i].actions,
					fmt.Sprintf("session %s: agent %s, bot %s", a.Name, k.Agent.Name, k.Bot.Name))
			case crew.ShellSpec:
				out[i].actions = append(out[i].actions, fmt.Sprintf("shell %s", a.Name))
			}
			if on := onNames(a.On); on != "" {
				last := len(out[i].actions) - 1
				out[i].actions[last] += "; " + on
			}
		}
		for _, route := range r.Routes {
			out[i].routes = append(out[i].routes, fmt.Sprintf("%s: %s", route.Name, stepNames(route.Steps)))
		}
	}
	return out
}

// stepNames sums steps up, as "report, move L".
func stepNames(steps []crew.Step) string {
	names := make([]string, len(steps))
	for i, step := range steps {
		switch s := step.(type) {
		case crew.MoveStep:
			names[i] = "move " + string(s.To)
		case crew.ReportStep:
			names[i] = "report"
		case crew.CloseStep:
			names[i] = "close"
		case crew.CommentStep:
			names[i] = "comment " + s.Template.Text()
		case crew.ShellStep:
			names[i] = string(s.Name)
		}
	}
	return strings.Join(names, ", ")
}

// onNames sums on up, as "verdict to target", sorted by verdict.
func onNames(on crew.On) string {
	names := make([]string, 0, len(on))
	for v, target := range on {
		to := "next"
		if r, ok := target.(crew.ToRoute); ok {
			to = string(r.Route)
		}
		names = append(names, fmt.Sprintf("%s to %s", v, to))
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
