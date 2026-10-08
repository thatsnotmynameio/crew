package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// checkRoutes rejects a rule whose actions and routes do not fit together:
// two actions of one name, a route missing or one too many, an on that
// leads nowhere, a route nothing leads to, or a route that does not end
// with its one move or close (R12, R15, R32).
func checkRoutes(r parsedRule) error {
	errs := []error{checkNames(r), checkDeclared(r)}
	declared := make(map[crew.RouteName]bool, len(r.routes))
	names := make([]string, len(r.routes))
	for i, route := range r.routes {
		declared[route.Name], names[i] = true, string(route.Name)
	}
	led := map[crew.RouteName]bool{}
	for _, a := range r.actions {
		for _, o := range a.on {
			to, ok := o.target.(crew.ToRoute)
			if !ok {
				continue
			}
			led[to.Route] = true
			if !declared[to.Route] {
				errs = append(errs, keyError(o.path, o.line, fmt.Sprintf("route %q is not one of rule %q's routes: %s",
					to.Route, r.Name, strings.Join(names, ", "))))
			}
		}
	}
	for _, route := range r.routes {
		if len(r.actions) > 0 && !led[route.Name] && route.Name != crew.PassedRoute && route.Name != crew.FailedRoute {
			errs = append(errs, keyError(route.path, route.line, fmt.Sprintf("no action's on leads to route %q", route.Name)))
		}
		errs = append(errs, checkSteps(route))
	}
	return errors.Join(errs...)
}

// checkNames rejects two actions of r that share a name, at the later one.
func checkNames(r parsedRule) error {
	var errs []error
	first := make(map[crew.ActionName]parsedAction, len(r.actions))
	for _, a := range r.actions {
		if a.Name == "" {
			continue // its agent is missing, which agentsInUse reports
		}
		if other, ok := first[a.Name]; ok {
			errs = append(errs, keyError(a.path, a.line, fmt.Sprintf(
				"action %q is also %s (line %d); set name: on one of them to tell them apart", a.Name, other.path, other.line)))
			continue
		}
		first[a.Name] = a
	}
	return errors.Join(errs...)
}

// checkDeclared rejects a rule that lacks passed, a rule with actions that
// lacks failed, and a rule without actions with any route but passed.
func checkDeclared(r parsedRule) error {
	if r.routesLine == 0 {
		return keyError(r.path+".routes", r.ruleLine, "required")
	}
	var passed, failed bool
	var errs []error
	for _, route := range r.routes {
		switch {
		case route.Name == crew.PassedRoute:
			passed = true
		case len(r.actions) == 0:
			errs = append(errs, keyError(route.path, route.line, "a rule without actions ends only through passed"))
		case route.Name == crew.FailedRoute:
			failed = true
		}
	}
	if !passed {
		errs = append(errs, keyError(r.path+".routes", r.routesLine,
			"must declare passed, the route a run ends through after its last action"))
	}
	if len(r.actions) > 0 && !failed {
		errs = append(errs, keyError(r.path+".routes", r.routesLine,
			"must declare failed, the route a run ends through when an action fails"))
	}
	return errors.Join(errs...)
}

// checkSteps rejects a route that does not end with move or close, and a
// move or close before its last step.
func checkSteps(r parsedRoute) error {
	last := len(r.Steps) - 1
	if last < 0 || !ends(r.Steps[last]) {
		return keyError(r.path, r.line, "must end with move or close, so the item leaves the rule's running label")
	}
	var errs []error
	for i, step := range r.Steps[:last] {
		if ends(step) {
			errs = append(errs, keyError(r.steps[i].path, r.steps[i].line, "move and close end a route, so they come only last"))
		}
	}
	return errors.Join(errs...)
}

// ends tells whether step ends a route.
func ends(step crew.Step) bool {
	switch step.(type) {
	case crew.MoveStep, crew.CloseStep:
		return true
	case crew.CommentStep, crew.ReportStep, crew.ShellStep, crew.FunctionStep, crew.QuestionStep, crew.DelegateStep:
	}
	return false
}

// checkGraph rejects a set of rules that would take an item twice, take
// back what one of them moved, or move an item to a label only crew sets
// while a rule runs. It runs after spellOnce, so it compares labels ignoring
// case.
func checkGraph(rules []parsedRule) error {
	var errs []error
	byReady := make(map[crew.State]parsedRule, len(rules))
	byRunning := make(map[crew.State]parsedRule, len(rules))
	for _, r := range rules {
		l := r.Labels
		if other, ok := byReady[l.Ready]; ok {
			errs = append(errs, keyError(r.path+".labels.ready", r.labels.Ready.line,
				fmt.Sprintf("rule %q takes %q, as rule %q (%s) does; two rules cannot take the same label",
					r.Name, l.Ready, other.Name, other.path)))
		} else {
			byReady[l.Ready] = r
		}
		if _, ok := byRunning[l.Running]; !ok {
			byRunning[l.Running] = r
		}
	}
	for _, r := range rules {
		if other, ok := byReady[r.Labels.Running]; ok {
			errs = append(errs, keyError(r.path+".labels.running", r.labels.Running.line,
				fmt.Sprintf("%q is the ready label of rule %q (%s.labels.ready), which would take the item while rule %q runs",
					r.Labels.Running, other.Name, other.path, r.Name)))
		}
		errs = append(errs, checkMoves(r, byRunning))
	}
	return errors.Join(errs...)
}

// checkMoves rejects each move of r's routes to r's own ready label, which
// would take the item again, or to a rule's running label in byRunning.
func checkMoves(r parsedRule, byRunning map[crew.State]parsedRule) error {
	var errs []error
	for _, route := range r.routes {
		for i, step := range route.Steps {
			m, ok := step.(crew.MoveStep)
			if !ok {
				continue
			}
			at := route.steps[i]
			if m.To == r.Labels.Ready {
				errs = append(errs, keyError(at.path, at.line,
					fmt.Sprintf("%q is the rule's own ready label, so rule %q would take the item again", m.To, r.Name)))
			}
			if other, ok := byRunning[m.To]; ok {
				errs = append(errs, keyError(at.path, at.line, fmt.Sprintf(
					"%q is the running label of rule %q (%s.labels.running), which only crew sets while that rule runs",
					m.To, other.Name, other.path)))
			}
		}
	}
	return errors.Join(errs...)
}
