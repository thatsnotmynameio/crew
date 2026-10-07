package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Decode strictly decodes one config section into target, which must be a
// non-nil pointer. It is what adapter factories receive for their section, so
// no adapter imports the YAML library or formats line-numbered errors itself.
//
// Struct targets are matched key by key against their `yaml` tags (or the
// lowercased field name, as the YAML library does), recursing into nested
// structs, pointers to structs and slices of structs. A key no field takes, a
// key given twice, or a value of the wrong type is an error naming the key
// path, such as agents.developer.harness.model, and its line. Keys inside a map field are
// not checked, and `,inline` fields are not supported. A key left out of the
// section leaves its field as the target had it, which is how a factory keeps
// its own defaults. A section absent from the file decodes nothing.
type Decode = func(target any) error

// entry is one key of a mapping, with its full key path. A section is the
// entries of one mapping that crew does not read itself, such as an agent's
// harness without its name.
type entry struct {
	key, value *yaml.Node
	path       string
}

// bind returns the Decode for a section made of entries; section names it in
// errors about the section as a whole.
func bind(section string, entries []entry) Decode {
	return func(target any) error {
		v := reflect.ValueOf(target)
		if v.Kind() != reflect.Pointer || v.IsNil() {
			return fmt.Errorf("%s: decode target must be a non-nil pointer, got %T", section, target)
		}
		v = v.Elem()
		if v.Kind() == reflect.Struct && !isUnmarshaler(v) {
			return decodeFields(entries, v)
		}
		mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, e := range entries {
			mapping.Content = append(mapping.Content, e.key, e.value)
		}
		return decodeLeaf(mapping, section, v)
	}
}

// nullTag is the tag of a YAML value left empty, such as "key:".
const nullTag = "!!null"

// nodesPerEntry is how many nodes each entry takes in a mapping node's
// Content: its key, then its value.
const nodesPerEntry = 2

// entries lists the keys of mapping n, whose own path is path.
func entries(n *yaml.Node, path string) []entry {
	out := make([]entry, 0, len(n.Content)/nodesPerEntry)
	for i := 0; i+1 < len(n.Content); i += nodesPerEntry {
		out = append(out, entry{key: n.Content[i], value: n.Content[i+1], path: join(path, n.Content[i].Value)})
	}
	return out
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// decodeFields decodes each entry into the field of struct v its key names.
func decodeFields(entries []entry, v reflect.Value) error {
	fields := fieldsByKey(v.Type())
	seen := make(map[string]int, len(entries))
	for _, e := range entries {
		name := e.key.Value
		if first, ok := seen[name]; ok {
			return keyError(e.path, e.key.Line, fmt.Sprintf("duplicate key, first set on line %d", first))
		}
		seen[name] = e.key.Line
		index, ok := fields[name]
		if !ok {
			return keyError(e.path, e.key.Line, "unknown key")
		}
		if err := decodeValue(e.value, e.path, v.FieldByIndex(index)); err != nil {
			return err
		}
	}
	return nil
}

// fieldsByKey maps each YAML key of struct type t to its field's index.
func fieldsByKey(t reflect.Type) map[string][]int {
	fields := make(map[string][]int, t.NumField())
	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = strings.ToLower(f.Name)
		}
		fields[name] = f.Index
	}
	return fields
}

// decodeValue decodes n into v, recursing where a mapping meets a struct or
// a sequence meets a slice of structs, so unknown keys are found at any depth.
func decodeValue(n *yaml.Node, path string, v reflect.Value) error {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if n.ShortTag() == nullTag {
		return nil // an empty value is as if the key were left out
	}
	if v.Type() == reflect.TypeFor[yaml.Node]() || isUnmarshaler(v) {
		return decodeLeaf(n, path, v)
	}
	switch {
	case v.Kind() == reflect.Struct && n.Kind == yaml.MappingNode:
		return decodeFields(entries(n, path), v)
	case v.Kind() == reflect.Pointer && v.Type().Elem().Kind() == reflect.Struct && n.Kind == yaml.MappingNode:
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return decodeValue(n, path, v.Elem())
	case v.Kind() == reflect.Slice && n.Kind == yaml.SequenceNode && holdsStructs(v.Type().Elem()):
		return decodeItems(n, path, v)
	}
	return decodeLeaf(n, path, v)
}

// decodeItems decodes sequence n into slice v item by item, so each item's
// path names its index.
func decodeItems(n *yaml.Node, path string, v reflect.Value) error {
	items := reflect.MakeSlice(v.Type(), len(n.Content), len(n.Content))
	for i, item := range n.Content {
		if err := decodeValue(item, fmt.Sprintf("%s[%d]", path, i), items.Index(i)); err != nil {
			return err
		}
	}
	v.Set(items)
	return nil
}

func holdsStructs(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct && t != reflect.TypeFor[yaml.Node]()
}

func isUnmarshaler(v reflect.Value) bool {
	return v.CanAddr() && v.Addr().Type().Implements(reflect.TypeFor[yaml.Unmarshaler]())
}

// decodeLeaf hands n to the YAML library, naming path in its errors.
func decodeLeaf(n *yaml.Node, path string, v reflect.Value) error {
	err := n.Decode(v.Addr().Interface())
	if err == nil {
		return nil
	}
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return keyError(path, n.Line, err.Error())
	}
	// The library prefixes each message with its own line; keep it only when
	// it points somewhere other than n.
	own := fmt.Sprintf("line %d: ", n.Line)
	msgs := make([]string, len(typeErr.Errors))
	for i, msg := range typeErr.Errors {
		msgs[i] = strings.TrimPrefix(msg, own)
	}
	return keyError(path, n.Line, strings.Join(msgs, "; "))
}

// keyError is the one shape of every config error: the key path, its line,
// and what is wrong.
func keyError(path string, line int, msg string) error {
	return &keyPathError{path: path, line: line, msg: msg}
}

// keyPathError is an error about the key at path, which Load names by the file
// the key came from.
type keyPathError struct {
	path string
	line int
	msg  string
}

func (e *keyPathError) Error() string {
	return fmt.Sprintf("%s (line %d): %s", e.path, e.line, e.msg)
}

// named returns the entries of the optional mapping n at path, whose keys
// name its items, such as rules or agents, in file order. A name given
// twice, which a yaml.Node does not refuse, is reported on its line with the
// line of its first, and so is an empty name; both are left out.
func named(n *yaml.Node, path string) ([]entry, error) {
	section, err := mapping(n, path)
	if err != nil {
		return nil, err
	}
	var errs []error
	out := make([]entry, 0, len(section))
	seen := make(map[string]int, len(section))
	for _, e := range section {
		name := e.key.Value
		if first, ok := seen[name]; ok {
			errs = append(errs, keyError(e.path, e.key.Line, fmt.Sprintf("duplicate key, first set on line %d", first)))
			continue
		}
		seen[name] = e.key.Line
		if name == "" {
			errs = append(errs, keyError(path, e.key.Line, "a name must not be empty"))
			continue
		}
		out = append(out, e)
	}
	return out, errors.Join(errs...)
}

// decodeItem decodes the named item n at path into the struct target points
// to. An item that is not a mapping is reported with shape, what it must be.
func decodeItem(n *yaml.Node, path, shape string, target any) error {
	if n.Kind != yaml.MappingNode {
		return keyError(path, n.Line, shape)
	}
	return decodeFields(entries(n, path), reflect.ValueOf(target).Elem())
}
