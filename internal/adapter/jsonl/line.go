package jsonl

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// version is the journal's line version crew writes and reads: one line
// per run event. Later events and fields join it as types and keys a line
// may leave out, never as a new version.
const version = 3

// The values of event, on the lines of an action's start and end, which
// tools such as jq select on.
const (
	eventStarted = "started"
	eventEnded   = "ended"
)

// line is one line of the journal. Its keys are a stable format, so other
// tools can read it: where a line holds a value an earlier version held,
// it keeps that version's key. A value the line does not hold is left out,
// never written as zero, so a reported zero stays apart from no report.
type line struct {
	head
	place
	outcome
	session
	usage
	pullRequest
	taken
	route
}

// head is what every line holds. Event is set on an action's start and end
// lines only, and Run is the crew process that wrote the line: neither is
// part of any run event, and the reader ignores both. Rule keeps the name
// stage, from before rules were called stages.
type head struct {
	Version int            `json:"v"`
	Type    string         `json:"type,omitempty"`
	Event   string         `json:"event,omitempty"`
	Time    time.Time      `json:"time"`
	Run     string         `json:"run,omitempty"`
	RuleRun crew.RuleRunID `json:"rule_run,omitempty"`
	Issue   string         `json:"issue"`
	Ref     string         `json:"ref"`
	Rule    crew.RuleName  `json:"stage"`
}

// place is the action an event is about, the bot it acts as, the login a
// session acts as and whether it may ask a question, the worktree the run
// works in and the states a move went between.
type place struct {
	Action    crew.ActionName    `json:"action,omitempty"`
	Bot       crew.BotName       `json:"bot,omitempty"`
	Login     string             `json:"login,omitempty"`
	Asks      bool               `json:"asks,omitempty"`
	Workspace crew.WorkspaceName `json:"workspace,omitempty"`
	Branch    string             `json:"branch,omitempty"`
	Log       string             `json:"log,omitempty"`
	Resumed   bool               `json:"resumed,omitempty"`
	From      crew.State         `json:"from,omitempty"`
	To        crew.State         `json:"to,omitempty"`
}

// outcome is how an action, its session or its script ended: the verdict
// it gave, where the verdict leads, and a script's exit status.
type outcome struct {
	Succeeded  *bool        `json:"succeeded,omitempty"`
	Reason     string       `json:"reason,omitempty"`
	Cause      string       `json:"cause,omitempty"`
	Verdict    crew.Verdict `json:"verdict,omitempty"`
	Target     string       `json:"target,omitempty"`
	ExitStatus *int         `json:"exit_status,omitempty"`
}

// session is when an action's session started, and how long it took up to
// the action's end; both are left out when no session started.
type session struct {
	SessionStarted *time.Time `json:"session_started,omitempty"`
	DurationMS     *int64     `json:"duration_ms,omitempty"`
}

// usage is what a session reported it used; a value it did not report is
// left out.
type usage struct {
	CostUSD          *float64 `json:"cost_usd,omitempty"`
	InputTokens      *int64   `json:"input_tokens,omitempty"`
	OutputTokens     *int64   `json:"output_tokens,omitempty"`
	CacheReadTokens  *int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int64   `json:"cache_write_tokens,omitempty"`
	Turns            *int     `json:"turns,omitempty"`
	Models           []string `json:"models,omitempty"`
}

// pullRequest is what the lookup of the run's pull requests found.
type pullRequest struct {
	PullRequest    string `json:"pull_request,omitempty"`
	PullRequestURL string `json:"pull_request_url,omitempty"`
	Lookup         string `json:"pull_request_lookup,omitempty"`
}

// The values of pull_request_lookup.
const (
	lookupFound       = "found"
	lookupNone        = "none"
	lookupNotLookedUp = "not looked up"
)

// taken is the issue as a rule took it, the run that rule run continues,
// its actions, how it starts and the open questions it inherits.
type taken struct {
	Title     string         `json:"title,omitempty"`
	URL       string         `json:"url,omitempty"`
	Created   *time.Time     `json:"created,omitempty"`
	Priority  int            `json:"priority,omitempty"`
	States    []crew.State   `json:"states,omitempty"`
	Blocked   bool           `json:"blocked,omitempty"`
	Kind      string         `json:"kind,omitempty"`
	Continues crew.RuleRunID `json:"continues,omitempty"`
	Actions   []takenAction  `json:"actions,omitempty"`
	Start     *start         `json:"start,omitempty"`
	Questions []question     `json:"questions,omitempty"`
}

// question is one open question: the rule run whose session may have
// asked it, its action and the login it acted as.
type question struct {
	RuleRun crew.RuleRunID  `json:"rule_run"`
	Action  crew.ActionName `json:"action"`
	Login   string          `json:"login,omitempty"`
}

// The values of kind.
const (
	kindIssue       = "issue"
	kindPullRequest = "pull_request"
)

// takenAction is one action of a take.
type takenAction struct {
	Name crew.ActionName `json:"name"`
}

// start is how a run starts: its kind, and, for a resume, the worktree it
// reopens, its log, the action it starts at, the route and reason the run
// it continues ended with, and that run's latest session and its bot.
type start struct {
	Kind      string             `json:"kind"`
	Workspace crew.WorkspaceName `json:"workspace,omitempty"`
	Branch    string             `json:"branch,omitempty"`
	Log       string             `json:"log,omitempty"`
	Action    crew.ActionName    `json:"action,omitempty"`
	Route     crew.RouteName     `json:"route,omitempty"`
	Reason    string             `json:"reason,omitempty"`
	Session   crew.ActionName    `json:"session,omitempty"`
	Bot       crew.BotName       `json:"bot,omitempty"`
}

