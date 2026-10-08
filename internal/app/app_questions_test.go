package app_test

import (
	"context"
	"os"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// The labels of the questions tests: the deps rule's, and crew's question
// rule's.
const (
	depsReady       crew.State = "crew:deps:ready"
	depsDone        crew.State = "crew:deps:done"
	questionLabel   crew.State = "crew:question"
	questionWaiting crew.State = "crew:question:waiting answer"
)

// asking is a config with questions answered by octocat and the rule deps,
// whose session judge leads its verdict unsure to the route ask, which asks
// whether the issue blocks #281 and returns it to deps. Its polls are a
// minute apart.
const asking = `
poll_interval_seconds: 60
questions: {answerer: octocat}
tracker:
  name: fake
agents:
  developer:
    harness: {name: fake}
rules:
  deps:
    labels: {ready: "crew:deps:ready", running: "crew:deps:in progress"}
    actions:
      - name: judge
        prompt: "Judge {{.Issue.Ref}}"
        on: {unsure: ask}
    routes:
      passed: "crew:deps:done"
      failed: [report, move: needs attention]
      ask:
        - question: {id: blocks, text: "Does {{.Issue.Ref}} block #281?", return: "crew:deps:ready"}
`

// askingBetweenShells is asking whose deps rule runs the shell action
// first, asks the question blocks as an action, then runs the shell action
// second.
const askingBetweenShells = `
poll_interval_seconds: 60
questions: {answerer: octocat}
tracker:
  name: fake
actions:
  first: make first
  second: make second
rules:
  deps:
    labels: {ready: "crew:deps:ready", running: "crew:deps:in progress"}
    actions:
      - first
      - question: {id: blocks, text: "Does {{.Issue.Ref}} block #281?", return: "crew:deps:ready"}
      - second
    routes:
      passed: "crew:deps:done"
      failed: [report, move: needs attention]
`

// poll is the time between the polls of asking and askingBetweenShells.
const poll = time.Minute

// questioned returns a routing tracker holding #1 in deps's ready label,
// which acts and posts as boss.
func questioned() fake.RoutingTracker {
	tr := fake.NewRoutingTracker(issue("1", depsReady))
	tr.SetLogin("boss")
	tr.SetWriter("boss")
	return tr
}

// stop stops r's crew and fails t unless it exits cleanly.
func stop(t *testing.T, r *crewRun) {
	t.Helper()
	r.signals <- syscall.SIGTERM
	if code := <-r.code; code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
	}
}

// wantIn fails t unless #1 of tr is in want alone.
func wantIn(t *testing.T, tr fake.RoutingTracker, want crew.State) {
	t.Helper()
	if got := states(t, tr.Tracker); !slices.Equal(got, []crew.State{want}) {
		t.Fatalf("#1 is in %v, want %s", got, want)
	}
}

// wantDelegated fails t unless tr posted, after the question blocks of
// deps on #1, one delegation of it to octocat, and listed #1's comments
// once, for it.
func wantDelegated(t *testing.T, tr fake.RoutingTracker) {
	t.Helper()
	posted := tr.Posted()
	if len(posted) != 1 || !strings.Contains(posted[0].Body, "Does #1 block #281?") ||
		!strings.Contains(posted[0].Body, crew.QuestionMarker("blocks", "deps", depsReady)) {
		t.Errorf("posted = %+v, want the question blocks of deps", posted)
	}
	want := crew.Delegation{IssueRef: "#1", Answerer: "octocat", Search: crew.QuestionFound, ID: "blocks", Rule: "deps"}
	got := tr.Delegations()
	if len(got) == 1 && got[0].IssueID.Key == "1" {
		want.IssueID = got[0].IssueID // in the repository the engine names
	}
	if !reflect.DeepEqual(got, []crew.Delegation{want}) {
		t.Errorf("delegations = %+v, want one of #1: %+v", got, want)
	}
	if n := tr.CommentLists("1"); n != 1 {
		t.Errorf("crew listed #1's comments %d times, want once, for the question rule", n)
	}
}

// Covers F1, AE1, AE7 and R13: on the in-memory tracker, a session whose
// verdict leads to a question route posts the question and moves the issue
// to crew:question without reading comments; at the next poll crew's
// question rule reads them, delegates the question it finds to the
// answerer, and leaves the issue waiting for the answer.
func TestASessionsQuestionIsPostedThenDelegatedToTheAnswerer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := questioned()
		h := fake.NewHarness()
		r := options(t, asking, tr, h)
		r.start()

		s := next(t, h)
		if err := os.WriteFile(s.Run().VerdictFile, []byte("unsure\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		s.End(success)
		synctest.Wait()
		wantIn(t, tr, questionLabel)
		if n := tr.CommentLists("1"); n != 0 {
			t.Errorf("crew listed #1's comments %d times for deps, want none", n)
		}

		time.Sleep(poll)
		synctest.Wait()
		stop(t, r)

		wantIn(t, tr, questionWaiting)
		wantDelegated(t, tr)
	})
}

