package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/mates"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// matesUsage is the one form crew mates takes.
const matesUsage = "usage: crew mates create <name>"

// runMates runs crew mates with args, the arguments after "mates", and
// returns crew's exit code. It takes exactly create <name>, checks the name
// before anything else, then creates the mate on the GitHub repository of
// the git repository it runs in. The first Ctrl-C, SIGTERM or SIGHUP stops
// the flow, which then exits 1.
func runMates(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "create" {
		_, _ = fmt.Fprintln(stderr, "crew: "+matesUsage)
		return app.ExitConfig
	}
	name := args[1]
	if err := mates.CheckName(name); err != nil {
		return matesExit(stderr, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	var group proc.Group
	root, err := repoRoot(ctx, group.Run)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "crew mates create must run inside a git repository: %v\n", err)
		return app.ExitConfig
	}
	store, err := mates.DefaultStore()
	if err != nil {
		return matesExit(stderr, err)
	}
	flow := mates.NewFlow(group.Run, store, mates.NewClient(mates.DefaultAPI, &http.Client{}), stdout, stderr)
	return matesExit(stderr, flow.Create(ctx, root, name))
}

// matesExit prints err, when there is one, and returns the exit code it
// means: 2 for an *mates.EnvError, found before anything was asked of
// GitHub's pages, 1 for any other error, and 0 for none.
func matesExit(stderr io.Writer, err error) int {
	if err == nil {
		return app.ExitClean
	}
	_, _ = fmt.Fprintf(stderr, "crew: %v\n", err)
	if _, ok := errors.AsType[*mates.EnvError](err); ok {
		return app.ExitConfig
	}
	return app.ExitFailure
}
