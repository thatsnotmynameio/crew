package core_test

import (
	"encoding/binary"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"
	"uuid"

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

// draft is the rules of your draft config (KTD5): implement runs the
// sessions acceptance then development, one after the other in one
// workspace, and review runs custom_review. Each moves the issue on through
// passed, and reports and moves it to needs attention through failed.
func draft() []crew.Rule {
	return []crew.Rule{
		{
			Name:   "implement",
			Labels: crew.Labels{Ready: ready, Running: inProgress},
			Actions: []crew.Action{
				sessionAction("acceptance", "Implement test acceptance for issue {{.Issue.Ref}}"),
				sessionAction("development", "Implement development for issue {{.Issue.Ref}}"),
			},
			Routes: routes(readyToReview, needsAttention),
		},
		{
			Name:    "review",
			Labels:  crew.Labels{Ready: readyToReview, Running: inReview},
			Actions: []crew.Action{sessionAction("custom_review", "Review implementation for issue {{.Issue.Ref}}")},
			Routes:  routes(readyToMerge, needsAttention),
		},
	}
}

// sessionAction is a session action named name, whose prompt is text.
func sessionAction(name crew.ActionName, text string) crew.Action {
	return crew.Action{Name: name, Kind: crew.SessionSpec{Prompt: parsedPrompt(name, text)}}
}

// shellAction is a shell action named name, which runs script.
func shellAction(name crew.ActionName, script string) crew.Action {
	return crew.Action{Name: name, Kind: crew.ShellSpec{Script: script}}
}

// routes are a rule's passed route, which moves the issue to passed, and
// its failed route, which reports and moves it to failed.
func routes(passed, failed crew.State) []crew.Route {
	return []crew.Route{
		{Name: crew.PassedRoute, Steps: []crew.Step{crew.MoveStep{To: passed}}},
		{Name: crew.FailedRoute, Steps: []crew.Step{crew.ReportStep{}, crew.MoveStep{To: failed}}},
	}
}

// withSpec returns rules with the session of the action named action of
// rule ri changed by change.
func withSpec(rules []crew.Rule, ri int, action crew.ActionName, change func(*crew.SessionSpec)) []crew.Rule {
	for i, a := range rules[ri].Actions {
		if spec, ok := a.Kind.(crew.SessionSpec); ok && a.Name == action {
			change(&spec)
			rules[ri].Actions[i].Kind = spec
		}
	}
	return rules
}

// issue returns an issue keyed key, opened minute minutes after t0.
func issue(key string, minute int, states ...crew.State) crew.Issue {
	return crew.NewIssue(crew.IssueData{
		ID: issueID(key), Ref: "#" + key, Title: "Issue " + key, URL: "https://example.com/issues/" + key,
		Created: t0.Add(time.Duration(minute) * time.Minute), States: states,
	})
}

// blockedIssue returns i blocked by an open issue.
func blockedIssue(i crew.Issue) crew.Issue {
	d := i.Data()
	d.Blocked = true
	return crew.NewIssue(d)
}

// pullRequest returns i as a pull request.
func pullRequest(i crew.Issue) crew.Issue {
	d := i.Data()
	d.Kind = crew.KindPullRequest
	return crew.NewIssue(d)
}

// parsedPrompt parses text as the prompt of the action named action, and
// panics when it does not parse, since tests build their rules from known
// prompts.
func parsedPrompt(action crew.ActionName, text string) crew.Prompt {
	p, err := crew.ParsePrompt(action, text)
	if err != nil {
		panic(err)
	}
	return p
}

// issueID returns the id of the issue keyed key, in no repository.
func issueID(key string) crew.IssueID { return crew.IssueID{Key: key} }

// space is the workspace an engine would create for the run of rule on
// key. It names no run: the driver fills in the run that holds the issue.
func space(key string, rule crew.RuleName) core.WorkspaceReady {
	name := "issue-" + key + "-" + string(rule)
	return core.WorkspaceReady{
		IssueID: issueID(key), Workspace: crew.WorkspaceName(name), Dir: "/repo/.crew/worktrees/" + name,
		Branch: "crew/" + name, Log: ".crew/logs/" + name + ".log",
	}
}

// driver feeds a model inputs one second apart, as the engine would stamp
// them, each with the next sequential seed, so run ids stay deterministic,
// and keeps every event the model emitted.
type driver struct {
	t   *testing.T
	m   *core.Model
	now time.Time
	// inputs counts the inputs sent: the nth gets seed(n).
	inputs uint64
	// listed is the seed stamped on the last IssuesListed sent, from which
	// the runs it took got their ids.
	listed uuid.UUID
	events []core.Published
	// recorded holds the events of every Record the model issued, in
	// order: the run journal a healthy engine would have written.
	recorded []crew.RunEvent
}

// seed returns the nth sequential seed: a UUID whose last bytes encode n.
func seed(n uint64) uuid.UUID {
	var u uuid.UUID
	binary.BigEndian.PutUint64(u[8:], n)
	return u
}

func newDriver(t *testing.T, rules []crew.Rule, maxParallel int) *driver {
	t.Helper()
	return &driver{t: t, m: core.New(rules, maxParallel), now: t0}
}

// send stamps in and feeds it to the model. A run input, or a said of a
// tick, that names no run is filled in with the last run that took its
// issue, the run whose command an engine's answer would carry.
func (d *driver) send(in core.Input) ([]core.Command, []core.Published) {
	in = d.named(in)
	d.now = d.now.Add(time.Second)
	d.inputs++
	stamp := seed(d.inputs)
	if _, ok := in.(core.IssuesListed); ok {
		d.listed = stamp
	}
	cmds, events := d.m.Update(in.Stamped(d.now, stamp))
	d.events = append(d.events, events...)
	d.recorded = append(d.recorded, records(cmds)...)
	return cmds, events
}

// run returns the id of the last run that took the issue identified by id,
// as its published take named it; empty when no run took it.
func (d *driver) run(id crew.IssueID) crew.RuleRunID {
	for _, e := range slices.Backward(d.events) {
		if taken, ok := e.(crew.RunTaken); ok && taken.IssueID == id {
			return taken.Run
		}
	}
	return ""
}

// named returns in with its run filled in, when it names none, as send
// does.
func (d *driver) named(in core.Input) core.Input {
	fill := d.fill
	switch in := in.(type) {
	case core.RunInput:
		return d.namedRun(in)
	case core.Tick:
		in.Said = slices.Clone(in.Said)
		for i := range in.Said {
			fill(&in.Said[i].Run, in.Said[i].IssueID)
		}
		return in
	case core.SchedulerInput:
	}
	return in
}

// namedRun returns in, an input about a run, with its run filled in, when
// it names none, as named does.
func (d *driver) namedRun(in core.RunInput) core.Input {
	fill := d.fill
	switch in := in.(type) {
	case core.WorkspaceReady:
		fill(&in.Run, in.IssueID)
		return in
	case core.WorkspaceGone:
		fill(&in.Run, in.IssueID)
		return in
	case core.WorkspaceFailed:
		fill(&in.Run, in.IssueID)
		return in
	case core.SessionStarted:
		fill(&in.Run, in.IssueID)
		return in
	case core.SessionFailedToStart:
		fill(&in.Run, in.IssueID)
		return in
	case core.SessionEnded:
		fill(&in.Run, in.IssueID)
		return in
	case core.ShellEnded:
		fill(&in.Run, in.IssueID)
		return in
	case core.StepShellEnded:
		fill(&in.Run, in.IssueID)
		return in
	case core.FunctionEnded:
		fill(&in.Run, in.IssueID)
		return in
	case core.StepFunctionEnded:
		fill(&in.Run, in.IssueID)
		return in
	case core.PullRequestFound:
		fill(&in.Run, in.IssueID)
		return in
	case core.AnswersRead:
		fill(&in.Run, in.IssueID)
		return in
	case core.QuestionRead:
		fill(&in.Run, in.IssueID)
		return in
	case core.ReturnRead:
		fill(&in.Run, in.IssueID)
		return in
	}
	return in
}

// fill fills in run, when empty, with the last run that took the issue
// identified by id.
func (d *driver) fill(run *crew.RuleRunID, id crew.IssueID) {
	if *run == "" {
		*run = d.run(id)
	}
}

// wantReason fails the test unless the last ActionEnded of action on issue
// key carried reason.
func (d *driver) wantReason(key string, action crew.ActionName, reason string) {
	d.t.Helper()
	for _, e := range slices.Backward(d.events) {
		if ended, ok := e.(crew.ActionEnded); ok && ended.IssueID == issueID(key) && ended.Action == action {
			if got := ended.End.Outcome().Reason.String(); got != reason {
				d.t.Errorf("%s of #%s ended with reason %q, want %q", action, key, got, reason)
			}
			return
		}
	}
	d.t.Fatalf("no end of %s of #%s in %#v", action, key, d.events)
}

// settle answers cmds as a healthy engine would: tracker writes succeed,
// workspaces are created and sessions start. Listings, scripts and stops
// are left unanswered.
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
			case core.Comment:
				out, _ = d.send(core.CallResult{ID: c.ID, Result: core.ResultDone})
			case core.Close:
				out, _ = d.send(core.CallResult{ID: c.ID, Result: core.ResultDone})
			case core.Delegate:
				out, _ = d.send(core.CallResult{ID: c.ID, Result: core.ResultDone})
			case core.CreateWorkspace:
				ready := space(c.Issue.ID().Key, c.Rule)
				ready.Run = c.Run
				out, _ = d.send(ready)
			case core.StartSession:
				out, _ = d.send(core.SessionStarted{IssueID: c.IssueID, Run: c.Run, Action: c.Action})
			case core.ListIssues, core.ListBoard, core.ReportStatus, core.ReportPullRequests, core.ReopenWorkspace,
				core.Record, core.StopSession, core.RunShell, core.StopShell, core.RunStepShell, core.StopStepShell,
				core.RunFunction, core.StopFunction, core.RunStepFunction, core.StopStepFunction, core.FindPullRequest,
				core.ReadAnswers, core.ReadQuestion, core.ReadReturn:
				// Left unanswered.
			}
			next = append(next, out...)
		}
		cmds = next
	}
}

