package crew

import "time"

// Workspace is where a rule run's actions work: its name, unique among the
// workspaces that exist, and the branch its work goes on. Its directory is
// the machine's, not the domain's.
type Workspace struct {
	Name   WorkspaceName
	Branch string
}

// ResumePoint is where a new rule run resumes the work of one that did not
// end well: the workspace it ran in, the repository-relative path of its
// log, why it failed, and the action the new run starts at.
type ResumePoint struct {
	Workspace Workspace
	Log       string
	Reason    SessionText
	// Action is the action the new run starts at; the actions before it
	// went on to the next action in the run it resumes. An action the rule
	// no longer has starts the new run at its first action.
	Action ActionName
}

// OpenedWorkspace is a rule run's workspace, once it is ready.
type OpenedWorkspace struct {
	Workspace Workspace
	// Log is the repository-relative path of the run's log file, which
	// each of its actions writes in turn; empty when crew stopped before
	// any action could start, as no action writes it.
	Log string
	// Resumed says whether the workspace is the reopened workspace of the
	// run the rule run resumes.
	Resumed bool
	// Opened is when the workspace was ready.
	Opened time.Time
}

// Since returns when the run's new workspace was made, from which its pull
// requests are looked up: Opened, or the zero time for a reopened one.
func (w OpenedWorkspace) Since() time.Time {
	if w.Resumed {
		return time.Time{}
	}
	return w.Opened
}

// WorkspaceState is where a rule run's workspace stands: NoWorkspace,
// CreatingWorkspace, ReopeningWorkspace or InWorkspace.
//
//sumtype:decl
type WorkspaceState interface {
	workspaceState()
}

// NoWorkspace is a run that has not asked for its workspace, or whose
// workspace could not be made or reopened.
type NoWorkspace struct{}

// CreatingWorkspace is a run whose new workspace is being made.
type CreatingWorkspace struct{}

// ReopeningWorkspace is a run whose resumed run's workspace is being
// reopened.
type ReopeningWorkspace struct {
	Workspace Workspace
}

// InWorkspace is a run whose workspace is ready, and which its actions
// share.
type InWorkspace struct {
	Opened OpenedWorkspace
}

func (NoWorkspace) workspaceState()        {}
func (CreatingWorkspace) workspaceState()  {}
func (ReopeningWorkspace) workspaceState() {}
func (InWorkspace) workspaceState()        {}
