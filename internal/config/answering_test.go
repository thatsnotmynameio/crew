package config_test

import (
	"reflect"
	"testing"
)

// R38, KTD-W4: answering_apps is the answering list as written, and
// whether it was written, since without it crew's bots answer and a
// written list, [] too, replaces them.
func TestLoadReadsTheAnsweringApps(t *testing.T) {
	tests := []struct {
		name        string
		head        string
		want        []string
		wantWritten bool
	}{
		{name: "left out"},
		{name: "empty", head: "answering_apps:\n", want: nil},
		{name: "one App", head: "answering_apps: [\"claude[bot]\"]\n", want: []string{"claude[bot]"}, wantWritten: true},
		{name: "no App", head: "answering_apps: []\n", want: []string{}, wantWritten: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := load(t, tt.head+oneRule)
			if !reflect.DeepEqual(cfg.AnsweringApps, tt.want) || cfg.AnsweringAppsWritten != tt.wantWritten {
				t.Errorf("AnsweringApps = %#v (written %v), want %#v (written %v)",
					cfg.AnsweringApps, cfg.AnsweringAppsWritten, tt.want, tt.wantWritten)
			}
		})
	}
}

func TestLoadRejectsInvalidAnsweringApps(t *testing.T) {
	testRejects(t, invalidAnsweringApps)
}

// invalidAnsweringApps are errors in answering_apps, each named by its
// file, key path and line (R39).
var invalidAnsweringApps = []rejectCase{
	{
		name:  "github-actions[bot]",
		body:  "answering_apps: [\"github-actions[bot]\"]\n" + oneRule,
		wants: []string{sharedName, "answering_apps[0]", "line 1", "github-actions[bot] never answers"},
	},
	{
		name:  "github-actions[bot] in another case",
		body:  "answering_apps:\n  - claude[bot]\n  - GitHub-Actions[bot]\n" + oneRule,
		wants: []string{sharedName, "answering_apps[1]", "line 3", "github-actions[bot] never answers"},
	},
	{
		name:  "a login without [bot]",
		body:  "answering_apps: [octocat]\n" + oneRule,
		wants: []string{sharedName, "answering_apps[0]", "line 1", `"octocat" is not an App's login`},
	},
	{
		name:  "[bot] alone",
		body:  "answering_apps: [\"[bot]\"]\n" + oneRule,
		wants: []string{"answering_apps[0]", "line 1", `"[bot]" is not an App's login`},
	},
	{
		name:  "an item that is a mapping",
		body:  "answering_apps: [{login: \"claude[bot]\"}]\n" + oneRule,
		wants: []string{"answering_apps[0]", "line 1", "must be an App's login"},
	},
	{
		name:  "a single login",
		body:  "answering_apps: claude[bot]\n" + oneRule,
		wants: []string{"answering_apps", "line 1", "must be a list of App logins"},
	},
}
