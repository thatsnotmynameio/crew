// Package port holds the interfaces the engine reaches the outside world
// through: a Tracker for issues, a Harness for coding-agent sessions and a
// Workspace for each action's checkout. Each port holds only what every
// adapter must provide; anything an adapter may or may not support is a
// separate optional interface, such as Preparer, StatusReporter,
// PullRequestReporter, Acting, CodeOwnerFinder, LoginFinder, WriterReporter,
// BoardLister, Narrator or Reopener, that the engine detects by type
// assertion. An adapter therefore never wraps another adapter value, because
// a wrapper hides the optional interfaces of what it wraps.
//
// The package imports only the domain, so adapters and the engine share it
// without knowing each other.
package port

import (
	"context"
	"errors"
	"io"
	"time"

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

// ErrWorkspaceGone is the error class of Reopener.Reopen for a workspace
// that no longer exists, such as a worktree you removed. An adapter
// wraps it with %w and its own context.
var ErrWorkspaceGone = errors.New("the workspace is gone")

// Tracker is an issue tracker, spoken to in the rules' states. A state
// is text the tracker shows, such as a label's name on GitHub or a status on
// another tracker; the adapter knows how its tracker shows it, and is built
// knowing the rules' states (TrackerFactory). Those states are crew's
// states, and crew touches no other label.
type Tracker interface {
	// List returns the open issues that are in any of states. Each issue
	// carries every crew state it is in, not only the ones asked for, so the
	// engine can skip an issue found in two states; it carries nothing that
	// is not a crew state. An issue is Blocked while an open issue blocks
	// it, when the tracker records dependencies. An error means the list
	// could not be read; it is transient.
	List(ctx context.Context, states []crew.State) ([]crew.Issue, error)
	// Move moves the issue identified by issueKey from one state to
	// another, and leaves it in exactly one crew state, to, without
	// touching what is not crew's. It returns an error wrapping
	// ErrMovedMeanwhile when the issue is closed or not in from, one
	// wrapping ErrRefused when the tracker refuses for good, and any other
	// error when the move failed transiently. It returns nil, changing
	// nothing, when the issue is already exactly in to and not in from,
	// whatever other labels it carries, so retrying a move that landed is
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
	// Identity is who the session acts as on the tracker; the zero Identity
	// is you.
	Identity Identity
	// CodeOwners and Bots are the code owners' logins and the logins of the
	// bots the config names. The session gets them as CREW_CODE_OWNERS and
	// CREW_BOTS, each joined by single spaces, so a prompt can name the
	// issues crew takes.
	CodeOwners []string
	Bots       []string
}

// Identity is who a child process, such as a session or a check, acts as on
// the tracker: one of crew's bots, or, as the zero Identity, you. The
// zero Identity changes nothing. An Identity never holds a key or a token,
// only where the child finds one.
type Identity struct {
	// Bot is the bot's name, as the config names it.
	Bot string
	// Login is the login the bot acts as, such as crew-ops[bot].
	Login string
	// Env holds KEY=value entries added to the child's environment.
	Env []string
	// Unset names the variables of crew's environment the child must not
	// inherit, such as a token of yours.
	Unset []string
	// Renew, when not nil, renews the identity's token at once, such as when
	// the tracker was refused for an expired one.
	Renew func(ctx context.Context) error
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
	// Name is unique among the workspaces that exist, and safe in a file
	// name: the session's log is named after it, so a reopened workspace
	// keeps its log, and a name reused once its workspace is gone reuses
	// the log too.
	Name string
	// Dir is the workspace's absolute directory.
	Dir string
	// Branch is the branch the action's work goes on.
	Branch string
}

// Preparer is an optional interface of any port's adapter: it checks the
// adapter's tools and prepares the adapter before the first poll.
type Preparer interface {
	// Prepare checks and prepares the adapter for a set of rules that can
	// request states, so an adapter creates or checks only what the rules
	// use. An error names the tool or setting at fault, and crew stops
	// before polling.
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

// StatusReporter is an optional interface of a Tracker: it keeps a status
// comment on each issue that shows where the issue stands, with one entry
// per rule run, oldest first. A tracker without it reports no status, and
// crew works as it does without status comments.
type StatusReporter interface {
	// ReportStatus shows status on its issue, formatted in the tracker's own
	// markup: it edits the latest entry of the issue's status comment when
	// that entry is of status's run, and appends a new entry otherwise. It
	// creates the comment when the issue has none, and continues a full
	// comment in a new one. The engine never has two calls for one issue in
	// flight. Its errors are classified as Tracker.Move's are.
	ReportStatus(ctx context.Context, status crew.Status) error
}

// PullRequestReporter is an optional interface of a Tracker: it shows crew's
// state on the open pull requests that close an issue. A tracker without it
// writes nothing to pull requests, and crew works as it does without them.
type PullRequestReporter interface {
	// ReportPullRequests puts each open pull request that closes report's
	// issue in report.State, as Move puts the issue, removing every other
	// crew state it carries without touching what is not crew's.
	// When report.End is set, it also posts a new comment on each saying
	// that the rule ended and nobody watches the pull request any more. An
	// issue without such a pull request gets nothing. A retry of the same
	// report, by its ID, posts no comment twice. The engine never has two
	// calls for one issue in flight. Its errors are classified as
	// Tracker.Move's are.
	ReportPullRequests(ctx context.Context, report crew.PullRequestReport) error
}

// Acting is an optional interface of a Tracker: it acts as one of crew's
// bots. A tracker without it acts as the gh login and takes only the code
// owners' items, and crew works as it does without bots.
type Acting interface {
	// ActAs makes the tracker's own writes, such as its moves, comments and
	// failure reports, as writer, the zero Identity being you, and makes it
	// take the items the logins in bots opened as well as the code owners'.
	// The engine calls it once, before Prepare.
	ActAs(writer Identity, bots []string)
}

// CodeOwnerFinder is an optional interface of a Tracker: it tells who the
// code owners are. A tracker without it names no code owner.
type CodeOwnerFinder interface {
	// CodeOwners returns the code owners' logins as Prepare found them. The
	// engine calls it once Prepare succeeded.
	CodeOwners() []string
}

// LoginFinder is an optional interface of a Tracker: it tells the login the
// tracker acts as when it acts as you. A tracker without it names no
// login.
type LoginFinder interface {
	// Login returns the login the tracker acts as when it acts as you,
	// as Prepare found it, or "" before. It is safe to call from any
	// goroutine.
	Login() string
}

// WriterReporter is an optional interface of a Tracker that acts as a bot:
// it tells when the tracker's own writes went back to you. A tracker
// without it never reports one.
type WriterReporter interface {
	// WriterLost returns the warning crew wrote when the tracker's writes
	// went back to you for the rest of the run, or "" while they go as
	// the writer, or the writer is you. It is safe to call from any
	// goroutine.
	WriterLost() string
}

// BoardLister is an optional interface of a Tracker: it lists the issues of
// the board the config draws, whose labels need not be crew's. crew refuses a
// config with a board when its tracker lacks it.
type BoardLister interface {
	// ListBoard returns the open issues the code owners or one of the bots
	// opened that carry any of labels, never a pull request, oldest first.
	// Each carries the labels of labels it carries, matched as the tracker
	// matches labels, spelled as labels spells them and in its order. An error
	// means the board could not be read; it is transient.
	ListBoard(ctx context.Context, labels []string) ([]crew.BoardIssue, error)
}

// Reopener is an optional interface of a Workspace: it reopens a workspace
// it created before, so a failed action can resume where it stopped. A
// workspace without it creates a fresh workspace for every action.
type Reopener interface {
	// Reopen returns the workspace space names, as it is now, without
	// changing what it holds: space carries the Name and Branch crew
	// recorded, and the returned Space the current Dir and Branch. It
	// returns an error wrapping ErrWorkspaceGone when the workspace no
	// longer exists, and any other error when it cannot be reopened.
	Reopen(ctx context.Context, space Space) (Space, error)
}

// Narrator is an optional interface of a harness's Session: it tells what
// the session last said, for the issue's status.
type Narrator interface {
	// Said returns the last thing the session said, on one line, or "" when
	// it said nothing yet. It may be called from any goroutine while the
	// session runs and after it ended.
	Said() string
}

// UsageReporter is an optional interface of a harness's Session: it tells
// what the session used, such as its cost and tokens. A session without it
// reports nothing, and crew shows its usage as not reported.
type UsageReporter interface {
	// Usage returns what the session used. It is called once Wait has
	// returned. A session that ended without a final result, such as one
	// that was stopped, killed or crashed, reports nothing.
	Usage() crew.Usage
}

// PullRequestFinder is an optional interface of a Tracker: it finds the
// pull request an action opened from its branch. A tracker without it
// leaves every action's pull request not looked up.
type PullRequestFinder interface {
	// FindPullRequest returns the pull request opened from branch in the
	// tracker's repository: an open one first, otherwise the newest closed
	// or merged one created at or after since, or none. A zero since
	// accepts any. It returns an error when it cannot tell, and the engine
	// then records the pull request as not looked up.
	FindPullRequest(ctx context.Context, branch string, since time.Time) (crew.PullRequest, error)
}

// ErrCheckFailed means a check ran and exited with a non-zero status.
var ErrCheckFailed = errors.New("the check failed")

// Checker runs action checks: a command you wrote, run in an action's
// workspace once its session succeeded, so crew does not judge the action
// by what its session says alone.
type Checker interface {
	// Check runs check to its end, with its output going to check.Output.
	// It returns nil when the command exited 0, and an error wrapping
	// ErrCheckFailed when it exited otherwise. When ctx ends first, it ends
	// the command and what the command started, and returns an error
	// wrapping ctx.Err(). Any other error means the command could not start.
	Check(ctx context.Context, check Check) error
}

// Check is what a Checker needs to run a check. The issue reaches the
// command only through these fields, as environment variables, never as
// part of the command, so no issue text can run as code.
type Check struct {
	// Dir is the action's workspace directory, where the command runs.
	Dir string
	// Command is the shell command to run.
	Command string
	// IssueRef, IssueKey and IssueURL identify the issue, as in crew.Issue.
	IssueRef string
	IssueKey string
	IssueURL string
	// Branch is the branch the action's work went on.
	Branch string
	// Output receives everything the command prints, stdout and stderr
	// together, from one goroutine at a time; nil discards it.
	Output io.Writer
	// Identity is who the command acts as on the tracker, the same as its
	// action's session; the zero Identity is you.
	Identity Identity
	// CodeOwners and Bots are the code owners' logins and the logins of the
	// bots the config names, as in Run.
	CodeOwners []string
	Bots       []string
}
