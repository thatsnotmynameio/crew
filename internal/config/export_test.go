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

// section says how a key whose value stays a raw yaml.Node is decoded: by
// item, the struct each value is decoded into, or nil for a scalar or a list
// of scalars; named, whether its keys are names, each value an item; and
// open, whether its keys besides item's go to an adapter.
type section struct {
	item  reflect.Type
	named bool
	open  bool
}

// sections are the keys of the document that stay raw nodes, each with how
// the config package decodes it, by key path.
var sections = map[string]section{
	"queues":                  {named: true},
	"tracker":                 {item: reflect.TypeFor[trackerDoc](), open: true},
	"agents":                  {item: reflect.TypeFor[agentDoc](), named: true},
	"agents.*.harness":        {item: reflect.TypeFor[harnessDoc](), open: true},
	"checks":                  {named: true},
	"board":                   {named: true},
	"rules":                   {item: reflect.TypeFor[ruleDoc](), named: true},
	"rules.*.labels":          {item: reflect.TypeFor[labelsDoc]()},
	"rules.*.actions":         {item: reflect.TypeFor[actionDoc](), named: true},
	"rules.*.actions.*.check": {},
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
	return w.keys, w.open, nil
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
	if s.item == nil {
		return nil
	}
	return w.fields(s.item, path)
}
