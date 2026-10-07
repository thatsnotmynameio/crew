package config_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// translation holds a full config, a translation team's, invented for the
// tests, so they pin crew's behaviour and never a real repository's setup.
const translation = "testdata/translation"

// ruleSummary is how a rule loads, its actions summed up as "session S:
// agent A, bot B" or "shell S", each followed by its on as "; verdict to
// target", and its routes as "route: step, step".
type ruleSummary struct {
	name    crew.RuleName
	labels  crew.Labels
	queue   crew.Queue
	notify  bool
	actions []string
	routes  []string
}

// A full config loads its rules with their labels, queues, notify, actions
// and routes: each session with its agent and its bot, each shell action by
// name, each action's on, each route's steps in order; its tracker's bot and
// every agent's; its board, one column per rule with actions when it sets
// none; its shell actions' scripts, verdicts and resume; and its prompts as
// they are written.
func TestAFullConfigLoads(t *testing.T) {
	cfg, err := config.Load(translation, "")
	if err != nil {
		t.Fatalf("Load(%s) = %v", translation, err)
	}
	if cfg.PollInterval != 2*time.Minute || cfg.MaxParallelIssues != 4 {
		t.Errorf("poll %v, parallel %d; want 2m0s, 4", cfg.PollInterval, cfg.MaxParallelIssues)
	}
	if got, want := summarize(cfg.Rules, cfg.Notify), wantTranslationRules(); !reflect.DeepEqual(got, want) {
		t.Errorf("rules = %+v\nwant %+v", got, want)
	}
	wantBots := []crew.BotName{"concierge", "linguist"}
	if cfg.Bot.Name != "concierge" || !reflect.DeepEqual(cfg.BotNames(), wantBots) {
		t.Errorf("Bot = %q, Bots = %q; want concierge, %q", cfg.Bot, cfg.BotNames(), wantBots)
	}
	columns := make([]string, 0, len(cfg.Board))
	for _, c := range cfg.Board {
		columns = append(columns, c.Name)
	}
	if want := []string{"translation", "proofreading"}; !reflect.DeepEqual(columns, want) || cfg.BoardWritten {
		t.Errorf("board columns = %q (written %v), want %q", columns, cfg.BoardWritten, want)
	}
	translationRule := cfg.Rules[1]
	pushed, ok := translationRule.Action("draft-pushed").Kind.(crew.ShellSpec)
	wantPushed := crew.ShellSpec{
		Script: "n=$(git rev-list --count \"origin/$CREW_BRANCH\" 2>/dev/null) || n=0\n" +
			`[ "$n" -gt 0 ] || { echo "nothing pushed to $CREW_BRANCH"; exit 3; }`,
		Verdicts:   map[int]crew.Verdict{3: "unpushed"},
		ResumeSelf: true,
	}
	if !ok || !reflect.DeepEqual(pushed, wantPushed) {
		t.Errorf("draft-pushed = %+v, want %+v", pushed, wantPushed)
	}
	if kept := shellScript(t, translationRule, "glossary-kept"); kept != "grep -rq glossary docs/" {
		t.Errorf("glossary-kept = %q, want the string it is defined as", kept)
	}
	wantPrompt := "Translate the page {{.Issue.Ref}} names, \"{{.Issue.Title}}\", into Portuguese.\n\n" +
		"Keep every term the glossary lists, and push the draft to a branch."
	if got := sessionNamed(t, translationRule, "draft").Prompt.Text(); got != wantPrompt {
		t.Errorf("draft's prompt = %q, want %q", got, wantPrompt)
	}
}

