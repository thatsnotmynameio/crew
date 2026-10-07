package core

import (
	"time"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// FunctionCall is the call of a RunFunction or a RunStepFunction: Function,
// called at Use, the place of the config it was built for, by Name, the
// function action's or step's name, with Texts, its text parameters
// rendered for the issue by name (KTD-F12). Dir, Log and Branch are the
// run's workspace, its log and its branch, all empty for a run without a
// workspace, whose function the engine logs under the name its workspace
// would have, of the issue and Rule. Bot is the run's bot, the latest
// session's, whom the function acts as on the tracker; empty means you.
type FunctionCall struct {
	Use      crew.FunctionUse
	Name     crew.ActionName
	Function crew.FunctionName
	Texts    map[string]string
	Dir      string
	Log      string
	Rule     crew.RuleName
	IssueRef string
	IssueURL string
	Branch   string
	Bot      crew.BotName
}

// RunFunction asks the engine to call the function of Action, the function
// action at the cursor of the rule run Run. Its result is FunctionEnded,
// carrying Run and Action.
type RunFunction struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
	Call    FunctionCall
}

// StopFunction asks the engine to stop the running function of Action of
// the rule run Run. The function's end still arrives as FunctionEnded.
type StopFunction struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
}

// RunStepFunction asks the engine to call the function of the function
// step at index Step of the route of the rule run Run. Its result is
// StepFunctionEnded, carrying Run and Step.
type RunStepFunction struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Step    int
	Call    FunctionCall
}

// StopStepFunction asks the engine to stop the running function of the
// function step at index Step of the route of the rule run Run. The
// function's end still arrives as StepFunctionEnded.
type StopStepFunction struct {
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Step    int
}

// FunctionEnded is a RunFunction whose function ended: it returned a
// verdict, or an error, ran out of time, was stopped or could not start,
// as its Outcome says.
type FunctionEnded struct {
	At      time.Time
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Action  crew.ActionName
	Outcome crew.FunctionOutcome
}

// StepFunctionEnded is a RunStepFunction whose function ended, as its
// Outcome says.
type StepFunctionEnded struct {
	At      time.Time
	IssueID crew.IssueID
	Run     crew.RuleRunID
	Step    int
	Outcome crew.FunctionOutcome
}

func (RunFunction) command()      {}
func (StopFunction) command()     {}
func (RunStepFunction) command()  {}
func (StopStepFunction) command() {}

func (RunFunction) runCommand()      {}
func (StopFunction) runCommand()     {}
func (RunStepFunction) runCommand()  {}
func (StopStepFunction) runCommand() {}

// Stamped implements Input.
func (i FunctionEnded) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

// Stamped implements Input.
func (i StepFunctionEnded) Stamped(at time.Time, _ uuid.UUID) Input { i.At = at; return i }

func (i FunctionEnded) arrival() time.Time     { return i.At }
func (i StepFunctionEnded) arrival() time.Time { return i.At }

func (i FunctionEnded) ruleRun() crew.RuleRunID     { return i.Run }
func (i StepFunctionEnded) ruleRun() crew.RuleRunID { return i.Run }

// runFunction calls the function of the function action e asked for,
// acting as the bot e names: the run's latest session's.
func (s *step) runFunction(h *heldRun, e crew.ActionFunctionAsked) {
	spec, _ := s.m.rules[h.rule].Action(e.Action).Kind.(crew.FunctionSpec)
	s.command(RunFunction{IssueID: h.id(), Run: h.run.ID(), Action: e.Action, Call: h.call(e.Action, spec, e.Bot)})
}

// call returns the call of spec by the function action or step named
// name, as h's run makes it, acting as bot: in its workspace, when it has
// one, with spec's text parameters rendered for its issue.
func (h *heldRun) call(name crew.ActionName, spec crew.FunctionSpec, bot crew.Bot) FunctionCall {
	w, _ := h.run.Workspace().Get()
	issue := h.run.Issue()
	// The run rendered the texts already, when it asked for the call: the
	// same templates and issue render the same text.
	texts, _ := spec.RenderTexts(issue)
	return FunctionCall{
		Use: spec.Use, Name: name, Function: spec.Function, Texts: texts, Dir: h.dir, Log: w.Log, Rule: h.run.Rule(),
		IssueRef: issue.Ref(), IssueURL: issue.URL(), Branch: w.Workspace.Branch, Bot: bot.Name,
	}
}

// runsItself reports whether a step of kind k is one crew runs itself, a
// shell or a function step, rather than one the outbox delivers.
func runsItself(k crew.StepKind) bool {
	return k == crew.StepShell || k == crew.StepFunction
}
