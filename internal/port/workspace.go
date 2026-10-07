package port

import (
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// WorkspaceBase returns the name a workspace of a run of rule on the issue
// identified by issue starts from: issue-<key>-<rule>, the key and the rule
// each lowercased, with every character outside [a-z0-9-] replaced by '-',
// so the name is safe in a file name and a branch. A Workspace adds -2, -3
// and so on while the name is taken, and the engine logs a run without a
// workspace under it.
func WorkspaceBase(issue crew.IssueID, rule crew.RuleName) crew.WorkspaceName {
	return crew.WorkspaceName("issue-" + sanitize(issue.Key) + "-" + sanitize(string(rule)))
}

// sanitize lowercases s and replaces every character outside [a-z0-9-]
// with '-'.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r - 'A' + 'a'
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			return r
		}
		return '-'
	}, s)
}
