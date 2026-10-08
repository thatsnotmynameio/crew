// Package registry resolves the adapter names of the config, tracker.name,
// each agent's harness.name and statistics.store, to adapters, and the names
// of crew's functions to the functions. A Registry is a value built from an
// explicit list of factories: cmd/crew passes the production list, and tests
// pass the fakes, so both go through the same lookup and validation path.
package registry

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Registry maps adapter and function names to their factories. Its zero
// value has no adapters and no functions. It is safe for concurrent use,
// since it never changes once built.
type Registry struct {
	trackers   map[string]port.TrackerFactory
	harnesses  map[string]port.HarnessFactory
	functions  map[string]port.FunctionDefinition
	statistics map[string]port.StatisticsFactory
}

// New returns a registry holding trackers, harnesses, functions and
// statistics stores, each keyed by the name the config selects it with. It
// copies the four maps.
func New(
	trackers map[string]port.TrackerFactory,
	harnesses map[string]port.HarnessFactory,
	functions map[string]port.FunctionDefinition,
	statistics map[string]port.StatisticsFactory,
) Registry {
	return Registry{
		trackers:   maps.Clone(trackers),
		harnesses:  maps.Clone(harnesses),
		functions:  maps.Clone(functions),
		statistics: maps.Clone(statistics),
	}
}

// Tracker builds the tracker adapter registered as name, the config's
// tracker.name, from its config section and the rules' states. An
// unregistered name is an error naming tracker.name and every registered
// tracker; a factory's error, such as an unknown key in the section, is
// returned with the adapter's name.
func (r Registry) Tracker(name string, section port.Decode, states []crew.State) (port.Tracker, error) {
	factory, err := lookup(r.trackers, "tracker", "tracker adapters", "tracker.name", name)
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
	factory, err := lookup(r.harnesses, "harness", "harness adapters", key, name)
	if err != nil {
		return nil, err
	}
	harness, err := factory(section)
	if err != nil {
		return nil, fmt.Errorf("harness %s: %w", name, err)
	}
	return harness, nil
}

// Statistics builds the statistics store registered as name, the config's
// statistics.store, from its config section and dir, the folder its data
// lives in, as Tracker does. An unregistered name is an error naming
// statistics.store and every registered store.
func (r Registry) Statistics(name string, section port.Decode, dir string) (port.Statistics, error) {
	factory, err := lookup(r.statistics, "store", "stores", "statistics.store", name)
	if err != nil {
		return nil, err
	}
	store, err := factory(section, dir)
	if err != nil {
		return nil, fmt.Errorf("store %s: %w", name, err)
	}
	return store, nil
}

// Functions returns the catalog of the registered functions: each one's name
// and the verdicts its definition declares. Both the map and the lists are
// copies.
func (r Registry) Functions() map[string][]crew.Verdict {
	catalog := make(map[string][]crew.Verdict, len(r.functions))
	for name, def := range r.functions {
		catalog[name] = slices.Clone(def.Verdicts)
	}
	return catalog
}

// Function builds the function registered as name, which a use names at key,
// such as rules.implement.actions[1].function, from the use's parameters. An
// unregistered name is an error naming key and every registered function; a
// factory's error is returned with the function's name, and a
// port.RefusedParameterError in it stays reachable through errors.As.
func (r Registry) Function(key, name string, section port.Decode) (port.Function, error) {
	def, err := lookup(r.functions, "function", "functions", key, name)
	if err != nil {
		return nil, err
	}
	function, err := def.New(section)
	if err != nil {
		return nil, fmt.Errorf("function %s: %w", name, err)
	}
	return function, nil
}

// lookup returns the factory registered as name. kind names the port,
// registered what the registry holds of it and key the config key that
// selected name, in its error.
func lookup[F any](factories map[string]F, kind, registered, key, name string) (F, error) {
	factory, ok := factories[name]
	if !ok {
		names := "none"
		if len(factories) > 0 {
			names = strings.Join(slices.Sorted(maps.Keys(factories)), ", ")
		}
		return factory, fmt.Errorf("%s: no %s is named %q; the registered %s are: %s",
			key, kind, name, registered, names)
	}
	return factory, nil
}
