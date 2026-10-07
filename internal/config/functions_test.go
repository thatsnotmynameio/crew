package config_test

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// prVerdicts are the verdicts the invented function pull-request declares.
var prVerdicts = []crew.Verdict{"merged", "open"}

// testFunctions is an invented catalog of one function, pull-request, for
// the tests: crew registers none.
func testFunctions() map[string][]crew.Verdict {
	return map[string][]crew.Verdict{"pull-request": prVerdicts}
}

// prParameters are the parameters the tests decode a pull-request use's
// into.
type prParameters struct {
	State string `yaml:"state"`
	Title string `yaml:"title"`
	Draft bool   `yaml:"draft"`
	Count int    `yaml:"count"`
}

// presetHead declares one preset, open-pr, of pull-request, with two
// parameters and resume, on lines 1 to 6; sequenceRule's rules key follows
// on line 7, and its first item is on line 11.
const presetHead = `actions:
  open-pr:
    name: pull-request
    state: open
    title: preset title
    resume: self
`

// presetOf declares one preset, open-pr, of pull-request, whose line 4 is
// line.
func presetOf(line string) string {
	return "actions:\n  open-pr:\n    name: pull-request\n    " + line + "\n"
}

// loadFunctions loads body as .crew/config.yaml with testFunctions
// registered.
func loadFunctions(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, err := config.Load(writeRoot(t, body), "", testFunctions())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// textParameters returns the text parameters of name and value pairs, in
// order, parsed as the config parses them.
func textParameters(t *testing.T, pairs ...string) []crew.TextParameter {
	t.Helper()
	var out []crew.TextParameter
	for i := 0; i+1 < len(pairs); i += 2 {
		tmpl, err := crew.ParseParameterTemplate(pairs[i], pairs[i+1])
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, crew.TextParameter{Name: pairs[i], Template: tmpl})
	}
	return out
}

// useOf returns cfg's function use at key, and fails t without one.
func useOf(t *testing.T, cfg *config.Config, key crew.FunctionUse) config.FunctionUse {
	t.Helper()
	for _, u := range cfg.Functions {
		if u.Use == key {
			return u
		}
	}
	t.Fatalf("no function use %s among %d", key, len(cfg.Functions))
	return config.FunctionUse{}
}

