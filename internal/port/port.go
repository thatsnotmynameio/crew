// Package port holds the interfaces the engine reaches the outside world
// through: a Tracker for issues, a Harness for coding-agent sessions and a
// Workspace for each action's checkout. Each port holds only what every
// adapter must provide; anything an adapter may or may not support is a
// separate optional interface, such as Preparer, StatusReporter or Narrator,
// that the engine detects by type assertion. An adapter therefore never wraps
// another adapter value, because a wrapper hides the optional interfaces of
// what it wraps.
//
// The package imports only the domain, so adapters and the engine share it
// without knowing each other.
package port

import (
	"context"
	"errors"
	"io"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The error classes of Tracker.Move and Tracker.ReportFailure. An adapter
// wraps one of them with %w and its own context; any other error is
// transient, and the engine retries the call at the next poll.
var (
	// ErrMovedMeanwhile means the issue is closed, or is no longer in the
	// state the move expected, because someone or something changed it.
	ErrMovedMeanwhile = errors.New("the issue moved meanwhile")
	// ErrRefused means the tracker refused the call for good, such as for a
	// missing permission. Retrying cannot help.
	ErrRefused = errors.New("the tracker refused")
)

// Tracker is an issue tracker, spoken to in the workflow's states. A state
// is text the tracker shows, such as a label's name on GitHub or a status on
// another tracker; the adapter knows how its tracker shows it, and is built
// knowing the workflow's states and the config's extra labels
// (TrackerFactory). Those states are crew's states. The extras are crew's
// too, for parked work no stage takes, but they are never states.
type Tracker interface {
	// List returns the open issues that are in any of states. Each issue
	// carries every crew state it is in, not only the ones asked for, so the
	// engine can skip an issue found in two states; it carries nothing that
	// is not a crew state, and never an extra. An issue is Blocked while an
	// open issue blocks it, when the tracker records dependencies. An error
	// means the list could not be read; it is transient.
	List(ctx context.Context, states []crew.State) ([]crew.Issue, error)
	// Move moves the issue identified by issueKey from one state to
	// another, and leaves it in exactly one crew state, to, removing every
	// extra it carries, without touching what is not crew's. It returns an
	// error wrapping ErrMovedMeanwhile when the issue is closed or not in
	// from, one wrapping ErrRefused when the tracker refuses for good, and
	// any other error when the move failed transiently. It returns nil,
	// changing nothing, when the issue is already exactly in to and not in
	// from, whatever extras it carries, so retrying a move that landed is
	// safe.
	Move(ctx context.Context, issueKey string, from, to crew.State) error
	// ReportFailure posts report on its issue, formatted in the tracker's
	// own markup. Its errors are classified as Move's are.
	ReportFailure(ctx context.Context, report crew.FailureReport) error
}

// Harness runs coding-agent sessions.
type Harness interface {
	// Start starts a session for run and returns once it is running. An
	// error means no session was started. The session runs until it ends on
	// its own or is stopped; ctx bounds only the start itself.
	Start(ctx context.Context, run Run) (Session, error)
}

// Run is what a Harness needs to start a session.
type Run struct {
	// Dir is the absolute directory the session works in.
	Dir string
	// Prompt is the rendered prompt the session starts with.
	Prompt string
	// Output receives everything the session prints, for its log. The
	// harness writes to it from one goroutine at a time and stops writing
	// once Wait has returned.
	Output io.Writer
}

// Session is a running harness session.
type Session interface {
	// Wait blocks until the session ends and returns the harness's verdict.
	// A session that ends cleanly succeeded; one that fails, dies or is
	// stopped did not, and its Reason says why in one line. Wait may be
	// called more than once, and from any goroutine.
	Wait() crew.Outcome
	// Stop asks the session to end and returns once it has. The adapter
	// terminates the session, then kills it when ctx is done: the caller owns
	// the deadline and the adapter owns the signals. Stopping a session that
	// already ended does nothing.
	Stop(ctx context.Context) error
}

// Workspace creates the place each action works in.
type Workspace interface {
	// Create creates a fresh workspace for action on issue. Each call gets
	// its own workspace, even for an issue and action seen before.
	Create(ctx context.Context, issue crew.Issue, action string) (Space, error)
}

// Space is a created workspace.
type Space struct {
	// Name is unique among the workspaces created, and safe in a file name:
	// the session's log is named after it.
	Name string
	// Dir is the workspace's absolute directory.
	Dir string
	// Branch is the branch the action's work goes on.
	Branch string
}

// Preparer is an optional interface of any port's adapter: it checks the
// adapter's tools and prepares the adapter before the first poll.
type Preparer interface {
	// Prepare checks and prepares the adapter for a workflow that can
	// request states, so an adapter creates or checks only what the workflow
	// uses; a Tracker also creates or checks the extras it was built with.
	// An error names the tool or setting at fault, and crew stops before
	// polling.
	Prepare(ctx context.Context, states []crew.State) error
}

// Prepare runs Prepare, with states, on each of adapters that implements
// Preparer, in order, and skips the others. It returns every failure, joined.
func Prepare(ctx context.Context, states []crew.State, adapters ...any) error {
	var errs []error
	for _, a := range adapters {
		if p, ok := a.(Preparer); ok {
			if err := p.Prepare(ctx, states); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// StatusReporter is an optional interface of a Tracker: it keeps one status
// comment per issue, edited in place, that shows where the issue stands. A
// tracker without it reports no status, and crew works as it does without
// status comments.
type StatusReporter interface {
	// ReportStatus shows status on its issue, formatted in the tracker's own
	// markup: it edits the issue's status comment, or creates it when the
	// issue has none. The engine never has two calls for one issue in
	// flight. Its errors are classified as Tracker.Move's are.
	ReportStatus(ctx context.Context, status crew.Status) error
}

// Narrator is an optional interface of a harness's Session: it tells what
// the session last said, for the issue's status.
type Narrator interface {
	// Said returns the last thing the session said, on one line, or "" when
	// it said nothing yet. It may be called from any goroutine while the
	// session runs and after it ended.
	Said() string
}
