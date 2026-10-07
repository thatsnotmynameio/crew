package app_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// mayWait is oneAction whose session leads waiting to the route
// waiting-answer, which moves the issue to waiting answer.
func mayWait() string {
	return strings.Replace(oneAction, `{{.Issue.Ref}}"`+"\n",
		`{{.Issue.Ref}}"`+"\n        on: {waiting: waiting-answer}\n", 1) +
		"      waiting-answer: waiting answer\n"
}

// R38, KTD-W4: the engine gets the answering list: answering_apps when the
// config writes it, [] included, and otherwise the logins of crew's bots.
func TestTheEngineGetsTheAnsweringApps(t *testing.T) {
	logins := []string{"crew-ops[bot]", "crew-developer[bot]"}
	tests := []struct {
		name, head string
		want       []string
	}{
		{name: "without answering_apps, crew's bots", want: logins},
		{name: "a written list replaces them", head: "answering_apps: [\"claude[bot]\"]\n", want: []string{"claude[bot]"}},
		{name: "an empty list, no App", head: "answering_apps: []\n", want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := options(t, tt.head+oneAction, fake.NewTracker(), fake.NewHarness())
			cfg, err := app.EngineConfig(r.opts, app.Bots{Logins: logins})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.AnsweringApps, tt.want) {
				t.Errorf("AnsweringApps = %#v, want %#v", cfg.AnsweringApps, tt.want)
			}
		})
	}
}

// KTD-W5: crew refuses at startup a session that may wait for answers when
// its tracker cannot list comments, before any listing, naming the rule and
// the action, with the config's exit code.
func TestASessionThatMayWaitWithoutACommentListerExitsTwo(t *testing.T) {
	tr := &listCounter{Tracker: fake.NewTracker(issue("1", ready))}
	r := options(t, mayWait(), tr, fake.NewHarness())

	if code := app.Run(context.Background(), r.opts); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if n := tr.listed(); n != 0 {
		t.Errorf("the tracker listed %d times, want none", n)
	}
	want := `rules.implement.actions[0]: session "development" may wait for answers, ` +
		`and tracker "fake" cannot list comments`
	if stderr := r.stderr.String(); !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want it to contain %q", stderr, want)
	}
}

// KTD-W5: crew starts a session that may wait when its tracker lists
// comments.
func TestASessionThatMayWaitWithACommentListerStarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := fake.NewHarness()
		runOnce(t, options(t, mayWait(), fake.NewRoutingTracker(issue("1", ready)), h), h)
	})
}
