// Package shell is the check adapter: it runs each action's check with sh,
// in the action's workspace, as a child process of crew.
package shell

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Compile-time guard.
var _ port.Checker = (*Checker)(nil)

// stopTimeout is how long a check ended by its context gets to stop before
// it is killed.
const stopTimeout = 10 * time.Second

// filePerm is the permission of the files holding the session's prompt and
// last message, which may hold what the session read or printed.
const filePerm = 0o600

// Checker runs checks with sh -c. It is safe for concurrent use.
type Checker struct {
	group *proc.Group
}

// New returns a checker that starts its checks in group, so a forced exit
// kills them with crew's other children.
func New(group *proc.Group) *Checker {
	return &Checker{group: group}
}

// Check implements port.Checker. The command runs as sh's -c argument,
// acting as check.Identity, with CREW_ISSUE_REF, CREW_ISSUE_KEY,
// CREW_ISSUE_URL, CREW_BRANCH, CREW_CODE_OWNERS, CREW_BOTS and CREW_ACTION
// set, and stdout and stderr on one pipe, so its output keeps the order it
// was printed in. The session's prompt and last message are in files that
// CREW_PROMPT_FILE and CREW_LAST_MESSAGE_FILE name, in a directory only you
// can read, removed once the check ended: a file has no size limit, where
// one environment string does.
func (c *Checker) Check(ctx context.Context, check port.Check) error {
	dir, err := os.MkdirTemp("", "crew-check-")
	if err != nil {
		return fmt.Errorf("create the directory of the check's files: %w", err)
	}
	// What a failed removal leaves is in the system's temporary directory,
	// readable by you alone.
	defer func() { _ = os.RemoveAll(dir) }()
	prompt, last := filepath.Join(dir, "prompt"), filepath.Join(dir, "last-message")
	for path, text := range map[string]string{prompt: check.Prompt, last: check.LastMessage} {
		if err := os.WriteFile(path, []byte(text), filePerm); err != nil {
			return fmt.Errorf("write the check's files: %w", err)
		}
	}
	env := slices.Clone(check.Identity.Env)
	env = append(env,
		"CREW_ISSUE_REF="+check.IssueRef,
		"CREW_ISSUE_KEY="+check.IssueKey,
		"CREW_ISSUE_URL="+check.IssueURL,
		"CREW_BRANCH="+check.Branch,
		"CREW_CODE_OWNERS="+strings.Join(check.CodeOwners, " "),
		"CREW_BOTS="+strings.Join(check.Bots, " "),
		"CREW_ACTION="+string(check.Action),
		"CREW_PROMPT_FILE="+prompt,
		"CREW_LAST_MESSAGE_FILE="+last,
	)
	p, err := c.group.Start(proc.Command{
		Name: "sh", Args: []string{"-c", check.Command}, Dir: check.Dir,
		Env: env, Unset: check.Identity.Unset,
	}, check.Output, check.Output)
	if err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- p.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("%w: %w", port.ErrCheckFailed, err)
		}
		return nil
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
		defer cancel()
		// Stop kills the group at its deadline, so its error adds nothing.
		_ = p.Stop(stop)
		<-exited
		return fmt.Errorf("the check was ended: %w", ctx.Err())
	}
}
