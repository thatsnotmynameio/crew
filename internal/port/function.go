package port

import (
	"context"
	"io"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Function is one of crew's own functions, which a rule calls by name as an
// action or a route step. It runs in crew's process, so it reads the issue
// from its call, not from CREW_* variables.
type Function interface {
	// Run runs the function once, to its end, and returns the verdict it
	// reached, one of those its FunctionDefinition declares. When ctx ends
	// first, it stops and returns an error wrapping ctx.Err(). Any other
	// error means the function could not reach a verdict.
	Run(ctx context.Context, call FunctionCall) (crew.Verdict, error)
}

// FunctionCall is what a Function needs for one run.
type FunctionCall struct {
	// Params strictly decodes the call's parameters, rendered from the
	// issue, as Decode does a section.
	Params Decode
	// IssueRef, IssueID and IssueURL identify the issue, as Ref, ID and URL
	// in crew.Issue.
	IssueRef string
	IssueID  crew.IssueID
	IssueURL string
	// Dir is the run's workspace, and Branch the branch its work goes on;
	// both empty for a run without a workspace.
	Dir    string
	Branch string
	// Log is the run's log, where the function writes what it did.
	Log io.Writer
	// Identity is the bot the function acts as on the tracker, the same as
	// the run's latest session; the zero Identity is you.
	Identity Identity
}

// RefusedParameterError is the error a FunctionFactory returns when it refuses a
// parameter's value, so the config can name that parameter's line.
type RefusedParameterError struct {
	// Parameter is the refused parameter's name, as the use writes it.
	Parameter string
	// Reason says why the value was refused, such as "must be at least 1".
	Reason string
}

// Error returns the parameter's name and the reason.
func (r RefusedParameterError) Error() string {
	return r.Parameter + ": " + r.Reason
}
