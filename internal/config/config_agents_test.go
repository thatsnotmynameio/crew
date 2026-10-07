package config_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// twoAgents declares the agents claude and codex.
const twoAgents = `agents:
  claude:
    harness: {name: claude}
  codex:
    harness: {name: codex}
`

// Covers AE3: an action may leave its agent out only when one agent is
// declared.
func TestAE3AnActionWithoutAgentRunsOnTheOnlyAgent(t *testing.T) {
	cfg := load(t, oneAgent+ruleOnly)
	if got := cfg.Rules[0].Actions[0].Agent; got != "claude" {
		t.Errorf("the action's agent = %q, want claude", got)
	}
	loadErr(t, twoAgents+ruleOnly,
		"rules.implement.actions.development.agent", "line 14", "required", "more than one agent: claude, codex")
}

func TestLoadGivesEveryActionItsAgent(t *testing.T) {
	body := twoAgents + strings.Replace(ruleOnly,
		"        prompt:", "        agent: codex\n        prompt:", 1)
	cfg := load(t, body)
	if got := cfg.Rules[0].Actions[0].Agent; got != "codex" {
		t.Errorf("the action's agent = %q, want codex", got)
	}
	used := map[crew.AgentName]bool{}
	for _, a := range cfg.Agents {
		used[a.Name] = a.Used
	}
	if want := map[crew.AgentName]bool{"claude": false, "codex": true}; !reflect.DeepEqual(used, want) {
		t.Errorf("agents in use = %v, want %v", used, want)
	}
	if got := cfg.Agents[1].HarnessKey(); got != "agents.codex.harness.name" {
		t.Errorf("HarnessKey = %q, want agents.codex.harness.name", got)
	}
}

// botAgents is a config whose tracker's bot, and whose agents developer,
// reviewer and idle, have the bots given; "" names none. implement runs
// development as developer and review as reviewer, fix runs fix as
// developer, and no action runs as idle.
func botAgents(tracker, developer, reviewer, idle string) string {
	bot := func(name string) string {
		if name == "" {
			return ""
		}
		return "\n    bot: " + name
	}
	tb := ""
	if tracker != "" {
		tb = "tracker:\n  bot: " + tracker + "\n"
	}
	return tb + `agents:
  developer:
    harness: {name: claude}` + bot(developer) + `
  reviewer:
    harness: {name: claude}` + bot(reviewer) + `
  idle:
    harness: {name: claude}` + bot(idle) + `
rules:
  implement:
    labels: {ready: ready, running: in progress, success: ready to fix, failure: failed}
    actions:
      development: {agent: developer, prompt: "Implement {{.Issue.Ref}}"}
      review: {agent: reviewer, prompt: "Review {{.Issue.Ref}}"}
  fix:
    labels: {ready: ready to fix, running: fixing, success: done, failure: failed}
    actions:
      fix: {agent: developer, prompt: "Fix {{.Issue.Ref}}"}
`
}

// An action acts as its agent's bot, else as tracker.bot, else as the gh
// login; crew makes tracker.bot act first, then the bots of the agents in
// use, each once (KTD7).
func TestLoadGivesEveryActionItsBot(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantBot  crew.BotName
		wantBots []crew.BotName
		// want is each action's bot, in rule order.
		want []crew.BotName
	}{
		{
			name: "no bot anywhere", body: botAgents("", "", "", "idler"),
			wantBot: "", wantBots: nil, want: []crew.BotName{"", "", ""},
		},
		{
			name: "only tracker.bot", body: botAgents("clerk", "", "", "idler"),
			wantBot: "clerk", wantBots: []crew.BotName{"clerk"}, want: []crew.BotName{"clerk", "clerk", "clerk"},
		},
		{
			// An agent's bot needs no tracker.bot; crew's own writes then go
			// as the gh login.
			name: "only the agents' bots", body: botAgents("", "developer", "reviewer", "idler"),
			wantBot: "", wantBots: []crew.BotName{"developer", "reviewer"},
			want: []crew.BotName{"developer", "reviewer", "developer"},
		},
		{
			name: "an agent's bot, or tracker.bot", body: botAgents("clerk", "developer", "", "idler"),
			wantBot: "clerk", wantBots: []crew.BotName{"clerk", "developer"},
			want: []crew.BotName{"developer", "clerk", "developer"},
		},
		{
			name: "each bot listed once, tracker.bot first", body: botAgents("developer", "developer", "clerk", ""),
			wantBot: "developer", wantBots: []crew.BotName{"developer", "clerk"},
			want: []crew.BotName{"developer", "clerk", "developer"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, tt.body)
			if got := actionBots(cfg.Rules); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("action bots = %q, want %q", got, tt.want)
			}
			if cfg.Bot != tt.wantBot || !reflect.DeepEqual(cfg.Bots, tt.wantBots) {
				t.Errorf("Bot = %q, Bots = %q; want %q, %q", cfg.Bot, cfg.Bots, tt.wantBot, tt.wantBots)
			}
			if idle := cfg.Agents[2]; idle.Used {
				t.Errorf("agent idle is in use, want it unused: %+v", idle)
			}
		})
	}
}

