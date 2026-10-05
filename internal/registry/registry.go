// Package registry resolves the adapter names of the config, tracker.name and
// each agent's harness.name, to adapters. A Registry is a value built from an explicit
// list of factories: cmd/crew passes the production list, and tests pass the
// fakes, so both go through the same lookup and validation path.
package registry

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Registry maps adapter names to their factories. Its zero value has no
// adapters. It is safe for concurrent use, since it never changes once built.
type Registry struct {
	trackers  map[string]port.TrackerFactory
	harnesses map[string]port.HarnessFactory
}

// New returns a registry holding trackers and harnesses, each keyed by the
// name the config selects it with. It copies both maps.
func New(trackers map[string]port.TrackerFactory, harnesses map[string]port.HarnessFactory) Registry {
	return Registry{trackers: maps.Clone(trackers), harnesses: maps.Clone(harnesses)}
}

// Tracker builds the tracker adapter registered as name, the config's
// tracker.name, from its config section and the rules' states. An
// unregistered name is an error naming
// tracker.name and every registered tracker; a factory's error, such as an
// unknown key in the section, is returned with the adapter's name.
func (r Registry) Tracker(name string, section port.Decode, states []crew.State) (port.Tracker, error) {
	factory, err := lookup(r.trackers, "tracker", "tracker.name", name)
	if err != nil {
		return nil, err
	}
	tracker, err := factory(section, states)
	if err != nil {
		return nil, fmt.Errorf("tracker %s: %w", name, err)
	}
	return tracker, nil
}

// Harness builds the harness adapter registered as name, an agent's
// harness.name at key, such as agents.developer.harness.name, from its
// config section, as Tracker does. An unregistered name is an error naming
// key and every registered harness.
func (r Registry) Harness(key, name string, section port.Decode) (port.Harness, error) {
	factory, err := lookup(r.harnesses, "harness", key, name)
	if err != nil {
		return nil, err
	}
	harness, err := factory(section)
	if err != nil {
		return nil, fmt.Errorf("harness %s: %w", name, err)
	}
	return harness, nil
}

// lookup returns the factory registered as name. kind names the port and key
// the config key that selected name, in its error.
func lookup[F any](factories map[string]F, kind, key, name string) (F, error) {
	factory, ok := factories[name]
	if !ok {
		registered := "none"
		if len(factories) > 0 {
			registered = strings.Join(slices.Sorted(maps.Keys(factories)), ", ")
		}
		return factory, fmt.Errorf("%s: no %s is named %q; the registered %s adapters are: %s",
			key, kind, name, kind, registered)
	}
	return factory, nil
}
