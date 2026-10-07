package crew

import (
	"fmt"
	"slices"
)

// Verdict is how one action ended, in a name the config writes: Passed,
// Failed, Waiting, or any other name an action's On or a shell action's
// exit codes give. An action's On turns it into a Target.
type Verdict string

// The verdicts crew knows by name. Passed and Failed can end any action;
// Waiting ends a session that asked a question and got no answer.
const (
	Passed  Verdict = "passed"
	Failed  Verdict = "failed"
	Waiting Verdict = "waiting"
)

// maxName is the longest a verdict or route name may be, in bytes.
const maxName = 64

// nextTarget is the word the config writes for the Next target. It is never
// a route's name.
const nextTarget = "next"

// ParseVerdict returns s as a Verdict, or an error naming s when it is not a
// verdict name: a lowercase ASCII letter, then lowercase letters, digits,
// "-" or "_", at most 64 in all. The config's verdict names and the verdict
// a session reports share this one grammar.
func ParseVerdict(s string) (Verdict, error) {
	if err := checkName(s); err != nil {
		return "", fmt.Errorf("verdict %w", err)
	}
	return Verdict(s), nil
}

// ParseRouteName returns s as a RouteName, or an error naming s when it is
// not a verdict name (ParseVerdict) or is "next", the word for the Next
// target.
func ParseRouteName(s string) (RouteName, error) {
	if s == nextTarget {
		return "", fmt.Errorf("route %q: %q names the next action, not a route", s, nextTarget)
	}
	if err := checkName(s); err != nil {
		return "", fmt.Errorf("route %w", err)
	}
	return RouteName(s), nil
}

// checkName returns an error naming s when it is not a verdict name.
func checkName(s string) error {
	switch {
	case s == "":
		return fmt.Errorf("%q: a name cannot be empty", s)
	case len(s) > maxName:
		return fmt.Errorf("%q: a name has at most %d characters", s, maxName)
	case !isLower(s[0]):
		return fmt.Errorf("%q: a name starts with a lowercase letter", s)
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !isLower(c) && !isDigit(c) && c != '-' && c != '_' {
			return fmt.Errorf("%q: a name has only lowercase letters, digits, \"-\" and \"_\"", s)
		}
	}
	return nil
}

func isLower(c byte) bool { return 'a' <= c && c <= 'z' }

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

// Target is where a verdict sends a rule run: Next, or ToRoute.
//
//sumtype:decl
type Target interface {
	target()
}

// Next is the target that runs the rule's next action, or ends the run
// through its PassedRoute after the last one.
type Next struct{}

// ToRoute is the target that ends the run through one of its rule's routes.
type ToRoute struct {
	Route RouteName
}

func (Next) target()    {}
func (ToRoute) target() {}

// ParseTarget returns the Target s names: Next for "next", or the route s
// names (ParseRouteName).
func ParseTarget(s string) (Target, error) {
	if s == nextTarget {
		return Next{}, nil
	}
	r, err := ParseRouteName(s)
	if err != nil {
		return nil, err
	}
	return ToRoute{Route: r}, nil
}

// On maps an action's verdicts to their targets, as its config's on: writes
// them. A verdict without an entry has its default target (On.Target).
type On map[Verdict]Target

// Target returns the target of verdict v: its entry, or without one Next
// for Passed and the FailedRoute for every other verdict.
func (o On) Target(v Verdict) Target {
	if t, ok := o[v]; ok {
		return t
	}
	if v == Passed {
		return Next{}
	}
	return ToRoute{Route: FailedRoute}
}

// VerdictReport is what a session reported as its verdict, as the engine
// read it: NoVerdictReported, VerdictReported or VerdictUnreadable.
//
//sumtype:decl
type VerdictReport interface {
	verdictReport()
}

// NoVerdictReported is a session that reported no verdict: what it had to
// report it in was empty or missing.
type NoVerdictReported struct{}

// VerdictReported is a session that reported Verdict.
type VerdictReported struct {
	Verdict Verdict
}

// VerdictUnreadable is a session that reported text with no verdict name
// first.
type VerdictUnreadable struct{}

func (NoVerdictReported) verdictReport() {}
func (VerdictReported) verdictReport()   {}
func (VerdictUnreadable) verdictReport() {}

// ShellOutcome is how a shell action's script ended, as the engine ran it.
type ShellOutcome struct {
	// Status is the script's exit status, -1 when a signal crew did not
	// send killed it; none when it did not run to its end: it could not
	// start, ran out of time, or crew stopped it.
	Status Optional[int]
	// Reason is crew's one line on how it ended, followed, for a shell
	// action whose script printed a line, by the last line it printed; a
	// route's shell step has crew's line alone (R49).
	Reason ShellReason
}

