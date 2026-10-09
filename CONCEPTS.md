# Concepts

> Shared domain vocabulary for this project — entities, named processes, and status concepts with project-specific meaning. Seeded with core domain vocabulary, then accretes as ce-compound and ce-compound-refresh process learnings; direct edits are fine. Glossary only, not a spec or catch-all.

## Rules

### Code owner

A user whose issues crew takes: every user the catch-all `*` rule of the repository's CODEOWNERS names, or crew's `gh` login when it names none.

crew takes the issues a code owner or a configured bot opened. A code owner is not the repository's owner on GitHub. crew's messages call the `gh` login crew runs as "you", code owner or not.

### Rule

What crew does with the items that carry one label: it takes the items of its kind that carry its ready label, issues by default or pull requests when it declares them, moves each to its running label, runs its actions on each one after another, and ends each rule run through one of its routes: `passed` after its last action, or the route an action's verdict leads to.

Rules form no sequence with each other: each reacts to its ready label alone, and the order of work comes only from how their labels chain, a label one rule's route moves to being another's ready label. Among items of equal priority, the rule later in the file takes first.

### Rule without actions

A rule that only moves the label: crew takes the item, moves it to the rule's running label and at once runs its `passed` route, its only route, without a workspace or a session.

It holds a slot of its queue while its route runs, ends through `passed` even while crew stops, sends no notification unless told to, has no column on the default board, and leaves an earlier handled entry that ended through `passed` in place.

### Action

One step of a rule's sequence: a session, an unattended coding-agent session written in the rule with its prompt and run on its agent's harness, a shell action, which the config defines once by name, a function, which crew registers, a question action, which asks a rule's question, or the answered rule's check, which crew adds itself. A session is named after its agent unless the rule names it, and no two actions of a rule share a name.

A rule's actions run one at a time, in the listed order, in the rule run's one workspace. Each ends with a verdict, which its `on:` sends to the next action or to one of the rule's routes.

### Agent

A named harness, with its model and its optional bot, that runs the sessions of the actions that name it.

Actions on different agents can run at once on different harnesses. An agent no action names is checked but never started.

### Harness

The coding-agent program a session runs on, such as Claude Code or Codex, which crew starts headless in the rule run's workspace.

A session's identity reaches the harness through its environment only, never its command line, which any process on the machine can read. The harness must pass that identity on to every command the session runs, so that the session never acts as you when its agent has a bot.

### Shell action

A named shell script the config defines once, under its top-level `actions`, and that a rule runs as an action of its sequence or as a step of a route, in the rule run's workspace. As an action, its exit status gives its verdict: 0 is `passed` and any other status `failed`, unless its definition's `verdicts` maps the status to another verdict.

It reads the issue from environment variables, and the run's latest session's name, prompt and last message from a variable and the files it names, so it can judge that session. It acts as that session's bot. As a route's step it has no verdict: one that does not exit 0 is recorded as failed, and the route goes on.

### Function

Go code built into crew that a rule calls by name, as an action of its sequence or as a step of a route, with parameters written where it is called or preset by a top-level action under `actions`, whose `name` is the function. A use's parameters replace its preset's key by key. Each parameter is text, a number or a boolean, and text is a template over the issue, filled just before each call. crew checks every use's parameters when it loads the config.

A function declares the verdicts it can return, beyond `passed` and `failed`. It runs in crew's process, in the run's workspace when there is one, and never makes crew create one. It writes into the run's log, acts as the run's latest session's bot, and has a shell action's time limit. As a route's step, one that does not return `passed` is recorded as failed, and the route goes on. crew registers no function yet.

### Check

The former name of a shell action that judged a session, run after it; see Shell action. The config no longer has checks.

### Verdict

An action's result: `passed`, `failed`, or another name the action's `on:` maps. A session gives `passed` or `failed` by how it ended, or the verdict it wrote to the file crew gave it; a shell action gives one by its exit status; a function gives the one it returns; a question action gives `asked`; the answered rule's check gives `passed`, `no-question`, `unanswered` or `unread`. A stop, an action that cannot start, a verdict the action's `on:` does not name, and a verdict a function returns without declaring it give `failed`.

The action's `on:` maps each verdict to `next`, the next action, or to a route. Without an entry, `passed` goes to `next` and every other verdict to `failed`.

### Route

A named way to end a rule run: steps that run in order and end by moving the item to a label or closing the issue. A step is a move, a close, a comment from a template that names only what crew knows of the run, the failure report, a shell action or a function.

