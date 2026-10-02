package registry_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/proc"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

func TestDefaultBuildsGithubAndClaudeTheConfigDefaults(t *testing.T) {
	r := registry.Default(&proc.Group{})
	cfg := load(t, workflow) // tracker.name and config.harness left to their defaults

	if _, err := r.Tracker(cfg.Tracker, cfg.TrackerSection, crew.WorkflowStates(cfg.Workflow)); err != nil {
		t.Errorf("Tracker(%q): %v", cfg.Tracker, err)
	}
	if _, err := r.Harness(cfg.Harness, cfg.HarnessSection); err != nil {
		t.Errorf("Harness(%q): %v", cfg.Harness, err)
	}
}
