// Package shell is the shell adapter: it runs scripts, the config's shell
// actions, with sh in a rule run's workspace, as a child process of crew.
package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Compile-time guard.
var _ port.Shell = (*Shell)(nil)

// stopTimeout is how long a script ended by its context gets to stop before
// it is killed.
const stopTimeout = 10 * time.Second

// filePerm is the permission of the files holding the session's prompt and
// last message, which may hold what the session read or printed.
const filePerm = 0o600

// Shell runs scripts with sh -c. It is safe for concurrent use.
type Shell struct {
	group *proc.Group
}

// New returns a shell that starts its scripts in group, so a forced exit
// kills them with crew's other children.
func New(group *proc.Group) *Shell {
	return &Shell{group: group}
}

// Run implements port.Shell. The command runs as sh's -c argument,
// acting as script.Identity, with CREW_ISSUE_REF, CREW_ISSUE_KEY,
// CREW_ISSUE_URL, CREW_BRANCH, CREW_CODE_OWNERS, CREW_BOTS, CREW_ACTION, the
// latest session's name, and CREW_COMMENT_MARKER, crew's marker
// (crew.PostedMarker), with which a script marks the comments it posts as
// crew's, set, and stdout and stderr on one pipe, so its output keeps the
// order it was printed in. The latest session's prompt and
// last message are in files that CREW_PROMPT_FILE and
// CREW_LAST_MESSAGE_FILE name, in a directory only you can read, removed
// once the script ended: a file has no size limit, where one environment
// string does.
func (sh *Shell) Run(ctx context.Context, script port.Script) (port.ShellResult, error) {
	dir, err := os.MkdirTemp("", "crew-script-")
	if err != nil {
		return port.ShellResult{}, fmt.Errorf("create the directory of the script's files: %w", err)
	}
	// What a failed removal leaves is in the system's temporary directory,
	// readable by you alone.
	defer func() { _ = os.RemoveAll(dir) }()
	prompt, last := filepath.Join(dir, "prompt"), filepath.Join(dir, "last-message")
	for path, text := range map[string]string{prompt: script.Prompt, last: script.LastMessage} {
		if err := os.WriteFile(path, []byte(text), filePerm); err != nil {
			return port.ShellResult{}, fmt.Errorf("write the script's files: %w", err)
		}
	}
	env := slices.Clone(script.Identity.Env)
	env = append(env,
		"CREW_ISSUE_REF="+script.IssueRef,
		"CREW_ISSUE_KEY="+script.IssueID.Key,
		"CREW_ISSUE_URL="+script.IssueURL,
		"CREW_BRANCH="+script.Branch,
		"CREW_CODE_OWNERS="+strings.Join(script.CodeOwners, " "),
		"CREW_BOTS="+strings.Join(script.Bots, " "),
		"CREW_ACTION="+string(script.Session),
		"CREW_COMMENT_MARKER="+crew.PostedMarker,
		"CREW_PROMPT_FILE="+prompt,
		"CREW_LAST_MESSAGE_FILE="+last,
	)
	p, err := sh.group.Start(proc.Command{
		Name: "sh", Args: []string{"-c", script.Command}, Dir: script.Dir,
		Env: env, Unset: script.Identity.Unset,
	}, script.Output, script.Output)
	if err != nil {
		return port.ShellResult{}, err
	}
	exited := make(chan error, 1)
	go func() { exited <- p.Wait() }()
	select {
	case err := <-exited:
		return port.ShellResult{Status: status(err)}, nil
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
		defer cancel()
		// Stop kills the group at its deadline, so its error adds nothing.
		_ = p.Stop(stop)
		<-exited
		return port.ShellResult{}, fmt.Errorf("the script was ended: %w", ctx.Err())
	}
}

// status is the exit status of a script whose wait returned err: 0 for nil,
// the script's status for an exit, and -1 for a signal or a wait that did
// not tell, so a script that did not exit 0 never passes.
func status(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode()
	}
	return -1
}