// decodePR decodes d into prParameters, failing t on an error.
func decodePR(t *testing.T, d config.Decode) prParameters {
	t.Helper()
	var p prParameters
	if err := d(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return p
}

// R3, R26, R29, KTD-F7: an item that names a function with parameters is a
// function action, named after the function, with its text parameters as
// templates, its declared verdicts and its key path as its use; its use
// decodes its parameters, the text rendered for the sample issue or for
// the texts given.
func TestLoadReadsAFunctionActionWithItsParameters(t *testing.T) {
	items := `      - pull-request:
          state: open
          title: "Fixes {{.Issue.Ref}}"
          draft: true
          count: 3
        on: {merged: next}
`
	cfg := loadFunctions(t, sequenceRule("", items, ""))
	a := cfg.Rules[0].Actions[0]
	want := crew.FunctionSpec{
		Function: "pull-request", Use: "rules.implement.actions[0]",
		Texts:    textParameters(t, "state", "open", "title", "Fixes {{.Issue.Ref}}"),
		Verdicts: prVerdicts,
	}
	if a.Name != "pull-request" || !reflect.DeepEqual(a.Kind, want) {
		t.Errorf("action %s runs %+v, want pull-request running %+v", a.Name, a.Kind, want)
	}
	if len(cfg.Functions) != 1 {
		t.Fatalf("Functions = %d uses, want 1", len(cfg.Functions))
	}
	use := cfg.Functions[0]
	if use.Use != "rules.implement.actions[0]" || use.Function != "pull-request" {
		t.Errorf("use %s of %s, want rules.implement.actions[0] of pull-request", use.Use, use.Function)
	}
	if got, want := decodePR(t, use.Section), (prParameters{"open", "Fixes #42", true, 3}); got != want {
		t.Errorf("Section = %+v, want %+v", got, want)
	}
	bound := use.Bind(map[string]string{"state": "closed", "title": "Fixes #7"})
	if got, want := decodePR(t, bound), (prParameters{"closed", "Fixes #7", true, 3}); got != want {
		t.Errorf("Bind = %+v, want %+v", got, want)
	}
}

// R26: a string that names a function calls it with no parameters.
func TestLoadReadsAFunctionNamedAlone(t *testing.T) {
	cfg := loadFunctions(t, sequenceRule("", "      - pull-request\n", ""))
	want := crew.FunctionSpec{Function: "pull-request", Use: "rules.implement.actions[0]", Verdicts: prVerdicts}
	if a := cfg.Rules[0].Actions[0]; a.Name != "pull-request" || !reflect.DeepEqual(a.Kind, want) {
		t.Errorf("action %s runs %+v, want pull-request running %+v", a.Name, a.Kind, want)
	}
	if got := decodePR(t, useOf(t, cfg, "rules.implement.actions[0]").Section); got != (prParameters{}) {
		t.Errorf("Section = %+v, want nothing decoded", got)
	}
}

// R5, R14, KTD-F8, KTD-F9: a preset of actions names its function and its
// parameters, which a use replaces key by key, keeping the keys only one
// side writes; a use is named after the preset unless its name says
// otherwise, keeps the preset's resume, and a route's step that names a
// function or a preset is a function step.
func TestAUsesParametersReplaceItsPresetsKeyByKey(t *testing.T) {
	items := `      - open-pr:
          title: "Fixes {{.Issue.Ref}}"
          draft: true
        on: {merged: merged}
      - open-pr: {count: 2}
        name: second
`
	extra := `      merged:
        - open-pr
        - pull-request: {state: merged}
        - move: done
`
	cfg := loadFunctions(t, sequenceRule(presetHead, items, extra))
	r := cfg.Rules[0]
	first := crew.FunctionSpec{
		Function: "pull-request", Use: "rules.implement.actions[0]",
		Texts:    textParameters(t, "state", "open", "title", "Fixes {{.Issue.Ref}}"),
		Verdicts: prVerdicts, ResumeSelf: true,
	}
	if a := r.Actions[0]; a.Name != "open-pr" || !reflect.DeepEqual(a.Kind, first) {
		t.Errorf("action %s runs %+v, want open-pr running %+v", a.Name, a.Kind, first)
	}
	if a := r.Actions[1]; a.Name != "second" {
		t.Errorf("second action is %s, want second", a.Name)
	}
	if got, want := decodePR(t, useOf(t, cfg, "rules.implement.actions[0]").Section),
		(prParameters{"open", "Fixes #42", true, 0}); got != want {
		t.Errorf("first use's Section = %+v, want %+v", got, want)
	}
	if got, want := decodePR(t, useOf(t, cfg, "rules.implement.actions[1]").Section),
		(prParameters{"open", "preset title", false, 2}); got != want {
		t.Errorf("second use's Section = %+v, want %+v", got, want)
	}
	if len(cfg.Functions) != 4 {
		t.Errorf("Functions = %d uses, want 4: two actions and two steps", len(cfg.Functions))
	}
	stepsCallFunctions(t, r.Routes[2].Steps)
}

// stepsCallFunctions checks the steps of the route merged of
// TestAUsesParametersReplaceItsPresetsKeyByKey: the preset, then the
// function by its own name, then a move.
func stepsCallFunctions(t *testing.T, steps []crew.Step) {
	t.Helper()
	wantSteps := []crew.Step{
		crew.FunctionStep{Name: "open-pr", Function: crew.FunctionSpec{
			Function: "pull-request", Use: "rules.implement.routes.merged[0]",
			Texts:    textParameters(t, "state", "open", "title", "preset title"),
			Verdicts: prVerdicts, ResumeSelf: true,
		}},
		crew.FunctionStep{Name: "pull-request", Function: crew.FunctionSpec{
			Function: "pull-request", Use: "rules.implement.routes.merged[1]",
			Texts: textParameters(t, "state", "merged"), Verdicts: prVerdicts,
		}},
		crew.MoveStep{To: "done"},
	}
	if !reflect.DeepEqual(steps, wantSteps) {
		t.Errorf("route merged = %+v\nwant %+v", steps, wantSteps)
	}
}

// Covers AE9 (R28, KTD-F9): a parameter the function does not take fails
// its decode naming its file, its key path and its line, the use's own in
// .crew/config.yaml and the preset's in the global file.
func TestAnUnknownParameterNamesItsFileKeyPathAndLine(t *testing.T) {
	var target struct {
		State string `yaml:"state"`
	}
	cfg := loadFunctions(t, sequenceRule("", "      - pull-request: {state: open, colour: red}\n", ""))
	err := useOf(t, cfg, "rules.implement.actions[0]").Section(&target)
	want := ".crew/config.yaml: rules.implement.actions[0].pull-request.colour (line 5): unknown key"
	if err == nil || err.Error() != want {
		t.Errorf("Section = %v, want %s", err, want)
	}
	global := writeGlobal(t, "actions:\n  open-pr:\n    name: pull-request\n    colour: red\n")
	shared := sequenceRule("", "      - open-pr: {state: open}\n", "")
	cfg, err = config.Load(writeFiles(t, shared, noFile), global, testFunctions())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	err = useOf(t, cfg, "rules.implement.actions[0]").Section(&target)
	want = global + ": actions.open-pr.colour (line 4): unknown key"
	if err == nil || err.Error() != want {
		t.Errorf("Section = %v, want %s", err, want)
	}
}

// KTD-F11: a function's refusal of a parameter names the parameter's file,
// key path and line, or the use's when the use does not write it; any
// other build error names the use's.
func TestAUseNamesTheErrorsOfItsBuild(t *testing.T) {
	cfg := loadFunctions(t, sequenceRule("", "      - pull-request:\n          state: shut\n", ""))
	use := useOf(t, cfg, "rules.implement.actions[0]")
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a written parameter refused",
			err:  use.Refused("state", "must be open or merged"),
			want: ".crew/config.yaml: rules.implement.actions[0].pull-request.state (line 6): must be open or merged",
		},
		{
			name: "a parameter not written refused",
			err:  use.Refused("base", "required"),
			want: `.crew/config.yaml: rules.implement.actions[0] (line 5): parameter "base": required`,
		},
		{
			name: "another error",
			err:  use.Failed(errors.New("function pull-request: no token")),
			want: ".crew/config.yaml: rules.implement.actions[0] (line 5): function pull-request: no token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil || tt.err.Error() != tt.want {
				t.Errorf("error = %v, want %s", tt.err, tt.want)
			}
		})
	}
}

