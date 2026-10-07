package config_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// sequenceRule is a config of the agents and top-level actions in head,
// written as is, and one rule, implement, whose actions are items, a list's
// lines indented under actions, and whose routes are passed and failed,
// then extra routes, indented under routes. Its actions key is on the line
// after head's last plus 4.
func sequenceRule(head, items, extra string) string {
	return head + `rules:
  implement:
    labels: {ready: ready, running: in progress}
    actions:
` + items + `    routes:
      passed: done
      failed: [report, move: failed]
` + extra
}

// claudeAgent is the agent oneAgent declares.
var claudeAgent = crew.Agent{Name: "claude", Harness: "claude"}

// R2, R5: a rule's actions are a list, run in its order: a string names a
// top-level shell action, and a mapping with prompt is a session.
func TestLoadReadsASequenceOfShellActionsAndSessions(t *testing.T) {
	cfg := load(t, sequenceRule("actions:\n  install: make install\n"+oneAgent,
		"      - install\n      - prompt: \"Implement {{.Issue.Ref}}\"\n", ""))
	want := []crew.Action{
		{Name: "install", Kind: crew.ShellSpec{Script: "make install"}},
		session(t, "claude", claudeAgent, "Implement {{.Issue.Ref}}"),
	}
	if got := cfg.Rules[0].Actions; !reflect.DeepEqual(got, want) {
		t.Errorf("Actions = %+v\nwant %+v", got, want)
	}
}

// R36, KTD16: a session is named after its agent unless name says
// otherwise, and with one agent declared it may leave its agent out.
func TestLoadNamesEverySession(t *testing.T) {
	codex := crew.Agent{Name: "codex", Harness: "codex"}
	tests := []struct {
		name   string
		head   string
		item   string
		want   crew.ActionName
		wantOn crew.Agent
	}{
		{name: "after its agent", head: twoAgents, item: "{agent: codex, prompt: go}", want: "codex", wantOn: codex},
		{name: "by name", head: twoAgents, item: "{agent: codex, name: lfg, prompt: go}", want: "lfg", wantOn: codex},
		{name: "after the only agent", head: oneAgent, item: "{prompt: go}", want: "claude", wantOn: claudeAgent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := load(t, sequenceRule(tt.head, "      - "+tt.item+"\n", "")).Rules[0].Actions[0]
			if s := sessionOf(t, a); a.Name != tt.want || s.Agent != tt.wantOn {
				t.Errorf("session %s on %+v, want %s on %+v", a.Name, s.Agent, tt.want, tt.wantOn)
			}
		})
	}
}

// R10, KTD14: an action's on maps its verdicts to next or a route, and a
// reference written as a mapping of one empty key takes on and name beside
// it.
func TestLoadReadsEveryActionsOn(t *testing.T) {
	items := `      - prompt: go
        on:
          blocked: blocked
          waiting: next
      - session-finished:
        on:
          needs_person: needs-person
      - session-finished: {}
        name: judge again
`
	extra := "      blocked: blocked\n      needs-person: [comment: ask a person, move: needs person]\n"
	cfg := load(t, sequenceRule("actions:\n  session-finished: ./judge\n"+oneAgent, items, extra))
	got := cfg.Rules[0].Actions
	wants := []struct {
		name crew.ActionName
		on   crew.On
	}{
		{"claude", crew.On{"blocked": crew.ToRoute{Route: "blocked"}, "waiting": crew.Next{}}},
		{"session-finished", crew.On{"needs_person": crew.ToRoute{Route: "needs-person"}}},
		{"judge again", nil},
	}
	for i, want := range wants {
		if got[i].Name != want.name || !reflect.DeepEqual(got[i].On, want.on) {
			t.Errorf("action %d = %s, on %v; want %s, on %v", i, got[i].Name, got[i].On, want.name, want.on)
		}
	}
	if shellOf(t, got[2]).Script != "./judge" {
		t.Errorf("judge again runs %+v, want session-finished's script", got[2].Kind)
	}
}

