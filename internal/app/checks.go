package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// routeSteps returns an error for each route of rules with a step tracker,
// named name, cannot take: a comment without a port.Commenter, a close
// without a port.Closer. Each error names the rule and the route by their
// key path in the config.
func routeSteps(name string, tracker port.Tracker, rules []crew.Rule) error {
	_, comments := tracker.(port.Commenter)
	_, closes := tracker.(port.Closer)
	var errs []error
	for _, r := range rules {
		for _, route := range r.Routes {
			path := fmt.Sprintf("rules.%s.routes.%s", r.Name, route.Name)
			if !comments && slices.ContainsFunc(route.Steps, isStep[crew.CommentStep]) {
				errs = append(errs, fmt.Errorf("%s: tracker %q cannot comment on issues", path, name))
			}
			if !closes && slices.ContainsFunc(route.Steps, isStep[crew.CloseStep]) {
				errs = append(errs, fmt.Errorf("%s: tracker %q cannot close issues", path, name))
			}
		}
	}
	return errors.Join(errs...)
}

// isStep reports whether s is a step of type T.
func isStep[T crew.Step](s crew.Step) bool {
	_, ok := s.(T)
	return ok
}

// waitingSessions returns an error for each session of rules that may wait
// for answers, one whose on has a waiting entry, when tracker, named name,
// cannot list the comments that answer it, as it is no port.CommentLister
// (KTD-W5). Each error names the rule and the action by its key path in the
// config, and the action by its name.
func waitingSessions(name string, tracker port.Tracker, rules []crew.Rule) error {
	if _, ok := tracker.(port.CommentLister); ok {
		return nil
	}
	var errs []error
	for _, r := range rules {
		for i, a := range r.Actions {
			_, session := a.Kind.(crew.SessionSpec)
			if _, waits := a.On[crew.Waiting]; session && waits {
				errs = append(errs, fmt.Errorf("rules.%s.actions[%d]: session %q may wait for answers, "+
					"and tracker %q cannot list comments", r.Name, i, a.Name, name))
			}
		}
	}
	return errors.Join(errs...)
}
