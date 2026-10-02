# Concepts

> Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

## Workflow

### Stage
One step of the workflow a repository declares: it takes an issue that carries its label, moves the issue to an in-progress state while its actions run, then moves it to its success state when every action succeeded, or to its failure state when any failed.

### Action
One coding-agent session a stage runs for an issue, from a prompt written for that stage. A stage's actions run in parallel, each in its own workspace. Two stages may each have an action of the same name; they are still different actions.

### Run
One attempt at an action on an issue, from the moment its workspace is ready until the action ends. A run succeeds or fails; a run that never recorded its end, because crew crashed or was killed, counts as failed.

### Workspace
The isolated checkout an action works in: a git worktree on its own branch, kept after the run ends. A workspace is identified by a name, which can be reused only once the earlier workspace of that name and its branch are gone.

## Recovery

### Resume
What crew does when a stage takes an issue whose last run of one of its actions failed: it runs that action again in the failed run's workspace, as that run left it, instead of a fresh one, and tells the new session that it continues earlier work. Only the same action in the same stage on the same issue resumes a run; a run that succeeded is never resumed.

Resuming is triggered only by the stage's label going back on the issue; crew never resumes on its own. Removing the workspace before that makes the action start over.

### Run journal
crew's local, append-only record of every run's start and end, which lets crew know after a restart which runs failed and so which actions resume.
