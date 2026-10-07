package engine_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// events returns every event r's engine published, once Run has returned.
func (r *rig) events() []core.Event {
	var out []core.Event
	for u := range r.queue.Updates() {
		out = append(out, u.Events...)
	}
	return out
}

// session waits for the next session to start.
func (r *rig) session() *fake.Session {
	r.t.Helper()
	for _, s := range r.sessions(1) {
		return s
	}
	return nil
}

// failOnce runs issue 1's development session in r and fails it with reason
// after it printed output, then puts the issue back in ready, as you
// would, and returns the session.
func failOnce(t *testing.T, r *rig, tr *fake.Tracker, output, reason string) *fake.Session {
	t.Helper()
	s := r.session()
	if _, err := fmt.Fprint(s.Run().Output, output); err != nil {
		t.Fatal(err)
	}
	s.End(crew.Outcome{Reason: reason})
	synctest.Wait()
	if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{needsAttention}) {
		t.Fatalf("issue 1 is in %v after its failure, want needs attention", got)
	}
	tr.SetStates("1", ready)
	time.Sleep(poll)
	return s
}

func TestAE1ARelabeledFailedRunResumesInItsWorkspaceAndLog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		r := start(t, config(t, tr, develop))

		first := failOnce(t, r, tr, "first output\n", "no pull request was found")
		second := r.session()

		if second.Run().Dir != first.Run().Dir {
			t.Fatalf("resumed in %s, want %s", second.Run().Dir, first.Run().Dir)
		}
		checkResumePrompt(t, second.Run().Prompt)
		if _, err := fmt.Fprint(second.Run().Output, "second output\n"); err != nil {
			t.Fatal(err)
		}
		second.End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		checkResumedLog(t, r.root)
		if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
			t.Errorf("issue 1 is in %v, want ready to review", got)
		}
	})
}

// checkResumePrompt checks that prompt is issue 1's development prompt
// followed by the paragraph resuming its failed run.
func checkResumePrompt(t *testing.T, prompt string) {
	t.Helper()
	if !strings.HasPrefix(prompt, "Implement development for issue #1\n\ncrew: this session continues") ||
		!strings.Contains(prompt, `That run failed: "no pull request was found".`) ||
		!strings.Contains(prompt, "`.crew/logs/issue-1-development.log`") ||
		!strings.Contains(prompt, "(`../../logs/issue-1-development.log` from this worktree)") {
		t.Fatalf("prompt does not end with the resume paragraph:\n%s", prompt)
	}
}

// checkResumedLog checks that the log of issue 1's development under root
// holds the first run's output, the resume marker, then the second run's.
func checkResumedLog(t *testing.T, root string) {
	t.Helper()
	log, err := os.ReadFile(filepath.Join(root, ".crew", "logs", "issue-1-development.log"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(log), "\n"), "\n")
	if len(lines) != 3 || lines[0] != "first output" || lines[2] != "second output" {
		t.Fatalf("log = %q, want the first output, the marker, then the second output", log)
	}
	var marker struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
	}
	err = json.Unmarshal([]byte(lines[1]), &marker)
	if err != nil || marker.Type != "crew" || marker.Subtype != "resumed" {
		t.Fatalf("marker line = %q, want a crew resumed JSON line", lines[1])
	}
}

// writeJournal writes lines, each a JSON object, as the run journal of cfg.
func writeJournal(t *testing.T, root string, lines ...string) {
	t.Helper()
	dir := filepath.Join(root, ".crew", "logs")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runs.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

const startedLine = `{"v":1,"event":"started","time":"2026-01-01T10:00:00Z",` +
	`"issue":"1","ref":"#1","stage":"implement",` +
	`"action":"development","workspace":"issue-1-development","branch":"crew/issue-1-development",` +
	`"log":".crew/logs/issue-1-development.log"}`

func TestAE5ARunKilledWithCrewResumesAfterARestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		// The journal a killed crew left: the run's start and no end.
		writeJournal(t, cfg.Root, startedLine)
		if err := os.Mkdir(filepath.Join(cfg.Root, ".crew", "worktrees", "issue-1-development"), 0o750); err != nil {
			t.Fatal(err)
		}
		r := start(t, cfg)

		s := r.session()
		if got := filepath.Base(s.Run().Dir); got != "issue-1-development" {
			t.Errorf("session runs in %s, want issue-1-development", got)
		}
		if p := s.Run().Prompt; !strings.Contains(p, "crew stopped before the run ended: it crashed or was killed") {
			t.Errorf("prompt does not say the run was cut short:\n%s", p)
		}
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}

func TestAE3AGoneWorkspaceGivesAFreshOneWithoutTheParagraph(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		r := start(t, cfg)

		s := r.session()
		s.End(crew.Outcome{Reason: "broke"})
		synctest.Wait()
		if err := os.RemoveAll(s.Run().Dir); err != nil {
			t.Fatal(err)
		}
		tr.SetStates("1", ready)
		time.Sleep(poll)

		if p := r.session().Run().Prompt; p != "Implement development for issue #1" {
			t.Errorf("prompt = %q, want the bare prompt", p)
		}
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if n := len(fakeWorkspace(t, cfg).Spaces()); n != 2 {
			t.Errorf("the workspace created %d spaces, want 2", n)
		}
		if !slices.ContainsFunc(r.events(), func(e core.Event) bool {
			m, ok := e.(core.WorkspaceMissing)
			return ok && m.Workspace == "issue-1-development"
		}) {
			t.Errorf("no WorkspaceMissing event for issue-1-development")
		}
	})
}

func TestAJournalThatCannotBeWrittenIsReportedAndTheRunGoesOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		writeJournal(t, cfg.Root)
		if err := os.Chmod(filepath.Join(cfg.Root, ".crew", "logs", "runs.jsonl"), 0o400); err != nil {
			t.Fatal(err)
		}
		r := start(t, cfg)

		r.session().End(crew.Outcome{Succeeded: true, Reason: "done"})
		synctest.Wait()
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}

		if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{readyToReview}) {
			t.Errorf("issue 1 is in %v, want ready to review", got)
		}
		var reasons []string
		for _, e := range r.events() {
			if n, ok := e.(core.RunNotRecorded); ok {
				reasons = append(reasons, n.Reason)
			}
		}
		if len(reasons) != 2 || !strings.Contains(reasons[0], "./.crew/logs/runs.jsonl") ||
			strings.Contains(reasons[0], cfg.Root) {
			t.Errorf("RunNotRecorded reasons = %q, want two naming ./.crew/logs/runs.jsonl", reasons)
		}
	})
}

// createOnly is a workspace that can only create, as one without
// port.Reopener.
type createOnly struct{ w *fake.Workspace }

func (c createOnly) Create(ctx context.Context, issue crew.Issue, action crew.ActionName) (port.Space, error) {
	return c.w.Create(ctx, issue, action)
}

func TestAWorkspaceThatCannotReopenStartsAFailedRunFresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, develop)
		cfg.Workspace = createOnly{w: fakeWorkspace(t, cfg)}
		r := start(t, cfg)

		first := failOnce(t, r, tr, "", "broke")
		second := r.session()

		if second.Run().Dir == first.Run().Dir || second.Run().Prompt != "Implement development for issue #1" {
			t.Errorf("second session = %+v, want a fresh workspace and the bare prompt", second.Run())
		}
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}
