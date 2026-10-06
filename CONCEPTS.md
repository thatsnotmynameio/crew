# Concepts

> Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

## Rules

### Code owner

A user whose issues crew takes: every user the catch-all `*` rule of the repository's CODEOWNERS names, or crew's `gh` login when it names none.

crew takes the issues a code owner or a configured bot opened. A code owner is not the repository's owner on GitHub. crew's messages call the `gh` login crew runs as "you", code owner or not.

### Rule

What crew does with the items that carry one label: it takes the items of its kind that carry its ready label, issues by default or pull requests when it declares them, moves each to its running label, runs its actions on each, and moves each to its success label when every action succeeded, or to its failure label when any failed.

Rules form no sequence: each reacts to its ready label alone, and the order of work comes only from how their labels chain, one rule's success label being another's ready label. Among items of equal priority, the rule later in the file takes first.

### Rule without actions

A rule that only moves the label: crew takes the item, moves it to the rule's running label and at once on to its success label, without a workspace or a session.

It holds a slot of its queue for those two moves, never fails, sends no notification unless told to, has no column on the default board, and leaves an earlier handled entry that ended well in place.

### Action

One unattended coding-agent session a rule runs on an item, in its own workspace, on its agent's harness, together with the action's optional check.

A rule's actions run in parallel, and the rule is judged only once every one of them has ended.

### Agent

A named harness, with its model and its optional bot, that runs the sessions of the actions that name it.

Actions on different agents can run at once on different harnesses. An agent no action names is checked but never started.

### Harness

The coding-agent program a session runs on, such as Claude Code or Codex, which crew starts headless in the action's workspace.

A session's identity reaches the harness through its environment only, never its command line, which any process on the machine can read. The harness must pass that identity on to every command the session runs, so that the session never acts as you when its agent has a bot.

### Check

A named shell script that an action points to, run in the action's workspace after its session succeeded, whose exit status decides whether the action succeeded. An action points to one check or a list of checks, which run one after another in the listed order.

A check runs only after a successful session, never after a failed one. It reads the issue from environment variables, and the prompt the session started with and the session's last message from files they name. A check that fails, cannot start, runs out of time or is ended by a stop fails its action, the checks after it do not run, and the action then takes the same path as any failed action. While its checks run, the action still counts as running.

### Rule run

One pass of an issue through one rule, from the first time crew reports the issue queued for that rule or takes it, until the next rule run of that issue starts.

An issue that is retried in the same rule starts a new rule run. A rule run belongs to one crew process: a rule run cut short by a crash is never continued, and the next one is new, even when its actions resume.

### Action run

One attempt at an action on an issue, from the moment its workspace is ready until the action ends, its check included. An action run succeeds or fails; one that never recorded its end, because crew crashed or was killed, counts as failed.

### Workspace

The isolated checkout an action works in: a git worktree on its own branch, kept after the action run ends. A workspace is identified by a name, which can be reused only once the earlier workspace of that name and its branch are gone.

### Priority

The rank the tracker gives an issue, which decides first which waiting issue crew takes when a slot is free, ahead of its rule's place in the file and its age. On GitHub it is the organization's issue field `Priority`, its first option the highest. An issue without one ranks after every issue that has one.

### Queue

A fixed share of `max_parallel_issues` that only the rules in it can use. Every rule runs in one queue: `default`, which gets the slots the other queues leave, or one the config declares.

A queue never lends an idle slot to another queue, so a slot is guaranteed to a rule only by its queue's size.

## Live view

### Board

The live view's columns of labels, each holding a card for each item that carries one of its labels, held by crew or idle.

The config may write the columns, any labels, crew's or not, which show issues only. Without that, the board has one column per rule that has actions, with the rule's ready and running labels and its kind. An item sits in every column whose labels it carries and nowhere else; no card waits for the next rule.

After its columns, the board shows a Not on board column, only while it holds a card, for each held item with actions that no column shows, and a last column, Handled, with a card for each Handled entry.

### Handled entry

The live view's record of how an issue's latest rule run in this crew process ended: the rule, where it moved the issue, what its sessions cost, and why it failed when it did. An issue has at most one: a later rule's entry replaces it and keeps what the earlier rules' sessions cost.

A later rule run that ends replaces the entry, except that a rule without actions ending well leaves an entry that ended well in place; without one, it leaves its own. While a rule holds the issue again, the entry stays and names that rule, and a failure in it no longer counts as needing you.

