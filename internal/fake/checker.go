package fake

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guard.
var _ port.Checker = (*Checker)(nil)

// CheckScript is how a fake check runs.
type CheckScript struct {
	// Print is written to the check's output.
	Print string
	// Exit is the check's exit status: zero passes, anything else fails.
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

// Checker is a scripted checker: each check runs as scripted for its branch
// and name, or else for its branch, and an unscripted check passes. It
// records every check it ran.
// Its zero value is not usable; use NewChecker.
type Checker struct {
	mu      sync.Mutex
	scripts map[string]CheckScript // by branch, or by branch and name
	checks  []port.Check
}

// NewChecker returns a checker whose checks all pass until scripted.
func NewChecker() *Checker {
	return &Checker{scripts: map[string]CheckScript{}}
}

// Script makes the check of the action whose branch is branch run as s.
func (c *Checker) Script(branch string, s CheckScript) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scripts[branch] = s
}

// ScriptCheck makes the check called name, of the action whose branch is
// branch, run as s, whatever Script set for the branch.
func (c *Checker) ScriptCheck(branch, name string, s CheckScript) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scripts[scriptKey(branch, name)] = s
}

// scriptKey is the key of the script ScriptCheck sets for the check called
// name, of the action whose branch is branch.
func scriptKey(branch, name string) string {
	return branch + "\x00" + name
}

// Checks returns the checks run so far, in the order they started.
func (c *Checker) Checks() []port.Check {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.checks)
}

// Check implements port.Checker.
func (c *Checker) Check(ctx context.Context, check port.Check) error {
	c.mu.Lock()
	c.checks = append(c.checks, check)
	s, ok := c.scripts[scriptKey(check.Branch, check.Name)]
	if !ok {
		s = c.scripts[check.Branch]
	}
	c.mu.Unlock()
	if s.StartErr != nil {
		return s.StartErr
	}
	if s.Print != "" && check.Output != nil {
		_, _ = io.WriteString(check.Output, s.Print)
	}
	if s.Block {
		<-ctx.Done()
		return fmt.Errorf("the check was ended: %w", ctx.Err())
	}
	if s.Delay > 0 {
		select {
		case <-time.After(s.Delay):
		case <-ctx.Done():
			return fmt.Errorf("the check was ended: %w", ctx.Err())
		}
	}
	if s.Exit != 0 {
		return fmt.Errorf("%w: exit status %d", port.ErrCheckFailed, s.Exit)
	}
	return nil
}
