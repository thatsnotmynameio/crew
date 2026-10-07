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

// board decodes and validates board: the live view's columns, a mapping
// from a column's name to its label, or a list of one or more labels. Its
// columns show issues. It reports every error it finds, naming each column
// by its path. Every label then takes one spelling: a rule state's when it
// is one of them ignoring case, as GitHub compares labels, otherwise the
// one it first has on the board. A label written twice in one column counts
// once. Without board, it returns the rules' default board and false.
func board(n *yaml.Node, rules []crew.Rule) ([]crew.BoardColumn, bool, error) {
	if n.Kind == 0 {
		return defaultBoard(rules), false, nil
	}
	section, err := named(n, "board")
	if err != nil {
		return nil, true, err
	}
	if len(section) == 0 {
		return nil, true, keyError("board", n.Line, "must name one or more columns")
	}
	var errs []error
	out := make([]crew.BoardColumn, 0, len(section))
	for _, e := range section {
		labels, err := columnLabels(e)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, crew.BoardColumn{Name: e.key.Value, Labels: labels, Takes: crew.KindIssue})
	}
	if len(errs) > 0 {
		return nil, true, errors.Join(errs...)
	}
	respell(out, crew.RuleStates(rules))
	return out, true, nil
}

// columnLabels returns the labels of the column e: one label, or a list of
// one or more, none empty.
func columnLabels(e entry) ([]crew.State, error) {
	var labels located[[]string]
	if e.value.Kind == yaml.ScalarNode && e.value.ShortTag() != "!!null" {
		var label located[string]
		if err := decodeValue(e.value, e.path, reflect.ValueOf(&label).Elem()); err != nil {
			return nil, err
		}
		labels = located[[]string]{value: []string{label.value}, line: label.line}
	} else if err := decodeValue(e.value, e.path, reflect.ValueOf(&labels).Elem()); err != nil {
		return nil, err
	}
	column := fmt.Sprintf("column %q", e.key.Value)
	switch {
	case len(labels.value) == 0:
		return nil, keyError(e.path, e.key.Line, column+" must name one or more labels")
	case slices.Contains(labels.value, ""):
		return nil, keyError(e.path, e.key.Line, column+" must not name an empty label")
	}
	out := make([]crew.State, len(labels.value))
	for i, l := range labels.value {
		out[i] = crew.State(l)
	}
	return out, nil
}

// defaultBoard is the board without board: one column per rule that has
// actions, in rule order, named after the rule, with its ready and running
// labels, showing the items of the rule's kind.
func defaultBoard(rules []crew.Rule) []crew.BoardColumn {
	var out []crew.BoardColumn
	for _, r := range rules {
		if len(r.Actions) == 0 {
			continue
		}
		out = append(out, crew.BoardColumn{
			Name: string(r.Name), Labels: []crew.State{r.Labels.Ready, r.Labels.Running}, Takes: r.Takes,
		})
	}
	return out
}

// respell gives every label of board one spelling: the one it has among
// known, ignoring case, else the one it first has on the board.
func respell(board []crew.BoardColumn, known []crew.State) {
	first := map[string]crew.State{}
	for _, s := range known {
		if key := strings.ToLower(string(s)); first[key] == "" {
			first[key] = s
		}
	}
	for i := range board {
		labels := board[i].Labels[:0]
		for _, l := range board[i].Labels {
			key := strings.ToLower(string(l))
			spelling, ok := first[key]
			if !ok {
				spelling = l
				first[key] = l
			}
			if !slices.Contains(labels, spelling) {
				labels = append(labels, spelling)
			}
		}
		board[i].Labels = labels
	}
}