// wantTranslationRules are the rules testdata/translation's config loads
// into, in file order.
func wantTranslationRules() []ruleSummary {
	return []ruleSummary{
		{
			name: "accept request", queue: crew.Queue{Name: "desk", Slots: 1},
			labels: crew.Labels{Ready: "request:new", Running: "request:accepting"},
			routes: []string{"passed: move translation:to do"},
		},
		{
			name: "translation", queue: crew.Queue{Name: "translators", Slots: 2}, notify: true,
			labels: crew.Labels{Ready: "translation:to do", Running: "translation:drafting"},
			actions: []string{
				"session draft: agent translator, bot linguist",
				"shell glossary-kept",
				"shell draft-pushed; unpushed to unpushed",
			},
			routes: []string{
				"passed: move translation:drafted",
				"failed: report, move translation:stuck",
				"unpushed: comment {{.Issue.Ref}}: `{{.Action}}` found no draft pushed. Its log is {{.Log}}., " +
					"move translation:stuck",
			},
		},
		{
			name: "proofreading", queue: crew.Queue{Name: "default", Slots: 1},
			labels: crew.Labels{Ready: "translation:drafted", Running: "translation:proofreading"},
			actions: []string{
				"session proofread: agent proofreader, bot concierge; failed to rejected",
				"shell glossary-kept",
				"session translator: agent translator, bot linguist",
			},
			routes: []string{
				"passed: move translation:published",
				"failed: report, move translation:rejected",
				"rejected: comment {{.Issue.Ref}} was rejected in {{.Rule}}., glossary-kept, close",
			},
		},
		{
			name: "archive", queue: crew.Queue{Name: "default", Slots: 1}, notify: true,
			labels: crew.Labels{Ready: "translation:published", Running: "translation:archiving"},
			routes: []string{"passed: move translation:archived"},
		},
	}
}

// sessionNamed returns the session of r called name, and fails t without one.
func sessionNamed(t *testing.T, r crew.Rule, name crew.ActionName) crew.SessionSpec {
	t.Helper()
	s, ok := r.Action(name).Kind.(crew.SessionSpec)
	if !ok {
		t.Fatalf("rule %s has no session %s", r.Name, name)
	}
	return s
}

// shellScript returns the script of the shell action of r called name, and
// fails t without one.
func shellScript(t *testing.T, r crew.Rule, name crew.ActionName) string {
	t.Helper()
	s, ok := r.Action(name).Kind.(crew.ShellSpec)
	if !ok {
		t.Fatalf("rule %s has no shell action %s", r.Name, name)
	}
	return s.Script
}

// summarize sums rules up as ruleSummary, with each rule's notify.
func summarize(rules []crew.Rule, notify map[crew.RuleName]bool) []ruleSummary {
	out := make([]ruleSummary, len(rules))
	for i, r := range rules {
		out[i] = ruleSummary{name: r.Name, labels: r.Labels, queue: r.Queue, notify: notify[r.Name]}
		for _, a := range r.Actions {
			var action string
			switch k := a.Kind.(type) {
			case crew.SessionSpec:
				action = fmt.Sprintf("session %s: agent %s, bot %s", a.Name, k.Agent.Name, k.Bot.Name)
			case crew.ShellSpec:
				action = fmt.Sprintf("shell %s", a.Name)
			}
			if on := onNames(a.On); on != "" {
				action += "; " + on
			}
			out[i].actions = append(out[i].actions, action)
		}
		for _, route := range r.Routes {
			out[i].routes = append(out[i].routes, fmt.Sprintf("%s: %s", route.Name, stepNames(route.Steps)))
		}
	}
	return out
}

// stepNames sums steps up, as "report, move L".
func stepNames(steps []crew.Step) string {
	names := make([]string, len(steps))
	for i, step := range steps {
		switch s := step.(type) {
		case crew.MoveStep:
			names[i] = "move " + string(s.To)
		case crew.ReportStep:
			names[i] = "report"
		case crew.CloseStep:
			names[i] = "close"
		case crew.CommentStep:
			names[i] = "comment " + s.Template.Text()
		case crew.ShellStep:
			names[i] = string(s.Name)
		}
	}
	return strings.Join(names, ", ")
}

// onNames sums on up, as "verdict to target", sorted by verdict.
func onNames(on crew.On) string {
	names := make([]string, 0, len(on))
	for v, target := range on {
		to := "next"
		if r, ok := target.(crew.ToRoute); ok {
			to = string(r.Route)
		}
		names = append(names, fmt.Sprintf("%s to %s", v, to))
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}
