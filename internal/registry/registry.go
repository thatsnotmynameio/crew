// Package registry resolves the adapter names of the config, tracker.name and
// config.harness, to adapters. A Registry is a value built from an explicit
// list of factories: cmd/crew passes the production list, and tests pass the
// fakes, so both go through the same lookup and validation path.
package registry

import (
	"fmt"
	"maps"
	"slices"
	"strings"

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
// tracker.name, from its config section. An unregistered name is an error
// naming tracker.name and every registered tracker; a factory's error, such
// as an unknown key in the section, is returned with the adapter's name.
func (r Registry) Tracker(name string, section port.Decode) (port.Tracker, error) {
	return build(r.trackers, "tracker", "tracker.name", name, section)
}

// Harness builds the harness adapter registered as name, the config's
// config.harness, from its config section, as Tracker does.
func (r Registry) Harness(name string, section port.Decode) (port.Harness, error) {
	return build(r.harnesses, "harness", "config.harness", name, section)
}

// build looks name up in factories and builds the adapter from section. kind
// names the port and key the config key that selected name, in errors.
func build[A any, F ~func(port.Decode) (A, error)](factories map[string]F, kind, key, name string, section port.Decode) (A, error) {
	var none A
	factory, ok := factories[name]
	if !ok {
		registered := "none"
		if len(factories) > 0 {
			registered = strings.Join(slices.Sorted(maps.Keys(factories)), ", ")
		}
		return none, fmt.Errorf("%s: no %s is named %q; the registered %ss are: %s", key, kind, name, kind, registered)
	}
	adapter, err := factory(section)
	if err != nil {
		return none, fmt.Errorf("%s %s: %w", kind, name, err)
	}
	return adapter, nil
}