// A session acts as its agent's bot, else as tracker.bot; a shell action
// has no bot of its own.
func TestLoadGivesEverySessionItsBot(t *testing.T) {
	head := "tracker: {bot: clerk}\nactions:\n  lint: make lint\n" + twoAgents +
		"  developer:\n    harness: {name: claude}\n    bot: developer\n"
	cfg := load(t, sequenceRule(head,
		"      - {agent: developer, prompt: go}\n      - lint\n      - {agent: codex, prompt: go}\n", ""))
	actions := cfg.Rules[0].Actions
	if got := sessionOf(t, actions[0]).Bot.Name; got != "developer" {
		t.Errorf("developer's session acts as %q, want developer", got)
	}
	if got := sessionOf(t, actions[2]).Bot.Name; got != "clerk" {
		t.Errorf("codex's session acts as %q, want clerk", got)
	}
	if want := []crew.BotName{"clerk", "developer"}; !reflect.DeepEqual(cfg.BotNames(), want) {
		t.Errorf("Bots = %q, want %q", cfg.BotNames(), want)
	}
}

// R20, KTD-W3: a session waits for answers 10 minutes unless its wait,
// a Go duration, says otherwise.
func TestLoadGivesEverySessionItsWait(t *testing.T) {
	cfg := load(t, sequenceRule(oneAgent, "      - prompt: go\n      - {name: patient, prompt: go, wait: 3m}\n", ""))
	actions := cfg.Rules[0].Actions
	if got := sessionOf(t, actions[0]).Wait; got != 10*time.Minute {
		t.Errorf("a session without wait waits %v, want 10m0s", got)
	}
	if got := sessionOf(t, actions[1]).Wait; got != 3*time.Minute {
		t.Errorf("a session with wait: 3m waits %v, want 3m0s", got)
	}
}

// A rule whose only actions are shell actions needs no agent, and runs on
// the board like any rule with actions.
func TestLoadARuleOfShellActionsWithoutAgents(t *testing.T) {
	cfg := load(t, sequenceRule("actions:\n  lint: make lint\n", "      - lint\n", ""))
	if len(cfg.Agents) != 0 || len(cfg.Rules[0].Actions) != 1 || !cfg.Notify["implement"] {
		t.Errorf("Agents = %+v, actions = %+v, notify %v; want no agent, lint, notify",
			cfg.Agents, cfg.Rules[0].Actions, cfg.Notify["implement"])
	}
	if len(cfg.Board) != 1 || cfg.Board[0].Name != "implement" {
		t.Errorf("Board = %+v, want the column implement", cfg.Board)
	}
}

func TestLoadRejectsInvalidSequences(t *testing.T) {
	testRejects(t, invalidSequences)
}