// poll ticks and answers the listing with issues.
func (d *driver) poll(issues ...crew.Issue) ([]core.Command, []core.Published) {
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
		switch call := c.(type) {
		case core.Move:
			call.ID = 0
			c = call
		case core.ReportFailure:
			call.ID = 0
			c = call
		case core.Comment:
			call.ID = 0
			c = call
		case core.Close:
			call.ID = 0
			c = call
		case core.Delegate:
			call.ID = 0
			c = call
		case core.ListIssues, core.ListBoard, core.ReportStatus, core.ReportPullRequests, core.CreateWorkspace,
			core.ReopenWorkspace, core.Record, core.StartSession, core.StopSession, core.RunShell, core.StopShell,
			core.RunStepShell, core.StopStepShell, core.RunFunction, core.StopFunction, core.RunStepFunction,
			core.StopStepFunction, core.FindPullRequest, core.ReadAnswers, core.ReadQuestion, core.ReadReturn:
		}
		out = append(out, c)
	}
	return out
}

// moveID returns the ID of the move of issue key in cmds.
func moveID(t *testing.T, cmds []core.Command, key string) core.CallID {
	t.Helper()
	for _, c := range cmds {
		if m, ok := c.(core.Move); ok && m.IssueID.Key == key {
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
		if r, ok := c.(core.ReportFailure); ok && r.Report.IssueID.Key == key {
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

// runHead returns the head of the events of issue key's last run at d.now:
// its run, issue and rule, as its published take named them.
func (d *driver) runHead(key string) crew.EventHead {
	d.t.Helper()
	for _, e := range slices.Backward(d.events) {
		if taken, ok := e.(crew.RunTaken); ok && taken.IssueID == issueID(key) {
			head := taken.EventHead
			head.At = d.now
			return head
		}
	}
	d.t.Fatalf("no take of #%s in %#v", key, d.events)
	return crew.EventHead{}
}

// taken is the event of rule taking it at d.now, as the nth run of d's last
// listing, from one state to another, with actions; it starts fresh.
func (d *driver) taken(n int, it crew.Issue, rule crew.RuleName, from, to crew.State,
	actions ...crew.ActionName,
) crew.RunTaken {
	return crew.RunTaken{
		Run: crew.NewRuleRunID(d.listed, n), At: d.now, IssueID: it.ID(), IssueRef: it.Ref(), Rule: rule,
		Issue: it.Data(), From: from, To: to, Actions: actions, Start: crew.StartFresh{},
	}
}

// end is an action's end as its line shows it: its head, its action and
// its outcome.
type end struct {
	head    crew.EventHead
	action  crew.ActionName
	outcome crew.Outcome
}

// hasEnd fails the test unless events hold the end of an action that shows
// as want.
func hasEnd(t *testing.T, events []core.Published, want end) {
	t.Helper()
	for _, e := range events {
		if ended, ok := e.(crew.ActionEnded); ok {
			got := end{head: ended.EventHead, action: ended.Action, outcome: ended.End.Outcome()}
			if got == want {
				return
			}
		}
	}
	t.Fatalf("no end %#v in %#v", want, events)
}

func hasEvent(t *testing.T, events []core.Published, want core.Published) {
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
		if iv.Issue.ID().Key == key {
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
		got = append(got, iv.Issue.ID().Key)
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
		return c.IssueID.Key
	case core.ReportFailure:
		return c.Report.IssueID.Key
	case core.CreateWorkspace:
		return c.Issue.ID().Key
	case core.StartSession:
		return c.IssueID.Key
	case core.StopSession:
		return c.IssueID.Key
	case core.Comment:
		return c.IssueID.Key
	case core.Close:
		return c.IssueID.Key
	case core.ListIssues, core.ListBoard, core.ReportStatus, core.ReportPullRequests, core.ReopenWorkspace,
		core.Record, core.RunShell, core.StopShell, core.RunStepShell, core.StopStepShell, core.RunFunction,
		core.StopFunction, core.RunStepFunction, core.StopStepFunction, core.FindPullRequest, core.ReadAnswers,
		core.ReadQuestion, core.ReadReturn, core.Delegate:
	}
	return ""
}

// endedNeedingAttention runs #1 to its failed route, as acceptance fails,
// and returns the commands of its first step, the failure report, in
// flight.
func endedNeedingAttention(d *driver) []core.Command {
	d.running(issue("1", 1, ready))
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID("1"), Action: "acceptance", Outcome: failed("broke")})
	return cmds
}

// failureOf is the report of issue key's run of rule through failed, ended
// by action's failed verdict in the run's one workspace.
func failureOf(key string, rule crew.RuleName, action crew.ActionName) core.ReportFailure {
	name := "issue-" + key + "-" + string(rule)
	return core.ReportFailure{Report: crew.FailureReport{
		IssueID: issueID(key), IssueRef: "#" + key, Rule: rule, Route: crew.FailedRoute,
		Failures: []crew.ActionFailure{{
			Action: action, Verdict: crew.Failed, Workspace: crew.WorkspaceName(name), Log: ".crew/logs/" + name + ".log",
		}},
	}}
}

// stepEnded is the event of the step at index step of issue key's last run
// settling as outcome, at d.now: the step of the route its published
// RouteChosen named, taking the issue from the running label its take
// moved it to when the step moves or closes it.
func (d *driver) stepEnded(key string, step int, outcome crew.StepOutcome) core.RouteStepEnded {
	d.t.Helper()
	h := d.runHead(key)
	var taken crew.RunTaken
	var chosen crew.RouteChosen
	for _, e := range d.events {
		switch e := e.(type) {
		case crew.RunTaken:
			if e.Run == h.Run {
				taken = e
			}
		case crew.RouteChosen:
			if e.Run == h.Run {
				chosen = e
			}
		}
	}
	if step >= len(chosen.Steps) {
		d.t.Fatalf("no step %d of #%s's route in %#v", step, key, d.events)
	}
	ended := core.RouteStepEnded{
		At: h.At, IssueID: h.IssueID, IssueRef: h.IssueRef, Rule: h.Rule, Route: chosen.Route, Step: step,
		Plan: chosen.Steps[step], Outcome: outcome,
	}
	if kind := ended.Plan.Kind; kind == crew.StepMove || kind == crew.StepClose {
		ended.From = taken.To
	}
	return ended
}

func wantEvents(t *testing.T, got []core.Published, want ...core.Published) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events:\n got %#v\nwant %#v", got, want)
	}
}

