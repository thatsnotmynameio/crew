package config

import (
	"reflect"

	"go.yaml.in/yaml/v3"
)

// The statistics store crew records to without statistics.store, and the
// store's name that turns recording off.
const (
	defaultStore = "sqlite"
	storeOff     = "off"
)

// statisticsDoc is crew's key of statistics; every other key of statistics
// goes to the store's adapter.
type statisticsDoc struct {
	Store string `yaml:"store"`
}

// statisticsSection reads statistics.store into cfg, "" when it is off, and
// returns the rest of statistics: for the store's adapter. An empty store is
// an error, so only off turns recording off. With the store off, any other
// key is an error, since nothing would read it.
func statisticsSection(n *yaml.Node, cfg *Config) (Decode, error) {
	const path = "statistics"
	section, err := mapping(n, path)
	if err != nil {
		return nil, err
	}
	own, rest := split(section, "store")
	doc := statisticsDoc{Store: defaultStore}
	if err := decodeFields(own, reflect.ValueOf(&doc).Elem()); err != nil {
		return nil, err
	}
	if doc.Store == "" {
		return nil, keyError(own[0].path, own[0].key.Line, "must name a store, or off")
	}
	if doc.Store == storeOff {
		if len(rest) > 0 {
			return nil, keyError(rest[0].path, rest[0].key.Line, "store is off, so nothing reads this key")
		}
		doc.Store = ""
	}
	cfg.Statistics = doc.Store
	return bind(path, rest), nil
}
