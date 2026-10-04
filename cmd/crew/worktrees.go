package main

import (
	"context"
	"fmt"
	"io"
	"os/signal"
	"syscall"

	"github.com/thatsnotmynameio/crew/internal/adapter/git"
	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
	"github.com/thatsnotmynameio/crew/internal/registry"
)

// worktreesUsage is the one form crew worktrees takes.
const worktreesUsage = "usage: crew worktrees clean"

// runWorktrees runs crew worktrees with args, the arguments after
// "worktrees", and returns crew's exit code. It takes exactly clean, and
// cleans the worktrees of the git repository it runs in, reading the
// answer from stdin; terminal tells whether stdin is a terminal. The first
// Ctrl-C, SIGTERM or SIGHUP stops it before its next removal, and it then
// exits 1.
func runWorktrees(args []string, stdin io.Reader, terminal bool, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "clean" {
		_, _ = fmt.Fprintln(stderr, "crew: "+worktreesUsage)
		return app.ExitConfig
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	var group proc.Group
	root, err := repoRoot(ctx, group.Run)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "crew worktrees clean must run inside a git repository: %v\n", err)
		return app.ExitConfig
	}
	return app.Clean(ctx, app.CleanOptions{
		Registry:  registry.Default(&group),
		Workspace: func(root string) port.Workspace { return git.New(&group, root) },
		Root:      root,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
		Terminal:  terminal,
	})
}
