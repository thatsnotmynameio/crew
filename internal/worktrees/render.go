package worktrees

import (
	"fmt"
	"strings"
)

// row is one worktree's line: what is or was done with it, its name and why.
type row struct {
	label, name, reason string
}

// none says dir holds no crew worktree.
func none(dir string) string {
	return "No crew worktrees in " + dir + ".\n"
}

// list words the worktrees in dir with what Clean would do with each.
func list(dir string, ds []decision) string {
	rows := make([]row, 0, len(ds))
	for _, d := range ds {
		rows = append(rows, row{label: plan(d.action), name: d.found.Space.Name, reason: d.reason})
	}
	return count(len(ds), "worktree", "worktrees") + " in " + dir + ":\n" + table(rows)
}

// question asks whether to remove that many worktrees and branches; the
// answer goes on the same line.
func question(worktrees, branches int) string {
	what := count(worktrees, "worktree", "worktrees")
	if branches > 0 {
		what += " and delete " + count(branches, "branch", "branches")
	}
	return "Remove " + what + "? [y/N] "
}

// report words what became of each worktree, then what was removed.
func report(ds []decision) string {
	rows := make([]row, 0, len(ds))
	for _, d := range ds {
		rows = append(rows, row{label: past(d.action), name: d.found.Space.Name, reason: d.reason})
	}
	worktrees, branches := tally(ds)
	summary := "Removed nothing.\n"
	switch {
	case branches > 0:
		summary = fmt.Sprintf("Removed %s and deleted %s.\n",
			count(worktrees, "worktree", "worktrees"), count(branches, "branch", "branches"))
	case worktrees > 0:
		summary = "Removed " + count(worktrees, "worktree", "worktrees") + ".\n"
	}
	return table(rows) + summary
}

// plan words what Clean would do.
func plan(a action) string {
	switch a {
	case removeAll:
		return "remove worktree and branch"
	case removeWorktree:
		return "remove worktree, keep branch"
	default:
		return "keep"
	}
}

// past words what Clean did.
func past(a action) string {
	switch a {
	case removeAll:
		return "removed worktree and branch"
	case removeWorktree:
		return "removed worktree, kept branch"
	default:
		return "kept"
	}
}

// table lines rows up in columns, indented, one per line.
func table(rows []row) string {
	labels, names := 0, 0
	for _, r := range rows {
		labels = max(labels, len(r.label))
		names = max(names, len(r.name))
	}
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "  %-*s  %-*s  %s\n", labels, r.label, names, r.name, r.reason)
	}
	return b.String()
}