A rule with actions declares `passed`, which its run takes after its last action, and `failed`; a rule without actions declares `passed` alone. A step that fails is recorded and the route goes on, so its final move or close still happens. A route is finished once its final move or close landed, or was dropped because the item moved meanwhile.

### Question step

The step that asks a rule's question on an issue: a comment with the question's text and its marker, which names the question's id, the rule that asked and its return label, the ready label of a rule in the config that takes issues. It names no answerer. It is the last step of its route, and crew adds the move to `crew:question` after it.

A rule writes it as a route's step or as an action. A question action ends at once with the verdict `asked`, which leads to a route crew adds under the action's name, holding the question step. Only a config with `questions` may ask, and only in a rule that takes issues. A question step that posted its question opens it on the rule's run.

### Question rule

The rule crew adds before the config's own when the config has `questions`: it takes the issues in `crew:question`, runs in `crew:question:in progress`, and its `passed` route, its only route, delegates the issue's open question, then moves the issue to `crew:question:waiting answer`, which no rule takes.

It is a rule without actions. The issue's open question is the latest question crew's writer posted and did not delegate yet; the delegation mentions the answerer, and still does, saying so, when crew finds no open question or cannot read the comments. Unless it found none, it asks the answerer to post the answer, then move the issue to `crew:answered`.

### Answered rule

The rule crew adds right after the question rule when the config has `questions`: it takes the issues in `crew:answered`, runs in `crew:answered:in progress`, and runs one action, `answer`, the check, which reads the issue's comments. The check finds the latest question crew's writer posted that a question of the config still declares, by its id, rule and return label, and an answer after it. With one, its `passed` route moves the issue to the question's return label; otherwise it fails with `no-question`, `unanswered` or `unread`, and its `failed` route posts a failure report that says why, then moves the issue to `crew:answered:failed`, which no rule takes.

The return label always comes from the question, never from the answer. Only a move to `crew:answered` returns the issue: a comment alone returns nothing. It runs in `questions.queue` too, needs no workspace, and has a column on the default board.

### Answerer

The one person or App, `questions.answerer`, whom the question rule mentions on every rule's question, and who answers it and moves the issue to `crew:answered`. Its answer counts by the same answer rule as a session's: a code owner who is not an App, or an App on the answering list other than crew's writers. crew does not check at startup that the answerer may answer; an answer that does not count fails the answered rule's check.

### Rule run

One pass of an issue through one rule, from the moment crew takes the issue for that rule until the next rule run of that issue starts. It runs the rule's actions in one workspace and ends through one route.

An issue that the same rule takes again starts a new rule run, with its own id, which continues that issue and rule's last rule run: crew rebuilds that run from the run journal, even when an earlier crew process left it, and decides from it where the new run starts. A rule run never goes on in another crew process: one cut short by a crash stays as the journal left it, and the next one resumes it.

Each rule run has an id that no other rule run has, in any repository or crew process. The status comment entry a rule run opens carries that id.

### Action run

One attempt at an action on an issue, within a rule run, from the moment it starts until it ends with a verdict. One that never recorded its end, because crew crashed or was killed, counts as failed, and the next rule run starts at it. An action a rule run did not reach is not run; one before the action a resumed run started at is done in an earlier run.

### Workspace

The isolated checkout a rule run works in: a git worktree on its own branch, `issue-<key>-<rule>`, shared by every action of the run and its route's shell and function steps, and kept after the run ends. Only a rule with a session or a shell action gets one; a function needs none. A workspace is identified by a name, which can be reused only once the earlier workspace of that name and its branch are gone.

### Priority

The rank the tracker gives an issue, which decides first which waiting issue crew takes when a slot is free, ahead of its rule's place in the file and its age. On GitHub it is the organization's issue field `Priority`, its first option the highest. An issue without one ranks after every issue that has one.

### Queue

A fixed share of `max_parallel_issues` that only the rules in it can use. Every rule runs in one queue: `default`, which gets the slots the other queues leave, or one the config declares.

A queue never lends an idle slot to another queue, so a slot is guaranteed to a rule only by its queue's size.

## Live view

### Board

The live view's columns of labels, each holding a card for each item that carries one of its labels, held by crew or idle.

