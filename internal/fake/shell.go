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

// ShellScript is how a fake script, such as a shell action's, runs.
type ShellScript struct {
	// Print is written to the script's output.
	Print string
	// Exit is the exit status the script reports.
	Exit int
	// Block makes the script run until its context ends, after printing.
	Block bool
	// Delay makes the script take that long, after printing, before it
	// exits as Exit says; its context ending first ends it.
	Delay time.Duration
	// StartErr, when set, makes the script fail to start; nothing else of
	// it applies.
	StartErr error
}

// Shell is a scripted shell: each script runs as scripted for its branch
// and name, or else for its branch, and an unscripted script exits 0. It
// records every script it ran.
// Its zero value is not usable; use NewShell.
type Shell struct {
	mu      sync.Mutex
	scripts map[string]ShellScript // by branch, or by branch and name
	runs    []port.Script
}

// NewShell returns a shell whose scripts all exit 0 until scripted.
func NewShell() *Shell {
	return &Shell{scripts: map[string]ShellScript{}}
}

// Script makes every script of the run whose branch is branch run as s.
func (sh *Shell) Script(branch string, s ShellScript) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.scripts[branch] = s
}

// ScriptAction makes the script of the shell action called name, in the
// run whose branch is branch, run as s, whatever Script set for the branch.
func (sh *Shell) ScriptAction(branch string, name crew.ActionName, s ShellScript) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.scripts[scriptKey(branch, name)] = s
}

// scriptKey is the key of the script ScriptAction sets for the shell
// action called name, in the run whose branch is branch.
func scriptKey(branch string, name crew.ActionName) string {
	return branch + "\x00" + string(name)
}

// Runs returns the scripts run so far, in the order they started.
func (sh *Shell) Runs() []port.Script {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return slices.Clone(sh.runs)
}

// Run implements port.Shell.
func (sh *Shell) Run(ctx context.Context, script port.Script) (port.ShellResult, error) {
	sh.mu.Lock()
	sh.runs = append(sh.runs, script)
	s, ok := sh.scripts[scriptKey(script.Branch, script.Name)]
	if !ok {
		s = sh.scripts[script.Branch]
	}
	sh.mu.Unlock()
	if s.StartErr != nil {
		return port.ShellResult{}, s.StartErr
	}
	if s.Print != "" && script.Output != nil {
		_, _ = io.WriteString(script.Output, s.Print)
	}
	if s.Block {
		<-ctx.Done()
		return port.ShellResult{}, fmt.Errorf("the script was ended: %w", ctx.Err())
	}
	if s.Delay > 0 {
		select {
		case <-time.After(s.Delay):
		case <-ctx.Done():
			return port.ShellResult{}, fmt.Errorf("the script was ended: %w", ctx.Err())
		}
	}
	return port.ShellResult{Status: s.Exit}, nil
}
