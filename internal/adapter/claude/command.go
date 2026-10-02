package claude

import "github.com/thatsnotmynameio/crew/internal/proc"

// binary is the Claude Code CLI the adapter runs.
const binary = "claude"

// command builds the headless Claude Code run of prompt with model, in dir.
// It is pure, and kept apart from the stream parser, so that building the
// command and judging the session change independently. The stream-json
// output, which needs --verbose with -p, is what the parser judges the
// session by; proc closes stdin. The prompt goes last, after --, so one that
// starts with a dash (a Markdown list, an issue title) is not read as an
// option.
func command(prompt, model, dir string) proc.Command {
	return proc.Command{
		Name: binary,
		Args: []string{
			"-p",
			"--model", model,
			"--permission-mode", "auto",
			"--output-format", "stream-json",
			"--verbose",
			"--", prompt,
		},
		Dir: dir,
	}
}
