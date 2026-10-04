package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// columnDoc is one item of board: a column of the live view's board.
type columnDoc struct {
	Name   located[string]   `yaml:"name"`
	Labels located[[]string] `yaml:"labels"`
}

// columnShape is what an item of board must be.
const columnShape = "must be a column with name and labels"

// board decodes and validates board: the live view's columns, each with a
// name no other column has and one or more non-empty labels. It reports every
// error it finds, naming each column by its path and name. Every label then
// takes one spelling: a workflow state's or an extra's when it is one of
// them ignoring case, as GitHub compares labels, otherwise the one it first
// has on the board. A label written twice in one column counts once.
func board(n *yaml.Node, workflow []crew.Stage, extras []crew.State) ([]crew.BoardColumn, error) {
	switch {
	case n.Kind == 0:
		return nil, nil
	case n.Kind != yaml.SequenceNode:
		return nil, keyError("board", n.Line, "must be a list of columns")
	case len(n.Content) == 0:
		return nil, keyError("board", n.Line, "must list one or more columns")
	}
	var errs []error
	firstPath := map[string]string{}
	out := make([]crew.BoardColumn, 0, len(n.Content))
	for i, item := range n.Content {
		path := fmt.Sprintf("board[%d]", i)
		var doc columnDoc
		if err := decodeItem(item, path, columnShape, &doc); err != nil {
			errs = append(errs, err)
			continue
		}
		column, err := boardColumn(doc, path, item.Line, firstPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, column)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	respell(out, slices.Concat(crew.WorkflowStates(workflow), extras))
	return out, nil
}

// boardColumn checks the column at path, whose mapping is on itemLine.
// firstPath holds the path of each earlier column by name; a column with a
// name is added to it.
func boardColumn(doc columnDoc, path string, itemLine int, firstPath map[string]string) (crew.BoardColumn, error) {
	var errs []error
	name, err := required(doc.Name, path+".name", itemLine)
	errs = append(errs, err)
	if first, ok := firstPath[name]; ok && name != "" {
		errs = append(errs, keyError(path+".name", doc.Name.line, fmt.Sprintf("%q is already %s.name", name, first)))
	} else if name != "" {
		firstPath[name] = path
	}
	errs = append(errs, columnLabels(doc.Labels, path, itemLine, name))
	if err := errors.Join(errs...); err != nil {
		return crew.BoardColumn{}, err
	}
	return crew.BoardColumn{Name: name, Labels: doc.Labels.value}, nil
}

// columnLabels checks the labels of the column at path named name: one or
// more, none empty.
func columnLabels(l located[[]string], path string, itemLine int, name string) error {
	column := "the column"
	if name != "" {
		column = fmt.Sprintf("column %q", name)
	}
	line := l.line
	if line == 0 {
		line = itemLine
	}
	switch {
	case len(l.value) == 0:
		return keyError(path+".labels", line, column+" must list one or more labels")
	case slices.Contains(l.value, ""):
		return keyError(path+".labels", line, column+" must not list an empty label")
	}
	return nil
}

// respell gives every label of board one spelling: the one it has among
// known, ignoring case, else the one it first has on the board.
func respell(board []crew.BoardColumn, known []crew.State) {
	first := map[string]string{}
	for _, s := range known {
		if key := strings.ToLower(string(s)); first[key] == "" {
			first[key] = string(s)
		}
	}
	for i := range board {
		labels := board[i].Labels[:0]
		for _, l := range board[i].Labels {
			key := strings.ToLower(l)
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
