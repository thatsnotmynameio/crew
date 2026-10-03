// Package config loads a repository's .crew/config.yaml: it decodes the file
// strictly, applies the engine-owned defaults, validates the workflow, and
// hands each adapter its own section as a strict Decode. It does not resolve
// adapter names; the registry does, so config holds no adapter knowledge.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The engine-owned defaults (R1). The model's default belongs to the harness
// adapter.
const (
	defaultPollInterval      = 300 * time.Second
	defaultMaxParallelIssues = 2
	defaultHarness           = "claude"
	defaultTracker           = "github"
)

// Config is a loaded and validated .crew/config.yaml.
type Config struct {
	// PollInterval is config.poll_interval_seconds, 300 seconds by default.
	PollInterval time.Duration
	// MaxParallelIssues is config.max_parallel_issues, 2 by default. It counts
	// issues, not sessions.
	MaxParallelIssues int
	// RunTimeLimit is config.run_time_limit_seconds: how long crew runs from
	// its first poll before it winds down. Zero, the default, is no limit.
	RunTimeLimit time.Duration
	// Harness is config.harness, the harness adapter's name, "claude" by
	// default. It is not checked against the registered adapters.
	Harness string
	// Tracker is tracker.name, the tracker adapter's name, "github" by
	// default. It is not checked against the registered adapters.
	Tracker string
	// Workflow is the stages in file order. Every state is non-empty text,
	// spelled everywhere as it is first written, since labels that differ
	// only in case are one label. The stages cannot loop or take an issue
	// twice, and every prompt renders.
	Workflow []crew.Stage
	// Extras is extra_labels' labels in file order: labels for parked work
	// that no stage takes. Each is written as in the file, is none of the
	// workflow's states and is no other extra, ignoring case.
	Extras []crew.State
	// HarnessSection decodes the harness adapter's settings: config.model plus
	// the keys of the optional top-level harness: section.
	HarnessSection Decode
	// TrackerSection decodes the tracker adapter's settings: every key under
	// tracker: except name.
	TrackerSection Decode
}

// document is the file's top level. The sections adapters own stay raw nodes.
type document struct {
	Config      settings  `yaml:"config"`
	Tracker     yaml.Node `yaml:"tracker"`
	Harness     yaml.Node `yaml:"harness"`
	Workflow    yaml.Node `yaml:"workflow"`
	ExtraLabels yaml.Node `yaml:"extra_labels"`
	Prompts     yaml.Node `yaml:"prompts"`
}

// settings is the config: section. model is the harness adapter's.
type settings struct {
	PollIntervalSeconds located[int]    `yaml:"poll_interval_seconds"`
	MaxParallelIssues   located[int]    `yaml:"max_parallel_issues"`
	RunTimeLimitSeconds located[int]    `yaml:"run_time_limit_seconds"`
	Harness             located[string] `yaml:"harness"`
	Model               yaml.Node       `yaml:"model"`
}

// located is a scalar that remembers its line, for errors found after
// decoding. A zero line means the key was left out.
type located[T any] struct {
	value T
	line  int
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (l *located[T]) UnmarshalYAML(n *yaml.Node) error {
	l.line = n.Line
	// Wrapped, a *yaml.TypeError would end the whole decode instead of
	// joining the library's other type errors.
	return n.Decode(&l.value) //nolint:wrapcheck // yaml merges a *yaml.TypeError only when returned as is
}

// Load reads root/.crew/config.yaml, where root is the repository's root,
// and returns it decoded, defaulted and validated. Every error names the file,
// and every error about the file's content names the key path and its line;
// all the workflow's errors are reported together.
func Load(root string) (*Config, error) {
	path := filepath.Join(root, ".crew", "config.yaml")
	data, err := os.ReadFile(path) //nolint:gosec // the path is the repository's own .crew/config.yaml
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read crew config: %w (create it: see docs/guide/crew.mdx in the crew repository)", err)
	}
	if err != nil {
		return nil, fmt.Errorf("read crew config: %w", err)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg, err := parse(&node)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// parse turns the file's YAML document into a Config, reporting every error
// it finds after the top level decodes.
func parse(root *yaml.Node) (*Config, error) {
	var doc document
	if err := decodeDocument(root, &doc); err != nil {
		return nil, err
	}
	cfg := &Config{
		PollInterval:      defaultPollInterval,
		MaxParallelIssues: defaultMaxParallelIssues,
		Harness:           defaultHarness,
		Tracker:           defaultTracker,
	}
	errs := engineSettings(&doc.Config, cfg)
	var err error
	cfg.HarnessSection, err = harnessSection(&doc)
	errs = append(errs, err)
	cfg.TrackerSection, err = trackerSection(&doc.Tracker, cfg)
	errs = append(errs, err)
	cfg.Workflow, err = workflow(&doc.Workflow)
	errs = append(errs, err)
	// With an invalid workflow, the extras are checked only on their own.
	cfg.Extras, err = extraLabels(&doc.ExtraLabels, cfg.Workflow)
	errs = append(errs, err)
	errs = append(errs, prompts(&doc.Prompts))
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cfg, nil
}

// decodeDocument decodes the file's top level into doc. An empty file has no
// content and leaves doc empty.
func decodeDocument(root *yaml.Node, doc *document) error {
	if len(root.Content) == 0 {
		return nil
	}
	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: the config must be a mapping with "+
			"config, tracker, harness, workflow, extra_labels and prompts", top.Line)
	}
	return decodeFields(entries(top, ""), reflect.ValueOf(doc).Elem())
}

