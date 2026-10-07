package core

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Every paragraph crew adds to a session's prompt is built here (KTD20).

// verdictFileVariable is the environment variable that names the file a
// session writes its verdict to; the engine sets it for every session.
const verdictFileVariable = "CREW_VERDICT_FILE"

// resumeParagraph is what crew appends to the prompt of a resumed session
// (R23, KTD7): that the session continues, in this worktree, the work of
// the run start resumes, which route that run ended through and why
// (start's Route and Reason), and where its output is. log is the
// repository-relative path of the log, and logFromDir the same path from
// the workspace; branch is the workspace's branch.
func resumeParagraph(start crew.StartAt, branch, log, logFromDir string) string {
	var b strings.Builder
	b.WriteString("crew: this session continues the work of an earlier run of this rule, in this worktree")
	if branch != "" {
		fmt.Fprintf(&b, ", on branch `%s`", branch)
	}
	reason := oneLine(start.Reason.String())
	if start.Route == "" {
		fmt.Fprintf(&b, ". That run stopped before it chose a route: %q.", reason)
	} else {
		fmt.Fprintf(&b, ". That run ended through the route `%s`: %q.", start.Route, reason)
	}
	fmt.Fprintf(&b, " Its output is in the log `%s` of the repository's main checkout", log)
	if logFromDir != "" {
		fmt.Fprintf(&b, " (`%s` from this worktree)", logFromDir)
	}
	b.WriteString(", above the line crew wrote there when this session started. ")
	b.WriteString("Check the worktree's state with `git status` and `git log` before you go on, ")
	b.WriteString("and continue from where it stopped instead of starting over.")
	return b.String()
}

// verdictParagraph is what crew appends to the prompt of a session whose
// on: names verdicts (R9): that it may end with one of them, by writing it
// to the file the CREW_VERDICT_FILE variable names, the verdicts in name
// order. It returns false for an on: that names none.
func verdictParagraph(on crew.On) (string, bool) {
	if len(on) == 0 {
		return "", false
	}
	verdicts := slices.Sorted(maps.Keys(on))
	names := make([]string, 0, len(verdicts))
	for _, v := range verdicts {
		names = append(names, "`"+string(v)+"`")
	}
	return fmt.Sprintf(
		"crew: you may end this session with a verdict, one of %s, by writing it on the first line of "+
			"the file the environment variable `%s` names. crew then sends the issue where that verdict "+
			"leads. Leave the file empty to be judged by how the session ends.",
		strings.Join(names, ", "), verdictFileVariable,
	), true
}

// oneLine joins the words of s with single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