// actionBots returns the bot of each of rules' actions, in rule order.
func actionBots(rules []crew.Rule) []crew.BotName {
	var out []crew.BotName
	for _, r := range rules {
		for _, a := range r.Actions {
			out = append(out, a.Bot)
		}
	}
	return out
}

// An agent no action names is still read, so the registry can check its
// harness's name.
func TestLoadReadsAnAgentNoActionNames(t *testing.T) {
	cfg := load(t, twoAgents+strings.Replace(ruleOnly,
		"        prompt:", "        agent: claude\n        prompt:", 1))
	want := []config.Agent{{Name: "claude", Harness: "claude", Used: true}, {Name: "codex", Harness: "codex"}}
	for i := range cfg.Agents {
		cfg.Agents[i].HarnessSection = nil
	}
	if !reflect.DeepEqual(cfg.Agents, want) {
		t.Errorf("Agents = %+v, want %+v", cfg.Agents, want)
	}
}

func TestLoadRejectsInvalidAgents(t *testing.T) {
	testRejects(t, invalidAgents)
}

// invalidAgents are errors in agents, and in how an action names its agent.
var invalidAgents = []rejectCase{
	{
		name:  "rules with actions and no agents",
		body:  ruleOnly,
		wants: []string{"agents: missing"},
	},
	{
		name:  "agents written as a list",
		body:  "agents:\n  - claude\n" + ruleOnly,
		wants: []string{"agents", "line 2", "must be a mapping"},
	},
	{
		name:  "an agent that is not a mapping",
		body:  "agents:\n  claude: claude\n" + ruleOnly,
		wants: []string{"agents.claude", "line 2", "must be an agent with harness"},
	},
	{
		name:  "an agent without harness",
		body:  "agents:\n  claude:\n    bot: developer\n" + ruleOnly,
		wants: []string{"agents.claude.harness", "line 2", "required"},
	},
	{
		name:  "a harness that is not a mapping",
		body:  "agents:\n  claude:\n    harness: claude\n" + ruleOnly,
		wants: []string{"agents.claude.harness", "line 3", "must be a harness with name"},
	},
	{
		name:  "a harness without name",
		body:  "agents:\n  claude:\n    harness: {model: opus}\n" + ruleOnly,
		wants: []string{"agents.claude.harness.name", "line 3", "required"},
	},
	{
		name:  "an agent key crew does not know",
		body:  "agents:\n  claude:\n    harness: {name: claude}\n    model: opus\n" + ruleOnly,
		wants: []string{"agents.claude.model", "line 4", "unknown key"},
	},
	{
		name:  "two agents share a name",
		body:  oneAgent + "  claude:\n    harness: {name: codex}\n" + ruleOnly,
		wants: []string{"agents.claude", "line 4", "duplicate key, first set on line 2"},
	},
	{
		name: "an action names an agent that does not exist",
		body: twoAgents + strings.Replace(ruleOnly, "        prompt:", "        agent: gemini\n        prompt:", 1),
		wants: []string{
			"rules.implement.actions.development.agent", "line 15", `agent "gemini" does not exist`,
			"the agents are claude, codex",
		},
	},
}