// engineSettings reads the engine's keys of config: into cfg, which holds
// their defaults, and returns what is wrong with them; the nil errors stand
// for the keys that are fine.
func engineSettings(s *settings, cfg *Config) []error {
	if s.PollIntervalSeconds.line > 0 {
		cfg.PollInterval = time.Duration(s.PollIntervalSeconds.value) * time.Second
	}
	if s.MaxParallelIssues.line > 0 {
		cfg.MaxParallelIssues = s.MaxParallelIssues.value
	}
	if s.RunTimeLimitSeconds.line > 0 {
		cfg.RunTimeLimit = time.Duration(s.RunTimeLimitSeconds.value) * time.Second
	}
	if s.Harness.line > 0 {
		cfg.Harness = s.Harness.value
	}
	return []error{
		positive(s.PollIntervalSeconds, "config.poll_interval_seconds", "must be a positive number of seconds"),
		positive(s.MaxParallelIssues, "config.max_parallel_issues", "must be a positive number of issues"),
		positive(s.RunTimeLimitSeconds, "config.run_time_limit_seconds", "must be a positive number of seconds"),
	}
}

// positive reports msg for the key at path when it is set and not above zero.
func positive(l located[int], path, msg string) error {
	if l.line > 0 && l.value <= 0 {
		return keyError(path, l.line, msg)
	}
	return nil
}

// harnessSection joins config.model with the keys of harness:, which is
// reserved for harness-specific keys and so cannot set the model again.
func harnessSection(doc *document) (Decode, error) {
	var section []entry
	if model := &doc.Config.Model; model.Kind != 0 {
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "model", Line: model.Line}
		section = append(section, entry{key: key, value: model, path: "config.model"})
	}
	harness, err := mapping(&doc.Harness, "harness")
	if err != nil {
		return nil, err
	}
	for _, e := range harness {
		if e.key.Value == "model" {
			return nil, keyError(e.path, e.key.Line, "set the model in config.model")
		}
	}
	return bind("harness", append(section, harness...)), nil
}

// trackerSection reads tracker.name into cfg and returns the rest of
// tracker: for the tracker adapter.
func trackerSection(n *yaml.Node, cfg *Config) (Decode, error) {
	tracker, err := mapping(n, "tracker")
	if err != nil {
		return nil, err
	}
	var name, rest []entry
	for _, e := range tracker {
		if e.key.Value == "name" {
			name = append(name, e)
		} else {
			rest = append(rest, e)
		}
	}
	engine := struct {
		Name string `yaml:"name"`
	}{Name: cfg.Tracker}
	if err := decodeFields(name, reflect.ValueOf(&engine).Elem()); err != nil {
		return nil, err
	}
	cfg.Tracker = engine.Name
	return bind("tracker", rest), nil
}

// mapping returns the entries of an optional section, which must be a
// mapping when present.
func mapping(n *yaml.Node, path string) ([]entry, error) {
	switch n.Kind {
	case 0:
		return nil, nil
	case yaml.MappingNode:
		return entries(n, path), nil
	default:
		return nil, keyError(path, n.Line, "must be a mapping")
	}
}
