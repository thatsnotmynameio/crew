package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The keys of an agent's harness in crew's JSON Schema, besides name, which
// is crew's, are this adapter's settings.
func TestSchemaHasTheHarnessSettings(t *testing.T) {
	path := filepath.Join("..", "..", "..", "schema", "config.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties struct {
			Agents struct {
				AdditionalProperties struct {
					Properties struct {
						Harness struct {
							Properties map[string]json.RawMessage `json:"properties"`
						} `json:"harness"`
					} `json:"properties"`
				} `json:"additionalProperties"`
			} `json:"agents"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var keys []string
	for key := range schema.Properties.Agents.AdditionalProperties.Properties.Harness.Properties {
		if key != "name" {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	if want := settingsKeys(); !slices.Equal(keys, want) {
		t.Errorf("%s: agents.*.harness takes %q besides name, want the claude settings' %q", path, keys, want)
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
