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

// queueTable is the queues a rule may name: the declared queues in file
// order, then default.
type queueTable []crew.Queue

// find returns the queue called name.
func (t queueTable) find(name crew.QueueName) (crew.Queue, bool) {
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
		names[i] = string(q.Name)
	}
	return strings.Join(names, ", ")
}

// queues splits limit, max_parallel_issues, into the queues n declares and
// default, which gets the slots they leave, and returns what is wrong with
// them. A limit that is not positive is reported elsewhere, so the sum that
// depends on it is not checked. Every declared queue stays in the table,
// even one whose slots are wrong, so a rule that names it is not reported
// again.
func queues(n *yaml.Node, limit int) (queueTable, []error) {
	declared, sum, err := declaredQueues(n)
	errs := []error{err}
	free := limit - sum
	if limit > 0 && free < 0 {
		errs = append(errs, keyError("queues", n.Line,
			fmt.Sprintf("the default queue would have %d - %d = %d slots "+
				"(max_parallel_issues - these queues' slots); it must have 0 or more",
				limit, sum, free)))
	}
	return append(queueTable(declared), crew.Queue{Name: crew.DefaultQueue, Slots: free}), errs
}

// declaredQueues reads queues: a mapping from a queue's name to its slots.
// It returns the queues in file order, the sum of their valid slots, and
// what is wrong with them.
func declaredQueues(n *yaml.Node) ([]crew.Queue, int, error) {
	section, err := named(n, "queues")
	errs := []error{err}
	out := make([]crew.Queue, 0, len(section))
	sum := 0
	for _, e := range section {
		slots, err := declaredQueue(e)
		if err != nil {
			errs = append(errs, err)
		} else {
			sum += slots
		}
		out = append(out, crew.Queue{Name: crew.QueueName(e.key.Value), Slots: slots})
	}
	return out, sum, errors.Join(errs...)
}

// declaredQueue returns the slots of the queue e declares, and what is wrong
// with it.
func declaredQueue(e entry) (int, error) {
	name := e.key.Value
	if strings.EqualFold(name, string(crew.DefaultQueue)) {
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
	name := crew.QueueName(l.value)
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
