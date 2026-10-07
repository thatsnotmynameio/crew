package jsonl

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// v1Event returns the run event l, a version 1 line, holds, its issue in
// repository, and false when l holds none this crew understands: a line
// without a workspace, an ended line without its outcome, or one of
// another event. A started line is an action's start and an ended line its
// end, in a rule run named after the line's crew run, issue and rule: a
// version 1 line names no rule run, and the line's crew run is missing on
// the oldest lines.
func (l line) v1Event(repository crew.RepositoryID) (crew.RunEvent, bool) {
	if l.Workspace == "" {
		return nil, false
	}
	h := l.eventHead(repository)
	h.Run = crew.RuleRunID("v1/" + l.Run + "/" + l.Issue + "/" + string(l.Rule))
	workspace := crew.Workspace{Name: l.Workspace, Branch: l.Branch}
	switch l.Event {
	case eventStarted:
		return crew.ActionOpened{EventHead: h, Action: l.Action, Workspace: workspace, Log: l.Log}, true
	case eventEnded:
		if l.Succeeded == nil {
			return nil, false
		}
		return crew.ActionEnded{
			EventHead: h, Action: l.Action, End: l.end(),
			Workspace: crew.Some(crew.OpenedWorkspace{Workspace: workspace, Log: l.Log}),
			// The line's reason is already the one a resume quotes: the
			// crew that wrote it gave an end without a session the reason
			// of the failure before it. So its event names a session
			// start, which keeps that reason: the line's time less its
			// duration, or its time when the line has none.
			SessionStarted: crew.Some(l.Time.Add(-time.Duration(deref(l.DurationMS)) * time.Millisecond)),
			Usage:          l.reported(), PullRequest: l.found(),
		}, true
	}
	return nil, false
}
