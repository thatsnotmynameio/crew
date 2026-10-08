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

// translationFunctions registers translation's invented function.
func translationFunctions() map[string][]crew.Verdict {
	return map[string][]crew.Verdict{"word-count": {"too_long"}}
}

// ruleSummary is how a rule loads, its actions summed up as "session S:
// agent A, bot B, wait W", "shell S" or "function S: F", each followed by
// its on as "; verdict to target", and its routes as "route: step, step".
type ruleSummary struct {
	name    crew.RuleName
	labels  crew.Labels
	queue   crew.Queue
	notify  bool
	actions []string
	routes  []string
}

// A full config loads its rules with their labels, queues, notify, actions
// and routes: each session with its agent, its bot and its wait, each shell
// action by name, each action's on, each route's steps in order; its
// tracker's bot and every agent's; its board, one column per rule with
// actions when it sets none; its shell actions' scripts, verdicts and
// resume; its function uses with their parameters; and its prompts as they
// are written.
func TestAFullConfigLoads(t *testing.T) {
	cfg, err := config.Load(translation, "", translationFunctions())
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
	pageLength(t, cfg)
}

// pageLength checks translation's function uses: the page-length action,
// whose use replaces its preset's most, and the word-count step.
func pageLength(t *testing.T, cfg *config.Config) {
	t.Helper()
	uses := make([]crew.FunctionUse, len(cfg.Functions))
	for i, u := range cfg.Functions {
		uses[i] = u.Use
	}
	want := []crew.FunctionUse{"rules.translation.actions[3]", "rules.proofreading.routes.rejected[2]"}
	if !reflect.DeepEqual(uses, want) {
		t.Fatalf("function uses = %q, want %q", uses, want)
	}
	var params struct {
		Page string `yaml:"page"`
		Most int    `yaml:"most"`
	}
	if err := cfg.Functions[0].Section(&params); err != nil || params.Page != "https://example.com/issues/42" ||
		params.Most != 1500 {
		t.Errorf("page-length's parameters = %+v, %v; want the sample issue's URL and 1500", params, err)
	}
}

// A full config loads its answering list as written (R38).
func TestAFullConfigLoadsItsAnsweringApps(t *testing.T) {
	cfg, err := config.Load(translation, "", translationFunctions())
	if err != nil {
		t.Fatalf("Load(%s) = %v", translation, err)
	}
	want := []string{"glossary-keeper[bot]", "linguist[bot]"}
	if !reflect.DeepEqual(cfg.AnsweringApps, want) || !cfg.AnsweringAppsWritten {
		t.Errorf("AnsweringApps = %q (written %v), want %q", cfg.AnsweringApps, cfg.AnsweringAppsWritten, want)
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
				"session draft: agent translator, bot linguist, wait 30m0s",
				"shell glossary-kept",
				"shell draft-pushed; unpushed to unpushed",
				"function page-length: word-count",
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
				"session proofread: agent proofreader, bot concierge, wait 10m0s; failed to rejected, waiting to next",
				"shell glossary-kept",
				"session translator: agent translator, bot linguist, wait 10m0s",
			},
			routes: []string{
				"passed: move translation:published",
				"failed: report, move translation:rejected",
				"rejected: comment {{.Issue.Ref}} was rejected in {{.Rule}}., glossary-kept, function word-count, close",
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
				action = fmt.Sprintf("session %s: agent %s, bot %s, wait %v", a.Name, k.Agent.Name, k.Bot.Name, k.Wait)
			case crew.ShellSpec:
				action = fmt.Sprintf("shell %s", a.Name)
			case crew.FunctionSpec:
				action = fmt.Sprintf("function %s: %s", a.Name, k.Function)
			case crew.QuestionSpec:
				action = fmt.Sprintf("question %s: %s", a.Name, k.Question.ID)
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
		case crew.FunctionStep:
			names[i] = "function " + string(s.Name)
		case crew.QuestionStep:
			names[i] = "question " + string(s.Question.ID)
		case crew.DelegateStep:
			names[i] = "delegate"
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
