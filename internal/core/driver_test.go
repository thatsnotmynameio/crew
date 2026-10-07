package core_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The rules' states in these tests, as label text.
const (
	ready          crew.State = "ready"
	inProgress     crew.State = "in progress"
	readyToReview  crew.State = "ready to review"
	inReview       crew.State = "in review"
	needsAttention crew.State = "needs attention"
	readyToMerge   crew.State = "ready to merge"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// draft is the rules of your draft config (KTD5).
func draft() []crew.Rule {
	return []crew.Rule{
		{
			Name:   "implement",
			Labels: crew.Labels{Ready: ready, Running: inProgress, Success: readyToReview, Failure: needsAttention},
			Actions: []crew.Action{
				{Name: "acceptance", Prompt: "Implement test acceptance for issue {{.Issue.Ref}}"},
				{Name: "development", Prompt: "Implement development for issue {{.Issue.Ref}}"},
			},
		},
		{
			Name:    "review",
			Labels:  crew.Labels{Ready: readyToReview, Running: inReview, Success: readyToMerge, Failure: needsAttention},
			Actions: []crew.Action{{Name: "custom_review", Prompt: "Review implementation for issue {{.Issue.Ref}}"}},
		},
	}
}

// issue returns an issue keyed key, opened minute minutes after t0.
func issue(key string, minute int, states ...crew.State) crew.Issue {
	return crew.Issue{
		Key: key, Ref: "#" + key, Title: "Issue " + key, URL: "https://example.com/issues/" + key,
		Created: t0.Add(time.Duration(minute) * time.Minute), States: states,
	}
}

// space is the workspace an engine would create for key and action.
func space(key, action string) core.WorkspaceReady {
	name := "issue-" + key + "-" + action
	return core.WorkspaceReady{
		IssueKey: key, Action: action, Workspace: name, Dir: "/repo/.crew/worktrees/" + name,
		Branch: "crew/" + name, Log: ".crew/logs/" + name + ".log",
	}
}

// driver feeds a model inputs one second apart, as the engine would stamp them.
type driver struct {
	t   *testing.T
	m   *core.Model
	now time.Time
}

func newDriver(t *testing.T, rules []crew.Rule, maxParallel int) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(rules, maxParallel), now: t0}
}

func (d *driver) send(in core.Input) ([]core.Command, []core.Event) {
	d.now = d.now.Add(time.Second)
	return d.m.Update(in.Stamped(d.now))
}

// settle answers cmds as a healthy engine would: moves succeed, workspaces
// are created and sessions start. Listings and stops are left unanswered.
func (d *driver) settle(cmds []core.Command) {
	d.t.Helper()
	for len(cmds) > 0 {
		var next []core.Command
		for _, c := range cmds {
			var out []core.Command
			switch c := c.(type) {
			case core.Move:
				out, _ = d.send(core.CallResult{ID: c.ID, Result: core.ResultDone})
			case core.ReportFailure:
				out, _ = d.send(core.CallResult{ID: c.ID, Result: core.ResultDone})
			case core.CreateWorkspace:
				out, _ = d.send(space(c.Issue.Key, c.Action))
			case core.StartSession:
				out, _ = d.send(core.SessionStarted{IssueKey: c.IssueKey, Action: c.Action})
			}
			next = append(next, out...)
		}
		cmds = next
	}
}

// poll ticks and answers the listing with issues.
func (d *driver) poll(issues ...crew.Issue) ([]core.Command, []core.Event) {
	d.t.Helper()
	cmds, _ := d.send(core.Tick{})
	if len(cmds) == 0 {
		d.t.Fatalf("tick issued no listing")
	}
	return d.send(core.IssuesListed{Issues: issues})
}

// running takes issues in ready and starts all their sessions.
func (d *driver) running(issues ...crew.Issue) {
	d.t.Helper()
	cmds, _ := d.poll(issues...)
	d.settle(cmds)
}

// noIDs returns cmds with the call IDs zeroed, so they compare by content.
func noIDs(cmds []core.Command) []core.Command {
	out := make([]core.Command, 0, len(cmds))
	for _, c := range cmds {
		switch c := c.(type) {
		case core.Move:
			c.ID = 0
			out = append(out, c)
		case core.ReportFailure:
			c.ID = 0
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return out
}

// moveID returns the ID of the move of issue key in cmds.
func moveID(t *testing.T, cmds []core.Command, key string) core.CallID {
	t.Helper()
	for _, c := range cmds {
		if m, ok := c.(core.Move); ok && m.IssueKey == key {
			return m.ID
		}
	}
	t.Fatalf("no move of %s in %#v", key, cmds)
	return 0
}

// reportID returns the ID of the failure report of issue key in cmds.
func reportID(t *testing.T, cmds []core.Command, key string) core.CallID {
	t.Helper()
	for _, c := range cmds {
		if r, ok := c.(core.ReportFailure); ok && r.Report.IssueKey == key {
			return r.ID
		}
	}
	t.Fatalf("no failure report of %s in %#v", key, cmds)
	return 0
}

func wantCommands(t *testing.T, got []core.Command, want ...core.Command) {
	t.Helper()
	if want == nil {
		want = []core.Command{}
	}
	if g := noIDs(got); !reflect.DeepEqual(g, want) {
		t.Fatalf("commands:\n got %#v\nwant %#v", g, want)
	}
}

func hasEvent(t *testing.T, events []core.Event, want core.Event) {
	t.Helper()
	for _, e := range events {
		if reflect.DeepEqual(e, want) {
			return
		}
	}
	t.Fatalf("no event %#v in %#v", want, events)
}

func claimOf(t *testing.T, m *core.Model, key string) core.Claim {
	t.Helper()
	for _, iv := range m.View().Issues {
		if iv.Issue.Key == key {
			return iv.Claim
		}
	}
	t.Fatalf("issue %s is not held", key)
	return 0
}

func wantHeld(t *testing.T, m *core.Model, keys ...string) {
	t.Helper()
	issues := m.View().Issues
	got := make([]string, 0, len(issues))
	for _, iv := range issues {
		got = append(got, iv.Issue.Key)
	}
	if !slices.Equal(got, keys) {
		t.Fatalf("held issues: got %v, want %v", got, keys)
	}
}

func failed(reason string) crew.Outcome {
	return crew.Outcome{Succeeded: false, Reason: crew.NewSessionText(reason)}
}

var succeeded = crew.Outcome{Succeeded: true, Reason: crew.NewSessionText("done")}

// issueKey returns the key of the issue c concerns.
func issueKey(c core.Command) string {
	switch c := c.(type) {
	case core.Move:
		return c.IssueKey
	case core.ReportFailure:
		return c.Report.IssueKey
	case core.CreateWorkspace:
		return c.Issue.Key
	case core.StartSession:
		return c.IssueKey
	case core.StopSession:
		return c.IssueKey
	}
	return ""
}

// judgedNeedingAttention runs #1 to a failed verdict and returns the verdict
// commands, both in flight.
func judgedNeedingAttention(d *driver) []core.Command {
	d.running(issue("1", 1, ready))
	d.send(core.SessionEnded{IssueKey: "1", Action: "acceptance", Outcome: failed("broke")})
	cmds, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "development", Outcome: succeeded})
	return cmds
}

func wantEvents(t *testing.T, got []core.Event, want ...core.Event) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\n got %#v\nwant %#v", got, want)
	}
}
