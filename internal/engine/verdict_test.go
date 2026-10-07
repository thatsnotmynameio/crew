package engine_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// blocked is the state the blocked route moves an issue to.
const blocked crew.State = "blocked"

// judged is develop whose session may end with the verdict blocked, which
// leads to the route blocked, moving the issue to blocked.
var judged = crew.Rule{
	Name:   develop.Name,
	Labels: develop.Labels,
	Actions: []crew.Action{{
		Name: "development", Kind: develop.Actions[0].Kind,
		On: crew.On{"blocked": crew.ToRoute{Route: "blocked"}},
	}},
	Routes: append(routes(needsAttention),
		crew.Route{Name: "blocked", Steps: []crew.Step{crew.MoveStep{To: blocked}}}),
}

// verdictRun runs #1 through judged, has its session write written to its
// verdict file and end with success, then stops crew, and returns the run's
// one action end, as journaled.
func verdictRun(t *testing.T, tr *fake.Tracker, written string) crew.ActionEnded {
	t.Helper()
	cfg := config(t, tr, judged)
	r := start(t, cfg)
	s := r.session()
	if err := os.WriteFile(s.Run().VerdictFile, []byte(written), 0o600); err != nil {
		t.Fatal(err)
	}
	s.End(port.SessionEnd{Succeeded: true, Reason: "done"})
	synctest.Wait()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	end := ended(t, cfg)
	if len(end) != 1 {
		t.Fatalf("ends = %#v, want one", end)
	}
	return end[0]
}

// KTD8, KTD-S4: the first word of the verdict file is the session's
// verdict when it is a verdict name, and an empty file reports none.
func TestASessionsVerdictFileGivesItsVerdict(t *testing.T) {
	tests := []struct {
		name, written string
		verdict       crew.Verdict
		to            crew.State
	}{
		{name: "a verdict on its own line", written: "blocked\n", verdict: "blocked", to: blocked},
		{name: "a verdict then words", written: "  blocked by #12\r\nwaiting on review\n", verdict: "blocked", to: blocked},
		{name: "empty", written: "", verdict: crew.Passed, to: readyToReview},
		{name: "spaces only", written: " \n\t\n", verdict: crew.Passed, to: readyToReview},
		{name: "a verdict any action may end with", written: "failed", verdict: crew.Failed, to: needsAttention},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr := fake.NewTracker(issue(1, ready))

				end := verdictRun(t, tr, tt.written)

				if end.Verdict != tt.verdict {
					t.Errorf("verdict = %q, want %q", end.Verdict, tt.verdict)
				}
				if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{tt.to}) {
					t.Errorf("#1 is in %v, want %s", got, tt.to)
				}
			})
		})
	}
}

// KTD-S4: text with no verdict name first, or with a control character,
// is unreadable: the session failed, in crew's words, and neither a
// cleaned-up name nor the raw bytes reach crew.
func TestAnUnreadableVerdictFileFailsTheSession(t *testing.T) {
	unreadable := []string{"\x00blocked\n", "\x1b[31mblocked\x1b[0m\n", "blocked\x1b[0m\n", "Blocked\n", "#blocked\n"}
	for _, written := range unreadable {
		t.Run(strings.ToValidUTF8(written, "?"), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr := fake.NewTracker(issue(1, ready))

				end := verdictRun(t, tr, written)

				reason := end.End.Outcome().Reason.String()
				if end.Verdict != crew.Failed || reason != "the session's verdict holds no verdict name" {
					t.Errorf("end = %q, %q, want failed for no verdict name", end.Verdict, reason)
				}
				if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{needsAttention}) {
					t.Errorf("#1 is in %v, want needs attention", got)
				}
			})
		})
	}
}

// KTD8: each session gets a verdict file of its own, empty, in a private
// directory outside the worktree and .crew/logs/, which is gone once its
// verdict was read.
func TestEachSessionGetsItsOwnVerdictDirectoryGoneOnceRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, ready))
		cfg := config(t, tr, implement)
		r := start(t, cfg)

		dirs := make([]string, 0, len(implement.Actions))
		for range implement.Actions {
			s := r.session()
			run := s.Run()
			checkVerdictFile(t, cfg.Root, run)
			dirs = append(dirs, run.VerdictDir)
			s.End(port.SessionEnd{Succeeded: true, Reason: "done"})
			synctest.Wait()
			if _, err := os.Stat(run.VerdictDir); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("verdict directory %s after the session ended: %v, want it gone", run.VerdictDir, err)
			}
		}
		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if dirs[0] == dirs[1] {
			t.Errorf("both sessions got the verdict directory %s, want one each", dirs[0])
		}
	})
}

// checkVerdictFile checks that run's verdict file is empty, alone in a
// directory only you can read, outside root.
func checkVerdictFile(t *testing.T, root string, run port.Run) {
	t.Helper()
	if filepath.Dir(run.VerdictFile) != run.VerdictDir {
		t.Fatalf("verdict file %s is not in its directory %s", run.VerdictFile, run.VerdictDir)
	}
	if rel, err := filepath.Rel(root, run.VerdictDir); err == nil && !strings.HasPrefix(rel, "..") {
		t.Errorf("verdict directory %s is inside the repository %s", run.VerdictDir, root)
	}
	info, err := os.Stat(run.VerdictDir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("verdict directory = %v, %v, want mode 0700", info, err)
	}
	entries, err := os.ReadDir(run.VerdictDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("verdict directory holds %v, %v, want the verdict file alone", entries, err)
	}
	if data, err := os.ReadFile(run.VerdictFile); err != nil || len(data) != 0 {
		t.Errorf("verdict file = %q, %v, want it empty", data, err)
	}
}
