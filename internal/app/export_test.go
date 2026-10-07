package app

import "github.com/thatsnotmynameio/crew/internal/engine"

// EngineConfig loads the config of o and its adapters, as Run does, and
// returns what Run hands the engine when bots are the bots that act.
func EngineConfig(o Options, bots Bots) (engine.Config, error) {
	b, err := build(o)
	if err != nil {
		return engine.Config{}, err
	}
	return b.engineConfig(o, bots), nil
}
