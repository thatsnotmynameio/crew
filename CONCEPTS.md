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

An issue that is retried in the same stage starts a new stage run. A stage run belongs to one crew process: a stage run cut short by a crash is never continued, and the next one is new, even when its actions resume.

### Action run

One attempt at an action on an issue, from the moment its workspace is ready until the action ends, its check included. An action run succeeds or fails; one that never recorded its end, because crew crashed or was killed, counts as failed.

### Workspace

The isolated checkout an action works in: a git worktree on its own branch, kept after the action run ends. A workspace is identified by a name, which can be reused only once the earlier workspace of that name and its branch are gone.

### Priority

The rank the tracker gives an issue, which decides first which waiting issue crew takes when a slot is free, ahead of its stage and its age. On GitHub it is the organization's issue field `Priority`, its first option the highest. An issue without one ranks after every issue that has one.

### Queue

A fixed share of `max_parallel_issues` that only the stages in it can use. Every stage runs in one queue: `clerk`, crew's bookkeeping queue, `default`, which gets the slots the other queues leave, or one the boss declares.

A queue never lends an idle slot to another queue, so a slot is guaranteed to a stage only by its queue's size.

## Recovery

### Resume

What crew does when a stage takes an issue whose last action run of one of its actions failed: it runs that action again in the failed action run's workspace, as that run left it, instead of a fresh one, and tells the new session that it continues earlier work. Only the same action in the same stage on the same issue resumes an action run; one that succeeded is never resumed.

Resuming is triggered only by the stage's label going back on the issue; crew never resumes on its own. Removing the workspace before that makes the action start over.

### Run journal

crew's local, append-only record of every action run's start and end, which lets crew know after a restart which action runs failed and so which actions resume.

## Reporting

### Status comment

The one comment per issue that shows the boss where the issue stands, with one entry per stage run, oldest first.

Only the latest entry changes; earlier entries keep the text they had when their stage run ended. Editing it notifies no one, so it is not how crew tells the boss something failed. A full comment is continued in a new one.

### Failure report

The comment crew posts when a stage run ends with a failed action, naming each failed action and where its log is.

It is a new comment, so the tracker notifies the boss, and it never quotes what a session or a tool said.

### Mirrored label

The crew label an issue's pull requests carry, which crew sets to the issue's own crew label each time it moves the issue. An issue's pull requests are the open ones in its own repository that are linked as closing it; merged and closed ones, and those in other repositories, are not.

It goes from the issue to its pull requests only: crew never takes work or a state from a pull request's labels, and replaces a crew label put on a pull request by hand at the issue's next move.

### Stop comment

The comment crew posts on each of an issue's open pull requests when a stage run ends, saying how the stage ended and that nobody watches the pull request any more.

It is a new comment at every stage end, so the boss is notified and a rerun leaves a trail. Like the failure report, it never quotes what a session or a tool said.

## Identity

### Mate

A GitHub identity of crew's own: a private GitHub App the boss creates with `crew mates create`, owned by the account that owns the repository, whose private key stays on the boss's machine. It acts on GitHub as its bot, such as `crew-tester[bot]`.

A boss can have many mates, and a mate is only an identity: it carries no model, prompt or settings. crew does not act as its mates yet; until it does, crew and its sessions act as the boss's `gh` login.

## Flagged ambiguities

- "Run" alone is ambiguous: a *stage run* is one pass through a stage, an *action run* is one attempt at one action, and crew's run time limit concerns the whole crew process.
