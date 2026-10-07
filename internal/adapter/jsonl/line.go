package jsonl

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The journal's line versions. version is the one crew writes: one line
// per run event. version1 lines record an action's start and end only.
const (
	version1 = 1
	version  = 2
)

// The values of event, on the lines of an action's start and end, which
// tools such as jq select on.
const (
	eventStarted = "started"
	eventEnded   = "ended"
)

// line is one line of the journal, of either version. Its keys are a
// stable format, so other tools can read it: where a version 2 line holds
// a value a version 1 line held, it keeps that line's key. A value the line
// does not hold is left out, never written as zero, so a reported zero
// stays apart from no report.
type line struct {
	head
	place
	outcome
	session
	usage
	pullRequest
	taken
	ending
}

// head is what every line holds. Event is set on an action's start and end
// lines only, and Run is the crew process that wrote the line: neither is
// part of any run event, and the reader of version 2 lines ignores both.
// Rule keeps the name stage, from before rules were called stages.
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

// place is the action an event is about, the workspace it works in, the
// states a move went between and the check that ran.
type place struct {
	Action    crew.ActionName    `json:"action,omitempty"`
	Workspace crew.WorkspaceName `json:"workspace,omitempty"`
	Branch    string             `json:"branch,omitempty"`
	Log       string             `json:"log,omitempty"`
	Resumed   bool               `json:"resumed,omitempty"`
	Opened    *time.Time         `json:"opened,omitempty"`
	From      crew.State         `json:"from,omitempty"`
	To        crew.State         `json:"to,omitempty"`
	Check     crew.CheckName     `json:"check,omitempty"`
	Passed    *bool              `json:"passed,omitempty"`
}

// session is when an action's session started, and how long it took up to
// the action's end; both are left out when no session started.
type session struct {
	SessionStarted *time.Time `json:"session_started,omitempty"`
	DurationMS     *int64     `json:"duration_ms,omitempty"`
}

// ending is the failed actions of a run's ending.
type ending struct {
	Failures []failure `json:"failures,omitempty"`
}

// outcome is how an action or its session ended.
type outcome struct {
	Succeeded *bool  `json:"succeeded,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Cause     string `json:"cause,omitempty"`
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

// pullRequest is what the lookup of an action's pull request found.
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

// taken is the issue as a rule took it, the run that rule run continues and
// its actions.
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
}

// The values of kind.
const (
	kindIssue       = "issue"
	kindPullRequest = "pull_request"
)

// takenAction is one action of a take, with the resume point it inherited:
// its workspace, branch, log and reason, all left out when it has none.
type takenAction struct {
	Name      crew.ActionName    `json:"name"`
	Workspace crew.WorkspaceName `json:"workspace,omitempty"`
	Branch    string             `json:"branch,omitempty"`
	Log       string             `json:"log,omitempty"`
	Reason    string             `json:"reason,omitempty"`
}

// failure is one failed action of a run's ending.
type failure struct {
	Action    crew.ActionName    `json:"action"`
	Workspace crew.WorkspaceName `json:"workspace,omitempty"`
	Log       string             `json:"log,omitempty"`
}

// The names of the failure causes on the wire.
const (
	causeSession   = "session"
	causeCheck     = "check"
	causeStopped   = "stopped"
	causeWorkspace = "workspace"
	causeStart     = "start"
	causePrompt    = "prompt"
)

// causeName returns the name of cause on the wire.
func causeName(cause crew.FailureCause) string {
	switch cause {
	case crew.CauseCheck:
		return causeCheck
	case crew.CauseStopped:
		return causeStopped
	case crew.CauseWorkspace:
		return causeWorkspace
	case crew.CauseStart:
		return causeStart
	case crew.CausePrompt:
		return causePrompt
	default:
		return causeSession
	}
}

// causeNamed returns the cause named name on the wire: a session's when
// the name is none crew knows.
func causeNamed(name string) crew.FailureCause {
	switch name {
	case causeCheck:
		return crew.CauseCheck
	case causeStopped:
		return crew.CauseStopped
	case causeWorkspace:
		return crew.CauseWorkspace
	case causeStart:
		return crew.CauseStart
	case causePrompt:
		return crew.CausePrompt
	default:
		return crew.CauseSession
	}
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
		o.Cause = causeName(failed.Cause)
	}
	return o
}

// end returns the action's end o holds.
func (o outcome) end() crew.ActionEnd {
	reason := crew.NewSessionText(o.Reason)
	if o.Succeeded != nil && *o.Succeeded {
		return crew.EndSucceeded{Reason: reason}
	}
	return crew.EndFailed{Reason: reason, Cause: causeNamed(o.Cause)}
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
// whether the lookup was not asked or did not answer, is "not looked up",
// as version 1 wrote it.
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
