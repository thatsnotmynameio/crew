package core

import "github.com/thatsnotmynameio/crew/internal/crew"

// Command is a side effect the core asks the engine to run through a port.
// The engine runs each command and feeds its result back as an Input. The
// set of commands is closed: only this package's types implement Command.
type Command interface {
	command()
}

// CallID identifies one tracker call, a Move or a ReportFailure, so its
// CallResult finds it. A retried call keeps its ID; the core never has two
// attempts of one call in flight.
type CallID uint64

// ListIssues asks the tracker for the open issues in any of States. Its
// result is IssuesListed or ListFailed. The core keeps at most one listing
// outstanding.
type ListIssues struct {
	// States are the stages' trigger states, in config order.
	States []crew.State
}

// Move asks the tracker to move an issue from one state to another. Its
// result is a CallResult carrying ID.
type Move struct {
	ID       CallID
	IssueKey string
	From     crew.State
	To       crew.State
}

// ReportFailure asks the tracker to post Report on its issue (R7). Its
// result is a CallResult carrying ID.
type ReportFailure struct {
	ID     CallID
	Report crew.FailureReport
}

// CreateWorkspace asks for a new workspace for Action on Issue. Its result
// is WorkspaceReady or WorkspaceFailed, carrying Issue.Key and Action.
type CreateWorkspace struct {
	Issue  crew.Issue
	Action string
}

// StartSession asks the harness to start a session in Dir with Prompt, its
// output going to the log file at Log (repository-relative, as received in
// WorkspaceReady). Its result is SessionStarted or SessionFailedToStart,
// then SessionEnded once a started session ends.
type StartSession struct {
	IssueKey string
	Action   string
	Dir      string
	Prompt   string
	Log      string
}

// StopSession asks the engine to stop the running session of Action on the
// issue. The session's end still arrives as SessionEnded.
type StopSession struct {
	IssueKey string
	Action   string
}

// ReportStatus asks the tracker to show Status on its issue's status
// comment (KTD3). Its result is a StatusResult carrying Status.IssueKey. The
// core never has two status writes of one issue in flight.
type ReportStatus struct {
	Status crew.Status
}

func (ListIssues) command()      {}
func (Move) command()            {}
func (ReportFailure) command()   {}
func (CreateWorkspace) command() {}
func (StartSession) command()    {}
func (StopSession) command()     {}
func (ReportStatus) command()    {}
