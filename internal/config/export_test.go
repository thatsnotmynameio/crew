package config

import (
	"fmt"
	"reflect"
	"slices"

	"go.yaml.in/yaml/v3"
)

// AnyName stands, in a key path, for a name the code owner chooses, such as
// a rule's or an agent's.
const AnyName = "*"

// ListItem stands, in a key path, after a key whose value is a list, for
// any of its items: rules.*.actions[].prompt is the prompt of any item of a
// rule's actions.
const ListItem = "[]"

// section says how a key whose value stays a raw yaml.Node is decoded: by
// items, the structs each value or list item may be decoded into, none for
// a scalar or a list of scalars; named, whether its keys are names, each
// value an item; list, whether each value may be a list, each of its items
// an item; free, whether an item also takes keys the code owner names,
// whose own keys sections describes at that key's path when they have any;
// and open, whether its keys besides the items' go to an adapter.
type section struct {
	items []reflect.Type
	named bool
	list  bool
	free  bool
	open  bool
}

// sections are the keys of the document that stay raw nodes, each with how
// the config package decodes it, by key path.
var sections = map[string]section{
	"queues":               {named: true},
	"answering_apps":       {},
	"tracker":              {items: item[trackerDoc](), open: true},
	"agents":               {items: item[agentDoc](), named: true},
	"agents.*.harness":     {items: item[harnessDoc](), open: true},
	"actions":              {items: types[shellDoc, presetDoc](), named: true, free: true},
	"actions.*.verdicts":   {named: true},
	"board":                {named: true},
	"rules":                {items: item[ruleDoc](), named: true},
	"rules.*.labels":       {items: item[labelsDoc]()},
	"rules.*.actions":      {items: types[sessionDoc, referenceDoc](), list: true, free: true},
	"rules.*.actions[].on": {named: true},
	"rules.*.actions[].*":  {named: true},
	"rules.*.routes":       {items: item[stepDoc](), named: true, list: true, free: true},
	"rules.*.routes.*[].*": {named: true},
}

// item returns the type T, an item's only shape.
func item[T any]() []reflect.Type { return []reflect.Type{reflect.TypeFor[T]()} }

// types returns the types A and B, an item's two shapes.
func types[A, B any]() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[A](), reflect.TypeFor[B]()}
}

// AcceptedKeys returns every key path the config accepts, read from the
// document's types through fieldsByKey, sorted, with AnyName for each name;
// and open, the paths whose mapping also takes keys an adapter owns. It
// fails on a raw node that sections does not describe.
func AcceptedKeys() ([]string, []string, error) {
	w := keyWalk{}
	if err := w.fields(reflect.TypeFor[document](), ""); err != nil {
		return nil, nil, err
	}
	slices.Sort(w.keys)
	slices.Sort(w.open)
	return slices.Compact(w.keys), w.open, nil
}

type keyWalk struct {
	keys, open []string
}

// fields adds the keys of struct t, whose own path is path, and those below
// them.
func (w *keyWalk) fields(t reflect.Type, path string) error {
	for key, index := range fieldsByKey(t) {
		child := join(path, key)
		w.keys = append(w.keys, child)
		ft := t.FieldByIndex(index).Type
		switch {
		case ft == reflect.TypeFor[yaml.Node]():
			if err := w.section(child); err != nil {
				return err
			}
		case ft.Kind() == reflect.Struct && !reflect.PointerTo(ft).Implements(reflect.TypeFor[yaml.Unmarshaler]()):
			if err := w.fields(ft, child); err != nil {
				return err
			}
		}
	}
	return nil
}

// section adds the keys below the raw node at path, as sections describes it.
func (w *keyWalk) section(path string) error {
	s, ok := sections[path]
	if !ok {
		return fmt.Errorf("%s is decoded by hand; describe it in sections in export_test.go", path)
	}
	if s.open {
		w.open = append(w.open, path)
	}
	if s.named {
		path = join(path, AnyName)
		w.keys = append(w.keys, path)
	}
	if s.list {
		path += ListItem
	}
	if s.free {
		free := join(path, AnyName)
		w.keys = append(w.keys, free)
		if _, ok := sections[free]; ok {
			if err := w.section(free); err != nil {
				return err
			}
		}
	}
	for _, item := range s.items {
		if err := w.fields(item, path); err != nil {
			return err
		}
	}
	return nil
}
