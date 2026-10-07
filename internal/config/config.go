// Package config loads the user's global crew/config.yaml, then a
// repository's .crew/config.yaml and .crew/config.local.yaml, each file's
// top-level keys replacing those of the files before it: it decodes them
// strictly, applies the engine-owned defaults, validates the rules, and
// hands each adapter its own section as a strict Decode. It does not
// resolve adapter names; the registry does, so config holds no adapter
// knowledge.
package config

import (
	"errors"
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

// Config is a loaded and validated config: the global file's top-level
// keys, replaced by those of .crew/config.yaml, then by those of
// .crew/config.local.yaml.
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
	Bot crew.Bot
	// Bots is every bot crew makes act, each once: Bot first, then the
	// bots of the agents some session names, in rule order. It is empty when
	// no bot is named.
	Bots []crew.Bot
	// AnsweringApps is answering_apps: the logins of the Apps whose
	// comments answer a session's question, each <slug>[bot], never
	// github-actions[bot] (R38, R39). It is nil when the file leaves the key
	// out.
	AnsweringApps []string
	// AnsweringAppsWritten tells whether AnsweringApps is the file's list,
	// which replaces the default, crew's bots, even when empty.
	AnsweringAppsWritten bool
	// Agents are the agents in file order, including those no session names
	// (see Agent.Used).
	Agents []Agent
	// Rules are the rules in file order. Every state is non-empty text,
	// spelled everywhere as it is first written, since labels that differ
	// only in case are one label. No two rules take the same label, no rule
	// takes back what it moved, and every prompt and comment renders. Every
	// rule has its queue, the one it names or default, with the queue's
	// slots, its actions in the order they run and its routes. Every session
	// has its agent and bot, every shell action its script from actions, and
	// every on leads to one of its rule's routes.
	Rules []crew.Rule
	// Notify tells, for each rule by name, whether the live view sends a
	// desktop notification when the rule ends for an item: the rule's
	// notify, or by default whether it has actions.
	Notify map[crew.RuleName]bool
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
	// Functions are the config's function uses, one per rule action or
	// route step that calls a function, in rule order and, in each rule,
	// its actions' and then its routes' order. It is empty when no rule
	// calls a function.
	Functions []FunctionUse
}

// document is the file's top level. The sections read on their own stay raw
// nodes.
type document struct {
	PollIntervalSeconds located[int]  `yaml:"poll_interval_seconds"`
	MaxParallelIssues   located[int]  `yaml:"max_parallel_issues"`
	RunTimeLimitSeconds located[int]  `yaml:"run_time_limit_seconds"`
	UsageInStatus       located[bool] `yaml:"usage_in_status"`
	AnsweringApps       yaml.Node     `yaml:"answering_apps"`
	Queues              yaml.Node     `yaml:"queues"`
	Tracker             yaml.Node     `yaml:"tracker"`
	Agents              yaml.Node     `yaml:"agents"`
	Actions             yaml.Node     `yaml:"actions"`
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

// Load reads the user's global config file at global, unless it is "",
// then root/.crew/config.yaml and root/.crew/config.local.yaml, where root
// is the repository's root, and returns them combined, decoded, defaulted
// and validated. Any of the files may be missing, but not all. Each
// top-level key of a later file replaces that key of the earlier ones
// whole. Every error names the file it is about, and every error about a
// file's content names the key path and its line; all the rules' errors
// are reported together. functions is the catalog of the registered
// functions, each with the verdicts it declares besides passed and failed:
// the names a rule may call beside its defined actions; nil registers none.
func Load(root, global string, functions map[string][]crew.Verdict) (*Config, error) {
	sources, err := readSources(root, global)
	if err != nil {
		return nil, err
	}
	top, o := merge(sources)
	cfg, err := parse(top, functions)
	if err != nil {
		return nil, o.name(err)
	}
	cfg.TrackerSection = o.decode(cfg.TrackerSection)
	for i := range cfg.Functions {
		cfg.Functions[i] = cfg.Functions[i].named(o)
	}
	for i := range cfg.Agents {
		cfg.Agents[i].HarnessSection = o.decode(cfg.Agents[i].HarnessSection)
	}
	return cfg, nil
}

// parse turns the config's top-level mapping into a Config, its names
// resolved against functions too, reporting every error it finds after the
// top level decodes.
func parse(top *yaml.Node, functions map[string][]crew.Verdict) (*Config, error) {
	var doc document
	if err := decodeFields(entries(top, ""), reflect.ValueOf(&doc).Elem()); err != nil {
		return nil, err
	}
	cfg := &Config{
		PollInterval:      defaultPollInterval,
		MaxParallelIssues: defaultMaxParallelIssues,
		Tracker:           defaultTracker,
	}
	errs := append(engineSettings(&doc, cfg), answeringApps(&doc.AnsweringApps, cfg))
	table, queueErrs := queues(&doc.Queues, cfg.MaxParallelIssues)
	errs = append(errs, queueErrs...)
	var err error
	cfg.TrackerSection, err = trackerSection(&doc.Tracker, cfg)
	errs = append(errs, err)
	shellActions, presets, err := shells(&doc.Actions, functions)
	errs = append(errs, err)
	cfg.Agents, err = agents(&doc.Agents)
	errs = append(errs, err)
	env := ruleEnv{
		queues: table, agents: cfg.Agents, actions: shellActions, presets: presets,
		functions: functions, bot: cfg.Bot, uses: &cfg.Functions,
	}
	cfg.Rules, cfg.Notify, err = rules(&doc.Rules, env)
	errs = append(errs, err, agentsInUse(cfg.Agents, cfg.Rules))
	cfg.Bots = namedBots(cfg.Bot, cfg.Rules)
	cfg.Board, cfg.BoardWritten, err = board(&doc.Board, cfg.Rules)
	errs = append(errs, err)
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cfg, nil
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
// session's bot in rule order, each once.
func namedBots(def crew.Bot, rules []crew.Rule) []crew.Bot {
	var out []crew.Bot
	if def.Name != "" {
		out = append(out, def)
	}
	for _, r := range rules {
		for _, a := range r.Actions {
			if s, ok := a.Kind.(crew.SessionSpec); ok && s.Bot.Name != "" && !slices.Contains(out, s.Bot) {
				out = append(out, s.Bot)
			}
		}
	}
	return out
}

// BotNames returns the names of Bots, in order: the engine and the bots'
// runtime refer to bots by name.
func (c *Config) BotNames() []crew.BotName {
	names := make([]crew.BotName, 0, len(c.Bots))
	for _, bot := range c.Bots {
		names = append(names, bot.Name)
	}
	return names
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
	cfg.Tracker, cfg.Bot = engine.Name, crew.Bot{Name: crew.BotName(engine.Bot)}
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