// invalidSequences are errors in a rule's list of actions. The actions key
// of sequenceRule(oneAgent, ...) is on line 7, its first item on line 8.
var invalidSequences = []rejectCase{
	{
		name:  "actions written as a mapping",
		body:  sequenceRule(oneAgent, "      development: {prompt: go}\n", ""),
		wants: []string{"rules.implement.actions", "line 8", "must be a list of actions"},
	},
	{
		name:  "a session without prompt",
		body:  sequenceRule(oneAgent, "      - agent: claude\n", ""),
		wants: []string{"rules.implement.actions[0].prompt", "line 8", "required"},
	},
	{
		name:  "a session key crew does not know",
		body:  sequenceRule(oneAgent, "      - prompt: go\n        model: opus\n", ""),
		wants: []string{"rules.implement.actions[0].model", "line 9", "unknown key"},
	},
	{
		name:  "a wait that is no duration",
		body:  sequenceRule(oneAgent, "      - prompt: go\n        wait: soon\n", ""),
		wants: []string{"rules.implement.actions[0].wait", "line 9", `"soon"`, "must be a duration"},
	},
	{
		name:  "a wait of zero",
		body:  sequenceRule(oneAgent, "      - prompt: go\n        wait: 0s\n", ""),
		wants: []string{"rules.implement.actions[0].wait", "line 9", "must be positive"},
	},
	{
		name:  "a negative wait",
		body:  sequenceRule(oneAgent, "      - prompt: go\n        wait: -1m\n", ""),
		wants: []string{"rules.implement.actions[0].wait", "line 9", "must be positive"},
	},
	{
		name:  "a string that names no action",
		body:  sequenceRule("actions:\n  lint: make lint\n  test: make test\n"+oneAgent, "      - vet\n", ""),
		wants: []string{"rules.implement.actions[0]", "line 11", `action "vet" does not exist; the actions are lint, test`},
	},
	{
		name:  "a string without actions",
		body:  sequenceRule(oneAgent, "      - vet\n", ""),
		wants: []string{"rules.implement.actions[0]", "line 8", `action "vet" does not exist; actions declares none`},
	},
	{
		name:  "a reference that names no action",
		body:  sequenceRule("actions:\n  lint: make lint\n", "      - vet:\n        on: {failed: next}\n", ""),
		wants: []string{"rules.implement.actions[0].vet", "line 7", `action "vet" does not exist`},
	},
	{
		// A shell action takes no parameters; a function's arrive with #256.
		name:  "a reference with parameters",
		body:  sequenceRule("actions:\n  lint: make lint\n", "      - lint: {fix: true}\n", ""),
		wants: []string{"rules.implement.actions[0].lint", "line 7", "takes no parameters"},
	},
	{
		name:  "a mapping that names two actions",
		body:  sequenceRule("actions:\n  lint: make lint\n  vet: go vet\n", "      - {lint: , vet: }\n", ""),
		wants: []string{"rules.implement.actions[0]", "line 8", "must be the name of one of actions"},
	},
	{
		name:  "a mapping that names no action",
		body:  sequenceRule(oneAgent, "      - {name: lfg}\n", ""),
		wants: []string{"rules.implement.actions[0]", "line 8", "must be the name of one of actions"},
	},
	{
		name:  "an item that is a list",
		body:  sequenceRule(oneAgent, "      - [lint]\n", ""),
		wants: []string{"rules.implement.actions[0]", "line 8", "must be the name of one of actions"},
	},
	{
		name:  "an empty name",
		body:  sequenceRule(oneAgent, "      - {prompt: go, name: \"\"}\n", ""),
		wants: []string{"rules.implement.actions[0].name", "line 8", "required"},
	},
	{
		name:  "an on key that is not a verdict's name",
		body:  sequenceRule(oneAgent, "      - prompt: go\n        on: {Blocked: failed}\n", ""),
		wants: []string{"rules.implement.actions[0].on.Blocked", "line 9", `verdict "Blocked"`},
	},
	{
		name:  "an on that is a list",
		body:  sequenceRule(oneAgent, "      - prompt: go\n        on: [failed]\n", ""),
		wants: []string{"rules.implement.actions[0].on", "line 9", "must be a mapping"},
	},
	{
		name:  "an on value that is not a route's name",
		body:  sequenceRule(oneAgent, "      - prompt: go\n        on: {blocked: Blocked}\n", ""),
		wants: []string{"rules.implement.actions[0].on.blocked", "line 9", `route "Blocked"`},
	},
	{
		name: "an on value that is a list",
		body: sequenceRule(oneAgent, "      - prompt: go\n        on: {blocked: [failed]}\n", ""),
		wants: []string{
			"rules.implement.actions[0].on.blocked", "line 9", "must be next or the name of one of the rule's routes",
		},
	},
	{
		name:  "a prompt that does not parse",
		body:  sequenceRule(oneAgent, "      - prompt: \"{{.Issue.Ref\"\n", ""),
		wants: []string{"rules.implement.actions[0].prompt", "line 8", `parse prompt of action "claude"`},
	},
	{
		name:  "a prompt that names an unknown issue field",
		body:  sequenceRule(oneAgent, "      - {name: lfg, prompt: \"{{.Issue.Number}}\"}\n", ""),
		wants: []string{"rules.implement.actions[0].prompt", "line 8", `render prompt of action "lfg"`, "Number"},
	},
	{
		name:  "a prompt that reads CREW_BOSS",
		body:  sequenceRule(oneAgent, "      - prompt: Ask $CREW_BOSS\n", ""),
		wants: []string{"rules.implement.actions[0].prompt", "line 8", "CREW_BOSS is now CREW_CODE_OWNERS"},
	},
}
