package github

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The keys of tracker in crew's JSON Schema, besides name and bot, which
// are crew's, are this adapter's settings.
func TestSchemaHasTheTrackerSettings(t *testing.T) {
	path := filepath.Join("..", "..", "..", "schema", "config.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Tracker struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"tracker"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var keys []string
	for key := range schema.Properties.Tracker.Properties {
		if key != "name" && key != "bot" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	if want := settingsKeys(); !slices.Equal(keys, want) {
		t.Errorf("%s: tracker takes %q besides name and bot, want the GitHub settings' %q", path, keys, want)
	}
}

// settingsKeys returns the YAML keys of settings, sorted.
func settingsKeys() []string {
	var keys []string
	for f := range reflect.TypeFor[settings]().Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "" {
			name = strings.ToLower(f.Name) // as the config's decoder reads it
		}
		keys = append(keys, name)
	}
	slices.Sort(keys)
	return keys
}
