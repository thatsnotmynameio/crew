package config

import (
	"errors"
	"reflect"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Agent is one agent of agents: the domain's agent, with what only config
// and the harness adapter read.
type Agent struct {
	crew.Agent

	// HarnessSection decodes the harness adapter's settings: every key under
	// agents.<name>.harness except name, model included.
	HarnessSection Decode
	// Used tells whether some session runs on the agent. crew builds,
	// prepares and makes act only the agents in use.
	Used bool
}

// HarnessKey is the key path of the agent's harness name, for an error about
// the harness it names.
func (a Agent) HarnessKey() string {
	return "agents." + string(a.Name) + ".harness.name"
}

// agentDoc is one agent of agents, keyed by its name.
type agentDoc struct {
	Harness yaml.Node       `yaml:"harness"`
	Bot     located[string] `yaml:"bot"`
}

// harnessDoc is crew's keys of an agent's harness; every other key of the
// harness goes to the harness adapter.
type harnessDoc struct {
	Name located[string] `yaml:"name"`
}

// What an agent and its harness must be, said when one is not a mapping.
const (
	agentShape   = "must be an agent with harness, and optionally bot"
	harnessShape = "must be a harness with name, and the harness adapter's own keys"
)

// agents decodes agents: a mapping from an agent's name to its harness and
// bot. It returns every agent in file order, even one with an error, so a
// session that names it is not reported again, and every error it finds.
func agents(n *yaml.Node) ([]Agent, error) {
	section, err := named(n, "agents")
	errs := append(make([]error, 0, len(section)+1), err)
	out := make([]Agent, 0, len(section))
	for _, e := range section {
		agent, err := parseAgent(e)
		errs = append(errs, err)
		out = append(out, agent)
	}
	return out, errors.Join(errs...)
}

// parseAgent decodes the agent e. Its harness's name is crew's, and every
// other key of its harness goes to the harness adapter.
func parseAgent(e entry) (Agent, error) {
	agent := Agent{Name: crew.AgentName(e.key.Value)}
	var doc agentDoc
	if err := decodeItem(e.value, e.path, agentShape, &doc); err != nil {
		return agent, err
	}
	agent.Bot = crew.BotName(doc.Bot.value)
	path := e.path + ".harness"
	switch doc.Harness.Kind {
	case 0:
		return agent, keyError(path, e.key.Line, "required")
	case yaml.MappingNode:
	default:
		return agent, keyError(path, doc.Harness.Line, harnessShape)
	}
	own, rest := split(entries(&doc.Harness, path), "name")
	var harness harnessDoc
	if err := decodeFields(own, reflect.ValueOf(&harness).Elem()); err != nil {
		return agent, err
	}
	agent.HarnessSection = bind(path, rest)
	name, err := required(harness.Name, path+".name", doc.Harness.Line)
	agent.Harness = crew.HarnessName(name)
	return agent, err
}

// agentsInUse marks each of agents that a session of rules runs on. It
// reports agents missing when some rule has a session and no agent is
// declared to run it; shell actions need none.
func agentsInUse(agents []Agent, rules []crew.Rule) error {
	sessions := 0
	for _, r := range rules {
		for _, a := range r.Actions {
			s, ok := a.Kind.(crew.SessionSpec)
			if !ok {
				continue
			}
			sessions++
			for i := range agents {
				if agents[i].Name == s.Agent.Name {
					agents[i].Used = true
				}
			}
		}
	}
	if sessions > 0 && len(agents) == 0 {
		return errors.New("agents: missing; declare the agent that runs the rules' sessions")
	}
	return nil
}
