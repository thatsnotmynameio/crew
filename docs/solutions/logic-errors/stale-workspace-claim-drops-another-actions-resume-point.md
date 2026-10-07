---
title: A stale workspace claim dropped another action's resume point
date: 2026-10-07
category: logic-errors
module: internal/core, internal/crew
problem_type: logic_error
component: core_reducer
symptoms:
  - "A failed action starts fresh in a new worktree instead of reopening its failed one with the resume paragraph"
  - "It happens only after another rule with the same action name starts in a worktree name the failed action used earlier"
root_cause: logic_error
resolution_type: code_fix
severity: medium
tags: [resume, run-journal, workspace, worktree, retire, history, claims]
---

# A stale workspace claim dropped another action's resume point

## Problem

When the run journal moved to rule run events (#243), workspace retirement was rebuilt as one "claim" per workspace name. The claim went stale once an action's later run moved to another workspace, and a later start in the old name by another rule then dropped that action's live resume point.

## Symptoms

- The next run of the failed action starts in a fresh worktree, and its session gets no resume paragraph, although the failed worktree still exists.
- It needs two rules whose actions share a name on one issue (development and fix both declare `lfg` in `.crew/config.yaml`), and a worktree name reused after you removed that worktree.

## What Didn't Work

- **One claim per workspace name.** The plan of #243 (`docs/plans/2026-10-07-0212-refactor-rule-run-aggregate-plan.md`, KTD-P9 and U5) had the core map each workspace name to the issue, rule and action whose action last started in it, and retire whoever held the claim when another action started there. Nothing removed an action's claim on `issue-9-lfg` when its next run opened `issue-9-lfg-2` instead, because `issue-9-lfg` was still taken. Later, after you removed `issue-9-lfg`, development's `lfg` got that name again, and the stale claim retired fix's `lfg`, whose failed run now lived in `issue-9-lfg-2`. Plans are not updated after they ship, so that plan still describes this mechanism. Treat it as overturned.
- **The tests did not catch it.** Every unit test, TUI golden file and acceptance scenario passed. The existing retirement test (`TestANewerStartInAWorkspaceRetiresAnotherKeysRecordOfIt` in `internal/core/resume_test.go`) covers an action retired in the workspace it last used, not an action that had moved on. The correctness and adversarial reviewers of `ce-code-review` found it independently, and its validator confirmed it against the code before the change.

## Solution

Retirement asks where each action's last action run is, not who last claimed a name. `crew.History.Retire` drops every other action, in any rule on any issue, whose last action run had the starting workspace (`internal/crew/history.go:96`):

```go
func (h *History) Retire(w WorkspaceName, issue IssueID, rule RuleName, action ActionName) {
	own := ruleKey{issue: issue, rule: rule}
	for k, actions := range h.actions {
		for name, p := range actions {
			if p.workspace.Name == w && (k != own || name != action) {
				delete(actions, name)
			}
		}
	}
}
```

The core calls it for each action start, before folding the start into the history (`internal/core/claims.go:44`), and the claims map is gone. This is the rule the core followed before the change (`remember` in `internal/core/resume.go` at `2a59c89`):

```go
if r.Event == RunStarted {
	for k, other := range m.lastRuns {
		if k != keyOf(r) && other.Workspace == r.Workspace {
			delete(m.lastRuns, k)
		}
	}
}
```

Tests: `TestAStartRetiresOnlyTheActionsWhoseLastRunIsInItsWorkspace` in `internal/core/resume_test.go` (fix fails in `issue-9-lfg-2` after a run in `issue-9-lfg`, development then starts in `issue-9-lfg`, and fix still reopens `issue-9-lfg-2`; it failed before the fix) and `TestRetireKeepsAnActionWhoseLastRunMovedToAnotherWorkspace` in `internal/crew/history_test.go`. The fix is on the branch of #243, not yet merged as of this writing.

## Why This Works

Worktree names repeat only once a worktree is gone (`free` in `internal/adapter/git/workspace.go` takes the first of `base`, `base-2`, `base-3` whose folder and branch do not exist). So a start in a name proves only that the earlier work in that name is gone. The work an action can still resume is wherever its last action run is. A claim per name records who used the name last, which stops being where that action's work is as soon as the action moves on. Asking the history for each action's last action run gives the right answer by construction, on replay of the journal and live alike.

## Prevention

- When you rebuild a derived index (who holds what) from events, check it against the old rule's question, not just its common case. Here the old rule asked "whose last record names this workspace", and the index answered "who last used this name". They agree until a key moves on.
- A refactor of resume or the journal needs a test where the same name is reused across two rules after one of them has moved to a `-2` workspace. Workspace names embed the issue and the action, not the rule, so two rules sharing an action name meet on the same names.
- Keep retirement in one place, keyed on the last action run. A second structure that mirrors it can drift.

## Related Issues

- #243, the redesign that introduced and fixed it; #237, its parent.
- #34, removing worktrees automatically, which would make name reuse common.
- `docs/plans/2026-10-02-1814-feat-resume-failed-action-plan.md`, KTD5: the retirement rule this restores.
- `docs/solutions/integration-issues/moved-repository-unlists-its-worktrees.md`: another way a failed run can lose its worktree on resume.
