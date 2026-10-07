package engine_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// notified is promote, a rule without actions, whose passed route runs the
// shell action notify before it moves the issue to ready to review.
var notified = crew.Rule{
	Name:   "promote",
	Labels: promote.Labels,
	Routes: []crew.Route{{Name: crew.PassedRoute, Steps: []crew.Step{
		crew.ShellStep{Name: "notify", Shell: crew.ShellSpec{Script: "./notify"}},
		crew.MoveStep{To: readyToReview},
	}}},
}

// dirShell is a fake shell that also records whether each script's
// directory existed while the script ran.
type dirShell struct {
	*fake.Shell

	mu      sync.Mutex
	existed []bool
}

func (d *dirShell) Run(ctx context.Context, script port.Script) (port.ShellResult, error) {
	info, err := os.Stat(script.Dir)
	d.mu.Lock()
	d.existed = append(d.existed, err == nil && info.IsDir())
	d.mu.Unlock()
	return d.Shell.Run(ctx, script)
}

// checkTemporaryDir checks that sh ran notify alone, in a directory that
// existed while it ran, outside root, and is gone since.
func checkTemporaryDir(t *testing.T, root string, sh *dirShell) {
	t.Helper()
	scripts := sh.Runs()
	if len(scripts) != 1 || scripts[0].Name != "notify" || scripts[0].Dir == "" || !sh.existed[0] {
		t.Fatalf("scripts = %+v, existed %v, want notify in a directory of its own", scripts, sh.existed)
	}
	if rel, err := filepath.Rel(root, scripts[0].Dir); err == nil && !strings.HasPrefix(rel, "..") {
		t.Errorf("notify ran in %s, inside the repository", scripts[0].Dir)
	}
	if _, err := os.Stat(scripts[0].Dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("notify's directory after it ended: %v, want it gone", err)
	}
}

// stepOutcomes returns the outcomes of the route steps r's engine
// published, once Run has returned.
func (r *rig) stepOutcomes() []crew.StepOutcome {
	var out []crew.StepOutcome
	for _, e := range r.events() {
		if ended, ok := e.(crew.StepEnded); ok {
			out = append(out, ended.Outcome)
		}
	}
	return out
}

// KTD9: a route's shell step in a run without a workspace runs in an empty
// temporary directory, gone once the script ended, and writes into the log
// of the workspace the run would have, after a marker line naming it. A
// step that fails says so in crew's words only (R49).
func TestARouteShellStepWithoutAWorkspaceRunsInATemporaryDirectory(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		sh := &dirShell{Shell: fake.NewShell()}
		sh.ScriptAction("", "notify", fake.ShellScript{Print: "posting the news\nchat is down\n", Exit: 1})
		cfg := config(t, tr, notified)
		cfg.Shell = sh
		r := start(t, cfg)
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		checkTemporaryDir(t, cfg.Root, sh)
		want := []crew.StepOutcome{
			crew.StepFailed{Reason: crew.NewCheckReason("the route's shell step notify exited with status 1")},
			crew.StepLanded{},
		}
		if got := r.stepOutcomes(); !reflect.DeepEqual(got, want) {
			t.Errorf("step outcomes = %#v, want %#v", got, want)
		}
		if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
		log, err := os.ReadFile(filepath.Join(cfg.Root, ".crew", "logs", "issue-1-promote.log"))
		if want := "\ncrew: running the route's shell step notify: ./notify\nposting the news\nchat is down\n"; err != nil ||
			string(log) != want {
			t.Errorf("log = %q, %v, want %q", log, err, want)
		}
	})
}

// KTD-S14: a stop cancels a route's shell step that runs, which is
// recorded as stopped, and the route's final move still happens (R16).
func TestAStopCancelsARunningRouteShellStep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := fake.NewTracker(issue(1, ready)), fake.NewShell()
		sh.ScriptAction("", "notify", fake.ShellScript{Block: true})
		cfg := config(t, tr, notified)
		cfg.Shell = sh
		r := start(t, cfg)
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		want := []crew.StepOutcome{
			crew.StepStopped{Reason: crew.NewCheckReason("the route's shell step notify was stopped")},
			crew.StepLanded{},
		}
		if got := r.stepOutcomes(); !reflect.DeepEqual(got, want) {
			t.Errorf("step outcomes = %#v, want %#v", got, want)
		}
		if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
			t.Errorf("#1 is in %v, want ready to review", got)
		}
	})
}

// commented is promote whose passed route comments, then closes the issue.
func commented(t *testing.T) crew.Rule {
	t.Helper()
	tmpl, err := crew.ParseCommentTemplate(crew.PassedRoute, "Promoted {{.Issue.Ref}}.")
	if err != nil {
		t.Fatal(err)
	}
	return crew.Rule{
		Name:   "promote",
		Labels: promote.Labels,
		Routes: []crew.Route{{Name: crew.PassedRoute, Steps: []crew.Step{
			crew.CommentStep{Template: tmpl}, crew.CloseStep{},
		}}},
	}
}

// KTD9: a route's comment and close go through the tracker's Commenter and
// Closer, and a comment that failed transiently is owed, then posted at
// the next poll.
func TestARoutesCommentAndCloseGoThroughTheTrackerAndAFailedCommentIsOwed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewRoutingTracker(issue(1, ready))
		tr.FailComments("1", errors.New("connection reset"))
		r := start(t, config(t, tr, commented(t)))
		synctest.Wait()
		if got := tr.Posted(); len(got) != 0 {
			t.Fatalf("posted %+v before the retry, want none", got)
		}
		time.Sleep(poll)
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		owed := slices.ContainsFunc(r.events(), func(e core.Published) bool {
			o, ok := e.(core.CallOwed)
			return ok && o.Call.Kind == core.CallComment && strings.Contains(o.Reason, "connection reset")
		})
		if !owed {
			t.Error("no comment owed for its transient failure")
		}
		if got, want := tr.Posted(), []fake.Comment{{Key: "1", Body: "Promoted #1."}}; !reflect.DeepEqual(got, want) {
			t.Errorf("posted %+v, want %+v", got, want)
		}
		if got, want := tr.Closings(), []fake.Closing{{Key: "1", From: inProgress}}; !reflect.DeepEqual(got, want) {
			t.Errorf("closings = %+v, want %+v", got, want)
		}
	})
}

// A tracker without a Commenter refuses a route's comment for good: crew
// refuses such a config before it starts, so this only keeps a run from
// waiting on a comment that cannot go.
func TestATrackerThatCannotCommentRefusesTheComment(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		r := start(t, config(t, tr, commented(t)))
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		dropped := slices.ContainsFunc(r.events(), func(e core.Published) bool {
			d, ok := e.(core.CallDropped)
			return ok && d.Call.Kind == core.CallComment && d.Result == core.ResultRefused
		})
		if !dropped {
			t.Error("no comment dropped as refused")
		}
	})
}
