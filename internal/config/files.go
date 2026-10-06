package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The two files crew reads, by their path from the repository's root: the
// shared config, and the local one whose top-level keys replace its keys.
const (
	sharedFile = ".crew/config.yaml"
	localFile  = ".crew/config.local.yaml"
)

// source is one config file that exists: its path from the root, and the
// mapping at its top level, empty when the file holds no YAML.
type source struct {
	name string
	top  *yaml.Node
}

// readSources reads the config files that exist, the shared one first. At
// least one must; neither is an error wrapping fs.ErrNotExist.
func readSources(root string) ([]source, error) {
	var out []source
	for _, name := range []string{sharedFile, localFile} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path) //nolint:gosec // the path is one of the repository's own config files
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read crew config: %w", err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		top, err := topMapping(&doc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, source{name: name, top: top})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("read crew config: %w: neither %s nor %s is in %s "+
			"(create one: see .crew/config.example.yaml in the crew repository)",
			fs.ErrNotExist, sharedFile, localFile, root)
	}
	return out, nil
}

// topMapping returns the mapping at the top of a file's YAML document, or
// an empty one when the file has no content, such as an empty file or
// comments only.
func topMapping(doc *yaml.Node) (*yaml.Node, error) {
	if len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.MappingNode}, nil
	}
	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("line %d: the config must be a mapping of crew's keys, such as "+
			"tracker, agents, checks, board and rules", top.Line)
	}
	return top, nil
}

// refuseOldKeys reports the old keys of every source, each with its file:
// an origin of no keys names every error by all, the one file.
func refuseOldKeys(sources []source) error {
	errs := make([]error, 0, len(sources))
	for _, s := range sources {
		errs = append(errs, origin{all: s.name}.name(oldKeys(s.top)))
	}
	return errors.Join(errs...)
}

// merge builds the mapping of the sources' top-level keys, each set by the
// last source that sets it: the shared file's keys the local file leaves
// out, in file order, then the local file's keys. It returns the mapping
// and where each of its keys came from.
func merge(sources []source) (*yaml.Node, origin) {
	o := origin{files: map[string]string{}}
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.name
		for _, e := range entries(s.top, "") {
			o.files[e.key.Value] = s.name
		}
	}
	o.all = strings.Join(names, " and ")
	top := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, s := range sources {
		for _, e := range entries(s.top, "") {
			if o.files[e.key.Value] == s.name {
				top.Content = append(top.Content, e.key, e.value)
			}
		}
	}
	return top, o
}

// origin tells which file each top-level key of the merged config came
// from, so every error names the file of its key.
type origin struct {
	// files maps each top-level key to the file that set it.
	files map[string]string
	// all names every file read, for an error about no key of any file,
	// such as a section neither file sets.
	all string
}

// fileOf returns the file of the longest top-level key that path is or
// starts, followed by "." or "[". A top-level key may itself hold a dot.
func (o origin) fileOf(path string) string {
	file, longest := o.all, -1
	for key, f := range o.files {
		rest, ok := strings.CutPrefix(path, key)
		if ok && (rest == "" || rest[0] == '.' || rest[0] == '[') && len(key) > longest {
			file, longest = f, len(key)
		}
	}
	return file
}

// name prefixes each error err joins with the file it is about: the file
// of its key for a key error, and every file read otherwise.
func (o origin) name(err error) error {
	if err == nil {
		return nil
	}
	leaves := flatten(err)
	out := make([]error, len(leaves))
	for i, e := range leaves {
		file := o.all
		if k := (*keyPathError)(nil); errors.As(e, &k) {
			file = o.fileOf(k.path)
		}
		out[i] = fmt.Errorf("%s: %w", file, e)
	}
	return errors.Join(out...)
}

// decode returns d with its errors named by their files.
func (o origin) decode(d Decode) Decode {
	return func(target any) error {
		return o.name(d(target))
	}
}

// flatten lists the errors err joins, at any depth, or err alone.
func flatten(err error) []error {
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []error{err}
	}
	var out []error
	for _, e := range joined.Unwrap() {
		out = append(out, flatten(e)...)
	}
	return out
}
