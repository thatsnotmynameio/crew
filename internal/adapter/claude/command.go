package claude

import (
	"slices"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// binary is the Claude Code CLI the adapter runs.
const binary = "claude"

// bashDefaultTimeout and bashMaxTimeout raise the Bash tool's timeouts, in
// milliseconds, from Claude Code's two-minute default and ten-minute ceiling
// to ten and thirty minutes. A Bash command that outlives its timeout moves to
// the background, and a headless session that ends its turn waiting on it
// ends there: claude stops the command and exits 0, which reads as a success.
// Ten minutes keeps a full test run in the foreground.
const (
	bashDefaultTimeout = "BASH_DEFAULT_TIMEOUT_MS=600000"
	bashMaxTimeout     = "BASH_MAX_TIMEOUT_MS=1800000"
)

// command builds the headless Claude Code run of run with model. It is pure,
// and kept apart from the stream parser, so that building the command and
// judging the session change independently. The stream-json output, which
// needs --verbose with -p, is what the parser judges the session by; proc
// closes stdin. The prompt goes last, after --, so one that starts with a dash
// (a Markdown list, an issue title) is not read as an option. The session acts
// as run's identity, with its environment added and the variables it unsets
// removed, and gets the code owners' and the bots' logins as
// CREW_CODE_OWNERS and CREW_BOTS.
func command(run port.Run, model string) proc.Command {
	env := slices.Concat(
		[]string{bashDefaultTimeout, bashMaxTimeout},
		run.Identity.Env,
		[]string{"CREW_CODE_OWNERS=" + strings.Join(run.CodeOwners, " "), "CREW_BOTS=" + strings.Join(run.Bots, " ")},
	)
	return proc.Command{
		Name: binary,
		Args: []string{
			"-p",
			"--model", model,
			"--permission-mode", "auto",
			"--output-format", "stream-json",
			"--verbose",
			"--", run.Prompt,
		},
		Dir:   run.Dir,
		Env:   env,
		Unset: run.Identity.Unset,
	}
}