// records returns the events of the Record commands in cmds, in order.
func records(cmds []core.Command) []crew.RunEvent {
	var out []crew.RunEvent
	for _, c := range cmds {
		if r, ok := c.(core.Record); ok {
			out = append(out, r.Event)
		}
	}
	return out
}

// rule returns the rule of issue key's last run, as its published take
// named it.
func (d *driver) rule(key string) crew.RuleName {
	d.t.Helper()
	return d.runHead(key).Rule
}

// ready answers the workspace of issue key's last run as ready, as space
// gives it, and returns the commands that follow.
func (d *driver) ready(key string) []core.Command {
	d.t.Helper()
	cmds, _ := d.send(space(key, d.rule(key)))
	return cmds
}

// session is the StartSession of action for prompt in the workspace space
// gives issue key's last run.
func (d *driver) session(key string, action crew.ActionName, prompt string) core.StartSession {
	d.t.Helper()
	w := space(key, d.rule(key))
	return core.StartSession{
		IssueID: issueID(key), Run: d.run(issueID(key)), Action: action, Dir: w.Dir, Prompt: prompt, Log: w.Log,
	}
}

// ended sends the end of the session of action on issue key with outcome,
// and returns the commands that follow.
func (d *driver) ended(key string, action crew.ActionName, outcome crew.Outcome) []core.Command {
	d.t.Helper()
	cmds, _ := d.send(core.SessionEnded{IssueID: issueID(key), Action: action, Outcome: outcome})
	return cmds
}

// exited is how a script that exited with status ended.
func exited(status int) crew.ShellOutcome {
	return crew.ShellOutcome{Status: crew.Some(status), Reason: crew.NewShellReason("exited " + strconv.Itoa(status))}
}

// runShellOf returns the RunShell in cmds.
func runShellOf(t *testing.T, cmds []core.Command) core.RunShell {
	t.Helper()
	for _, c := range cmds {
		if r, ok := c.(core.RunShell); ok {
			return r
		}
	}
	t.Fatalf("no RunShell in %#v", cmds)
	return core.RunShell{}
}
