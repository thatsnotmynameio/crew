package registry_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/proc"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

func TestDefaultBuildsGithubAndClaude(t *testing.T) {
	r := registry.Default(&proc.Group{})
	cfg := load(t, agent("claude")+rules) // tracker.name left to its default

	if _, err := r.Tracker(cfg.Tracker, cfg.TrackerSection, crew.RuleStates(cfg.Rules)); err != nil {
		t.Errorf("Tracker(%q): %v", cfg.Tracker, err)
	}
	a := cfg.Agents[0]
	if _, err := r.Harness(a.HarnessKey(), string(a.Harness), a.HarnessSection); err != nil {
		t.Errorf("Harness(%q): %v", a.Harness, err)
	}
}

// A config can run one rule's actions on Claude Code and another's on Codex,
// each agent with its own bot: both harnesses build from the one registry.
func TestDefaultBuildsAClaudeAgentAndACodexAgentSideBySide(t *testing.T) {
	r := registry.Default(&proc.Group{})
	cfg := load(t, `agents:
  developer:
    harness: {name: claude}
    bot: developer
  reviewer:
    harness: {name: codex, model: gpt-5.5}
    bot: reviewer
rules:
  implement:
    labels: {ready: ready, running: in progress, success: ready to review, failure: needs attention}
    actions:
      development: {agent: developer, prompt: "Implement {{.Issue.Ref}}"}
  review:
    labels: {ready: ready to review, running: in review, success: reviewed, failure: review failed}
    actions:
      review: {agent: reviewer, prompt: "Review {{.Issue.Ref}}"}
`)

	for _, a := range cfg.Agents {
		h, err := r.Harness(a.HarnessKey(), string(a.Harness), a.HarnessSection)
		if err != nil || h == nil {
			t.Errorf("Harness(%q) for %s = %v, %v; want a harness", a.Harness, a.Name, h, err)
		}
	}
}
