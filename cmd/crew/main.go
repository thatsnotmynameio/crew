// Command crew polls the issue tracker and runs each rule's actions in
// coding-agent sessions, as configured in the repository's .crew/config.yaml
// and .crew/config.local.yaml.
//
// Usage:
//
//	crew [--plain] [--version]
//	crew bots create <name>
//
// It runs from anywhere inside a git repository. On a terminal it shows a
// TUI; otherwise, or with --plain, it prints timestamped event lines. The
// first Ctrl-C, SIGTERM or SIGHUP stops it cleanly, and a second one forces
// the exit. A closed output stops it cleanly as well. It exits 0 on a clean
// stop, 1 on a runtime failure or a forced exit, and 2 on a config or
// environment error.
//
// crew bots create <name> creates a bot, a GitHub identity of crew's own,
// for the GitHub repository of the git repository it runs in, and installs
// it there. It exits 0 once the bot is ready, 2 when nothing was asked of
// GitHub yet, and 1 on any later failure.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
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

// version is set at build time with -ldflags "-X main.version=vX.Y.Z", as
// the release builds do. Without it, crewVersion falls back to Go's build
// info.
var version string

// signalBuffer holds the two signals crew acts on, the one that stops it and
// the one that forces the exit, so neither is lost while crew is busy.
const signalBuffer = 2

func main() {
	os.Exit(run(os.Args[1:]))
}

// run gathers what app.Run needs from the process and returns crew's exit
// code.
func run(args []string) int {
	stdout, stderr := os.Stdout, os.Stderr
	// The subcommand comes before crew's own flags, so every other argument
	// list is parsed as it always was.
	if len(args) > 0 && args[0] == "bots" {
		return runBots(args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("crew", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprint(stderr, "Usage:\n  crew [--plain] [--version]\n  crew bots create <name>\n\nFlags:\n")
		flags.PrintDefaults()
	}
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
		info, _ := debug.ReadBuildInfo()
		_, _ = fmt.Fprintln(stdout, "crew", crewVersion(version, info))
		return app.ExitClean
	}
	return start(*plain, stdout, stderr)
}

// crewVersion is the version crew prints: the one stamped at build time,
// otherwise the module version Go recorded in info (go install …@vX.Y.Z
// records vX.Y.Z, and a go build in a checkout a version derived from it),
// otherwise "dev". info is nil when the binary carries no build info.
func crewVersion(stamped string, info *debug.BuildInfo) string {
	if stamped != "" {
		return stamped
	}
	if info != nil && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// start catches the stop signals, finds the repository's root and runs crew
// there, returning its exit code. plain is the --plain flag.
func start(plain bool, stdout, stderr *os.File) int {
	// Signals are caught from here on: none may kill crew before the
	// engine's stop sequence, or a forced exit, has ended its children.
	// SIGHUP, from a closing terminal, stops crew as SIGINT and SIGTERM do.
	// SIGPIPE is ignored, so a closed stdout fails the renderer's write,
	// which stops crew cleanly too.
	signals := make(chan os.Signal, signalBuffer)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	signal.Ignore(syscall.SIGPIPE)

	ctx := context.Background()
	var group proc.Group
	root, err := repoRoot(ctx, group.Run)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "crew must run inside a git repository: %v\n", err)
		return app.ExitConfig
	}
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
		Plain:     plain,
		Group:     &group,
		Signals:   signals,
		Bots:      actingBots(group.Run, root),
	})
}

// repoRoot returns the root of the git repository crew runs in, asking git
// through run.
func repoRoot(ctx context.Context, run proc.Runner) (string, error) {
	out, err := run(ctx, proc.Command{Name: "git", Args: []string{"rev-parse", "--show-toplevel"}})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out.Stdout)), nil
}
