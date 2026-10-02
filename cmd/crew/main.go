// Command crew polls the issue tracker and runs each workflow stage's actions in
// coding-agent sessions, as configured in the repository's .crew/config.yaml.
//
// Usage:
//
//	crew [--plain] [--version]
//
// It runs from anywhere inside a git repository. On a terminal it shows a TUI;
// otherwise, or with --plain, it prints timestamped event lines. The first
// Ctrl-C, SIGTERM or SIGHUP stops it cleanly, and a second one forces the
// exit. A closed output stops it cleanly as well. It
// exits 0 on a clean stop, 1 on a runtime failure or a forced exit, and 2 on
// a config or environment error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"

	"github.com/thatsnotmynameio/crew/internal/adapter/git"
	"github.com/thatsnotmynameio/crew/internal/adapter/shell"
	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

// version is set at build time with -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

// run gathers what app.Run needs from the process and returns crew's exit
// code.
func run(args []string) int {
	stdout, stderr := os.Stdout, os.Stderr
	flags := flag.NewFlagSet("crew", flag.ContinueOnError)
	flags.SetOutput(stderr)
	plain := flags.Bool("plain", false, "print timestamped event lines instead of the TUI")
	showVersion := flags.Bool("version", false, "print crew's version and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return app.ExitClean
		}
		return app.ExitConfig
	}
	if flags.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "crew: unexpected argument %q\n", flags.Arg(0))
		flags.Usage()
		return app.ExitConfig
	}
	if *showVersion {
		_, _ = fmt.Fprintln(stdout, "crew", version)
		return app.ExitClean
	}

	// Signals are caught from here on: none may kill crew before the
	// engine's stop sequence, or a forced exit, has ended its children.
	// SIGHUP, from a closing terminal, stops crew as SIGINT and SIGTERM do.
	// SIGPIPE is ignored, so a closed stdout fails the renderer's write,
	// which stops crew cleanly too.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	signal.Ignore(syscall.SIGPIPE)

	ctx := context.Background()
	var group proc.Group
	out, err := group.Run(ctx, proc.Command{Name: "git", Args: []string{"rev-parse", "--show-toplevel"}})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "crew must run inside a git repository: %v\n", err)
		return app.ExitConfig
	}
	root := strings.TrimSpace(string(out.Stdout))
	home, _ := os.UserHomeDir() // without one, nothing is shortened to ~

	return app.Run(ctx, app.Options{
		Registry:  registry.Default(&group),
		Workspace: func(root string) port.Workspace { return git.New(&group, root) },
		Checker:   shell.New(&group),
		Root:      root,
		Home:      home,
		Stdout:    stdout,
		Stderr:    stderr,
		Terminal:  term.IsTerminal(int(stdout.Fd())),
		Plain:     *plain,
		Group:     &group,
		Signals:   signals,
	})
}
