package config

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// checks decodes checks: a mapping from a check's name to its shell script,
// which an action names in its check. Each script is non-empty and is never
// a template: it reads the issue from environment variables. It returns
// every check by name, even one with an error, so an action that names it
// is not reported again, and every error it finds.
func checks(n *yaml.Node) (map[string]string, error) {
	section, err := named(n, "checks")
	errs := []error{err}
	out := make(map[string]string, len(section))
	for _, e := range section {
		var script located[string]
		err := decodeValue(e.value, e.path, reflect.ValueOf(&script).Elem())
		out[e.key.Value] = script.value
		switch {
		case err != nil:
			errs = append(errs, err)
		case strings.TrimSpace(script.value) == "":
			// A blank check would run nothing and pass every time.
			errs = append(errs, keyError(e.path, e.key.Line, "must not be empty"))
		default:
			errs = append(errs, retiredVariables(script.value, e.path, script.line))
		}
	}
	return out, errors.Join(errs...)
}

// retiredVariables reports, at path and line, each environment variable
// crew no longer gives sessions and checks that text, a prompt or a check's
// script, names, with the one that replaced it (KTD14). Without this, a
// prompt or check that still reads one would get it empty without a word.
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
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
