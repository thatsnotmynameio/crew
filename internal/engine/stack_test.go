package engine_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// blockerBranch is the head branch of #1's pull request #45, which #2
// stacks on.
const blockerBranch = "crew/issue-1-implement"

// stacking is develop with stacking on, whose prompt names the run's base
// and, when stacked, the blocker and its pull request.
var stacking = crew.Rule{
	Name: develop.Name, Labels: develop.Labels, Stacking: true,
	Actions: []crew.Action{sessionAction("development",
		"Implement {{.Issue.Ref}} on {{.Base}}"+
			"{{if .Stack.Stacked}} after {{.Stack.Blocker.Ref}}'s {{.Stack.PullRequest.Ref}}{{end}}")},
	Routes: develop.Routes,
}

// stackOn45 is #2's stack on #1's pull request #45.
var stackOn45 = crew.Stack{
	Blocker: issueID("1"), BlockerRef: "#1", BlockerURL: "https://example.test/issues/1",
	PullRequestRef: "#45", PullRequestURL: "https://example.test/pull/45", Branch: blockerBranch,
}

// blockedIssue is the issue keyed n as issue builds it, blocked by an open
// issue.
func blockedIssue(n int, states ...crew.State) crew.Issue {
	d := issue(n, states...).Data()
	d.Blocked = true
	return crew.NewIssue(d)
}

// readingOn45 is what a tracker reads of #2: #1, which waits for review,
// blocks it alone, with its open pull request #45, which a person
// connected to it.
func readingOn45() crew.StackReading {
	return crew.StackReading{Blocker: crew.Some(crew.StackBlocker{
		ID: issueID("1"), Ref: "#1", URL: "https://example.test/issues/1", States: []crew.State{readyToReview},
		PullRequests: []crew.StackPullRequest{{
			Ref: "#45", URL: "https://example.test/pull/45", Head: blockerBranch, Base: "main", Connected: true,
		}},
	})}
}

// stackTracker is a StackTracker holding #1, waiting for review, #2,
// blocked by it in ready, and #3 in ready, with #2's reading on #45.
func stackTracker() fake.StackTracker {
	tr := fake.NewStackTracker(issue(1, readyToReview), blockedIssue(2, ready), issue(3, ready))
	tr.SetStack("2", readingOn45())
	return tr
}

// stopAndWait stops r's engine and waits for its Run to return.
func (r *rig) stopAndWait() {
	r.t.Helper()
	r.engine.Stop()
	if _, err := r.wait(); err != nil {
		r.t.Fatalf("Run: %v", err)
	}
}

// basesByWorkspace returns the base each workspace of w was made from, by
// the workspace's name.
func basesByWorkspace(w *fake.Workspace) map[crew.WorkspaceName]string {
	out := map[crew.WorkspaceName]string{}
	bases := w.Bases()
	for i, s := range w.Spaces() {
		out[s.Workspace.Name] = bases[i]
	}
	return out
}

// KTD1, KTD8, F1: a listing reads the stack of the blocked issue in a
// stacking rule's ready label alone; the rule takes it with a workspace
// made from its blocker's pull request's branch, and its session starts
// with the stacked prompt and the stack in its run. The unblocked issue
// beside it is taken from the default branch, as today.
func TestF1AStackingRuleTakesABlockedIssueOnItsBlockersPullRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := stackTracker()
		cfg := config(t, tr, stacking)
		r := start(t, cfg)
		sessions := r.sessions(2)

		if got, want := tr.StackReads(), [][]crew.IssueID{{issueID("2")}}; !reflect.DeepEqual(got, want) {
			t.Errorf("stack reads = %v, want %v", got, want)
		}
		wantBases := map[crew.WorkspaceName]string{"issue-2-implement": blockerBranch, "issue-3-implement": ""}
		if got := basesByWorkspace(fakeWorkspace(t, cfg)); !reflect.DeepEqual(got, wantBases) {
			t.Errorf("workspace bases = %v, want %v", got, wantBases)
		}
		stacked := sessions["issue-2-implement"].Run()
		if want := "Implement #2 on " + blockerBranch + " after #1's #45"; stacked.Prompt != want {
			t.Errorf("#2's prompt = %q, want %q", stacked.Prompt, want)
		}
		if stacked.Base != blockerBranch || stacked.Stack != crew.Some(stackOn45) {
			t.Errorf("#2's run has base %q and stack %+v, want %q and %+v",
				stacked.Base, stacked.Stack, blockerBranch, stackOn45)
		}
		plain := sessions["issue-3-implement"].Run()
		if plain.Prompt != "Implement #3 on main" || plain.Base != "main" || plain.Stack != (crew.Optional[crew.Stack]{}) {
			t.Errorf("#3's run has prompt %q, base %q and stack %+v, want it on main unstacked",
				plain.Prompt, plain.Base, plain.Stack)
		}

		r.stopAndWait()
	})
}

