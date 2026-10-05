package config

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// defaultClerkSlots is clerk's size when config.clerk_slots is left out.
const defaultClerkSlots = 1

// queueTable is the queues a rule may name: clerk, the declared queues in
// file order, then default.
type queueTable []crew.Queue

// find returns the queue called name.
func (t queueTable) find(name string) (crew.Queue, bool) {
	i := slices.IndexFunc(t, func(q crew.Queue) bool { return q.Name == name })
	if i < 0 {
		return crew.Queue{}, false
	}
	return t[i], true
}

// names lists the queues' names, for an error about a queue that does not
// exist.
func (t queueTable) names() string {
	names := make([]string, len(t))
	for i, q := range t {
		names[i] = q.Name
	}
	return strings.Join(names, ", ")
}

// queues splits limit, max_parallel_issues, into the queues of config:, and
// returns what is wrong with them. A limit that is not positive is reported
// elsewhere, so the sums that depend on it are not checked. Every declared
// queue stays in the table, even one whose slots are wrong, so a rule that
// names it is not reported again.
func queues(s *settings, limit int) (queueTable, []error) {
	clerk, clerkOK, err := clerkSlots(s, limit)
	errs := []error{err}
	declared, sum, err := declaredQueues(&s.Queues)
	errs = append(errs, err)
	free := limit - clerk - sum
	if clerkOK && free < 0 {
		errs = append(errs, keyError("config.queues", s.Queues.Line,
			fmt.Sprintf("the default queue would have %d - %d - %d = %d slots "+
				"(max_parallel_issues - clerk_slots - these queues' slots); it must have 0 or more",
				limit, clerk, sum, free)))
	}
	table := append(queueTable{{Name: crew.ClerkQueue, Slots: clerk}}, declared...)
	table = append(table, crew.Queue{Name: crew.DefaultQueue, Slots: free})
	return table, errs
}

// clerkSlots returns clerk's slots, whether they and limit are valid, so
// default's slots are worth checking, and what is wrong with them.
func clerkSlots(s *settings, limit int) (int, bool, error) {
	clerk := s.ClerkSlots
	switch {
	case clerk.line == 0 && limit > 0 && limit <= defaultClerkSlots:
		return defaultClerkSlots, false, keyError("config.max_parallel_issues", s.MaxParallelIssues.line,
			fmt.Sprintf("must be at least %d, to leave room for the %d-slot clerk queue",
				defaultClerkSlots+1, defaultClerkSlots))
	case clerk.line == 0:
		return defaultClerkSlots, limit > 0, nil
	case clerk.value < 1:
		return clerk.value, false, keyError("config.clerk_slots", clerk.line, "must be a positive number of slots")
	case limit > 0 && clerk.value >= limit:
		return clerk.value, false, keyError("config.clerk_slots", clerk.line,
			fmt.Sprintf("must be below max_parallel_issues (%d), which the other queues share too", limit))
	}
	return clerk.value, limit > 0, nil
}

// declaredQueues reads config.queues: a mapping from a queue's name to its
// slots. It returns the queues in file order, the sum of their valid slots,
// and what is wrong with them.
func declaredQueues(n *yaml.Node) ([]crew.Queue, int, error) {
	section, err := mapping(n, "config.queues")
	if err != nil {
		return nil, 0, err
	}
	var errs []error
	out := make([]crew.Queue, 0, len(section))
	sum := 0
	seen := make(map[string]int, len(section))
	for _, e := range section {
		name := e.key.Value
		if first, ok := seen[name]; ok {
			errs = append(errs, keyError(e.path, e.key.Line, fmt.Sprintf("duplicate key, first set on line %d", first)))
			continue
		}
		seen[name] = e.key.Line
		slots, err := declaredQueue(e)
		if err != nil {
			errs = append(errs, err)
		} else {
			sum += slots
		}
		out = append(out, crew.Queue{Name: name, Slots: slots})
	}
	return out, sum, errors.Join(errs...)
}

// declaredQueue returns the slots of the queue e declares, and what is wrong
// with it.
func declaredQueue(e entry) (int, error) {
	name := e.key.Value
	switch {
	case strings.EqualFold(name, crew.ClerkQueue):
		return 0, keyError(e.path, e.key.Line,
			fmt.Sprintf("%q is crew's clerk queue; set its slots in config.clerk_slots", name))
	case strings.EqualFold(name, crew.DefaultQueue):
		return 0, keyError(e.path, e.key.Line,
			fmt.Sprintf("%q is crew's default queue, which has the slots the other queues leave; "+
				"name this queue another way", name))
	}
	var slots located[int]
	if err := decodeValue(e.value, e.path, reflect.ValueOf(&slots).Elem()); err != nil {
		return 0, err
	}
	if slots.value < 1 {
		return slots.value, keyError(e.path, e.key.Line, "must be a positive number of slots")
	}
	return slots.value, nil
}

// ruleQueue returns the queue of the rule at path: the one its queue key
// names, or default when it names none.
func ruleQueue(l located[string], path string, table queueTable) (crew.Queue, error) {
	name := l.value
	switch {
	case l.line == 0:
		name = crew.DefaultQueue
	case name == "":
		return crew.Queue{}, keyError(path+".queue", l.line, "must not be empty")
	}
	q, ok := table.find(name)
	if !ok {
		return crew.Queue{}, keyError(path+".queue", l.line,
			fmt.Sprintf("queue %q does not exist; the queues are %s", name, table.names()))
	}
	return q, nil
}
