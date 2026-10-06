package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/config"
)

// exampleConfig is the reference of every key crew accepts, each commented
// out: a config line is "# " and YAML, and a prose line starts with "##".
var exampleConfig = filepath.Join("..", "..", ".crew", "config.example.yaml")

// modeline is the first line of the example and of crew's own config.
const modeline = "# yaml-language-server: $schema=" + schemaURL

// uncomment takes one "#", and the space after it, off each line of a
// commented config but the modeline, so its config lines become YAML and its
// "##" prose lines stay comments.
func uncomment(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == modeline {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "#"); ok {
			lines[i] = strings.TrimPrefix(rest, " ")
		}
	}
	return strings.Join(lines, "\n")
}

func readExample(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(exampleConfig)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// missingKeys returns each of keys, key paths with config.AnyName for any
// name, that the YAML text does not set.
func missingKeys(t *testing.T, text string, keys []string) []string {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, key := range keys {
		if len(doc.Content) == 0 || !hasPath(doc.Content[0], strings.Split(key, ".")) {
			missing = append(missing, key)
		}
	}
	return missing
}

// hasPath tells whether the mapping n holds the key path segs, where
// config.AnyName matches any key.
func hasPath(n *yaml.Node, segs []string) bool {
	if len(segs) == 0 {
		return true
	}
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if (segs[0] == config.AnyName || n.Content[i].Value == segs[0]) && hasPath(n.Content[i+1], segs[1:]) {
			return true
		}
	}
	return false
}

// Every key the schema defines is in the example, so a key cannot ship
// without its entry (R12 of #135).
func TestTheExampleListsEveryKey(t *testing.T) {
	var w schemaWalk
	w.object(readSchema(t), "")
	for _, key := range missingKeys(t, uncomment(readExample(t)), w.keys) {
		t.Errorf("%s does not list %s", exampleConfig, key)
	}
}

func TestMissingKeysNamesAKeyTheTextLacks(t *testing.T) {
	text := uncomment("## the poll\n# poll_interval_seconds: 300\n# rules:\n#   implement:\n#     queue: default\n")
	keys := []string{"poll_interval_seconds", "usage_in_status", "rules.*.queue", "rules.*.notify"}
	want := []string{"usage_in_status", "rules.*.notify"}
	if got := missingKeys(t, text, keys); !reflect.DeepEqual(got, want) {
		t.Errorf("missingKeys = %q, want %q", got, want)
	}
}

// The example, uncommented, is a config crew loads.
func TestTheUncommentedExampleLoads(t *testing.T) {
	cfg, err := config.Load(writeFiles(t, uncomment(readExample(t)), noFile))
	if err != nil {
		t.Fatalf("Load(the uncommented %s) = %v", exampleConfig, err)
	}
	if len(cfg.Rules) == 0 || len(cfg.Agents) < 2 || !cfg.BoardWritten {
		t.Errorf("Config = %+v, want rules, both sample agents and the board", cfg)
	}
}

// The example as committed sets no key, so copying it changes nothing.
func TestTheExampleSetsNoKey(t *testing.T) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(readExample(t)), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Content) != 0 {
		t.Errorf("%s sets keys; every key must be commented out", exampleConfig)
	}
}
