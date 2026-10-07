package crew

// Workspace is where an action's session works: its name, unique among the
// workspaces that exist, and the branch its work goes on. Its directory is
// the machine's, not the domain's.
type Workspace struct {
	Name   WorkspaceName
	Branch string
}

// ResumePoint is where a new action run resumes the work of a failed one:
// the workspace it ran in, the repository-relative path of its log, and why
// it failed.
type ResumePoint struct {
	Workspace Workspace
	Log       string
	Reason    SessionText
}
