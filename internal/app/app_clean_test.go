package app_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

// The worktree the clean tests find, and its branch.
const (
	cleanName   = "issue-42-development"
	cleanBranch = "crew/issue-42-development"
)

// cleanRun is one app.Clean against a fake tracker and a fake workspace.
type cleanRun struct {
	opts   app.CleanOptions
	ws     *fake.Workspace
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

// cleanOptions returns clean options over tracker, registered as fake, and a
// fake workspace listing one worktree, for a repository whose
// .crew/config.yaml is body. The registry has no harness, so building one
// fails. Standard input is a terminal answering yes.
func cleanOptions(t *testing.T, body string, tracker port.Tracker) *cleanRun {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew", "worktrees"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".crew", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".crew", "worktrees")
	ws := fake.NewWorkspace(dir)
	ws.ScriptWorkspaces(fake.Listing{Found: []port.Found{{
		Space:  port.Space{Name: cleanName, Dir: filepath.Join(dir, cleanName), Branch: cleanBranch},
		Listed: true, Tip: "head-45", Created: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC),
	}}})
	r := &cleanRun{ws: ws, stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	r.opts = app.CleanOptions{
		Registry:  registry.New(map[string]port.TrackerFactory{"fake": fake.TrackerFactory(tracker)}, nil),
		Workspace: func(string) port.Workspace { return ws },
		Root:      root,
		Stdin:     strings.NewReader("y\n"),
		Stdout:    r.stdout,
		Stderr:    r.stderr,
		Terminal:  true,
	}
	return r
}

// merged scripts the pull request from the worktree's branch as merged at
// the branch's tip.
func merged(tr fake.FindingTracker) {
	tr.ScriptLookup(cleanBranch, fake.LookupScript{Found: crew.PullRequest{
		Lookup: crew.PullRequestFound, Ref: "#45", URL: "https://github.com/o/r/pull/45",
		State: crew.PullRequestMerged, Head: "head-45",
	}})
}

func (r *cleanRun) clean(ctx context.Context) int {
	return app.Clean(ctx, r.opts)
}

func TestCleanRemovesAMergedWorktreeOnAYesWithoutPreparingOrMates(t *testing.T) {
	tr := fake.NewFindingTracker()
	merged(tr)
	// The config names mates: Clean still reads GitHub as the boss.
	r := cleanOptions(t, matedAction, tr)
	if code := r.clean(t.Context()); code != app.ExitClean {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, app.ExitClean, r.stderr)
	}
	want := []fake.Removal{{Name: cleanName, DeleteBranch: true}}
	if got := r.ws.Removals(); !slices.Equal(got, want) {
		t.Errorf("removals = %#v, want %#v", got, want)
	}
	if calls := tr.Calls(); len(calls) != 0 {
		t.Errorf("the tracker's Prepare ran %d times, want never", len(calls))
	}
	dir := filepath.Join(r.opts.Root, ".crew", "worktrees")
	if out := r.stdout.String(); !strings.Contains(out, "1 worktree in "+dir+":") {
		t.Errorf("stdout = %q, want the list of %s", out, dir)
	}
	if r.stderr.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", r.stderr)
	}
}

func TestCleanKeepsAWorktreeTheJournalShowsInUse(t *testing.T) {
	tr := fake.NewFindingTracker()
	merged(tr)
	r := cleanOptions(t, oneAction, tr)
	logs := filepath.Join(r.opts.Root, ".crew", "logs")
	if err := os.MkdirAll(logs, 0o750); err != nil {
		t.Fatal(err)
	}
	started := `{"v":1,"event":"started","time":"2026-10-04T14:02:00Z","issue":"42","ref":"#42",` +
		`"stage":"implement","action":"development","workspace":"` + cleanName + `","branch":"` + cleanBranch +
		`","log":".crew/logs/issue-42-development.log"}` + "\n"
	if err := os.WriteFile(filepath.Join(logs, "runs.jsonl"), []byte(started), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := r.clean(t.Context()); code != app.ExitClean {
		t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, app.ExitClean, r.stderr)
	}
	if got := r.ws.Removals(); len(got) != 0 {
		t.Errorf("removals = %#v, want none", got)
	}
	if out := r.stdout.String(); !strings.Contains(out, "has not ended") {
		t.Errorf("stdout = %q, want the unended run as the reason", out)
	}
}

func TestCleanExitsOneWhenALookupFails(t *testing.T) {
	tr := fake.NewFindingTracker()
	tr.ScriptLookup(cleanBranch, fake.LookupScript{Err: errors.New("gh: HTTP 502")})
	r := cleanOptions(t, oneAction, tr)
	if code := r.clean(t.Context()); code != app.ExitFailure {
		t.Fatalf("exit code = %d, want %d; stdout:\n%s", code, app.ExitFailure, r.stdout)
	}
	if got := r.ws.Removals(); len(got) != 0 {
		t.Errorf("removals = %#v, want none", got)
	}
}

func TestCleanExitsOneWhenStopped(t *testing.T) {
	tr := fake.NewFindingTracker()
	merged(tr)
	r := cleanOptions(t, oneAction, tr)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if code := r.clean(ctx); code != app.ExitFailure {
		t.Fatalf("exit code = %d, want %d", code, app.ExitFailure)
	}
	if got := r.ws.Removals(); len(got) != 0 {
		t.Errorf("removals = %#v, want none", got)
	}
}

func TestCleanEnvironmentErrorsListNothing(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		tracker port.Tracker
		// setup, when set, changes the run before it starts.
		setup func(r *cleanRun)
		want  string
	}{
		{name: "config that does not load", body: "config: [", tracker: fake.NewFindingTracker(),
			want: ".crew/config.yaml: yaml: "},
		{name: "tracker not registered", body: strings.Replace(oneAction, "name: fake", "name: jira", 1),
			tracker: fake.NewFindingTracker(), want: `tracker.name: no tracker is named "jira"`},
		{name: "tracker without a pull request lookup", body: oneAction, tracker: fake.NewPreparingTracker(),
			want: "crew: the tracker fake cannot look up pull requests"},
		{
			name: "workspace without worktrees to clean", body: oneAction, tracker: fake.NewFindingTracker(),
			setup: func(r *cleanRun) {
				r.opts.Workspace = func(string) port.Workspace { return struct{ port.Workspace }{r.ws} }
			},
			want: "crew: the workspace cannot list its worktrees",
		},
		{
			name: "listing that fails", body: oneAction, tracker: fake.NewFindingTracker(),
			setup: func(r *cleanRun) { r.ws.ScriptWorkspaces(fake.Listing{Err: errors.New("git: not a repository")}) },
			want:  "git: not a repository",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := cleanOptions(t, tt.body, tt.tracker)
			if tt.setup != nil {
				tt.setup(r)
			}
			if code := r.clean(t.Context()); code != app.ExitConfig {
				t.Fatalf("exit code = %d, want %d; stderr:\n%s", code, app.ExitConfig, r.stderr)
			}
			if r.stdout.Len() != 0 || len(r.ws.Removals()) != 0 {
				t.Errorf("stdout = %q, removals = %#v; want nothing listed or removed", r.stdout, r.ws.Removals())
			}
			if got := r.stderr.String(); !strings.Contains(got, tt.want) {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
		})
	}
}
