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

// waitingParagraph is what crew appends, after the verdict paragraph, to
// the prompt of a session that may wait for an answer (R19, R40, R41,
// R43, KTD-W10): how to ask on the issue with its marker, how long to wait
// and in what checks, who may answer, the one command to read the
// comments with, and when to write and end with waiting.
func waitingParagraph(w waiting) string {
	wait, check := forPeople(w.wait), forPeople(checkLimit)
	var b strings.Builder
	fmt.Fprintf(&b, "crew: this session may wait for an answer on the issue. When you need a decision you cannot "+
		"make on your own, ask it as one comment on issue %s, and put your marker `%s` in it. Every comment you "+
		"post on the issue while you may wait carries that marker. Right after you post the question, write "+
		"`%s` on the first line of the file `%s` names.\n\n", w.issue, w.marker, crew.Waiting, verdictFileVariable)
	fmt.Fprintf(&b, "Wait up to %s for an answer. Wait through repeated checks, each one command of at most %s, "+
		"such as a `sleep` and then the command below; never run one command as long as the whole wait. When "+
		"your tool has a command timeout, set it above %s.\n\n", wait, check, check)
	b.WriteString(whoMayAnswer(w))
	fmt.Fprintf(&b, "\n\nRead the issue's comments only with this command, and never list comment bodies any "+
		"other way:\n\n```sh\n%s\n```\n\n%s\n\n", readCommand(w), readOutput(w))
	fmt.Fprintf(&b, "Once an answer counts, replace `%[1]s` in the file with your final verdict, or empty the "+
		"file, and go on with the work. Check once more right before you end with `%[1]s`. When no answer came "+
		"within %[2]s, end the session with the verdict `%[1]s`.", crew.Waiting, wait)
	return b.String()
}

// whoMayAnswer returns who may answer a waiting session (R37, R40, R41):
// the code owners who are not Apps and the Apps on the list other than
// the session's own login, and that no other comment is an answer.
func whoMayAnswer(w waiting) string {
	owners, apps := "no person, as crew found no code owner", "no App"
	if len(w.owners) > 0 {
		owners = "the code owners " + logins(w.owners) + ", when `user.type` is not `Bot`"
	}
	if len(w.apps) > 0 {
		apps = "the Apps on crew's answering list other than you, " + logins(w.apps) + ", only when `user.type` is `Bot`"
	}
	var you string
	switch {
	case w.login == "":
	case slices.ContainsFunc(w.owners, func(o string) bool { return strings.EqualFold(o, w.login) }):
		you = fmt.Sprintf(" You act as `%[1]s`, one of the code owners: a comment of `%[1]s` without your marker "+
			"is an answer, and yours carry the marker, so they never are.", w.login)
	default:
		you = fmt.Sprintf(" You act as `%s`.", w.login)
	}
	return fmt.Sprintf("Only these may answer: %s; and %s. Logins match ignoring case and keep their `[bot]` "+
		"suffix.%s A comment that holds `%s` is never an answer. Any other comment is not an answer: ignore it "+
		"and keep waiting.", owners, apps, you, crew.MarkerPrefix)
}

// readOutput returns what the read command prints, and which of its lines
// are the answers.
func readOutput(w waiting) string {
	if w.login == "" {
		return "It prints one JSON object per line: the `created_at`, `login` and `body` of each comment that may " +
			"answer. crew does not know the login you act as, so the command prints no line for your question: " +
			"your question is your own latest comment with your marker, and the answers are the lines created after it."
	}
	return "It prints one JSON object per line: a line with `\"question\":true` and the `created_at` of each " +
		"comment of yours that holds your marker, and the `created_at`, `login` and `body` of each comment that " +
		"may answer. The answers are the lines without `question` created after your latest question line."
}

// logins returns each of logins in backquotes, joined by commas.
func logins(logins []string) string {
	quoted := make([]string, len(logins))
	for i, l := range logins {
		quoted[i] = "`" + l + "`"
	}
	return strings.Join(quoted, ", ")
}

// oneLine joins the words of s with single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
