package registry_test

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

// section returns a strict port.Decode over body, a YAML mapping, as the
// config hands a factory its section.
func section(body string) port.Decode {
	return func(target any) error {
		dec := yaml.NewDecoder(strings.NewReader(body))
		dec.KnownFields(true)
		return dec.Decode(target)
	}
}

// The key path of a function use in these tests.
const useKey = "rules.implement.actions[1].function"

func TestRegisteredFunctionIsBuiltFromItsSection(t *testing.T) {
	f := fake.NewFunction()
	r := registry.New(nil, nil, map[string]port.FunctionDefinition{
		"open-pull-request": fake.FunctionDefinition(f, crew.Passed),
	})

	got, err := r.Function(useKey, "open-pull-request", section("{title: Fix it, count: 2, draft: true}"))
	if err != nil {
		t.Fatalf("Function: %v", err)
	}
	if got != port.Function(f) {
		t.Errorf("Function = %v, want the registered fake", got)
	}
	want := []fake.FunctionSettings{{Title: "Fix it", Count: 2, Draft: true}}
	if builds := f.Builds(); !slices.Equal(builds, want) {
		t.Errorf("the factory decoded %+v, want %+v", builds, want)
	}
}

func TestUnregisteredFunctionNamesTheKeyAndTheRegisteredFunctionsSorted(t *testing.T) {
	r := registry.New(nil, nil, map[string]port.FunctionDefinition{
		"open-pull-request": fake.FunctionDefinition(fake.NewFunction()),
		"check-ci":          fake.FunctionDefinition(fake.NewFunction()),
	})

	got, err := r.Function(useKey, "nosuch", section("{}"))
	assertErr(t, err, useKey, `no function is named "nosuch"`, "the registered functions are: check-ci, open-pull-request")
	if got != nil {
		t.Errorf("Function = %v, want none", got)
	}
}

func TestRegistryWithoutFunctionsSaysNoneIsRegistered(t *testing.T) {
	_, err := registry.Registry{}.Function(useKey, "check-ci", section("{}"))
	assertErr(t, err, useKey, `"check-ci"`, "the registered functions are: none")
}

// KTD-F11: a refusal stays a port.RefusedParameterError through the registry's
// wrapping, so the app can name the parameter's line.
func TestAFactorysRefusedParameterStaysReachable(t *testing.T) {
	f := fake.NewFunction()
	f.Refuse(port.RefusedParameterError{Parameter: "count", Reason: "must be at least 1"})
	r := registry.New(nil, nil, map[string]port.FunctionDefinition{"check-ci": fake.FunctionDefinition(f)})

	_, err := r.Function(useKey, "check-ci", section("{count: 0}"))
	assertErr(t, err, "function check-ci", "count", "must be at least 1")
	var refused port.RefusedParameterError
	if !errors.As(err, &refused) {
		t.Fatalf("error %v holds no port.RefusedParameterError", err)
	}
	if want := (port.RefusedParameterError{Parameter: "count", Reason: "must be at least 1"}); refused != want {
		t.Errorf("refused %+v, want %+v", refused, want)
	}
}

func TestAFactorysDecodeErrorIsWrappedWithTheFunctionsName(t *testing.T) {
	functions := map[string]port.FunctionDefinition{"check-ci": fake.FunctionDefinition(fake.NewFunction())}
	r := registry.New(nil, nil, functions)

	_, err := r.Function(useKey, "check-ci", section("{titel: Fix it}"))
	assertErr(t, err, "function check-ci", "titel")
}

func TestFunctionsListsEachFunctionWithItsDeclaredVerdicts(t *testing.T) {
	r := registry.New(nil, nil, map[string]port.FunctionDefinition{
		"check-ci":          fake.FunctionDefinition(fake.NewFunction(), crew.Passed, "blocked"),
		"open-pull-request": fake.FunctionDefinition(fake.NewFunction()),
	})

	got := r.Functions()
	want := map[string][]crew.Verdict{"check-ci": {crew.Passed, "blocked"}, "open-pull-request": nil}
	if !maps.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Functions = %v, want %v", got, want)
	}
	got["check-ci"][0] = "changed"
	if again := r.Functions(); again["check-ci"][0] != crew.Passed {
		t.Errorf("changing the catalog changed the registry's: %v", again)
	}
}
