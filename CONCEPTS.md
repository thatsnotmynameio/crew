# Concepts

> Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

## Workflow

### Boss

The person who runs crew in their own repositories, writes its workflow and checks, and reviews the issues crew moves.

### Stage

One step of the workflow: it takes an issue that carries its label, runs its actions on it, and moves the issue to its success state when every action succeeded, or to its failure state when any failed.

### Action

One unattended coding-agent session a stage runs on an issue, in its own workspace, together with the action's optional check.

A stage's actions run in parallel, and the stage is judged only once every one of them has ended.

### Check

A shell command the boss attaches to an action, run in the action's workspace after its session succeeded, whose exit status decides whether the action succeeded.

A check runs only after a successful session, never after a failed one. A check that fails, cannot start, runs out of time or is ended by a stop fails its action, which then takes the same path as any failed action. While its check runs, the action still counts as running.

### Stage run

One pass of an issue through one stage, from the first time crew reports the issue queued for that stage or takes it, until the next stage run of that issue starts.

An issue that is retried in the same stage starts a new stage run. A stage run belongs to one crew process: a stage run cut short by a crash is never resumed, and the next one is new.

## Reporting

### Status comment

The one comment per issue that shows the boss where the issue stands, with one entry per stage run, oldest first.

Only the latest entry changes; earlier entries keep the text they had when their stage run ended. Editing it notifies no one, so it is not how crew tells the boss something failed. A full comment is continued in a new one.

### Failure report

The comment crew posts when a stage run ends with a failed action, naming each failed action and where its log is.

It is a new comment, so the tracker notifies the boss, and it never quotes what a session or a tool said.

## Flagged ambiguities

- "Run" alone is ambiguous: a *stage run* is one pass through a stage, while crew's run time limit concerns the whole crew process.
