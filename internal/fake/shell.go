package fake

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guard.
var _ port.Shell = (*Shell)(nil)

// CheckScript is how a fake script, such as a check, runs.
type CheckScript struct {
	// Print is written to the check's output.
	Print string
	// Exit is the exit status the script reports.
	Exit int
	// Block makes the check run until its context ends, after printing.
	Block bool
	// Delay makes the check take that long, after printing, before it
	// exits as Exit says; its context ending first ends it.
	Delay time.Duration
	// StartErr, when set, makes the check fail to start; nothing else of
	// the script applies.
	StartErr error
}

// Shell is a scripted shell: each script runs as scripted for its branch
// and name, or else for its branch, and an unscripted script exits 0. It
// records every script it ran.
// Its zero value is not usable; use NewShell.
type Shell struct {
	mu      sync.Mutex
	scripts map[string]CheckScript // by branch, or by branch and name
	runs    []port.Script
}

// NewShell returns a shell whose scripts all exit 0 until scripted.
func NewShell() *Shell {
	return &Shell{scripts: map[string]CheckScript{}}
}

// Script makes the check of the action whose branch is branch run as s.
func (c *Shell) Script(branch string, s CheckScript) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scripts[branch] = s
}

// ScriptCheck makes the check called name, of the action whose branch is
// branch, run as s, whatever Script set for the branch.
func (c *Shell) ScriptCheck(branch string, name crew.CheckName, s CheckScript) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scripts[scriptKey(branch, name)] = s
}

// scriptKey is the key of the script ScriptCheck sets for the check called
// name, of the action whose branch is branch.
func scriptKey(branch string, name crew.CheckName) string {
	return branch + "\x00" + string(name)
}

// Runs returns the scripts run so far, in the order they started.
func (c *Shell) Runs() []port.Script {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.runs)
}

// Run implements port.Shell.
func (c *Shell) Run(ctx context.Context, script port.Script) (port.ShellResult, error) {
	c.mu.Lock()
	c.runs = append(c.runs, script)
	s, ok := c.scripts[scriptKey(script.Branch, script.Name)]
	if !ok {
		s = c.scripts[script.Branch]
	}
	c.mu.Unlock()
	if s.StartErr != nil {
		return port.ShellResult{}, s.StartErr
	}
	if s.Print != "" && script.Output != nil {
		_, _ = io.WriteString(script.Output, s.Print)
	}
	if s.Block {
		<-ctx.Done()
		return port.ShellResult{}, fmt.Errorf("the check was ended: %w", ctx.Err())
	}
	if s.Delay > 0 {
		select {
		case <-time.After(s.Delay):
		case <-ctx.Done():
			return port.ShellResult{}, fmt.Errorf("the check was ended: %w", ctx.Err())
		}
	}
	return port.ShellResult{Status: s.Exit}, nil
}
