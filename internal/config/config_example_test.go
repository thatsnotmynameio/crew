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
// keys (KTD8): the same names, so failed runs still resume.
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
	if want := []string{"triage", "development", "fix"}; !reflect.DeepEqual(columns, want) || cfg.BoardWritten {
		t.Errorf("board columns = %q (written %v), want %q", columns, cfg.BoardWritten, want)
	}
	if script := cfg.Rules[3].Actions[0].Check; !strings.Contains(script, `"Closes " + env.CREW_ISSUE_REF`) {
		t.Errorf("development's check = %q, want the script of pr-closes-issue", script)
	}
}

// wantExampleRules are the example config's rules, as in the old keys: the
// promote rules, now without actions, notify nothing and need no failure.
func wantExampleRules() []exampleRule {
	clerk, developer := crew.Queue{Name: "clerk", Slots: 1}, crew.Queue{Name: "developer", Slots: 2}
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
				Ready: "crew:brainstorm:done", Running: "crew:brainstorm:promoting", Success: "crew:triage:ready",
			},
		},
		{
			name: "triage", queue: clerk, notify: true, labels: labels("triage", "crew:triage:done"),
			actions: []string{"triage: agent product-manager, bot product-manager, check false"},
		},
		{
			name: "promote triage", queue: clerk,
			labels: crew.Labels{
				Ready: "crew:triage:done", Running: "crew:triage:promoting", Success: "crew:development:ready",
			},
		},
		{
			name: "development", queue: developer, notify: true,
			labels:  labels("development", "crew:development:waiting review"),
			actions: []string{"lfg: agent developer, bot developer, check true"},
		},
		{
			name: "fix", queue: developer, notify: true, labels: labels("fix", "crew:fix:waiting review"),
			actions: []string{"lfg: agent developer, bot developer, check true"},
		},
	}
	return want
}

// exampleRules sums rules up as exampleRule.
func exampleRules(rules []crew.Rule) []exampleRule {
	out := make([]exampleRule, len(rules))
	for i, r := range rules {
		out[i] = exampleRule{name: r.Name, labels: r.Labels, queue: r.Queue, notify: r.Notify}
		for _, a := range r.Actions {
			out[i].actions = append(out[i].actions,
				fmt.Sprintf("%s: agent %s, bot %s, check %t", a.Name, a.Agent, a.Bot, a.Check != ""))
		}
	}
	return out
}