The config may write the columns, any labels, crew's or not, which show issues only. Without that, the board has one column per rule that has actions, with the rule's ready and running labels and its kind, and a column of its own for each label where the route of a `waiting` verdict leaves the item, so an item paused there stays on screen. An item sits in every column whose labels it carries and nowhere else; no card waits for the next rule. Within a column, the cards of the items crew holds, whatever their claim, come first, then the idle ones, each in the board's order, oldest first.

After its columns, the board shows a Not on board column, only while it holds a card, for each held item with actions that no column shows. An issue whose rule ended has only the cards its labels give it.

### Handled entry

The live view's record of how an issue's latest rule run in this crew process ended: the rule, the route it ended through and where that moved the issue, what its sessions cost, and, when the route was not `passed`, the action that ended the run with its verdict. It feeds the desktop notifications and the count of issues needing you; the board draws no card for it. An issue has at most one: a later rule's entry replaces it and keeps what the earlier rules' sessions cost.

An entry needs you when its run ended through any route other than `passed`, or when its route's final move or close did not land. A later rule run that ends replaces the entry, except that a rule without actions ending through `passed` leaves an entry that ended so too in place; without one, it leaves its own. While a rule holds the issue again, the entry stays and names that rule, and it no longer counts as needing you.

## Recovery

### Resume

What crew does when a rule takes an issue whose last rule run in that rule ended through any route other than `passed`, chose such a route and never finished it, or was cut short during an action: it reopens that run's workspace, as the run left it, and starts at the action that ended the run, without running the actions before it again. When that action is a shell action or a function that judged a session before it, the new run starts at that session instead, unless its definition says `resume: self`. A resumed session is a new session, told that it continues earlier work and which route the last run ended through. When an earlier session at its action asked a question, it is told instead that a question was asked, and gets the answers that count. The first session of a run whose rule has an open question gets that question's answers the same way.

When the last run ended at a question action whose question was posted, the new run starts at the action after it, in that run's workspace, or runs only the `passed` route when the question was the rule's last action; when an action follows and the workspace cannot be reopened, it starts over at the first action and asks the question again. A question that was not posted is asked again. A step back from a judge to its session never crosses a question action. When the last run chose `passed` and its final move or close never landed, crew runs only the `passed` route again, in that run's workspace when it still exists. When the workspace to reopen is gone, the run starts over at the first action in a new one, since the actions before the resume point would not have run there. A run that started no action and opened no workspace, such as one stopped at its take, passes its own start on to the next run. A run that finished its `passed` route is never resumed.

Resuming is triggered only by the rule's ready label going back on the issue; crew never resumes on its own. When another rule run later opens a workspace of the same name, which crew gives out again only once the earlier workspace is gone, the run that worked there no longer resumes.

### Run journal

crew's local, append-only record of every rule run's events, one line each, such as its take, its actions' starts and their ends with their verdicts, the route it chose, each step's outcome and its release. After a restart it tells crew how each issue's last rule run in each rule went, and so where the next one starts.

Its lines are version 3. It skips the lines older versions wrote, so a run that failed before the upgrade starts over in a new workspace. Its lines keep the key `stage` for the rule's name, the wire name of earlier versions.

### Statistics store

crew's local record of its work across restarts and repositories, kept apart from the run journal: a SQLite database, `statistics.db` in crew's data folder (`$XDG_DATA_HOME/crew`, or `~/.local/share/crew`). It holds each crew process, recorded once when it starts, with crew's version, the repository's root and the start time; each repository crew works in, by its tracker and the id and name the tracker gives it; each issue crew manages, from the first time crew lists it with one of its rules' labels; and each move of those issues from one of the rules' labels to another, an event with the two labels, the time crew made or saw it and the rule run that made it, or none when it was made outside crew; and each rule run as a span, work with a start and an end, under the issue it ran for, with its rule, its queue, the route it ended through, how it ended (`routed`, `route_dropped`, `route_given_up` or `not_taken`), the halt that sent it through `failed` (`stop` or `run_time_limit`) when one did, and the run it continues. Processes, repositories and issues are entities; moves are events, instants; rule runs are spans, which crew writes when the rule takes the issue and completes when the run ends, so the span of a run a killed crew left keeps no end. A process is not a level of the work: one process works on many issues, and one issue's runs span many processes. How long an issue waited at a label comes from the gaps between its moves. The config chooses the store with `statistics.store`, or turns recording off with `off`. A record it cannot write is a warning, never a failed rule.

## Reporting

### Status comment

