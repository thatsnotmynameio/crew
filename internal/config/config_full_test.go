package config_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// translation holds a full config, a translation team's, invented for the
// tests, so they pin crew's behaviour and never a real repository's setup.
const translation = "testdata/translation"

// ruleSummary is how a rule loads, its actions summed up as
// "action: agent A, bot B, checks [C...]".
type ruleSummary struct {
	name    crew.RuleName
	labels  crew.Labels
	queue   crew.Queue
	notify  bool
	actions []string
}

// A full config loads its rules with their labels, queues, notify and
// actions, each action with its agent, its bot and its checks in order; its
// tracker's bot and every agent's; its board, one column per rule with
// actions when it sets none; and its checks' scripts and its prompts as
// they are written.
func TestAFullConfigLoads(t *testing.T) {
	cfg, err := config.Load(translation, "")
	if err != nil {
		t.Fatalf("Load(%s) = %v", translation, err)
	}
	if cfg.PollInterval != 2*time.Minute || cfg.MaxParallelIssues != 4 {
		t.Errorf("poll %v, parallel %d; want 2m0s, 4", cfg.PollInterval, cfg.MaxParallelIssues)
	}
	if got, want := summarize(cfg.Rules, cfg.Notify), wantTranslationRules(); !reflect.DeepEqual(got, want) {
		t.Errorf("rules = %+v\nwant %+v", got, want)
	}
	wantBots := []crew.BotName{"concierge", "linguist"}
	if cfg.Bot.Name != "concierge" || !reflect.DeepEqual(cfg.BotNames(), wantBots) {
		t.Errorf("Bot = %q, Bots = %q; want concierge, %q", cfg.Bot, cfg.BotNames(), wantBots)
	}
	columns := make([]string, 0, len(cfg.Board))
	for _, c := range cfg.Board {
		columns = append(columns, c.Name)
	}
	if want := []string{"translation", "proofreading"}; !reflect.DeepEqual(columns, want) || cfg.BoardWritten {
		t.Errorf("board columns = %q (written %v), want %q", columns, cfg.BoardWritten, want)
	}
	draft := cfg.Rules[1].Actions[0]
	wantScript := "n=$(git rev-list --count \"origin/$CREW_BRANCH\" 2>/dev/null) || n=0\n" +
		`[ "$n" -gt 0 ] || { echo "nothing pushed to $CREW_BRANCH"; exit 1; }`
	if got := checkScript(draft, "draft-pushed"); got != wantScript {
		t.Errorf("draft's check draft-pushed = %q, want %q", got, wantScript)
	}
	wantPrompt := "Translate the page {{.Issue.Ref}} names, \"{{.Issue.Title}}\", into Portuguese.\n\n" +
		"Keep every term the glossary lists, and push the draft to a branch."
	if got := draft.Prompt.Text(); got != wantPrompt {
		t.Errorf("draft's prompt = %q, want %q", got, wantPrompt)
	}
}

// wantTranslationRules are the rules testdata/translation's config loads
// into, in file order.
func wantTranslationRules() []ruleSummary {
	return []ruleSummary{
		{
			name: "accept request", queue: crew.Queue{Name: "desk", Slots: 1},
			labels: crew.Labels{Ready: "request:new", Running: "request:accepting", Success: "translation:to do"},
		},
		{
			name: "translation", queue: crew.Queue{Name: "translators", Slots: 2}, notify: true,
			labels: crew.Labels{
				Ready: "translation:to do", Running: "translation:drafting",
				Success: "translation:drafted", Failure: "translation:stuck",
			},
			actions: []string{"draft: agent translator, bot linguist, checks [glossary-kept draft-pushed]"},
		},
		{
			name: "proofreading", queue: crew.Queue{Name: "default", Slots: 1},
			labels: crew.Labels{
				Ready: "translation:drafted", Running: "translation:proofreading",
				Success: "translation:published", Failure: "translation:rejected",
			},
			actions: []string{
				"proofread: agent proofreader, bot concierge, checks [glossary-kept]",
				"typeset: agent translator, bot linguist, checks []",
			},
		},
		{
			name: "archive", queue: crew.Queue{Name: "default", Slots: 1}, notify: true,
			labels: crew.Labels{
				Ready: "translation:published", Running: "translation:archiving", Success: "translation:archived",
			},
		},
	}
}

// checkScript returns the script of a's check called name, or "" when a
// names no such check.
func checkScript(a crew.Action, name crew.CheckName) string {
	for _, c := range a.Checks {
		if c.Name == name {
			return c.Script
		}
	}
	return ""
}

// checkNames returns the names of checks, in order, as [a b].
func checkNames(checks []crew.Check) string {
	names := make([]crew.CheckName, len(checks))
	for i, c := range checks {
		names[i] = c.Name
	}
	return fmt.Sprint(names)
}

// summarize sums rules up as ruleSummary, with each rule's notify.
func summarize(rules []crew.Rule, notify map[crew.RuleName]bool) []ruleSummary {
	out := make([]ruleSummary, len(rules))
	for i, r := range rules {
		out[i] = ruleSummary{name: r.Name, labels: r.Labels, queue: r.Queue, notify: notify[r.Name]}
		for _, a := range r.Actions {
			out[i].actions = append(out[i].actions,
				fmt.Sprintf("%s: agent %s, bot %s, checks %s", a.Name, a.Agent.Name, a.Bot.Name, checkNames(a.Checks)))
		}
	}
	return out
}
