package lines

import (
	"fmt"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// stepEnded is the line for a step of a route that settled (R16): a move,
// close, comment, report, question or delegation that landed, or a shell or
// function step that ran, failed, was stopped or was skipped. A tracker step given up or dropped has none:
// its CallDropped says so.
func stepEnded(e core.RouteStepEnded) string {
	route := fmt.Sprintf("%s %s through %s: ", e.IssueRef, e.Rule, e.Route)
	step := stepName(e.Plan)
	switch o := e.Outcome.(type) {
	case crew.StepLanded:
		return landed(e)
	case crew.StepRan:
		return withReason(route+step+" ran", o.Reason.String())
	case crew.StepFailed:
		return withReason(route+step+" failed", o.Reason.String())
	case crew.StepStopped:
		return route + "crew stopped " + step
	case crew.StepSkipped:
		return route + "crew skipped " + step + ", as it was stopping"
	case crew.StepGivenUp, crew.StepDropped:
	}
	return ""
}

// landed is the line for e's tracker step that landed.
func landed(e core.RouteStepEnded) string {
	switch e.Plan.Kind {
	case crew.StepMove:
		return moved(e.IssueRef, e.From, e.Plan.To)
	case crew.StepClose:
		return "closed " + e.IssueRef
	case crew.StepComment:
		return "commented on " + e.IssueRef
	case crew.StepReport:
		return "posted the report on " + e.IssueRef
	case crew.StepQuestion:
		return "posted the question on " + e.IssueRef
	case crew.StepDelegate:
		return "asked the answerer on " + e.IssueRef
	case crew.StepShell, crew.StepFunction:
	}
	return ""
}

// stepName names step p of a route.
func stepName(p crew.StepPlan) string {
	switch p.Kind {
	case crew.StepMove:
		return "the move to " + string(p.To)
	case crew.StepClose:
		return "the close"
	case crew.StepComment:
		return "the comment"
	case crew.StepReport:
		return "the report"
	case crew.StepShell:
		return "the shell step " + string(p.Shell)
	case crew.StepFunction:
		return "the function step " + string(p.Function)
	case crew.StepQuestion:
		return "the question"
	case crew.StepDelegate:
	}
	return "the step"
}