The one comment per issue that shows where the issue stands, with one entry per rule run, oldest first.

Only the latest entry changes; earlier entries keep the text they had when their rule run ended. Editing it notifies no one, so it is not how crew tells you something failed. A full comment is continued in a new one.

### Failure report

The comment a route's `report` step posts: the rule, the route its run ended through, and the action that ended the run, with its verdict and where its log is, or, for the answered rule's check, why it did not return the issue, in crew's own words.

It is a new comment, so the tracker notifies the people who watch the issue, and it never quotes what a session or a tool said.

### Owed call

A tracker write crew decided on whose last attempt failed transiently, and which crew tries again at each poll: an issue's take, a route's move, close, comment or report, or a pull request report.

An issue shows owed from the first such failure of its take or a route's step until all of them settle, and keeps its slot meanwhile; an owed pull request report or status comment write holds no slot. After a stop, each owed call gets one final try, and crew gives it up if that fails.

### Mirrored label

The rule label an issue's pull requests carry, which crew sets to the issue's own rule label each time it moves the issue. An issue's pull requests are the open ones in its own repository that are linked as closing it; merged and closed ones, and those in other repositories, are not.

It goes from the issue to its pull requests only: crew replaces a rule label put on a pull request by hand at the issue's next move. A rule takes the items of its kind that carry its ready label, so a pull request that carries a rule's ready label, mirrored or not, is taken only by a rule that takes pull requests; crew leaves it alone otherwise, with a notice.

### Stop comment

The comment crew posts on each of an issue's open pull requests when a run of a rule with actions ends, saying which route the rule ended through and where that moved the issue and the pull request, or that the route closed the issue and took crew's labels off the pull request, and that nobody watches the pull request any more.

It is a new comment at every rule end, so its watchers are notified and a rerun leaves a trail. Like the failure report, it never quotes what a session or a tool said.

### crew's marker

The hidden HTML comment `<!-- crew:posted -->` that crew puts on every comment it posts or edits on an issue or a pull request: reports, route comments, rule questions and their delegations, status comments and stop comments. Shell actions get it as `CREW_COMMENT_MARKER`, to put on the comments they post.

Every marker of crew's starts with `<!-- crew:`, which GitHub renders as nothing, and a comment that holds one anywhere is never an answer. A session's own marker, `<!-- crew:session run=<run id> action=<action> -->`, marks the comment that asks its question. A comment that holds crew's marker is never a session's question, even when it also holds a session's marker. A rule's question also holds `<!-- crew:question id=<id> rule=<rule> return=<label> -->`, and the comment that delegates it `<!-- crew:delegated id=<id> -->`, or `<!-- crew:delegated unread -->` when crew could not read the comments; crew trusts either only on a comment its own writer posted with crew's marker. An answer may carry the question's parameters as `<!-- crew:answer question=<id> rule=<rule> return=<label> -->`: crew strips it before it judges the answer and reads none of its values.

## Identity

### Repository

The repository crew works on, as its tracker identifies it: on GitHub by an id that survives a rename, with `owner/name` as its display name. A tracker that names no repository gets the name of crew's root directory.

crew identifies an issue by its repository and its key, so two repositories' issues with the same number are two issues.

### Bot

A GitHub identity of crew's own: a private GitHub App created with `crew bots create`, owned by the account that owns the repository, whose private key stays on the machine that created it. It acts on GitHub as `<slug>[bot]`, such as `crew-tester[bot]`.

You can have many bots, and a bot is only an identity: it carries no model, prompt or settings. `tracker.bot` names the bot crew's own writes on GitHub act as, and the default bot of every agent; an agent's `bot` names the one its sessions act as. A shell action or a function, as an action or a route's step, acts as the run's latest session's bot, or as `tracker.bot` before any session. Commits stay yours, with the bot as co-author. Without a bot, crew and its sessions act as your `gh` login.

A bot acts when crew could make it act at startup. One that cannot act then stays that way until crew restarts, and its actions act as you. A bot that acts can stop acting while crew runs: when crew's own writes as `tracker.bot` go back to you, which lasts until restart, or when its token fails to renew, which lasts until a renewal succeeds. An action's cost counts on the identity it acted as.

## Sessions

### Captain

Who answers what a coding-agent session crew runs should do next, given the session's id. The session is what is asked about, and the captain is who answers, as an issue is to the tracker.

Its only captain today decides nothing: it hands every session the same placeholder task.

