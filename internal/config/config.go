// Package config loads a repository's .crew/config.yaml: it decodes the file
// strictly, applies the engine-owned defaults, validates the rules, and
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
	"slices"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The engine-owned defaults (R1). The model's default belongs to the harness
// adapter.
const (
	defaultPollInterval      = 300 * time.Second
	defaultMaxParallelIssues = 2
	defaultTracker           = "github"
)

// Config is a loaded and validated .crew/config.yaml.
type Config struct {
	// PollInterval is poll_interval_seconds, 300 seconds by default.
	PollInterval time.Duration
	// MaxParallelIssues is max_parallel_issues, 2 by default. It counts
	// issues, not sessions, and is split into the rules' queues.
	MaxParallelIssues int
	// RunTimeLimit is run_time_limit_seconds: how long crew runs from its
	// first poll before it winds down. Zero, the default, is no limit.
	RunTimeLimit time.Duration
	// UsageInStatus is usage_in_status: whether the status comment shows
	// each ended action's cost, tokens and pull request. False by default.
	UsageInStatus bool
	// Tracker is tracker.name, the tracker adapter's name, "github" by
	// default. It is not checked against the registered adapters.
	Tracker string
	// Bot is tracker.bot: the bot crew's own writes on the tracker act as,
	// and the bot of every agent that names none. Empty, the default, means
	// the gh login crew runs as. Its spelling is not checked here.
	Bot string
	// Bots is every bot crew makes act, each once: Bot first, then the
	// bots of the agents some action names, in rule order. It is empty when
	// no bot is named.
	Bots []string
	// Agents are the agents in file order, including those no action names
	// (see Agent.Used).
	Agents []Agent
	// Rules are the rules in file order. Every state is non-empty text,
	// spelled everywhere as it is first written, since labels that differ
	// only in case are one label. No two rules take the same label, no rule
	// takes back what it moved, and every prompt renders. Every rule has its
	// queue, the one it names or default, with the queue's slots, and every
	// action its agent, check script and bot.
	Rules []crew.Rule
	// Board is the live view's board: board's columns in file order, which
	// show issues, or without board one column per rule that has actions,
	// in file order, with the rule's ready and running labels and its kind.
	// Every label is spelled once across the board, as the rules spell it
	// when it is one of their labels ignoring case.
	Board []crew.BoardColumn
	// BoardWritten tells whether Board is the file's board rather than the
	// rules' default one.
	BoardWritten bool
	// TrackerSection decodes the tracker adapter's settings: every key under
	// tracker: except name and bot.
	TrackerSection Decode
}

