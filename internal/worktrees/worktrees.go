// Package worktrees cleans crew's worktrees on the boss's word: it lists
// each worktree with what it would do and why, asks, and on a yes removes
// the worktrees whose pull request merged, with their branches when nothing
// was added after the merge. Every other worktree stays.
//
// It reaches git and the tracker only through the ports, so it imports only
// the domain and the ports.
package worktrees

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/port"
)

// Result is how a clean ended, for the exit code.
type Result int

// The results of Clean.
const (
	// Done means it ran to its end and every check, lookup and removal it
	// made worked, whether it removed anything or not: answered no, without
	// a terminal or with nothing to remove included.
	Done Result = iota
	// Failed means it ran to its end, but kept a worktree or a branch
	// because a lookup, a check or a removal failed.
	Failed
	// Stopped means its context ended first. A removal under way when it
	// ended was finished, and no other was started.
	Stopped
)

// Options is what Clean works with.
type Options struct {
	// Sweeper lists, inspects and removes the workspace's worktrees.
	Sweeper port.Sweeper
	// Finder finds the pull request opened from a worktree's branch.
	Finder port.PullRequestFinder
	// Runs returns, for each worktree whose run started and has not ended,
	// when that run started. Clean calls it before the list and again before
	// the removals.
	Runs func() (map[string]time.Time, error)
	// Dir is the folder the worktrees are in, named on the first line.
	Dir string
	// In is where the answer is read from.
	In io.Reader
	// Out is where the list, the question and the report are written.
	Out io.Writer
	// Terminal means In is a terminal, so the boss can be asked. Without
	// one, Clean lists and removes nothing.
	Terminal bool
}

// Clean lists the worktrees with what it would do with each and why, asks,
// and on a yes checks each worktree to remove again, removes what both
// checks allow, and reports. It asks only when something can be removed and
// o.Terminal is set. When ctx ends, it stops waiting for the answer, and
// starts no further removal; a removal under way finishes, as it runs on a
// context ctx's end does not cancel. It returns an error, having written
// nothing, only when the worktrees cannot be listed.
func Clean(ctx context.Context, o Options) (Result, error) {
	found, err := o.Sweeper.Workspaces(ctx)
	if err != nil {
		return Done, err
	}
	if len(found) == 0 {
		o.write(none(o.Dir))
		return result(ctx, false), nil
	}
	runs, err := o.Runs()
	j := journal{runs: runs, err: err}
	listed := make([]decision, 0, len(found))
	for _, f := range found {
		listed = append(listed, decide(ctx, f, j, o.Finder, o.Sweeper))
	}
	failed := anyFailed(listed)
	o.write(list(o.Dir, listed))
	worktrees, branches := tally(listed)
	switch {
	case worktrees == 0:
		o.write("Nothing to remove.\n")
		return result(ctx, failed), nil
	case !o.Terminal:
		o.write("Removing worktrees needs a confirmation at a terminal; nothing was removed.\n")
		return result(ctx, failed), nil
	}
	o.write(question(worktrees, branches))
	a := ask(ctx, o.In)
	o.write("\n")
	if a == stopped {
		o.write("Stopped; nothing was removed.\n")
		return Stopped, nil
	}
	if a == no {
		o.write("Nothing was removed.\n")
		return result(ctx, failed), nil
	}
	done := o.removeAll(ctx, listed)
	o.write(report(done))
	return result(ctx, failed || anyFailed(done)), nil
}

// removeAll checks again, with a fresh listing and journal, each worktree
// listed to remove, and removes what both checks allow. It returns every
// listed worktree with what became of it.
func (o Options) removeAll(ctx context.Context, listed []decision) []decision {
	runs, err := o.Runs()
	j := journal{runs: runs, err: err}
	fresh, listErr := o.Sweeper.Workspaces(ctx)
	byName := make(map[string]port.Found, len(fresh))
	for _, f := range fresh {
		byName[f.Space.Name] = f
	}
	done := make([]decision, 0, len(listed))
	for _, first := range listed {
		if first.action == keep {
			done = append(done, first)
			continue
		}
		f, ok := byName[first.found.Space.Name]
		switch {
		case ctx.Err() != nil:
			done = append(done, keeping(first.found, "stopped before it was removed", false))
		case listErr != nil:
			done = append(done, keeping(first.found, "it could not be checked again: "+listErr.Error(), true))
		case !ok:
			done = append(done, keeping(first.found, changed("it is no longer there"), false))
		default:
			done = append(done, o.remove(ctx, first, f, j))
		}
	}
	return done
}

// remove checks first's worktree again, as f, and removes what both checks
// allow, unless ctx ended meanwhile.
func (o Options) remove(ctx context.Context, first decision, f port.Found, j journal) decision {
	second := decide(ctx, f, j, o.Finder, o.Sweeper)
	d := again(first, second)
	d.failed = d.failed || second.failed
	if d.action == keep {
		return d
	}
	if ctx.Err() != nil {
		return keeping(d.found, "stopped before it was removed", d.failed)
	}
	err := o.Sweeper.Remove(context.WithoutCancel(ctx), d.found.Space, d.action == removeAll)
	if errors.Is(err, port.ErrBranchKept) {
		return decision{found: d.found, action: removeWorktree, reason: d.reason + "; " + err.Error(), failed: true}
	}
	if err != nil {
		return keeping(d.found, "removing it failed: "+err.Error(), true)
	}
	return d
}

// write writes s to o.Out. A write that fails is not reported: the output
// is all there is to report it on.
func (o Options) write(s string) {
	_, _ = io.WriteString(o.Out, s)
}

// answer is the boss's answer to the question.
type answer int

// The answers.
const (
	// no is any answer but a yes, or none at all.
	no answer = iota
	// yes is y or yes.
	yes
	// stopped means the context ended before the answer came.
	stopped
)

// ask reads one line from in and tells whether it is a yes: y or yes, in any
// case, spaces around it ignored. Anything else, or the end of in, is a no.
// It returns stopped when ctx ends before the answer comes; the read then
// goes on in the background until in gives a line or ends.
func ask(ctx context.Context, in io.Reader) answer {
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(in).ReadString('\n')
		lines <- line
	}()
	select {
	case <-ctx.Done():
		return stopped
	case line := <-lines:
		if s := strings.ToLower(strings.TrimSpace(line)); s == "y" || s == "yes" {
			return yes
		}
		return no
	}
}

// tally returns how many worktrees ds removes, and how many branches it
// deletes.
func tally(ds []decision) (int, int) {
	worktrees, branches := 0, 0
	for _, d := range ds {
		if d.action != keep {
			worktrees++
		}
		if d.action == removeAll {
			branches++
		}
	}
	return worktrees, branches
}

// anyFailed reports whether a lookup, a check or a removal failed for any of
// ds.
func anyFailed(ds []decision) bool {
	for _, d := range ds {
		if d.failed {
			return true
		}
	}
	return false
}

// result is how a clean that ran to its end ended.
func result(ctx context.Context, failed bool) Result {
	switch {
	case ctx.Err() != nil:
		return Stopped
	case failed:
		return Failed
	default:
		return Done
	}
}
