package crew

// RouteName names one of a rule's routes, as the config's routes key it. It
// follows the verdicts' grammar (ParseRouteName).
type RouteName string

// The routes every rule with actions declares. A run ends through
// PassedRoute after its last action went Next, and through FailedRoute for
// any verdict its action's On does not map.
const (
	PassedRoute RouteName = "passed"
	FailedRoute RouteName = "failed"
)

// Route is one of a rule's ways to end a run: steps that run one after
// another, the last of which moves or closes the item. A route the config
// writes as a single label is one MoveStep.
type Route struct {
	// Name identifies the route within its rule.
	Name RouteName
	// Steps run in this order. The config makes the last one, and only the
	// last one, a MoveStep or a CloseStep.
	Steps []Step
}

// Step is one step of a route: MoveStep, CloseStep, CommentStep, ReportStep
// or ShellStep. A session is never a step.
//
//sumtype:decl
type Step interface {
	step()
}

// MoveStep moves the item to a state.
type MoveStep struct {
	To State
}

// CloseStep closes the issue.
type CloseStep struct{}

// CommentStep posts a comment the config wrote on the issue. Its template
// reads only what crew knows (CommentData), never what a session or a
// script printed.
type CommentStep struct {
	Template CommentTemplate
}

// ReportStep posts crew's report on how the run ended, naming the action
// that ended it and its log.
type ReportStep struct{}

// ShellStep runs one of the config's shell actions.
type ShellStep struct {
	// Name is the shell action's name in the config's actions.
	Name ActionName
	// Shell is the shell action's definition.
	Shell ShellSpec
}

func (MoveStep) step()    {}
func (CloseStep) step()   {}
func (CommentStep) step() {}
func (ReportStep) step()  {}
func (ShellStep) step()   {}
