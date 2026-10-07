package crew

// Start is how a rule run starts, which its take decides from the last run
// of its rule on its issue (History.Start): StartFresh, StartAt,
// StartPassedRoute or StartWithoutAction. RunTaken carries it.
//
//sumtype:decl
type Start interface {
	start()
}

// StartFresh is a run that starts at its rule's first action, in a new
// worktree: there was no last run, it ended through PassedRoute, or its
// worktree is gone.
type StartFresh struct{}

// StartAt is a run that resumes the work of the run it continues: it
// reopens that run's worktree and starts at Action, the restart point. The
// actions before Action went on to the next action in an earlier run, so
// they do not run again.
type StartAt struct {
	Workspace Workspace
	// Log is the repository-relative path of the continued run's log.
	Log string
	// Action is the action the run starts at.
	Action ActionName
	// Route is the route the continued run ended through, or chose and
	// never finished; empty when it never chose one, as crew crashed while
	// it ran an action.
	Route RouteName
	// Reason is why the continued run ended: the reason of the action that
	// ended it, or crew's when it never recorded that action's end.
	Reason SessionText
	// Session is the latest session the continued run knew: the one it
	// started, or the one it inherited itself. The run's actions that are
	// not sessions act as it until a session of the run starts.
	Session Optional[LatestSession]
}

// StartPassedRoute is a run that runs only PassedRoute, which the run it
// continues chose and never finished: its final move or close was given
// up, or never settled. It starts no action, and takes the route at its
// take, even while crew stops.
type StartPassedRoute struct {
	// Workspace is the continued run's worktree, which the run reopens
	// when it still exists; none when that run had none.
	Workspace Optional[Workspace]
	// Log is the repository-relative path of the continued run's log.
	Log string
	// Session is the latest session the continued run knew; the route's
	// steps act as it.
	Session Optional[LatestSession]
}

// StartWithoutAction is a run that starts at its rule's first action, in a
// new worktree, because its rule no longer has Action, the action the run
// it continues would resume at.
type StartWithoutAction struct {
	Action ActionName
}

func (StartFresh) start()         {}
func (StartAt) start()            {}
func (StartPassedRoute) start()   {}
func (StartWithoutAction) start() {}

// LatestSession is a run's latest session: the session action's name, and
// the bot it acted as.
type LatestSession struct {
	Action ActionName
	Bot    Bot
}

// reopens returns the worktree s reopens, and the log it names, when it
// reopens one.
func reopens(s Start) (Workspace, string, bool) {
	switch s := s.(type) {
	case StartAt:
		return s.Workspace, s.Log, true
	case StartPassedRoute:
		if w, ok := s.Workspace.Get(); ok {
			return w, s.Log, true
		}
	case StartFresh, StartWithoutAction:
	}
	return Workspace{}, "", false
}

// inherited returns the latest session s carries from the run it
// continues: only a resumed run inherits one.
func inherited(s Start) Optional[LatestSession] {
	switch s := s.(type) {
	case StartAt:
		return s.Session
	case StartPassedRoute:
		return s.Session
	case StartFresh, StartWithoutAction:
	}
	return Optional[LatestSession]{}
}

// WithoutWorktree returns s once the worktree it reopens is gone, or crew
// cannot reopen worktrees: a resume at an action starts fresh, since the
// actions before it would not have run in a new worktree, and the passed
// route alone runs without one.
func WithoutWorktree(s Start) Start {
	switch s := s.(type) {
	case StartAt:
		return StartFresh{}
	case StartPassedRoute:
		return StartPassedRoute{Session: s.Session}
	case StartFresh, StartWithoutAction:
	}
	return s
}