// KTD1: a listing reads no stack when no blocked issue waits alone in a
// stacking rule's ready label: an unblocked issue in it, or a blocked one
// in a rule without stacking, which waits as today.
func TestAListingWithoutCandidatesReadsNoStack(t *testing.T) {
	for _, tt := range []struct {
		name  string
		rule  crew.Rule
		issue crew.Issue
	}{
		{"unblocked issue", stacking, issue(1, ready)},
		{"rule without stacking", develop, blockedIssue(1, ready)},
		{"blocked issue in two labels", stacking, blockedIssue(1, ready, readyToReview)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				tr := fake.NewStackTracker(tt.issue)
				r := start(t, config(t, tr, tt.rule))
				synctest.Wait()
				r.stopAndWait()

				if got := tr.StackReads(); len(got) != 0 {
					t.Errorf("stack reads = %v, want none", got)
				}
			})
		})
	}
}

// KTD1: with a tracker that cannot read stacks, a blocked issue in a
// stacking rule's ready label waits there, as a blocked issue does today.
func TestABlockedIssueWaitsWithATrackerThatReadsNoStack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(blockedIssue(2, ready))
		cfg := config(t, tr, stacking)
		r := start(t, cfg)
		synctest.Wait()
		r.stopAndWait()

		if got := states(t, tr, "2"); !slices.Equal(got, []crew.State{ready}) {
			t.Errorf("#2 is in %v, want ready", got)
		}
		if spaces := fakeWorkspace(t, cfg).Spaces(); len(spaces) != 0 {
			t.Errorf("workspaces = %+v, want none", spaces)
		}
	})
}

// KTD1, R4: a read of the stacks that fails still delivers the listing:
// the unblocked issue is taken, the blocked one waits, and the view gets a
// StacksNotFound warning saying why.
func TestAFailedStackReadStillDeliversTheListingWithAWarning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := stackTracker()
		tr.FailStacks(errors.New("gh: HTTP 502"))
		r := start(t, config(t, tr, stacking))
		if s := r.session(); s.Run().Prompt != "Implement #3 on main" {
			t.Errorf("the session started has prompt %q, want #3's", s.Run().Prompt)
		}
		synctest.Wait()
		if got := states(t, tr, "2"); !slices.Equal(got, []crew.State{ready}) {
			t.Errorf("#2 is in %v, want ready", got)
		}
		r.stopAndWait()

		var warnings []core.StacksNotFound
		for _, e := range r.events() {
			if w, ok := e.(core.StacksNotFound); ok {
				warnings = append(warnings, w)
			}
		}
		if len(warnings) != 1 || !strings.Contains(warnings[0].Reason, "gh: HTTP 502") {
			t.Errorf("StacksNotFound = %+v, want one saying gh: HTTP 502", warnings)
		}
	})
}

// R8: a shell action and a route's shell step of a stacked run get its
// base and stack, as its session does; an unstacked run's get the default
// branch and no stack.
func TestShellActionsAndRouteShellStepsGetTheRunsBaseAndStack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr, sh := stackTracker(), fake.NewShell()
		rule := stacking
		rule.Actions = []crew.Action{stacking.Actions[0], shellAction("check", "./check")}
		rule.Routes = []crew.Route{
			{Name: crew.PassedRoute, Steps: []crew.Step{
				crew.ShellStep{Name: "notify", Shell: crew.ShellSpec{Script: "./notify"}},
				crew.MoveStep{To: readyToReview},
			}},
			develop.Routes[1],
		}
		cfg := config(t, tr, rule)
		cfg.Shell = sh
		r := start(t, cfg)
		for _, s := range r.sessions(2) {
			s.End(port.SessionEnd{Succeeded: true, Reason: "done"})
		}
		synctest.Wait()
		r.stopAndWait()

		type got struct {
			base  string
			stack crew.Optional[crew.Stack]
		}
		byScript := map[string]got{}
		for _, s := range sh.Runs() {
			byScript[s.Branch+" "+string(s.Name)] = got{s.Base, s.Stack}
		}
		stacked, plain := got{blockerBranch, crew.Some(stackOn45)}, got{"main", crew.Optional[crew.Stack]{}}
		want := map[string]got{
			"crew/issue-2-implement check": stacked, "crew/issue-2-implement notify": stacked,
			"crew/issue-3-implement check": plain, "crew/issue-3-implement notify": plain,
		}
		if !reflect.DeepEqual(byScript, want) {
			t.Errorf("scripts' base and stack = %+v, want %+v", byScript, want)
		}
	})
}