// KTD-F7: each Decode Bind returns fills nodes of its own, so one call's
// texts never reach another's, even at the same time.
func TestBindGivesEachCallItsOwnTexts(t *testing.T) {
	cfg := loadFunctions(t, sequenceRule(presetHead, "      - open-pr\n", ""))
	use := useOf(t, cfg, "rules.implement.actions[0]")
	titles := []string{"one", "two", "three", "four"}
	decodes := make([]config.Decode, len(titles))
	for i, title := range titles {
		decodes[i] = use.Bind(map[string]string{"state": "open", "title": title})
	}
	got := make([]string, len(titles))
	var wg sync.WaitGroup
	for i := range titles {
		wg.Go(func() {
			var p prParameters
			if err := decodes[i](&p); err == nil {
				got[i] = p.Title
			}
		})
	}
	wg.Wait()
	if !reflect.DeepEqual(got, titles) {
		t.Errorf("titles = %q, want %q", got, titles)
	}
}

// R28, R29, R32, KTD-F8: what a function's presets and uses cannot be. Each
// body's rules key is on the line after its head's last.
var invalidFunctions = []rejectCase{
	{
		name: "a parameter that is a list",
		body: sequenceRule("", "      - pull-request: {labels: [a, b]}\n", ""),
		wants: []string{
			"rules.implement.actions[0].pull-request.labels", "line 5", "a function's parameter is text, a number or a boolean",
		},
	},
	{
		name:  "a preset's parameter that is a mapping",
		body:  sequenceRule(presetOf("filter: {state: open}"), "      - open-pr\n", ""),
		wants: []string{"actions.open-pr.filter", "line 4", "a function's parameter is text, a number or a boolean"},
	},
	{
		name:  "a text parameter that names an unknown issue field",
		body:  sequenceRule("", "      - pull-request: {title: \"{{.Issue.Number}}\"}\n", ""),
		wants: []string{"rules.implement.actions[0].pull-request.title", "line 5", `parameter "title"`, "Number"},
	},
	{
		name:  "a text parameter that does not parse",
		body:  sequenceRule("", "      - pull-request: {title: \"{{.Issue.Ref\"}\n", ""),
		wants: []string{"rules.implement.actions[0].pull-request.title", "line 5", `parse parameter "title"`},
	},
	{
		name:  "a parameter written twice",
		body:  sequenceRule("", "      - pull-request: {state: open, state: merged}\n", ""),
		wants: []string{"rules.implement.actions[0].pull-request.state", "line 5", "duplicate key"},
	},
	{
		name:  "a shell action named like a function",
		body:  sequenceRule("actions:\n  pull-request: gh pr list\n", "      - pull-request\n", ""),
		wants: []string{".crew/config.yaml: actions.pull-request (line 2)", "is the name of a registered function"},
	},
	{
		name:  "a preset named like a function",
		body:  sequenceRule("actions:\n  pull-request:\n    name: pull-request\n", "      - pull-request\n", ""),
		wants: []string{".crew/config.yaml: actions.pull-request (line 2)", "is the name of a registered function"},
	},
	{
		name: "a preset of a function not registered",
		body: sequenceRule("actions:\n  open-pr:\n    name: merge-request\n", "      - open-pr\n", ""),
		wants: []string{
			"actions.open-pr.name", "line 3", `no function is named "merge-request"; the registered functions are pull-request`,
		},
	},
	{
		name:  "a preset whose name is empty",
		body:  sequenceRule("actions:\n  open-pr:\n    name:\n    state: open\n", "      - open-pr\n", ""),
		wants: []string{"actions.open-pr.name", "line 3", "required"},
	},
	{
		name:  "a preset with a script",
		body:  sequenceRule(presetOf("script: gh pr create"), "      - open-pr\n", ""),
		wants: []string{"actions.open-pr", "line 3", "must be a shell script, or a mapping with script"},
	},
	{
		name:  "a preset's resume that is not self",
		body:  sequenceRule(presetOf("resume: always"), "      - open-pr\n", ""),
		wants: []string{"actions.open-pr.resume", "line 4", `"always" must be self`},
	},
	{
		name:  "a shell reference with parameters",
		body:  sequenceRule("actions:\n  lint: make lint\n", "      - lint: {fix: true}\n", ""),
		wants: []string{"rules.implement.actions[0].lint", "line 7", "a shell action takes no parameters"},
	},
	{
		name:  "a function reference whose parameters are a list",
		body:  sequenceRule("", "      - pull-request: [open]\n", ""),
		wants: []string{"rules.implement.actions[0].pull-request", "line 5", "a function's parameters are a mapping"},
	},
	{
		name: "a step's parameter that is a list",
		body: sequenceRule("", "      - pull-request\n",
			"      merged:\n        - pull-request: {state: [a]}\n        - close\n"),
		wants: []string{"rules.implement.routes.merged[0].pull-request.state", "line 10", "a function's parameter is text"},
	},
	{
		name:  "a route that ends with a function step",
		body:  sequenceRule("", "      - pull-request:\n        on: {merged: merged}\n", "      merged: [pull-request]\n"),
		wants: []string{"rules.implement.routes.merged", "must end with move or close"},
	},
	{
		name: "a name that is neither an action nor a function",
		body: sequenceRule("", "      - vet\n", ""),
		wants: []string{
			"rules.implement.actions[0]", "line 5",
			`action "vet" does not exist; actions declares none; the registered functions are pull-request`,
		},
	},
}

func TestLoadRejectsInvalidFunctions(t *testing.T) {
	for _, tt := range invalidFunctions {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := config.Load(writeRoot(t, tt.body), "", testFunctions())
			if err == nil {
				t.Fatalf("Load succeeded with %+v, want an error", cfg)
			}
			for _, want := range tt.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// KTD-F3: without a registered function, a preset says none is, and a
// config that calls none loads as before.
func TestWithoutFunctionsAPresetSaysNoneIsRegistered(t *testing.T) {
	loadErr(t, sequenceRule("actions:\n  open-pr:\n    name: pull-request\n", "      - open-pr\n", ""),
		"actions.open-pr.name", "line 3", `no function is named "pull-request"; the registered functions are none`)
	if cfg := load(t, oneRule); len(cfg.Functions) != 0 {
		t.Errorf("Functions = %d uses, want none", len(cfg.Functions))
	}
}