// document is the file's top level. The sections read on their own stay raw
// nodes.
type document struct {
	PollIntervalSeconds located[int]  `yaml:"poll_interval_seconds"`
	MaxParallelIssues   located[int]  `yaml:"max_parallel_issues"`
	RunTimeLimitSeconds located[int]  `yaml:"run_time_limit_seconds"`
	UsageInStatus       located[bool] `yaml:"usage_in_status"`
	Queues              yaml.Node     `yaml:"queues"`
	Tracker             yaml.Node     `yaml:"tracker"`
	Agents              yaml.Node     `yaml:"agents"`
	Checks              yaml.Node     `yaml:"checks"`
	Board               yaml.Node     `yaml:"board"`
	Rules               yaml.Node     `yaml:"rules"`
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
// all the rules' errors are reported together.
func Load(root string) (*Config, error) {
	path := filepath.Join(root, ".crew", "config.yaml")
	data, err := os.ReadFile(path) //nolint:gosec // the path is the repository's own .crew/config.yaml
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read crew config: %w (create it: see .crew/config.example.yaml in the crew repository)", err)
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
// it finds after the top level decodes. A file with old keys is refused with
// all of them before anything is decoded.
func parse(root *yaml.Node) (*Config, error) {
	if err := oldKeys(root); err != nil {
		return nil, err
	}
	var doc document
	if err := decodeDocument(root, &doc); err != nil {
		return nil, err
	}
	cfg := &Config{
		PollInterval:      defaultPollInterval,
		MaxParallelIssues: defaultMaxParallelIssues,
		Tracker:           defaultTracker,
	}
	errs := engineSettings(&doc, cfg)
	table, queueErrs := queues(&doc.Queues, cfg.MaxParallelIssues)
	errs = append(errs, queueErrs...)
	var err error
	cfg.TrackerSection, err = trackerSection(&doc.Tracker, cfg)
	errs = append(errs, err)
	scripts, err := checks(&doc.Checks)
	errs = append(errs, err)
	cfg.Agents, err = agents(&doc.Agents)
	errs = append(errs, err)
	cfg.Rules, err = rules(&doc.Rules, ruleEnv{queues: table, agents: cfg.Agents, checks: scripts, bot: cfg.Bot})
	errs = append(errs, err, agentsInUse(cfg.Agents, cfg.Rules))
	cfg.Bots = namedBots(cfg.Bot, cfg.Rules)
	cfg.Board, cfg.BoardWritten, err = board(&doc.Board, cfg.Rules)
	errs = append(errs, err)
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
		return fmt.Errorf("line %d: the config must be a mapping of crew's keys, such as "+
			"tracker, agents, checks, board and rules", top.Line)
	}
	return decodeFields(entries(top, ""), reflect.ValueOf(doc).Elem())
}

// engineSettings reads the engine's top-level keys into cfg, which holds
// their defaults, and returns what is wrong with them; the nil errors stand
// for the keys that are fine.
func engineSettings(doc *document, cfg *Config) []error {
	if doc.PollIntervalSeconds.line > 0 {
		cfg.PollInterval = time.Duration(doc.PollIntervalSeconds.value) * time.Second
	}
	if doc.MaxParallelIssues.line > 0 {
		cfg.MaxParallelIssues = doc.MaxParallelIssues.value
	}
	if doc.RunTimeLimitSeconds.line > 0 {
		cfg.RunTimeLimit = time.Duration(doc.RunTimeLimitSeconds.value) * time.Second
	}
	cfg.UsageInStatus = doc.UsageInStatus.value
	return []error{
		positive(doc.PollIntervalSeconds, "poll_interval_seconds", "must be a positive number of seconds"),
		positive(doc.MaxParallelIssues, "max_parallel_issues", "must be a positive number of issues"),
		positive(doc.RunTimeLimitSeconds, "run_time_limit_seconds", "must be a positive number of seconds"),
	}
}

// namedBots lists the bot def, tracker.bot, when set, and then each
// action's bot in rule order, each once.
func namedBots(def string, rules []crew.Rule) []string {
	var out []string
	if def != "" {
		out = append(out, def)
	}
	for _, r := range rules {
		for _, a := range r.Actions {
			if a.Bot != "" && !slices.Contains(out, a.Bot) {
				out = append(out, a.Bot)
			}
		}
	}
	return out
}

// positive reports msg for the key at path when it is set and not above zero.
func positive(l located[int], path, msg string) error {
	if l.line > 0 && l.value <= 0 {
		return keyError(path, l.line, msg)
	}
	return nil
}

// trackerDoc is crew's keys of tracker; every other key of tracker goes to
// the tracker adapter.
type trackerDoc struct {
	Name string `yaml:"name"`
	Bot  string `yaml:"bot"`
}

// trackerSection reads tracker.name and tracker.bot into cfg and returns
// the rest of tracker: for the tracker adapter.
func trackerSection(n *yaml.Node, cfg *Config) (Decode, error) {
	tracker, err := mapping(n, "tracker")
	if err != nil {
		return nil, err
	}
	own, rest := split(tracker, "name", "bot")
	engine := trackerDoc{Name: cfg.Tracker}
	if err := decodeFields(own, reflect.ValueOf(&engine).Elem()); err != nil {
		return nil, err
	}
	cfg.Tracker, cfg.Bot = engine.Name, engine.Bot
	return bind("tracker", rest), nil
}

// split parts a section's entries into those whose key is one of keys,
// which crew reads, and the rest, which go to an adapter.
func split(section []entry, keys ...string) ([]entry, []entry) {
	var own, rest []entry
	for _, e := range section {
		if slices.Contains(keys, e.key.Value) {
			own = append(own, e)
		} else {
			rest = append(rest, e)
		}
	}
	return own, rest
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