// Covers F1, AE7, KTD4 and R13: a question action between two shell
// actions asks once the first ran; once the issue returns to deps's ready
// label, the next run starts at the second, and the first does not run
// again. Only the question rule read the comments.
func TestARunAfterAQuestionActionStartsAfterIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := questioned()
		sh := fake.NewShell()
		r := options(t, askingBetweenShells, tr, fake.NewHarness())
		r.opts.Shell = sh
		r.start()

		synctest.Wait()
		wantIn(t, tr, questionLabel)
		time.Sleep(poll)
		synctest.Wait()
		wantIn(t, tr, questionWaiting)
		wantDelegated(t, tr)

		tr.SetStates("1", depsReady) // the answer came, and you moved #1 back
		time.Sleep(poll)
		synctest.Wait()
		stop(t, r)

		wantIn(t, tr, depsDone)
		runs := sh.Runs()
		ran := make([]crew.ActionName, 0, len(runs))
		for _, s := range runs {
			ran = append(ran, s.Name)
		}
		if want := []crew.ActionName{"first", "second"}; !slices.Equal(ran, want) {
			t.Errorf("shell actions run = %v, want %v", ran, want)
		}
		if n := tr.CommentLists("1"); n != 1 {
			t.Errorf("crew listed #1's comments %d times, want once, for the question rule", n)
		}
	})
}

// uncommenting is a tracker that lists comments, delegates and finds its
// login, but cannot comment.
type uncommenting struct {
	fake.ReportingTracker
	port.CommentLister
	port.Delegator
	port.LoginFinder
}

// unlisting is a tracker that comments, delegates and finds its login, but
// cannot list comments.
type unlisting struct {
	fake.ReportingTracker
	port.Commenter
	port.Delegator
	port.LoginFinder
}

// undelegating is a tracker that comments, lists comments and finds its
// login, but cannot delegate.
type undelegating struct {
	fake.ReportingTracker
	port.Commenter
	port.CommentLister
	port.LoginFinder
}

// loginless is a tracker that comments, lists comments and delegates, but
// finds no login.
type loginless struct {
	fake.ReportingTracker
	port.Commenter
	port.CommentLister
	port.Delegator
}

// KTD10: crew refuses at startup a question its tracker cannot carry, with
// the config's exit code, naming the key path: a question step or action
// needs a tracker that comments, and questions one that lists comments and
// delegates.
func TestAQuestionTheTrackerCannotCarryExitsTwo(t *testing.T) {
	rt := fake.NewRoutingTracker(issue("1", depsReady))
	tests := []struct {
		name, config string
		tracker      port.Tracker
		want         string
	}{
		{"a question step without a Commenter", asking, uncommenting{rt.ReportingTracker, rt, rt, rt},
			`rules.deps.routes.ask: tracker "fake" cannot comment on issues`},
		{"a question action without a Commenter", askingBetweenShells, uncommenting{rt.ReportingTracker, rt, rt, rt},
			`rules.deps.actions[1]: tracker "fake" cannot comment on issues`},
		{"no CommentLister", asking, unlisting{rt.ReportingTracker, rt, rt, rt},
			`questions: tracker "fake" cannot list comments`},
		{"no Delegator", asking, undelegating{rt.ReportingTracker, rt, rt, rt},
			`questions: tracker "fake" cannot delegate questions`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := options(t, tt.config, tt.tracker, fake.NewHarness())

			if code := app.Run(context.Background(), r.opts); code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if stderr := r.stderr.String(); !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.want)
			}
		})
	}
}

// KTD10: with questions, crew must know the login it posts as to find the
// question it asked: a tracker that finds no login needs a tracker.bot
// that acts, and crew refuses to start otherwise.
func TestQuestionsWithoutAKnownWriterExitTwo(t *testing.T) {
	withOpsAsking := strings.Replace(asking, "  name: fake\n", "  name: fake\n  bot: ops\n", 1)
	tests := []struct {
		name, config string
		bots         app.Bots
	}{
		{name: "no tracker.bot", config: asking},
		{name: "a tracker.bot that cannot act", config: withOpsAsking,
			bots: app.Bots{Unable: map[crew.BotName]string{"ops": "no key"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := fake.NewRoutingTracker(issue("1", depsReady))
			r := options(t, tt.config, loginless{rt.ReportingTracker, rt, rt, rt}, fake.NewHarness())
			r.opts.Bots = (&resolver{bots: tt.bots}).resolve

			if code := app.Run(context.Background(), r.opts); code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			want := `questions: tracker "fake" finds no login crew posts as, and no tracker.bot acts`
			if stderr := r.stderr.String(); !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, want)
			}
		})
	}
}

// KTD10: a tracker.bot that acts tells crew the login it posts as, so
// questions start on a tracker that finds no login.
func TestQuestionsWithATrackerBotThatActsStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := fake.NewRoutingTracker(issue("1", depsReady))
		h := fake.NewHarness()
		r := options(t, strings.Replace(asking, "  name: fake\n", "  name: fake\n  bot: ops\n", 1),
			loginless{rt.ReportingTracker, rt, rt, rt}, h)
		r.opts.Bots = (&resolver{bots: app.Bots{Writer: opsWriter,
			Identities: map[crew.BotName]port.Identity{"ops": opsID}}}).resolve
		runOnce(t, r, h)
	})
}
