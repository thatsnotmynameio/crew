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
	"github.com/thatsnotmynameio/crew/internal/bots"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// botsUsage is the one form crew mates takes.
const botsUsage = "usage: crew mates create <name>"

// runBots runs crew mates with args, the arguments after "mates", and
// returns crew's exit code. It takes exactly create <name>, checks the name
// before anything else, then creates the bot on the GitHub repository of
// the git repository it runs in. The first Ctrl-C, SIGTERM or SIGHUP stops
// the flow, which then exits 1.
func runBots(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "create" {
		_, _ = fmt.Fprintln(stderr, "crew: "+botsUsage)
		return app.ExitConfig
	}
	name := args[1]
	if err := bots.CheckName(name); err != nil {
		return botsExit(stderr, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	var group proc.Group
	root, err := repoRoot(ctx, group.Run)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "crew mates create must run inside a git repository: %v\n", err)
		return app.ExitConfig
	}
	store, err := bots.DefaultStore()
	if err != nil {
		return botsExit(stderr, err)
	}
	flow := bots.NewFlow(group.Run, store, bots.NewClient(bots.DefaultAPI, &http.Client{}), stdout, stderr)
	return botsExit(stderr, flow.Create(ctx, root, name))
}

// botsExit prints err, when there is one, and returns the exit code it
// means: 2 for an *bots.EnvError, found before anything was asked of
// GitHub's pages, 1 for any other error, and 0 for none.
func botsExit(stderr io.Writer, err error) int {
	if err == nil {
		return app.ExitClean
	}
	_, _ = fmt.Fprintf(stderr, "crew: %v\n", err)
	if _, ok := errors.AsType[*bots.EnvError](err); ok {
		return app.ExitConfig
	}
	return app.ExitFailure
}
