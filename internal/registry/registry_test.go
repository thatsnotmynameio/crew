package registry_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

// load writes body as a repository's .crew/config.yaml and loads it.
func load(t *testing.T, body string) *config.Config {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".crew", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// rules is one rule of one action, which runs on the agent agents declares.
const rules = `rules:
  implement:
    labels: {ready: ready, running: in progress, success: ready to review, failure: needs attention}
    actions:
      development: {prompt: "Implement {{.Issue.Ref}}"}
`

// agent declares the agent developer, on the harness name.
func agent(name string) string {
	return "agents:\n  developer:\n    harness: {name: " + name + ", model: some-model}\n"
}

func assertErr(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one containing %q", wants)
	}
	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

// R13: a harness name no adapter has names the agent's key.
func TestUnregisteredHarnessNamesTheKeyAndTheRegisteredHarnesses(t *testing.T) {
	r := registry.New(nil, map[string]port.HarnessFactory{"claude": fake.HarnessFactory(fake.NewHarness())})
	a := load(t, agent("codex")+rules).Agents[0]

	h, err := r.Harness(a.HarnessKey(), a.Harness, a.HarnessSection)
	assertErr(t, err, "agents.developer.harness.name", `"codex"`, "the registered harness adapters are: claude")
	if h != nil {
		t.Errorf("Harness = %v, want none", h)
	}
}

func TestUnregisteredTrackerNamesTheKeyAndTheRegisteredTrackersSorted(t *testing.T) {
	r := registry.New(map[string]port.TrackerFactory{
		"jira":   fake.TrackerFactory(fake.NewTracker()),
		"github": fake.TrackerFactory(fake.NewTracker()),
	}, nil)

	_, err := r.Tracker("linear", func(any) error { return nil }, nil)
	assertErr(t, err, "tracker.name", `"linear"`, "github, jira")
}

func TestRegistryWithoutAdaptersSaysNoneIsRegistered(t *testing.T) {
	_, err := registry.Registry{}.Harness("agents.developer.harness.name", "claude", func(any) error { return nil })
	assertErr(t, err, "agents.developer.harness.name", `"claude"`, "none")
}

func TestFactoryValidationErrorNamesTheSectionKeyAndItsLine(t *testing.T) {
	r := registry.New(map[string]port.TrackerFactory{"fake": fake.TrackerFactory(fake.NewTracker())}, nil)
	cfg := load(t, `tracker:
  name: fake
  lables:
    ready: todo
`+agent("claude")+rules)

	tr, err := r.Tracker(cfg.Tracker, cfg.TrackerSection, crew.RuleStates(cfg.Rules))
	assertErr(t, err, "tracker.lables", "line 3", "unknown key")
	if tr != nil {
		t.Errorf("Tracker = %v, want none", tr)
	}
}

// Covers AE3: tracker.labels is no longer a key, for the fake as for github.
func TestTrackerLabelsIsAnUnknownKey(t *testing.T) {
	r := registry.New(map[string]port.TrackerFactory{"fake": fake.TrackerFactory(fake.NewTracker())}, nil)
	cfg := load(t, `tracker:
  name: fake
  labels:
    ready: ready
`+agent("claude")+rules)

	_, err := r.Tracker(cfg.Tracker, cfg.TrackerSection, crew.RuleStates(cfg.Rules))
	assertErr(t, err, "tracker.labels", "line 3", "unknown key")
}

func TestTheTrackerFactoryGetsTheStates(t *testing.T) {
	var gotStates []crew.State
	r := registry.New(map[string]port.TrackerFactory{
		"fake": func(_ port.Decode, states []crew.State) (port.Tracker, error) {
			gotStates = states
			return fake.NewTracker(), nil
		},
	}, nil)
	states := []crew.State{"ready", "in progress"}

	if _, err := r.Tracker("fake", func(any) error { return nil }, states); err != nil {
		t.Fatalf("Tracker: %v", err)
	}
	if !slices.Equal(gotStates, states) {
		t.Errorf("the factory got the states %v, want %v", gotStates, states)
	}
}

func TestRegisteredAdaptersAreBuiltFromTheirSections(t *testing.T) {
	tracker, harness := fake.NewTracker(), fake.NewHarness()
	r := registry.New(
		map[string]port.TrackerFactory{"fake": fake.TrackerFactory(tracker)},
		map[string]port.HarnessFactory{"fake": fake.HarnessFactory(harness)},
	)
	cfg := load(t, `tracker:
  name: fake
`+agent("fake")+rules)

	gotTracker, err := r.Tracker(cfg.Tracker, cfg.TrackerSection, crew.RuleStates(cfg.Rules))
	if err != nil {
		t.Fatalf("Tracker: %v", err)
	}
	if gotTracker != port.Tracker(tracker) {
		t.Errorf("Tracker = %v, want the registered fake", gotTracker)
	}
	a := cfg.Agents[0]
	gotHarness, err := r.Harness(a.HarnessKey(), a.Harness, a.HarnessSection)
	if err != nil {
		t.Fatalf("Harness: %v", err)
	}
	if gotHarness != port.Harness(harness) {
		t.Errorf("Harness = %v, want the registered fake", gotHarness)
	}
}