## Recovery

### Resume

What crew does when a rule takes an issue whose last action run of one of its actions failed: it runs that action again in the failed action run's workspace, as that run left it, instead of a fresh one, and tells the new session that it continues earlier work. Only the same action in the same rule on the same issue resumes an action run; one that succeeded is never resumed.

Resuming is triggered only by the rule's ready label going back on the issue; crew never resumes on its own. Removing the workspace before that makes the action start over.

### Run journal

crew's local, append-only record of every action run's start and end, which lets crew know after a restart which action runs failed and so which actions resume.

Its lines keep the key `stage` for the rule's name, the wire name of earlier versions, so their journals still resume.

## Reporting

### Status comment

The one comment per issue that shows where the issue stands, with one entry per rule run, oldest first.

Only the latest entry changes; earlier entries keep the text they had when their rule run ended. Editing it notifies no one, so it is not how crew tells you something failed. A full comment is continued in a new one.

### Failure report

The comment crew posts when a rule run ends with a failed action, naming each failed action and where its log is.

It is a new comment, so the tracker notifies the people who watch the issue, and it never quotes what a session or a tool said.

### Mirrored label

The rule label an issue's pull requests carry, which crew sets to the issue's own rule label each time it moves the issue. An issue's pull requests are the open ones in its own repository that are linked as closing it; merged and closed ones, and those in other repositories, are not.

It goes from the issue to its pull requests only: crew replaces a rule label put on a pull request by hand at the issue's next move. A rule takes the items of its kind that carry its ready label, so a pull request that carries a rule's ready label, mirrored or not, is taken only by a rule that takes pull requests; crew leaves it alone otherwise, with a notice.

### Stop comment

The comment crew posts on each of an issue's open pull requests when a run of a rule with actions ends, saying how the rule ended and that nobody watches the pull request any more.

It is a new comment at every rule end, so its watchers are notified and a rerun leaves a trail. Like the failure report, it never quotes what a session or a tool said.

## Identity

### Bot

A GitHub identity of crew's own: a private GitHub App created with `crew bots create`, owned by the account that owns the repository, whose private key stays on the machine that created it. It acts on GitHub as `<slug>[bot]`, such as `crew-tester[bot]`.

You can have many bots, and a bot is only an identity: it carries no model, prompt or settings. `tracker.bot` names the bot crew's own writes on GitHub act as, and the default bot of every agent; an agent's `bot` names the one the sessions and checks of its actions act as. Commits stay yours, with the bot as co-author. Without a bot, crew and its sessions act as your `gh` login.

A bot acts when crew could make it act at startup. One that cannot act then stays that way until crew restarts, and its actions act as you. A bot that acts can stop acting while crew runs: when crew's own writes as `tracker.bot` go back to you, which lasts until restart, or when its token fails to renew, which lasts until a renewal succeeds. An action's cost counts on the identity it acted as.

## TypeSafe

### Judge

An optional local service, configured by the `judge` section of `.crew/config.yaml` like the tracker, that answers named questions about a state with typed verdicts. crew starts it before polling, gives every session its address, and stops it with crew; TypeSafe is its first adapter.

crew never depends on it: without the section, or when its service cannot start, crew runs as it would without it.

### Question bank

The repository's own file of named TypeSafe questions in `.crew/`, each with its primitive, pinned model, verdict bands, conservative default, question stage and evidence bar, which the judge answers by name.

A new use of TypeSafe is a new question in the bank. Any change to a question's content, pinned model or bands makes a new version of it.

### Ledger

The judge's local, append-only record, one per repository and written only by its service, of every question asked, every answer, what the caller did with it and what really happened.

It replays the first recorded answer when the same question version and state are asked again. Unlike the run journal, it records judgments, not action runs.

### Question stage

How far a question is trusted: shadow (answers are recorded and change nothing), confirm (a person approves), or act.

A question moves up only by a person's edit to the question bank, once its evidence passes the question's bar. A later version of a question is treated as shadow until a recheck against the last version that held its declared stage passes, or a calibration of the version itself passes its bar.

## Flagged ambiguities

- "Run" alone is ambiguous: a *rule run* is one pass through a rule, an *action run* is one attempt at one action, and crew's run time limit concerns the whole crew process.
- "Stage" alone is ambiguous: the run journal's `stage` key is a rule's name, kept from earlier versions, while a *question stage* is how far a TypeSafe question is trusted.