// Judged is an action's verdict, with the end its run records.
type Judged struct {
	Verdict Verdict
	// End is EndFailed, with what made the action fail, when Verdict is
	// Failed, and EndSucceeded otherwise.
	End ActionEnd
}

// judgeSession returns the verdict of a session action whose harness said
// outcome and which reported report, nil counting as NoVerdictReported,
// with on its action's targets. Harm
// wins over what the session says: a stop or a harness that failed is
// Failed whatever it reported. A session that succeeded is Passed without
// a report, and has the verdict it reported when the verdict is one any
// action may end with or one on names. Any other report is Failed, in
// crew's words.
func judgeSession(on On, outcome Outcome, report VerdictReport, stopping bool) Judged {
	switch {
	case stopping:
		return failedBy(outcome.Reason, CauseStopped)
	case !outcome.Succeeded:
		return failedBy(outcome.Reason, CauseSession)
	}
	switch r := report.(type) {
	case VerdictReported:
		if !on.names(r.Verdict) {
			return failedBy(NewSessionText(fmt.Sprintf(
				"the session reported the verdict %q, which its on: does not name", r.Verdict)), CauseVerdict)
		}
		return judged(r.Verdict, outcome.Reason, CauseSession)
	case VerdictUnreadable:
		return failedBy(NewSessionText("the session's verdict holds no verdict name"), CauseVerdict)
	case NoVerdictReported:
	}
	return judged(Passed, outcome.Reason, CauseSession)
}

// judgeShell returns the verdict of the shell action spec, whose script
// ended as outcome says, with on its action's targets. A stop, or a script
// that did not run to its end, is Failed. Otherwise the exit status gives
// the verdict spec's Verdicts names for it, or without one Passed for 0
// and Failed for any other. A verdict that is not one any action may end
// with, and that on does not name, is Failed, in crew's words.
func judgeShell(spec ShellSpec, on On, outcome ShellOutcome, stopping bool) Judged {
	reason := NewSessionText(outcome.Reason.String())
	status, exited := outcome.Status.Get()
	switch {
	case stopping:
		return failedBy(reason, CauseStopped)
	case !exited:
		return failedBy(reason, CauseShell)
	}
	v, ok := spec.Verdicts[status]
	switch {
	case !ok && status == 0:
		v = Passed
	case !ok:
		v = Failed
	case !on.names(v):
		return failedBy(NewSessionText(fmt.Sprintf(
			"the script's exit status %d gives the verdict %q, which its on: does not name", status, v)), CauseVerdict)
	}
	return judged(v, reason, CauseShell)
}

// names reports whether an action with on may end with v: v is Passed,
// Failed or Waiting, or on has an entry for it.
func (o On) names(v Verdict) bool {
	_, ok := o[v]
	return ok || v == Passed || v == Failed || v == Waiting
}

// judged returns v with reason, failed by cause when v is Failed.
func judged(v Verdict, reason SessionText, cause FailureCause) Judged {
	if v == Failed {
		return failedBy(reason, cause)
	}
	return Judged{Verdict: v, End: EndSucceeded{Reason: reason}}
}

// failedBy returns the Failed verdict, with reason and cause.
func failedBy(reason SessionText, cause FailureCause) Judged {
	return Judged{Verdict: Failed, End: EndFailed{Reason: reason, Cause: cause}}
}

// FunctionOutcome is how a function action or step ended, as the engine
// called it.
type FunctionOutcome struct {
	// Verdict is the verdict the function returned; none when it returned
	// an error, ran out of time, was stopped or did not start.
	Verdict Optional[Verdict]
	// Reason is crew's one line on how it ended.
	Reason ShellReason
	// Log is the repository-relative path of the log the function wrote
	// into: the run's, or for a run without a workspace the log of the
	// workspace it would have; empty when it could not open one.
	Log string
}

// judgeFunction returns the verdict of the function action spec, which
// ended as outcome says, with on its action's targets. A stop, or a
// function that returned no verdict, is Failed. Otherwise the verdict it
// returned counts when it is Passed or Failed, or when spec declares it
// and it is one any action may end with or one on names. Any other verdict
// is Failed, in crew's words.
func judgeFunction(spec FunctionSpec, on On, outcome FunctionOutcome, stopping bool) Judged {
	reason := NewSessionText(outcome.Reason.String())
	v, returned := outcome.Verdict.Get()
	switch {
	case stopping:
		return failedBy(reason, CauseStopped)
	case !returned:
		return failedBy(reason, CauseFunction)
	case v == Passed || v == Failed:
	case !slices.Contains(spec.Verdicts, v):
		return failedBy(NewSessionText(fmt.Sprintf(
			"the function returned the verdict %q, which it does not declare", v)), CauseVerdict)
	case !on.names(v):
		return failedBy(NewSessionText(fmt.Sprintf(
			"the function returned the verdict %q, which its on: does not name", v)), CauseVerdict)
	}
	return judged(v, reason, CauseFunction)
}
