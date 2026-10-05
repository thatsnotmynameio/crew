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
	if _, err := r.Harness(a.HarnessKey(), a.Harness, a.HarnessSection); err != nil {
		t.Errorf("Harness(%q): %v", a.Harness, err)
	}
}
