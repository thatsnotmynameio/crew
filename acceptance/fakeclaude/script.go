// Package fakeclaude is a scripted Claude Code for tests. The program under
// test runs claude in headless mode, with a prompt and a working directory,
// and reads the session from claude's stream-json output: one JSON event per
// line, ending with a result event that says whether the session succeeded,
// and the process's exit code.
//
// A test registers scripts with Script, each keyed by a piece of text that
// the prompt must contain. Every claude invocation runs the first script not
// yet used whose key appears in its prompt, so the same key registered twice
// answers two invocations in order. A script is a Go function: it receives
// the session (working directory, prompt, model and a handle on the fake
// GitHub), may change files in the working directory or GitHub's state,
// writes its events with Session.Emit and returns claude's exit code.
// Succeed, Fail and NoResult are the common scripts, and the Session methods
// build their events, with a fixed usage (see Cost) so screens that show it
// stay the same from run to run.
//
// The fake is strict: an invocation whose flags it does not know, or whose
// prompt no script matches, is a violation. Run then prints why on standard
// error, exits 1 and names the call in Outcome.Violation.
package fakeclaude

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
)

// Invocation is one run of claude.
type Invocation struct {
	// Args is claude's argument list, without the leading "claude".
	Args []string
	// Dir is the working directory claude was started in.
	Dir string
	// Env holds GH_CONFIG_DIR and the CREW_* variables the invocation
	// carried; the double forwards no other variable.
	Env map[string]string
	// IgnoreStop, when set, makes the double that runs the invocation ignore
	// SIGTERM from then on, and returns once it does. Session.IgnoreStop
	// calls it.
	IgnoreStop func()
}

// Outcome is how a claude invocation ended.
type Outcome struct {
	// Code is claude's exit code.
	Code int
	// Violation is "" for an invocation the fake knows. For one it does not
	// know, it is the call as a quoted command line, the prompt cut after
	// 200 characters, followed by what the fake did not know, in
	// parentheses; Code is then 1.
	Violation string
}

// ScriptFunc is the body of a scripted session. It writes the session's
// stream-json events with s.Emit as the session goes, and returns claude's
// exit code. ctx is cancelled when the program under test stops claude, for
// example with SIGTERM: a script that blocks should give up then.
type ScriptFunc func(ctx context.Context, s *Session) int

// Session is one scripted claude session.
type Session struct {
	// Dir is the session's working directory, where a script may write
	// files. A script runs in the test process, so a git command it runs
	// there needs an environment of its own, not the test's.
	Dir string
	// Prompt is the prompt claude was given.
	Prompt string
	// Model is the model named with --model, "" when none was.
	Model string
	// PermissionMode is the mode named with --permission-mode, "" when none
	// was.
	PermissionMode string
	// Env holds GH_CONFIG_DIR and the CREW_* variables the invocation
	// carried; the double forwards no other variable.
	Env map[string]string
	// GitHub is the fake GitHub the Claude was built with, which a script
	// may change as a session that runs gh would; nil when it was built
	// without one.
	GitHub *fakegithub.GitHub

	out        io.Writer
	ignoreStop func()
}

// script is a registered ScriptFunc.
type script struct {
	key  string
	fn   ScriptFunc
	used bool
}

// Claude is the fake Claude Code: its scripts, in the order they were
// registered. Build it with New. Its methods are safe for concurrent use,
// and scripts of parallel invocations run concurrently.
type Claude struct {
	mu      sync.Mutex
	github  *fakegithub.GitHub
	scripts []*script
}

// New returns a Claude with no scripts, whose sessions get gh as their
// GitHub handle; gh may be nil.
func New(gh *fakegithub.GitHub) *Claude {
	return &Claude{github: gh}
}

// Script registers fn to answer the first invocation not yet answered whose
// prompt contains key. Scripts registered earlier are tried first.
func (c *Claude) Script(key string, fn ScriptFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scripts = append(c.scripts, &script{key: key, fn: fn})
}

// Run answers one claude invocation: it runs the matching script, which
// writes its stream-json events to stdout, and returns the exit code the
// script returned. An invocation the fake does not know is a violation: Run
// writes why to stderr and returns code 1 with the violation.
func (c *Claude) Run(ctx context.Context, inv Invocation, stdout, stderr io.Writer) Outcome {
	s, reason := parseArgs(inv.Args)
	if reason == "" {
		var fn ScriptFunc
		if fn = c.take(s.Prompt); fn != nil {
			s.Dir, s.Env, s.GitHub, s.out, s.ignoreStop = inv.Dir, inv.Env, c.github, stdout, inv.IgnoreStop
			return Outcome{Code: fn(ctx, s)}
		}
		reason = "no script matches the prompt"
	}
	v := fakegithub.CommandLine("claude", inv.Args) + " (" + reason + ")"
	fmt.Fprintf(stderr, "acceptance: unknown claude call: %s\n", v)
	return Outcome{Code: 1, Violation: v}
}

// take marks the first unused script whose key prompt contains as used and
// returns it, or nil when there is none.
func (c *Claude) take(prompt string) ScriptFunc {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := slices.IndexFunc(c.scripts, func(s *script) bool {
		return !s.used && strings.Contains(prompt, s.key)
	})
	if i < 0 {
		return nil
	}
	c.scripts[i].used = true
	return c.scripts[i].fn
}

// parseArgs reads claude's headless command line: -p (--print), --verbose,
// --model, --permission-mode and --output-format stream-json, then the
// prompt as the one argument after --. It returns the session it describes
// and what it does not know about args, "" when it knows everything.
func parseArgs(args []string) (*Session, string) {
	s := &Session{}
	values := map[string]*string{"--model": &s.Model, "--permission-mode": &s.PermissionMode}
	var format string
	values["--output-format"] = &format
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if len(args)-i != 2 { //nolint:mnd // -- and the prompt
				return nil, "want one prompt after --"
			}
			s.Prompt = args[i+1]
			return s, check(seen, format)
		case a == "-p" || a == "--print" || a == "--verbose":
			seen[a] = true
		case values[a] != nil && i+1 < len(args):
			i++
			*values[a] = args[i]
		default:
			return nil, "unknown argument " + a
		}
	}
	return nil, "no prompt after --"
}

// check returns what is wrong with a headless run that gave the flags seen
// and the output format: claude prints stream-json only in print mode, and
// only with --verbose.
func check(seen map[string]bool, format string) string {
	switch {
	case !seen["-p"] && !seen["--print"]:
		return "not in print mode (-p)"
	case format != "stream-json":
		return "output format " + fmt.Sprintf("%q", format) + ", not stream-json"
	case !seen["--verbose"]:
		return "stream-json without --verbose"
	}
	return ""
}
