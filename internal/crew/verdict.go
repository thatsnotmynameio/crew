package crew

import "fmt"

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
