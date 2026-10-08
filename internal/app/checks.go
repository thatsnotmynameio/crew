package app

import (
	"errors"
	"fmt"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// routeSteps returns an error for each route of rules with a step tracker,
// named name, cannot take: a comment or a question without a
// port.Commenter, a close without a port.Closer. Each error names the
// rule and the route by their key path in the config (routePath).
func routeSteps(name string, tracker port.Tracker, rules []crew.Rule) error {
	_, comments := tracker.(port.Commenter)
	_, closes := tracker.(port.Closer)
	var errs []error
	for _, r := range rules {
		for _, route := range r.Routes {
			path := routePath(r, route)
			if !comments && slices.ContainsFunc(route.Steps, isComment) {
				errs = append(errs, fmt.Errorf("%s: tracker %q cannot comment on issues", path, name))
			}
			if !closes && slices.ContainsFunc(route.Steps, isStep[crew.CloseStep]) {
				errs = append(errs, fmt.Errorf("%s: tracker %q cannot close issues", path, name))
			}
		}
	}
	return errors.Join(errs...)
}

// routePath returns the key path of route, one of r's: the question
// action's, rules.<rule>.actions[<i>], for the route crew declares for it
// (KTD3), and rules.<rule>.routes.<route> for a route the config writes.
func routePath(r crew.Rule, route crew.Route) string {
	for i, a := range r.Actions {
		if _, ok := a.Kind.(crew.QuestionSpec); ok && string(a.Name) == string(route.Name) {
			return fmt.Sprintf("rules.%s.actions[%d]", r.Name, i)
		}
	}
	return fmt.Sprintf("rules.%s.routes.%s", r.Name, route.Name)
}

// isComment reports whether s posts a comment: a comment or a question.
func isComment(s crew.Step) bool {
	return isStep[crew.CommentStep](s) || isStep[crew.QuestionStep](s)
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
			if a.MayWait() {
				errs = append(errs, fmt.Errorf("rules.%s.actions[%d]: session %q may wait for answers, "+
					"and tracker %q cannot list comments", r.Name, i, a.Name, name))
			}
		}
	}
	return errors.Join(errs...)
}

// questionRule returns an error for each capability crew's question rule
// needs and tracker, named name, lacks, when the config writes questions
// (KTD10): it lists comments, a port.CommentLister, to find the question,
// and delegates, a port.Delegator. Each error names questions, the key
// that adds the rule.
func questionRule(name string, tracker port.Tracker, questions *config.Questions) error {
	if questions == nil {
		return nil
	}
	var errs []error
	if _, ok := tracker.(port.CommentLister); !ok {
		errs = append(errs, fmt.Errorf("questions: tracker %q cannot list comments", name))
	}
	if _, ok := tracker.(port.Delegator); !ok {
		errs = append(errs, fmt.Errorf("questions: tracker %q cannot delegate questions", name))
	}
	return errors.Join(errs...)
}

// questionWriter returns an error when the config writes questions and
// crew cannot know a login its questions are posted as, which finding
// them needs (KTD10): tracker, named name, is no port.LoginFinder, and
// writer, the default bot that acts at startup, is none.
func questionWriter(name string, tracker port.Tracker, questions *config.Questions, writer port.Identity) error {
	if _, ok := tracker.(port.LoginFinder); questions == nil || ok || writer.Login != "" {
		return nil
	}
	return fmt.Errorf("questions: tracker %q finds no login crew posts as, and no tracker.bot acts", name)
}