// The values of a start's kind.
const (
	startFresh         = "fresh"
	startAt            = "at"
	startPassedRoute   = "passed_route"
	startWithoutAction = "without_action"
)

// route is the route a run chose with its steps, the index of the step a
// line is about, and how that step settled.
type route struct {
	Route   crew.RouteName `json:"route,omitempty"`
	Steps   []step         `json:"steps,omitempty"`
	Step    *int           `json:"step,omitempty"`
	Settled string         `json:"outcome,omitempty"`
}

// step is one step of a route: its kind, the state a move moves the item
// to, and the shell action a shell step runs.
type step struct {
	Kind  string          `json:"kind"`
	To    crew.State      `json:"to,omitempty"`
	Shell crew.ActionName `json:"shell,omitempty"`
}

// stepKinds returns the names of the kinds of a route's steps on the wire.
func stepKinds() map[crew.StepKind]string {
	return map[crew.StepKind]string{
		crew.StepMove: "move", crew.StepClose: "close", crew.StepComment: "comment", crew.StepReport: "report",
		crew.StepShell: "shell",
	}
}

// causes returns the names of the failure causes on the wire.
func causes() map[crew.FailureCause]string {
	return map[crew.FailureCause]string{
		crew.CauseSession: "session", crew.CauseStopped: "stopped",
		crew.CauseWorkspace: "workspace", crew.CauseStart: "start", crew.CausePrompt: "prompt",
		crew.CauseShell: "shell", crew.CauseVerdict: "verdict", crew.CauseStoppedBeforeStart: "stopped_before_start",
		crew.CauseTimeUp: "time_up",
	}
}

// named returns the key of names whose name is name, and whether one has
// it.
func named[K comparable](names map[K]string, name string) (K, bool) {
	for k, n := range names {
		if n == name {
			return k, true
		}
	}
	var zero K
	return zero, false
}

// eventHead returns the head of the event l holds, its issue in
// repository.
func (l line) eventHead(repository crew.RepositoryID) crew.EventHead {
	return crew.EventHead{
		Run: l.RuleRun, At: l.Time, IssueID: crew.IssueID{Repository: repository, Key: l.Issue}, IssueRef: l.Ref,
		Rule: l.Rule,
	}
}

// headLine returns the line of an event with head h, of type kind, written
// in the crew process run.
func headLine(h crew.EventHead, kind, run string) line {
	return line{
		Version: version, Type: kind, Time: h.At.UTC(), Run: run, RuleRun: h.Run, Issue: h.IssueID.Key,
		Ref: h.IssueRef, Rule: h.Rule,
	}
}

// outcomeOf returns the keys of an action's end.
func outcomeOf(end crew.ActionEnd) outcome {
	o := outcome{Succeeded: new(end.Outcome().Succeeded), Reason: end.Outcome().Reason.String()}
	if failed, ok := end.(crew.EndFailed); ok {
		o.Cause = causes()[failed.Cause]
	}
	return o
}

// end returns the action's end o holds: a session's failure when its
// cause is none crew knows.
func (o outcome) end() crew.ActionEnd {
	reason := crew.NewSessionText(o.Reason)
	if o.Succeeded != nil && *o.Succeeded {
		return crew.EndSucceeded{Reason: reason}
	}
	cause, _ := named(causes(), o.Cause)
	return crew.EndFailed{Reason: reason, Cause: cause}
}

// usageOf returns the keys of u.
func usageOf(u crew.Usage) usage {
	var out usage
	if cost, ok := u.Cost.Get(); ok {
		out.CostUSD = new(cost)
	}
	if t, ok := u.Tokens.Get(); ok {
		out.InputTokens, out.OutputTokens = new(t.Input), new(t.Output)
		out.CacheReadTokens, out.CacheWriteTokens = new(t.CacheRead), new(t.CacheWrite)
	}
	if turns, ok := u.Turns.Get(); ok {
		out.Turns = new(turns)
	}
	out.Models = u.Models
	return out
}

// reported returns the usage u holds. Tokens are reported together or not
// at all, so the input tokens tell whether they were.
func (u usage) reported() crew.Usage {
	var out crew.Usage
	if u.CostUSD != nil {
		out.Cost = crew.Some(*u.CostUSD)
	}
	if u.InputTokens != nil {
		out.Tokens = crew.Some(crew.Tokens{
			Input: *u.InputTokens, Output: deref(u.OutputTokens), CacheRead: deref(u.CacheReadTokens),
			CacheWrite: deref(u.CacheWriteTokens),
		})
	}
	if u.Turns != nil {
		out.Turns = crew.Some(*u.Turns)
	}
	out.Models = u.Models
	return out
}

// deref returns what p points to, or zero when p is nil.
func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// pullRequestOf returns the keys of pr. A pull request not looked up,
// whether the lookup was not asked or did not answer, is "not looked up".
func pullRequestOf(pr crew.PullRequest) pullRequest {
	switch pr := pr.(type) {
	case crew.PullRequestFound:
		return pullRequest{PullRequest: pr.Ref, PullRequestURL: pr.URL, Lookup: lookupFound}
	case crew.PullRequestNone:
		return pullRequest{Lookup: lookupNone}
	case crew.PullRequestNotLookedUp:
	}
	return pullRequest{Lookup: lookupNotLookedUp}
}

// found returns the pull request p holds; nil when it holds none.
func (p pullRequest) found() crew.PullRequest {
	switch p.Lookup {
	case lookupFound:
		return crew.PullRequestFound{Ref: p.PullRequest, URL: p.PullRequestURL}
	case lookupNone:
		return crew.PullRequestNone{}
	case lookupNotLookedUp:
		return crew.PullRequestNotLookedUp{}
	}
	return nil
}

// timeOf returns t in UTC, for a key that holds a time.
func timeOf(t time.Time) *time.Time {
	return new(t.UTC())
}