### Task

What a session is asked to do next, as its captain answers it: the task's own id, the id of the session it belongs to, and a prompt.

`crew sessions <session-id> tasks next|current` asks the captain for one and prints it as JSON. Nothing stores a task or checks that its session exists.

### Question

A question a session asked on the issue, in one comment with its own marker, because it needs an answer to go on: the rule run it ran in, its action and the login it acted as, which crew finds the comment by. Only a session whose `on:` maps `waiting` may ask one, and it waits up to its `wait` before it ends with `waiting`.

A rule's question, posted by a question step, is open on the rule that asked, by its id: crew finds it by its marker on a comment crew's writer posted. It is tied to no action: the first session of a run of that rule gets it.

A question stays open from rule run to rule run, through the run journal, until a session at its action succeeds with a verdict other than `waiting`, which closes every question asked there; one that ends with `waiting` leaves them all open, and the latest question asked is the one crew finds. A session that failed, was stopped or crashed with crew closes nothing. A run that finished its `passed` route passes no session's question on. A rule's question stays open, also past a finished `passed` route, until the first session of a later run that got it succeeds with a verdict other than `waiting`.

### Answer

A comment after a question that counts as its answer: written by a code owner whom GitHub does not mark as an App, or by an App on the answering list that asked none of the open questions, and holding none of crew's markers once crew stripped the answer marker. crew's writers ask every rule's question, so they never answer one. Any other comment is ignored and reported nowhere. The answered rule's check and a session's answers follow this one rule.

crew does not watch for answers: whoever answers a session's question moves the issue back to the rule's ready label, and the answerer of a rule's question moves it to `crew:answered`, which returns it to the question's return label. Before the next session at the question's action starts, or the first session of a run when the question is the rule's, crew reads the issue's comments and hands it the answers, newest first, capped at 32 KiB, with how many it left out. The answers reach only that session's prompt and the prompt file kept beside the run's log, never the run journal or a comment. A rule's question's answers reach only the rule that asked: when its return label is another rule's, that rule's sessions get none.

### Answering list

The App logins, each `<slug>[bot]`, whose comments may answer a question, besides the code owners: the top-level `answering_apps`, or crew's bots without it. `[]` lets no App answer, and `github-actions[bot]` is never on it, since any workflow can post anyone's text as it.

A session never answers itself: its own login is left off the list it is told about, and an App that asked one of the open questions does not answer them.

## TypeSafe

### Judge

An optional local service, configured by the `judge` section of `.crew/config.yaml` like the tracker, that answers named questions about a state with typed verdicts. crew starts it before polling, gives every session its address, and stops it with crew; TypeSafe is its first adapter.

crew never depends on it: without the section, or when its service cannot start, crew runs as it would without it.

### Question bank

The repository's own file of named TypeSafe questions in `.crew/`, each with its primitive, pinned model, verdict bands, conservative default, question stage and evidence bar, which the judge answers by name.

A new use of TypeSafe is a new question in the bank. Any change to a question's content, pinned model or bands makes a new version of it.

### Ledger

The judge's local, append-only record, one per repository and written only by its service, of every question asked, every answer, what the caller did with it and what really happened.

It replays the first recorded answer when the same question version and state are asked again. Unlike the run journal, it records judgments, not rule runs.

### Question stage

How far a question is trusted: shadow (answers are recorded and change nothing), confirm (a person approves), or act.

A question moves up only by a person's edit to the question bank, once its evidence passes the question's bar. A later version of a question is treated as shadow until a recheck against the last version that held its declared stage passes, or a calibration of the version itself passes its bar.

## Flagged ambiguities

- "Run" alone is ambiguous: a *rule run* is one pass through a rule, an *action run* is one attempt at one action, and crew's run time limit concerns the whole crew process.
- "Verdict" and "ending" are two things: a *verdict* is one action's result, which its `on:` sends on, while a rule run *ends* through a route, the way the whole run finished.
- "Question" alone is ambiguous: a *question* is one a session asked on its issue and waits for, answered by code owners and the answering list; a rule's question, posted by a *question step*, is delegated by the *question rule* to the *answerer*, the run that asked ends, and the *answered rule* returns the issue once answered; a question of the *question bank* is a named TypeSafe question the judge answers.
- "Stage" alone is ambiguous: the run journal's `stage` key is a rule's name, kept from earlier versions, while a *question stage* is how far a TypeSafe question is trusted.
