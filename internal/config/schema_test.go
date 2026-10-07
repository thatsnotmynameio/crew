package config_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
)

// The JSON Schema of .crew/config.yaml, and the URL it is published at,
// which crew's own config names in its modeline.
var configSchema = filepath.Join("..", "..", "schema", "config.schema.json")

const schemaURL = "https://raw.githubusercontent.com/thatsnotmynameio/crew/main/schema/config.schema.json"

// modeline is the first line of crew's own config and of the example.
const modeline = "# yaml-language-server: $schema=" + schemaURL

func readSchema(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(configSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("%s: %v", configSchema, err)
	}
	return schema
}

// schemaWalk gathers the key paths of a schema, with config.AnyName for
// each name of a map keyed by name or a key the code owner names, and
// config.ListItem for any item of a list, and what is wrong with its keys.
type schemaWalk struct {
	keys, problems []string
}

// add adds key, once.
func (w *schemaWalk) add(key string) {
	if !slices.Contains(w.keys, key) {
		w.keys = append(w.keys, key)
	}
}

// object adds the keys below the schema n, whose own path is path: those of
// each of its alternatives, of its list's items, and of its own keys. An
// object of named keys must refuse any other key, or describe it with an
// additionalProperties schema, and describe each named one; a map keyed by
// name has an additionalProperties schema for its items.
func (w *schemaWalk) object(n map[string]any, path string) {
	for _, key := range []string{"oneOf", "anyOf"} {
		alternatives, _ := n[key].([]any)
		for _, alt := range alternatives {
			if alt, ok := alt.(map[string]any); ok {
				w.object(alt, path)
			}
		}
	}
	if items, ok := n["items"].(map[string]any); ok {
		w.object(items, path+config.ListItem)
	}
	props, named := n["properties"].(map[string]any)
	item, free := n["additionalProperties"].(map[string]any)
	if named && !free && n["additionalProperties"] != false {
		w.problems = append(w.problems, fmt.Sprintf("%q: additionalProperties must be false or a schema", path))
	}
	if free {
		child := schemaKey(path, config.AnyName)
		w.add(child)
		w.object(item, child)
	}
	for key, value := range props {
		child := schemaKey(path, key)
		w.add(child)
		prop, _ := value.(map[string]any)
		if desc, _ := prop["description"].(string); desc == "" {
			w.problems = append(w.problems, child+": no description")
		}
		w.object(prop, child)
	}
}

func schemaKey(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// keyDiff names each key the config accepts and the schema lacks, and each
// key the schema has and the config does not accept. Below a path of open,
// the keys the config does not read are an adapter's: the adapter's own
// test checks them.
func keyDiff(accepted, open, schema []string) []string {
	var out []string
	for _, k := range accepted {
		if !slices.Contains(schema, k) {
			out = append(out, k+": the config accepts it, but the schema lacks it")
		}
	}
	for _, k := range schema {
		if slices.Contains(accepted, k) || adapterKey(k, accepted, open) {
			continue
		}
		out = append(out, k+": the schema has it, but the config does not accept it")
	}
	return out
}

// adapterKey tells whether key sits below a path of open, under a key the
// config does not read there.
func adapterKey(key string, accepted, open []string) bool {
	for _, o := range open {
		rest, ok := strings.CutPrefix(key, o+".")
		if !ok {
			continue
		}
		first, _, _ := strings.Cut(rest, ".")
		if !slices.Contains(accepted, o+"."+first) {
			return true
		}
	}
	return false
}

func TestSchemaAndConfigTakeTheSameKeys(t *testing.T) {
	accepted, open, err := config.AcceptedKeys()
	if err != nil {
		t.Fatal(err)
	}
	var w schemaWalk
	w.object(readSchema(t), "")
	for _, p := range w.problems {
		t.Errorf("%s: %s", configSchema, p)
	}
	for _, d := range keyDiff(accepted, open, w.keys) {
		t.Errorf("%s: %s", configSchema, d)
	}
}

func TestKeyDiffNamesAKeyMissingOnEitherSide(t *testing.T) {
	open := []string{"tracker"}
	tests := []struct {
		name             string
		accepted, schema []string
		want             []string
	}{
		{
			name:     "agree",
			accepted: []string{"rules", "rules.*", "tracker", "tracker.name"},
			schema:   []string{"rules", "rules.*", "tracker", "tracker.name"},
		},
		{
			name:     "a key only the config accepts",
			accepted: []string{"rules", "rules.*", "rules.*.notify"},
			schema:   []string{"rules", "rules.*"},
			want:     []string{"rules.*.notify: the config accepts it, but the schema lacks it"},
		},
		{
			name:     "a key only the schema has",
			accepted: []string{"rules", "rules.*"},
			schema:   []string{"rules", "rules.*", "rules.*.on_board"},
			want:     []string{"rules.*.on_board: the schema has it, but the config does not accept it"},
		},
		{
			name:     "an adapter's key below an open path",
			accepted: []string{"tracker", "tracker.name"},
			schema:   []string{"tracker", "tracker.name", "tracker.host", "tracker.host.port"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := keyDiff(tt.accepted, open, tt.schema); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("keyDiff = %q, want %q", got, tt.want)
			}
		})
	}
}

// crew's own config and the example both name the published schema on
// their first line, as editors read it.
func TestTheConfigsNameThePublishedSchema(t *testing.T) {
	if id := readSchema(t)["$id"]; id != schemaURL {
		t.Errorf("%s: $id = %v, want %s", configSchema, id, schemaURL)
	}
	for _, path := range []string{ownConfig, exampleConfig} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(string(data), "\n"); first != modeline {
			t.Errorf("%s: first line = %q, want %q", path, first, modeline)
		}
	}
}
