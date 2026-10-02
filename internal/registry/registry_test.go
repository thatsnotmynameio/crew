package registry_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
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

const workflow = `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`

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

// Covers AE4.
func TestUnregisteredHarnessNamesTheKeyAndTheRegisteredHarnesses(t *testing.T) {
	r := registry.New(nil, map[string]port.HarnessFactory{"claude": fake.HarnessFactory(fake.NewHarness())})
	cfg := load(t, "config:\n  harness: codex\n"+workflow)

	h, err := r.Harness(cfg.Harness, cfg.HarnessSection)
	assertErr(t, err, "harness", `"codex"`, "the registered harness adapters are: claude")
	if h != nil {
		t.Errorf("Harness = %v, want none", h)
	}
}

func TestUnregisteredTrackerNamesTheKeyAndTheRegisteredTrackersSorted(t *testing.T) {
	r := registry.New(map[string]port.TrackerFactory{
		"jira":   fake.TrackerFactory(fake.NewTracker()),
		"github": fake.TrackerFactory(fake.NewTracker()),
	}, nil)

	_, err := r.Tracker("linear", func(any) error { return nil })
	assertErr(t, err, "tracker.name", `"linear"`, "github, jira")
}

func TestRegistryWithoutAdaptersSaysNoneIsRegistered(t *testing.T) {
	_, err := registry.Registry{}.Harness("claude", func(any) error { return nil })
	assertErr(t, err, "config.harness", `"claude"`, "none")
}

func TestFactoryValidationErrorNamesTheSectionKeyAndItsLine(t *testing.T) {
	r := registry.New(map[string]port.TrackerFactory{"fake": fake.TrackerFactory(fake.NewTracker())}, nil)
	cfg := load(t, `tracker:
  name: fake
  lables:
    ready: todo
`+workflow)

	tr, err := r.Tracker(cfg.Tracker, cfg.TrackerSection)
	assertErr(t, err, "tracker.lables", "line 3", "unknown key")
	if tr != nil {
		t.Errorf("Tracker = %v, want none", tr)
	}
}

func TestRegisteredAdaptersAreBuiltFromTheirSections(t *testing.T) {
	tracker, harness := fake.NewTracker(), fake.NewHarness()
	r := registry.New(
		map[string]port.TrackerFactory{"fake": fake.TrackerFactory(tracker)},
		map[string]port.HarnessFactory{"fake": fake.HarnessFactory(harness)},
	)
	cfg := load(t, `config:
  harness: fake
  model: some-model
tracker:
  name: fake
  labels:
    ready: todo
`+workflow)

	gotTracker, err := r.Tracker(cfg.Tracker, cfg.TrackerSection)
	if err != nil {
		t.Fatalf("Tracker: %v", err)
	}
	if gotTracker != port.Tracker(tracker) {
		t.Errorf("Tracker = %v, want the registered fake", gotTracker)
	}
	gotHarness, err := r.Harness(cfg.Harness, cfg.HarnessSection)
	if err != nil {
		t.Fatalf("Harness: %v", err)
	}
	if gotHarness != port.Harness(harness) {
		t.Errorf("Harness = %v, want the registered fake", gotHarness)
	}
}
