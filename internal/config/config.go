// Package config loads a repository's .crew/config.yaml: it decodes the file
// strictly, applies the engine-owned defaults, validates the workflow, and
// hands each adapter its own section as a strict Decode. It does not resolve
// adapter names; the registry does, so config holds no adapter knowledge.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The engine-owned defaults (R1). The model's default belongs to the harness
// adapter, and tracker.labels' to the tracker adapter.
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
	// Harness is config.harness, the harness adapter's name, "claude" by
	// default. It is not checked against the registered adapters.
	Harness string
	// Tracker is tracker.name, the tracker adapter's name, "github" by
	// default. It is not checked against the registered adapters.
	Tracker string
	// Workflow is the stages in file order. Every state is one of the eight,
	// the stages cannot loop or take an issue twice, and every prompt renders.
	Workflow []crew.Stage
	// HarnessSection decodes the harness adapter's settings: config.model plus
	// the keys of the optional top-level harness: section.
	HarnessSection Decode
	// TrackerSection decodes the tracker adapter's settings: every key under
	// tracker: except name.
	TrackerSection Decode
}

// document is the file's top level. The sections adapters own stay raw nodes.
type document struct {
	Config   settings  `yaml:"config"`
	Tracker  yaml.Node `yaml:"tracker"`
	Harness  yaml.Node `yaml:"harness"`
	Workflow yaml.Node `yaml:"workflow"`
}

// settings is the config: section. model is the harness adapter's.
type settings struct {
	PollIntervalSeconds located[int]    `yaml:"poll_interval_seconds"`
	MaxParallelIssues   located[int]    `yaml:"max_parallel_issues"`
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
	return n.Decode(&l.value)
}

// Load reads root/.crew/config.yaml, where root is the repository's root,
// and returns it decoded, defaulted and validated. Every error names the file,
// and every error about the file's content names the key path and its line;
// all the workflow's errors are reported together.
func Load(root string) (*Config, error) {
	path := filepath.Join(root, ".crew", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read crew config: %w", err)
	}
	cfg, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func parse(data []byte) (*Config, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var doc document
	if len(root.Content) > 0 { // an empty file has no content
		top := root.Content[0]
		if top.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("line %d: the config must be a mapping with config, tracker, harness and workflow", top.Line)
		}
		if err := decodeFields(entries(top, ""), reflect.ValueOf(&doc).Elem()); err != nil {
			return nil, err
		}
	}

	cfg := &Config{
		PollInterval:      defaultPollInterval,
		MaxParallelIssues: defaultMaxParallelIssues,
		Harness:           defaultHarness,
		Tracker:           defaultTracker,
	}
	var errs []error
	if s := doc.Config.PollIntervalSeconds; s.line > 0 {
		if s.value <= 0 {
			errs = append(errs, keyError("config.poll_interval_seconds", s.line, "must be a positive number of seconds"))
		}
		cfg.PollInterval = time.Duration(s.value) * time.Second
	}
	if s := doc.Config.MaxParallelIssues; s.line > 0 {
		if s.value <= 0 {
			errs = append(errs, keyError("config.max_parallel_issues", s.line, "must be a positive number of issues"))
		}
		cfg.MaxParallelIssues = s.value
	}
	if s := doc.Config.Harness; s.line > 0 {
		cfg.Harness = s.value
	}

	var err error
	if cfg.HarnessSection, err = harnessSection(&doc); err != nil {
		errs = append(errs, err)
	}
	if cfg.TrackerSection, err = trackerSection(&doc.Tracker, cfg); err != nil {
		errs = append(errs, err)
	}
	if cfg.Workflow, err = workflow(&doc.Workflow); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
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
	}
	return nil, keyError(path, n.Line, "must be a mapping")
}
