package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// exampleConfig is the committed reference of .crew/config.yaml: every key,
// commented out. A code owner's own copy is ignored by git.
var exampleConfig = filepath.Join("..", "..", ".crew", "config.example.yaml")

// uncommented returns the example with its settings uncommented: "# " and a
// lone "#" are taken off each line but the schema modeline, so its "## "
// explanations stay comments.
func uncommented(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(exampleConfig)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "# yaml-language-server:") {
			continue
		}
		if line == "#" {
			lines[i] = ""
		} else if rest, ok := strings.CutPrefix(line, "# "); ok {
			lines[i] = rest
		}
	}
	return strings.Join(lines, "\n")
}

// The example sets nothing as it is: every line is a comment, so a copy
// does only what its code owner uncomments.
func TestTheExampleIsAllComments(t *testing.T) {
	data, err := os.ReadFile(exampleConfig)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			t.Errorf("%s:%d: %q is not a comment", exampleConfig, i+1, line)
		}
	}
}

// Uncommented whole, the example is a valid config.
func TestTheExampleLoadsUncommented(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".crew", "config.yaml"), []byte(uncommented(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root, "")
	if err != nil {
		t.Fatalf("Load(uncommented %s) = %v", exampleConfig, err)
	}
	names := make([]crew.RuleName, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		names = append(names, r.Name)
	}
	if want := []crew.RuleName{"development", "review", "approve"}; !slices.Equal(names, want) {
		t.Errorf("rules = %q, want %q", names, want)
	}
}

// The example is the reference of every key: it sets each key the schema
// describes at least once.
func TestTheExampleSetsEveryKey(t *testing.T) {
	var w schemaWalk
	w.object(readSchema(t), "")
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(uncommented(t)), &doc); err != nil {
		t.Fatal(err)
	}
	var set []string
	exampleKeys(doc.Content[0], "", w.keys, &set)
	for _, k := range w.keys {
		if !slices.Contains(set, k) {
			t.Errorf("%s does not set %s", exampleConfig, k)
		}
	}
}

// exampleKeys adds to set the key path of each key below n, whose own path
// is path, as schemaWalk names it: config.AnyName for a name the code owner
// chooses, and config.ListItem for an item of a list.
func exampleKeys(n *yaml.Node, path string, schema []string, set *[]string) {
	switch n.Kind {
	case yaml.SequenceNode:
		for _, item := range n.Content {
			exampleKeys(item, path+config.ListItem, schema, set)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := schemaKey(path, n.Content[i].Value)
			if !slices.Contains(schema, key) {
				key = schemaKey(path, config.AnyName)
			}
			*set = append(*set, key)
			exampleKeys(n.Content[i+1], key, schema, set)
		}
	default:
	}
}
