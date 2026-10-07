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

// shellDoc is a shell action of actions written as a mapping.
type shellDoc struct {
	Script   located[string] `yaml:"script"`
	Verdicts yaml.Node       `yaml:"verdicts"`
	Resume   located[string] `yaml:"resume"`
}

// shellShape is what a shell action of actions must be, said when it is
// neither.
const shellShape = "must be a shell script, or a mapping with script, and optionally verdicts and resume"

// resumeSelf is the one value of a shell action's resume: a resume that
// restarts at the action starts at the action itself.
const resumeSelf = "self"

// reservedNames are the words of the rules' grammar, which no action of
// actions may be named, so that a rule's items and a route's steps read one
// way only (KTD-S16).
func reservedNames() []string {
	return []string{
		"agent", "prompt", nameKey, onKey, "wait", "resume", "script", "verdicts",
		reportWord, closeWord, moveWord, commentWord, "next",
	}
}

// shells decodes actions: a mapping from a shell action's name to its
// script, or to a mapping with its script, the verdicts its exit statuses
// give and where a resume starts. Each script is non-empty and is never a
// template: it reads the issue from environment variables. It returns every
// action by name, even one with an error, so a rule that names it is not
// reported again, and every error it finds.
func shells(n *yaml.Node) (map[crew.ActionName]crew.ShellSpec, error) {
	section, err := named(n, "actions")
	errs := append(make([]error, 0, len(section)+1), err)
	out := make(map[crew.ActionName]crew.ShellSpec, len(section))
	for _, e := range section {
		spec, err := parseShell(e)
		out[crew.ActionName(e.key.Value)] = spec
		errs = append(errs, err)
	}
	return out, errors.Join(errs...)
}

// parseShell decodes the shell action e.
func parseShell(e entry) (crew.ShellSpec, error) {
	if slices.Contains(reservedNames(), e.key.Value) {
		return crew.ShellSpec{}, keyError(e.path, e.key.Line, fmt.Sprintf(
			"%q is a word of the rules' grammar, so it cannot name an action", e.key.Value))
	}
	value := e.value
	if value.Kind == yaml.AliasNode {
		value = value.Alias
	}
	var doc shellDoc
	var scriptErr error
	switch value.Kind {
	case yaml.ScalarNode:
		if err := decodeValue(value, e.path, reflect.ValueOf(&doc.Script).Elem()); err != nil {
			return crew.ShellSpec{}, err
		}
		scriptErr = checkScript(doc.Script, e.path, value.Line)
	case yaml.MappingNode:
		if err := decodeItem(value, e.path, shellShape, &doc); err != nil {
			return crew.ShellSpec{}, err
		}
		if doc.Script.line == 0 {
			scriptErr = keyError(e.path+".script", value.Line, "required")
		} else {
			scriptErr = checkScript(doc.Script, e.path+".script", value.Line)
		}
	default:
		return crew.ShellSpec{}, keyError(e.path, value.Line, shellShape)
	}
	spec := crew.ShellSpec{Script: doc.Script.value, ResumeSelf: doc.Resume.value == resumeSelf}
	var verdictsErr, resumeErr error
	spec.Verdicts, verdictsErr = exitVerdicts(&doc.Verdicts, e.path+".verdicts")
	if doc.Resume.line != 0 && !spec.ResumeSelf {
		resumeErr = keyError(e.path+".resume", doc.Resume.line, fmt.Sprintf(
			"%q must be %s, the only start a shell action can choose", doc.Resume.value, resumeSelf))
	}
	return spec, errors.Join(scriptErr, verdictsErr, resumeErr)
}

// checkScript reports what is wrong with the script l at path, on its line,
// or on line when it has none.
func checkScript(l located[string], path string, line int) error {
	if l.line != 0 {
		line = l.line
	}
	if strings.TrimSpace(l.value) == "" {
		// A blank script would run nothing and pass every time.
		return keyError(path, line, "must not be empty")
	}
	return retiredVariables(l.value, path, line)
}

// maxExitStatus is the highest exit status a process can return.
const maxExitStatus = 255

// exitVerdicts decodes the optional verdicts n of a shell action at path: a
// mapping from an exit status to the verdict it gives.
func exitVerdicts(n *yaml.Node, path string) (map[int]crew.Verdict, error) {
	section, err := named(n, path)
	if err != nil || len(section) == 0 {
		return nil, err
	}
	out := make(map[int]crew.Verdict, len(section))
	var errs []error
	for _, e := range section {
		var code int
		if err := e.key.Decode(&code); err != nil || code < 0 || code > maxExitStatus {
			errs = append(errs, keyError(e.path, e.key.Line, fmt.Sprintf("must be an exit status, from 0 to %d", maxExitStatus)))
			continue
		}
		v, err := verdictName(e.value, e.path)
		errs = append(errs, err)
		out[code] = v
	}
	return out, errors.Join(errs...)
}

// verdictName returns the verdict the scalar n names, at path.
func verdictName(n *yaml.Node, path string) (crew.Verdict, error) {
	if n.Kind != yaml.ScalarNode {
		return "", keyError(path, n.Line, "must be a verdict's name")
	}
	v, err := crew.ParseVerdict(n.Value)
	if err != nil {
		return "", keyError(path, n.Line, err.Error())
	}
	return v, nil
}

// retiredVariables reports, at path and line, each environment variable
// crew no longer gives sessions and shell actions that text, a prompt or a
// shell action's script, names, with the one that replaced it (KTD14).
// Without this, a prompt or script that still reads one would get it empty
// without a word.
func retiredVariables(text, path string, line int) error {
	var found []string
	for _, v := range []struct{ old, now string }{{"CREW_BOSS", "CREW_CODE_OWNERS"}, {"CREW_MATES", "CREW_BOTS"}} {
		if strings.Contains(text, v.old) {
			found = append(found, fmt.Sprintf("%s is now %s", v.old, v.now))
		}
	}
	if len(found) == 0 {
		return nil
	}
	return keyError(path, line, "names a variable crew no longer sets: "+strings.Join(found, "; "))
}

// sortedKeys returns m's keys in order, for an error that lists them.
func sortedKeys[K ~string, V any](m map[K]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	slices.Sort(keys)
	return keys
}
