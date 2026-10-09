# crew

[![CI](https://github.com/thatsnotmynameio/crew/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/thatsnotmynameio/crew/actions/workflows/ci.yml)
[![Release](https://github.com/thatsnotmynameio/crew/actions/workflows/release.yml/badge.svg)](https://github.com/thatsnotmynameio/crew/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platforms: Linux and macOS](https://img.shields.io/badge/platforms-linux%20%7C%20macOS-lightgrey)](.goreleaser.yaml)

crew moves your GitHub issues through rules you declare in the repository. Each rule reacts to one label: crew polls for the issues and pull requests that carry it, moves each to the rule's running label and runs the rule's actions one after another, in one git worktree and branch per run. An action is a headless Claude Code or Codex session, a shell script, or a function built into crew. Each action ends with a verdict, which either runs the next action or ends the run through one of the rule's routes. A route may comment on the issue, post crew's report and run scripts and functions, then moves the issue to another label or closes it. You name every label in the rules: crew has no fixed ones.

crew only runs sessions, scripts and its own functions, comments on issues, moves their labels and closes them. Opening pull requests, reviewing and merging are your prompts' job and yours.

## Quick start

On macOS or Linux, on amd64 or arm64, with `gh` and `claude` or `codex` on your `PATH` and logged in, install the latest release into `~/.local/bin`, without `sudo` or a password. The command checks the download against the release's `checksums.txt` and creates `~/.local/bin` when it is missing. When `~/.local/bin` is not on your `PATH`, it prints the line to add to your shell's startup file; when another `crew` comes first on your `PATH`, such as one installed in `/usr/local/bin`, it names that file:

```sh
(
  set -eu
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case $(uname -m) in
    x86_64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) echo "crew has no build for $(uname -m)" >&2; exit 1 ;;
  esac
  archive="crew_${os}_${arch}.tar.gz"
  url=https://github.com/thatsnotmynameio/crew/releases/latest/download
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  cd "$tmp"
  curl -fsSLO "$url/$archive"
  curl -fsSLO "$url/checksums.txt"
  if command -v sha256sum > /dev/null; then
    grep " $archive\$" checksums.txt | sha256sum -c -
  else
    grep " $archive\$" checksums.txt | shasum -a 256 -c -
  fi
  tar -xzf "$archive" crew
  bin="$HOME/.local/bin"
  install -d "$bin"
  install -m 0755 crew "$bin/crew"
  "$bin/crew" --version
  case ":$PATH:" in
    *":$bin:"*)
      hash -r 2>/dev/null || true
      found=$(command -v crew || true)
      if [ -n "$found" ] && [ "$found" != "$bin/crew" ]; then
        echo "warning: crew runs $found, not $bin/crew; remove $found to run the crew just installed" >&2
      fi
      ;;
    *)
      echo "warning: $bin is not on your PATH; add this line to your shell's startup file, such as ~/.profile, ~/.bashrc or ~/.zshrc:" >&2
      echo "  export PATH=\"\$HOME/.local/bin:\$PATH\"" >&2
      ;;
  esac
)
```

With Go 1.27 or later, `go install github.com/thatsnotmynameio/crew/cmd/crew@vX.Y.Z` builds a release instead, where `vX.Y.Z` is its tag from the [releases page](https://github.com/thatsnotmynameio/crew/releases).

Each release after v0.1.1 carries a signed build-provenance attestation for each archive and for the `crew` inside it, which shows that crew's own Release workflow built it from `main`. To check one, with `gh` logged in, run `gh attestation verify` on a downloaded archive or on the `crew` you installed:

```sh
gh attestation verify crew_linux_amd64.tar.gz --repo thatsnotmynameio/crew --signer-workflow thatsnotmynameio/crew/.github/workflows/release.yml --source-ref refs/heads/main
gh attestation verify ~/.local/bin/crew --repo thatsnotmynameio/crew --signer-workflow thatsnotmynameio/crew/.github/workflows/release.yml --source-ref refs/heads/main
```

The install command above does not run this check, so it works without it. Releases up to v0.1.1 have no attestation, and neither has a crew built by `go install` or in a checkout: `gh` reports that it found none.

Commit a `.crew/config.yaml` that declares your agents and rules ([`.crew/config.example.yaml`](.crew/config.example.yaml) lists every key, commented out and explained: copy it and uncomment what you need, and [`schema/config.schema.json`](schema/config.schema.json) gives your editor completion for every key in any of crew's config files), then run `crew` in the repository's main checkout:

```sh
crew
```

Keep your own settings, such as `poll_interval_seconds` or a `board`, in `.crew/config.local.yaml` beside it, and have git ignore that file. Each top-level key the local file sets replaces that whole key of `config.yaml`: a local `board` replaces the board, and a local `agents` replaces every agent. A local key with no value, such as `board:`, brings back crew's default for it. Keys the local file leaves out keep their value from `config.yaml`. Either file may hold any key. Every config error names the file its key came from.

Settings you share across repositories, such as your agents, bots or board, can live in one global file outside any repository: `~/.config/crew/config.yaml` on Linux and macOS, or `$XDG_CONFIG_HOME/crew/config.yaml` when `XDG_CONFIG_HOME` is set. crew reads it in every repository it runs in, first: each top-level key of `.crew/config.yaml` replaces that whole key of the global file, and each top-level key of `.crew/config.local.yaml` replaces it in both, so a repository's own files always win. The global file takes every key the other two take, and crew needs at least one of the three, so the global file alone is enough: in a repository with no `.crew/` files, its `rules` act on that repository's issues. A relative `XDG_CONFIG_HOME` or home directory, or none, leaves crew without a global file. Config errors name the global file by its full path.

crew also records each of its runs in a database on your machine; [Statistics](#statistics) says what it records, where, and how to turn it off.

crew shows a live view of the issues it holds and the sessions it runs; `--plain` prints one line per event instead. You act on issues through their labels on GitHub. Under the header, Bots shows a card for each bot crew acts as, then one for you: whether it can act, what it cost this run, what acts as it and what runs as it now. Below them, the board has a card for each issue in each of its columns. Without a `board` in the config, it has one column per rule that has actions, holding the rule's ready and running labels, and a column of its own for each label where the route of a `waiting` verdict leaves the issue, so an issue paused there stays on screen. A card shows the issue's reference and title, then `run` (the action its run is on and how long it has run, how many of the rule's actions are left after it, and the route the run ends through once it chose one, or its state with none: `blocked` when crew does not hold it and an open issue blocks it), `bots` (the bots its running actions act as) and `via` (the queue its actions run in). In each column, the cards of the issues crew holds come first, then the others, each group oldest first. An issue whose rule ended shows only in the columns its labels put it in, and Events says how the rule ended. A held issue that no column shows, such as a pull request, gets a card in a Not on board column after the others. Queues and Events sit under the board. The board has focus when the view opens, with one card highlighted: ↑↓ move the highlight within a column, ←→ move it between columns, and Enter opens a box over the dimmed view with that issue's rule, its labels as chips with a `blocked` chip after them when an open issue blocks it, its kind, priority and URL, its actions with the bot, queue, state and branch of each and the last thing each said or why it failed, and its events. In the box ←→ move to the previous or next card, ↑↓ scroll it, and Esc closes it. Tab cycles the board, Bots and Events, `b` and `e` jump to Bots and Events, and Esc returns to the board; while Bots has focus, ←→ scroll its cards when they do not all fit. Ctrl+P pauses crew: it takes no new issue while each issue it holds runs on to its end label, and the board keeps refreshing. The header then reads `PAUSED · 2 running`, or `PAUSED · nothing running` once no held issue is left, and Events says when crew pauses and when it resumes. Ctrl+P again resumes crew, which lists at once. `?` lists every key.

## Upgrading crew

A crew installed from a release upgrades itself to the latest release, or to the release you name, which is also how you go back to an older one:

```sh
crew upgrade
crew upgrade v0.4.0
```

crew prints the version it replaced, the one it installed and where:

```text
crew: upgraded v0.4.0 to v0.5.0 at /home/you/.local/bin/crew
```

When crew already is that version, it prints `crew: already v0.5.0` and changes nothing. The version is `vX.Y.Z` or `X.Y.Z`. crew downloads the release's archive for your OS and architecture, checks it against the release's `checksums.txt`, and replaces the crew that runs, at its own path, without `sudo` and without asking first. It needs no login; when `gh` is logged in, it sends that login, which raises GitHub's rate limit. A crew that is already running keeps its version until you start it again. Only a crew installed from a release upgrades: a crew built by `go install` is left as it is, and `crew upgrade` prints the `go install` command to run instead; a crew built in a checkout, such as with `go build`, is left too, and `crew upgrade` says it is a local build. When crew cannot write the directory it runs from, such as `/usr/local/bin` owned by root, it changes nothing and tells you to install crew into `~/.local/bin` with the Quick start's command, then remove the old binary. Any other failure also leaves the installed crew as it was and names its cause, such as no network, GitHub's rate limit, a release that does not exist or a checksum that does not match. `crew upgrade` exits 0 once crew is the version asked, 1 when the download or the check failed, and 2 on a command line it cannot use, a crew built by `go install` or locally, or a directory it cannot write.

crew v0.1.1 and earlier came out before `crew upgrade` and answer `crew: unexpected argument "upgrade"`: install crew once more with the Quick start's command, then use `crew upgrade` from there on. Going back with `crew upgrade` to such a release leaves a crew that needs that command again to come forward.

## Rules

When crew moves an issue or pull request, it adds the new state label before removing the old ones. If adding fails, the old labels stay; if removal fails, the new label stays and crew retries the removal. An interrupted move can leave an item in two states; crew skips items in multiple states and reports them in Events.

A rule takes the items that carry its `ready` label, issues by default or pull requests when its `takes` says so, and moves each to its `running` label. It then runs its `actions` one at a time, in the order listed, and ends the run through one of its `routes`:

```yaml
actions:
  tests: go test ./...
  pr-opened: |-
    n=$(gh pr list --head "$CREW_BRANCH" --state open --json number --jq length) || exit 1
    [ "$n" -gt 0 ] || { echo "no open pull request from $CREW_BRANCH"; exit 1; }

rules:
  development:
    labels:
      ready: ready
      running: in progress
    actions:
      - agent: developer
        name: implement
        prompt: |-
          Implement {{.Issue.Ref}}. Open a pull request whose body has the line `Closes {{.Issue.Ref}}`.
        on:
          blocked: blocked
      - tests
      - pr-opened
    routes:
      passed: in review
      failed:
        - report
        - move: failed
      blocked:
        - comment: "{{.Issue.Ref}} is blocked: `{{.Action}}` ended with `{{.Verdict}}`."
        - move: blocked
```

An action is a session, a shell action or a [function](#functions). A session is written in the rule, with its `prompt` and the `agent` that runs it, which you can leave out when `agents` declares only one. Its prompt is a Go template over the item: `{{.Issue.Ref}}`, `{{.Issue.Key}}`, `{{.Issue.Title}}` and `{{.Issue.URL}}`. A session is named after its agent unless its `name` says otherwise, and no two actions of a rule share a name. A shell action is defined once, under the top-level `actions`, and a rule names it in its list: by its name alone, or as a key left empty, with `on` and `name` beside it. All the actions of a run share one git worktree and branch, `issue-<number>-<rule>`, so each sees what the ones before it left. crew creates the worktree only for a rule with a session or a shell action: a function never needs one.

Every action ends with a verdict. A session that succeeds gives `passed` and one that fails gives `failed`. It may instead end with one of the verdicts its `on` names, by writing it on the first line of the file that `CREW_VERDICT_FILE` names; crew's prompt tells it how, and a first line that is no verdict counts as `failed`. A shell action passes when it exits 0 and fails otherwise, unless its definition's `verdicts` names the verdict of its exit status. A function gives the verdict it returns. A verdict other than `passed`, `failed` and `waiting` that the action's `on` does not name counts as `failed`. A stop, a session or script that cannot start and a prompt that does not render always give `failed`, except that a stop leaves the answered rule's check, below, its own verdict once it reads the issue's comments. A verdict's name is a lowercase letter, then lowercase letters, digits, `-` or `_`.

An action's `on` maps each verdict to `next`, which runs the next action, or to one of the rule's routes, which ends the run there: the actions after it do not run. Without an entry, `passed` leads to `next` and every other verdict to `failed`. When the last action's verdict leads to `next`, the run ends through `passed`.

A rule with actions declares the routes `passed` and `failed`, and every route an `on` leads to. A route is a label to move the item to, or a list of steps that run in order:

- `report` posts crew's report on the issue: the action that ended the run, its verdict, the route and the action's log.
- `comment` posts a comment, a Go template that may name `{{.Issue.Ref}}`, `{{.Issue.Key}}`, `{{.Issue.Title}}`, `{{.Issue.URL}}`, `{{.Rule}}`, `{{.Action}}` (the action that ended the run), `{{.Verdict}}`, `{{.Route}}` and `{{.Log}}`, and nothing else.
- The name of a shell action of `actions` runs it, as a step. The name of a function, or of a preset of one, calls it, as a step, alone or as a key whose value holds its parameters.
- `question` asks a question on the issue, for the answerer `questions` names; see below.
- `move` moves the item to a label. `close` closes the issue, takes crew's labels off it and off its pull requests, which stay open, and posts the stop comment on each of them.

The last step, and only it, is a `move`, a `close` or a `question`, after which crew adds the move to `crew:question` itself. A step that fails shows on the issue's status comment, and the route goes on, so the final move or close still happens. Neither a comment nor a report ever carries what a session or a script printed. A rule without actions declares only `passed`: crew runs it as soon as it takes the item, without a worktree or a session.

crew refuses a config, naming the file, the key and its line, when an `on` names a route the rule does not declare, when nothing leads to a route other than `passed` and `failed`, when a route does not end with `move` or `close` or has one before its last step, when a route moves the item to the rule's own `ready` label or to any rule's `running` label, when two actions of a rule share a name, or when a route is named `next`. A top-level action cannot take a word of the rules' own, such as `agent`, `prompt`, `on`, `move` or `next`, or the name of a function crew registers, as its name. crew also refuses to start, with exit status 2, when a route comments on or closes issues and the tracker cannot, or when a session may wait for an answer and the tracker cannot list an issue's comments, naming the rule and the action.

When an issue returns to a rule's `ready` label and that rule's last run on it ended through any route other than `passed`, or chose one and never finished it, crew resumes that run: it reopens the run's worktree and starts at the action that ended it. The actions before it do not run again. When that action is a shell action or a function that judged a session before it, crew starts at that session instead, unless its definition says `resume: self`, as one that checks something outside the worktree, such as CI, would. A resumed session is a new session, whose prompt tells it that it continues an earlier run, which route that run ended through and where its log is. A run that crashed during an action resumes at that action. When the last run chose `passed` but its final move or close never landed, crew runs only the `passed` route again, in that run's worktree when it still exists. When the worktree is gone, crew starts over at the first action in a new one. crew never resumes on its own: only the `ready` label going back on the issue does. Runs that failed under a crew release without routes start over in a new worktree.

A session can ask a question on the issue and wait for an answer. Map `waiting` in its `on`, usually to a route that moves the issue to a label of its own, and set how long it waits with `wait`:

```yaml
rules:
  development:
    labels:
      ready: ready
      running: in progress
    actions:
      - agent: developer
        name: implement
        wait: 30m
        prompt: |-
          Implement {{.Issue.Ref}}. Ask on the issue when a decision is not yours to make.
        on:
          waiting: waiting
    routes:
      passed: in review
      failed:
        - report
        - move: failed
      waiting:
        - comment: "{{.Issue.Ref}} waits for an answer. Answer on the issue, then move it back to `ready`."
        - move: waiting for answer
```

crew's prompt tells such a session how to ask and how to wait. The session asks one question as one comment on the issue, with its own hidden marker in it, `<!-- crew:session run=<run id> action=<action> -->`, and writes `waiting` in the file `CREW_VERDICT_FILE` names as soon as it posts. It then waits up to its `wait`, a Go duration such as `30m` or `1h30m`, or 10 minutes when left out, through short checks of the issue's comments. It reads them only with the one `gh api` command crew writes into its prompt, which prints only the comments that count as answers, so no one else's text reaches the session while it waits. Once an answer counts, the session replaces `waiting` in the file, or empties it, and goes on with the work. When none came, it checks once more and ends with `waiting`, and the run ends through the route `waiting` leads to. Each check is one command of at most 5 minutes. crew raises Claude Code's command timeout above that; a Codex session is asked to set its tool's timeout itself, which has not been tried yet.

Only the code owners, the logins sessions get as `CREW_CODE_OWNERS`, and the Apps on the top-level `answering_apps` list may answer. Without `answering_apps`, that list is crew's bots; a list you write replaces them, and `[]` lets no App answer. Each entry is an App's login, `<slug>[bot]`, such as `claude[bot]`. crew refuses any other entry, and `github-actions[bot]` in any case, since any workflow can post anyone's text as it. The asking session's own login is left off the list. Logins match ignoring case. A comment by a code owner counts only when GitHub does not mark its author as an App, and one by a listed App only when it does. crew ignores a comment by anyone else and reports it nowhere. A comment that holds `<!-- crew:` anywhere never counts, and crew puts its own hidden marker, `<!-- crew:posted -->`, on every comment it posts: its reports, route comments, questions and their delegations, status comments and stop comments on pull requests.

crew does not watch the issue for answers. Whoever answers moves the issue back to the rule's `ready` label, and crew resumes the run. Before a session starts at an action where an earlier session may have asked a question, crew reads the issue's comments. It finds the question by that session's marker and login, and hands the new session the answers that count, newest first and each whole, up to 32 KiB, with how many older ones it left out on the issue. When no answer came after the question, the prompt says so. The answers go only into the prompt, and the prompt file crew keeps beside the run's log for the shell actions after the session, never into the run journal, a comment or the status comment. When crew cannot read the comments, the prompt says so and gives the session the filtered command to read them with. A question stays open from run to run until a session at that action succeeds with a verdict other than `waiting`, which closes every question asked there; one that ends with `waiting` again leaves them all open, its own and the earlier ones, and the latest question asked is the one crew finds. A run that ended through `passed` and finished its route leaves no session's question open.

A rule can also ask a question that someone else answers. The top-level `questions` names who answers every question, and turns questions on:

```yaml
questions:
  answerer: octocat
  queue: default

rules:
  triage:
    labels:
      ready: triage
      running: triaging
    actions:
      - agent: triager
        prompt: Find the component {{.Issue.Ref}} belongs to.
        on:
          unsure: unsure
      - question:
          id: priority
          text: "How urgent is {{.Issue.Ref}}?"
          return: triage
        name: ask-priority
      - agent: triager
        name: plan
        prompt: Plan the work for {{.Issue.Ref}}.
    routes:
      passed: planned
      failed:
        - report
        - move: triage failed
      unsure:
        - comment: "`{{.Action}}` could not place {{.Issue.Ref}}."
        - question: {id: component, text: "Which component does {{.Issue.Ref}} belong to?", return: triage}
```

`answerer` is one login: a person's, or an App's as `<slug>[bot]`. crew refuses `github-actions[bot]`. `queue` names the queue crew's question and answered rules run in, one of `queues` or `default`, which it is when left out. Without `questions`, crew refuses every question.

A question is a route's step, `question: {id, text, return}`, or an action of the same form, with an optional `name` beside it, `question` by default. `id` names the question, with a verdict's grammar. `text` is the question, a Go template as a route's `comment` is. `return` is the label the issue goes back to once the question is answered: it must be the `ready` label of one of the rules, compared ignoring case, and crew refuses any other label, naming the file, the key and its line. It refuses the `ready` label of a rule that takes pull requests too, as that rule would never take the issue back. A question names no one: the config's answerer answers every question. A question step is the last step of its route: crew adds the move to `crew:question` after it and refuses any step written after it. A question action ends as soon as the run reaches it, with the verdict `asked`, and takes no `on`. `asked` leads to a route named like the action, which crew adds to the rule: it posts the question, then moves the issue to `crew:question`. crew refuses a route you declare with that name, so two question actions of one rule need two names. crew refuses a question in a rule that takes pull requests.

The question is a new comment, posted as crew's own writes are, as `tracker.bot` or as you. It holds the rendered text, then a hidden marker, `<!-- crew:question id=<id> rule=<rule> return=<label> -->`, then crew's own marker.

With `questions`, crew adds two rules of its own, `question` and `answered`, before yours. The question rule takes the issues in `crew:question`, moves each to `crew:question:in progress` and at once runs its `passed` route, as for any rule without actions. That route reads the issue's comments, finds its open question, posts a comment that asks the answerer to answer it, then moves the issue to `crew:question:waiting answer`, which no rule takes. The answered rule, below, takes the issues in `crew:answered`, runs in `crew:answered:in progress` and moves an issue it cannot return to `crew:answered:failed`, which no rule takes. crew creates the six labels at startup with the others. With `questions` written, crew refuses a rule of yours named `question` or `answered`, and one that takes, runs in or moves an issue to one of those labels. Neither rule sends a notification. The default board shows none of the question rule's labels, since it has no actions, and an `answered` column with `crew:answered` and `crew:answered:in progress`; a `board` you write can show any of them.

The open question is the latest comment that holds a question's marker and crew's own marker, written by `tracker.bot` or by you, so a marker anyone else writes counts for nothing. The comment that delegates it reads, for example, ``@octocat, crew asks you to answer the question `priority` that `triage` asked on #12.``, then ``Post your answer on #12 first, then move #12 to `crew:answered`, and crew hands it to the rule that asked.``, and carries a hidden marker of its own, `<!-- crew:delegated id=<id> -->`. It never quotes the question, which the issue already shows. When crew finds no open question, or cannot read the comments, the comment still mentions the answerer and says so, so an issue put in `crew:question` by hand is not left there unseen. When it found none, it asks for no move, since the answered rule would find no question either; when it could not read them, it still asks for the answer and the move, and its marker is `<!-- crew:delegated unread -->`. A question followed by its delegation is no longer open, so crew never delegates one twice. crew reads an issue's comments only for this, for the answered rule's check and for the answers a session gets.

An App answerer, such as `claude[bot]`, is written as `@claude` in a code span, so GitHub notifies no user who owns the login `claude`. GitHub notifies no App when a comment mentions it: an App answers only when its own webhook or workflow acts on the comment's text, and it must be set up to act on comments crew's bot writes. Some, such as Claude Code's GitHub Action, skip comments that bots write unless told otherwise.

The answerer posts the answer on the issue, then moves the issue to `crew:answered`. A comment alone returns nothing: the issue stays where it is and the rule that asked does not resume, so a reply such as "let me think about it" sends nothing back early. The answered rule takes the issue, moves it to `crew:answered:in progress` and runs its one action, `answer`, which reads the issue's comments and checks the answer. The question it checks is the latest comment that holds a question's marker and crew's own marker, written by `tracker.bot` or by you, whose id, rule and return label a question in the config still declares; a delegation crew posted after it that found no open question, or found another, closes it. An answer counts when it comes after the question, holds none of crew's markers, and a code owner whom GitHub does not mark as an App wrote it, or an App on the answering list other than the logins crew posts as. When one counts, the action passes, and its `passed` route moves the issue to the question's `return` label. Otherwise it ends with one of three verdicts, and its `failed` route posts crew's report, then moves the issue to `crew:answered:failed`:

- `no-question`: crew found no open question that the config declares.
- `unanswered`: no answer counts after the question.
- `unread`: crew could not read the issue's comments.

The report says why in crew's own words, never quoting a comment. crew does not check at startup that the answerer may answer: an answer from a user who is not a code owner, or from an App off the answering list, fails the check with `unanswered`.

An answer is plain text. It may also carry the question's parameters as a hidden marker, `<!-- crew:answer question=<id> rule=<rule> return=<label> -->`, the way a bot that answers would write them. crew takes that marker out of the answer and reads none of its values: the issue always returns to the label the question names, whatever the answer's marker says. Any other of crew's markers in a comment still keeps it from counting.

The answer reaches the rule that asked. Its question stays open from run to run of that rule, also past a `passed` route the rule finished, until the first session of a later run, which gets it, succeeds with a verdict other than `waiting`. Before that session starts, crew reads the issue's comments, finds the question and hands the session the answers that count, as for a session's own question: newest first and each whole, up to 32 KiB, after a line that names the question's id, or the command to read them with when it cannot read the comments. When `return` is the `ready` label of another rule, the issue goes to that rule, whose sessions get no answers.

When the rule that asked takes the issue back, it resumes the run: after a question step, at the action whose verdict chose the route; after a question action whose question was posted, at the action after it, or with only its `passed` route when the question was its last action. When the question was not posted, the run starts at the question action and asks it again. A shell action or function that judged a session and resumes at it never steps back past a question action: it resumes at the latest session after the question, or at itself when there is none. When an action follows the question and the run's worktree cannot be reopened, because the run opened none or it is gone, the run starts over at the first action and asks the question again.

Each of the two rules holds a slot of its queue while it runs. Both run in the default queue unless `questions.queue` names another, so when the default queue has no slots, issues wait in `crew:question` and `crew:answered` until `questions.queue` names a queue that has some. crew refuses to start, with exit status 2, when a rule asks a question and the tracker cannot comment on issues, or when `questions` is written and the tracker cannot list an issue's comments or post a delegation, or crew can learn no login it posts as. GitHub's tracker can do all of these.

## Stopping crew

In the live view, `q` or Ctrl+C stops crew only when pressed twice within 3 seconds, in any mix: the first press only says in the footer that another stops crew, so a stray press costs nothing. When 3 seconds pass without a second press, crew carries on and the next press asks again. With `--plain`, Ctrl+C stops crew at once, as SIGINT, SIGTERM and SIGHUP do. crew takes nothing new, starts no other action, and asks each running session and script to stop, giving it up to ten seconds before it kills it, and stops each running function. An action crew stopped gives `failed`, and every run whose action ends while crew stops ends through its `failed` route, except the answered rule's check: once it reads the issue's comments, it ends with its own verdict, and its run ends through the route that verdict leads to. In the route of a stopping run, crew stops a running shell or function step, skips the shell and function steps that have not started and shows them as skipped, and gives each move, close, comment and report its final try. A rule without actions still ends through `passed`. crew exits once every run it held has ended. Once crew is stopping, whatever started the stop, one more `q` or Ctrl+C in the live view, or a second signal, does not wait: it kills every process crew started and exits at once.

To stop crew without losing work, such as to quit for the day or to update crew, press Ctrl+P in the live view, wait for the header to read `PAUSED · nothing running`, then stop crew: nothing runs, so nothing fails. Stopping a paused crew while issues still run stops them as above. A pause lasts only for the running crew: a restarted crew starts unpaused, `--plain` has no pause, and a stop or the run time limit ends it: the header then shows `STOPPING` or `WINDING DOWN` instead, and Ctrl+P does nothing.

`run_time_limit_seconds` ends crew another way. crew takes nothing new, lets each running action finish and starts no other. A run whose action then leads to the next action ends through `failed` instead; one whose action leads to a route ends through that route. Every route runs all its steps, shell and function steps included, before crew exits. A tracker write that keeps failing does not keep crew past the limit: crew then stops as above. This wind-down is not a stop, so stopping it from the live view still takes two presses.

crew exits 0 after a stop or at its run time limit, 1 when it failed while running or a second stop forced its exit, and 2 on a command line it cannot use or a config or environment error, such as a repository without `.crew/config.yaml`, `.crew/config.local.yaml` or a global config file.

## Statistics

crew keeps statistics of its work in a SQLite database on your machine: `statistics.db` in crew's data folder, which is `$XDG_DATA_HOME/crew` when `XDG_DATA_HOME` is set and `~/.local/share/crew` otherwise, on Linux and macOS alike. For now it records one row each time crew starts: crew's version as `crew --version` prints it, the repository's root folder, and the start time. `crew --version`, `crew bots create` and `crew sessions` record nothing. Several crew processes, in one repository or in many, write to the same database at once. crew sends none of it anywhere.

crew creates the folder and the database on its first write. When it cannot write a record, because the disk is full, another crew holds the database locked too long, or there is no data folder, crew shows a warning in Events, or as a line with `--plain`, such as `warning: could not record this crew process in the statistics store: no data folder: set XDG_DATA_HOME or HOME`, and carries on: a failed write never stops, fails or delays a rule. A relative `XDG_DATA_HOME` or home directory, or none, leaves crew without a data folder. Keep the data folder off a network filesystem: SQLite's write-ahead log, which lets several crew processes write at once, does not work there.

Recording is on unless the config turns it off. `statistics.store` names the store: `sqlite`, the default and the only one crew ships, or `off`, which records nothing and creates no database:

```yaml
statistics:
  store: off
```

Set it in the global config file to turn recording off in every repository; a repository's own files still replace it, as they replace any key. `off` takes no other key. A store crew does not have is a config error, and crew exits with status 2.

## Shell actions

A shell action is a script you define once under the top-level `actions`, by name, and name in a rule's `actions` or in a route's steps. Its definition is the script itself, or a mapping with its `script`, the `verdicts` its exit statuses give, such as `3: needs_person`, and `resume: self`. It runs with `sh -c` in the run's worktree, with ten minutes to finish, or in an empty temporary directory when the run has no worktree, as in a rule without actions. Its output goes to the run's log, `.crew/logs/<worktree>.log`, after a line naming it.

A shell action reads the issue from `CREW_ISSUE_REF`, `CREW_ISSUE_KEY`, `CREW_ISSUE_URL` and `CREW_BRANCH`, and the logins from `CREW_CODE_OWNERS` and `CREW_BOTS`. `CREW_ACTION` names the run's latest session, so a script that judges a session knows which one. `CREW_PROMPT_FILE` names a file with the prompt that session started with, resume note included, and `CREW_LAST_MESSAGE_FILE` a file with its last message as it wrote it, empty when it ended without one. crew keeps both beside the run's log, as `.crew/logs/<worktree>.prompt` and `.crew/logs/<worktree>.last-message`, so a script resumed after a restart still judges the same session, and hands each script its own copies, which it removes once the script ended. A resumed run starts with the latest session of the run it resumes. In a fresh run, before its first session, `CREW_ACTION` and both files are empty. A shell action acts on GitHub as the latest session's bot, and before any session as crew's own writes do: as `tracker.bot`, or as you. `CREW_COMMENT_MARKER` holds crew's hidden marker, `<!-- crew:posted -->`. A script that comments on the issue should put it in its comments: otherwise a comment it posts as a code owner or as a listed App counts as an answer to a waiting session's question.

A shell action passes when it exits 0 and fails on any other status, unless its `verdicts` names that status. One that cannot start, runs out of time or is stopped fails. The issue's status comment shows each shell action that ran, with crew's words and the last line it printed, such as `the shell action pr-opened exited with status 1: no open pull request from <branch>`. Make that last line your reason: crew never shows what a session itself said. A shell step of a route shows only crew's words.

In this repository's own config, the lfg sessions are followed by `session-finished`, which asks TypeSafe's Jev whether the session's last message says it is still waiting on work it started or stopped without doing it, and fails if so. When the session did its part but a person must act before its result can be used, it exits 3, which its definition maps to the verdict `needs_person`, and the run ends through the `needs-person` route, which comments on the issue and moves it to `crew:<rule>:needs person`. It sends the session's prompt and last message to TypeSafe, whose zero data retention is offered only on its enterprise plan. It needs `TYPESAFE_API_KEY` in crew's environment, and `jq` and `curl` on the `PATH`; without them it fails. When the last message is empty or TypeSafe cannot answer, it passes and says it did not judge.

## Functions

A function is Go code built into crew that a rule calls by name, as an action or as a route's step. crew registers no function yet, so a config cannot name one: crew refuses a name that is neither one of `actions` nor a registered function, and lists the registered functions, which are none today.

A rule's action or a route's step calls a function by its name, alone or as a mapping's one key whose value holds the function's parameters. As an action, that key may have `on` and `name` beside it, as a shell action's does. A top-level action can preset a function: a mapping under `actions` whose `name` is the function, with `resume: self` if you want it, and every other key one of its parameters. A rule names the preset as it names a shell action, and the parameters written where it is used replace the preset's key by key; the preset's other keys stay. `name`, `resume` and `script` are a preset's own keys, so a preset cannot set a parameter of those names. In this example, `pull-request` is a made-up function, so the config would not load:

```yaml
actions:
  open-pr:
    name: pull-request
    title: "Fixes {{.Issue.Ref}}"
    draft: false

rules:
  development:
    actions:
      - agent: developer
        prompt: Implement {{.Issue.Ref}}.
      - open-pr:
          draft: true
        on:
          blocked: blocked
```

A parameter is text, a number or a boolean; crew refuses a list or a mapping. Text is a Go template over the item, `{{.Issue.Ref}}`, `{{.Issue.Key}}`, `{{.Issue.Title}}` and `{{.Issue.URL}}`, which crew fills just before each call. crew checks every use's parameters when it loads the config, and refuses to start, with exit status 2, naming the file, the parameter's key path and its line, when the function does not take a parameter, when a parameter has the wrong type, or when the function refuses its value.

A function returns a verdict. `passed` and `failed` always count. Any other verdict counts only when the function declares that it can return it, and it then follows the rule for every action: unless it is `waiting`, the action's `on` must name it, or it counts as `failed`. A function that cannot start, returns an error, runs out of time or is stopped fails. The issue's status comment shows each function action that ran, with crew's words and the error the function returned, if any.

A function runs inside crew's process, in the run's worktree when the run has one. It never makes crew create one, so a rule whose actions are all functions runs without a worktree. It writes to the run's log, after a line naming it, acts as the run's latest session's bot, as a shell action does, and has the same ten minutes to finish. As a route's step, a function that returns anything but `passed` is a failed step, shown in crew's words only, and the route goes on.

## A session's task

A coding-agent session crew runs can ask crew what to do next, from the terminal, by its Claude Code or Codex session id:

```sh
crew sessions 0199b2a4-7c1e-7d3a-9f00-2b6c1e8a4d10 tasks next
```

crew prints the task as one JSON line: its own id, the session's id and a prompt.

```json
{"id":"019a3c51-2b7e-7f10-8c4d-5e6f7a8b9c0d","session_id":"0199b2a4-7c1e-7d3a-9f00-2b6c1e8a4d10","prompt":"Carry on with the work your session was started with."}
```

For now the captain, which answers, decides nothing: every task carries that same prompt, `tasks current` answers as `tasks next` does, and each answer has a new task id. The session id may be any form of UUID, and crew prints it in its canonical lower-case form; nothing checks that the session exists. The command needs no repository or config, and asks GitHub and the agents nothing. It exits 0 once it printed the task, 2 on a command line it cannot use, such as an id that is not a UUID, and 1 when it failed while running.

## What's inside

| Path | What it does |
| --- | --- |
| `cmd/crew` | The `crew` binary. |
| `internal/` | crew's engine, its adapters (`github`, `claude`, `codex`, `git`, and `sqlite` for the statistics), its TUI, `bots` for `crew bots create`, `captain` for `crew sessions`, and `upgrade` for `crew upgrade`. See `AGENTS.md`. |
| `.crew/config.yaml` | crew's own rules: crew runs on this repository too, with rules for features, bugs, refinement of brainstormed features (splitting a large plan into sub-issues, then finding their dependencies, reading in full only the open issues Jev's shortlist names, or every open issue when the shortlist is unavailable) and the hand-offs between them, and labels that start with `crew:`. A split plan's issue stays open as the parts' parent and leaves crew, so crew reports its move to done as given up; that is how a split ends. Keep your own settings in `.crew/config.local.yaml`, which git ignores. |
| `.crew/config.example.yaml` | The reference of every key crew's config accepts, commented out, with what each does and its default. |
| `schema/config.schema.json` | The JSON Schema of `.crew/config.yaml`, `.crew/config.local.yaml` and the global `~/.config/crew/config.yaml`, for editors that complete and explain their keys. |
| `docs/` | Plans (`docs/plans/`), ideation and documented solutions (`docs/solutions/`). |
| `STRATEGY.md` | What crew is for, who it serves, and its boundaries. |
| [`SECURITY.md`](SECURITY.md) | How to report a vulnerability privately, and what crew counts as one. |
| `AGENTS.md` (`CLAUDE.md`) | Instructions for coding agents. |
| `.agents/agents/acceptance-tester.md` | The acceptance tester: writes behavior tests from a plan's acceptance examples, without reading the implementation. |
| `.agents/skills/cw-create-issue/` | The `/cw-create-issue` skill: creates an issue with the label and filled template of one of the issue types it lists. This repository's own aid, with its own labels. |
| `.agents/skills/cw-update-issue-plan/` | The `/cw-update-issue-plan` skill: copies the session's plan file into the issue it is working on, and can move it to one of its types' labels. |
| `.agents/skills/cw-brainstorm/` | The `/cw-brainstorm` skill: runs the brainstorm prompt it holds for an issue, in your own session. |
| `.agents/skills/cw-split-plan/` | The `/cw-split-plan` skill, which crew's refinement rule runs: measures an issue's plan with `measure.sh` and splits one above 10,000 characters or 12 requirements into sub-issues that each merge alone. This repository's own aid. |
| `.agents/skills/cw-rank-blockers/` | The `/cw-rank-blockers` skill, which crew's refinement rule runs: its `rank.sh` asks Jev about each open issue crew's code owners and bots opened, and prints the 5 most likely to block an issue and the 5 it most likely blocks. Its `backtest.sh` replays `rank.sh` against the `blocked_by` links GitHub records and reports how often the real dependency made the top 5. Both record nothing. This repository's own aid. |
| `.github/ISSUE_TEMPLATE/` | The issue templates of crew's own work, one per kind of work; several labels may share one. |
| `.compound-engineering/` | The Compound Engineering plugin's settings for this repository. |
| `.github/workflows/ci.yml` | Pull requests and pushes to `main`: `actionlint`, `go (linux)` and `go (macos)` (on each system: tidy `go.mod`, gofmt, vet, lint, tests, govulncheck; Linux also checks the coverage floor and puts lint's findings in code scanning) and `snapshot` (the release build from `.goreleaser.yaml`, publishing nothing). |
| `.github/workflows/security.yml` | Pull requests and pushes to `main`: `codeql (go)` and `codeql (actions)`, CodeQL's `security-extended` suite (`.github/codeql/codeql-config.yml`) over the Go module, tests included, and over the workflows, published to GitHub code scanning. No job holds a secret. It also runs `grype` (vulnerable and malicious packages in every manifest, through `anchore/scan-action`; fails only on a tool error, never on its findings, which `.grype.yaml` ignores by vulnerability id and package with a reason) and `dependency-review` (on a pull request, fails when it adds a dependency with a license off the workflow's `allow-licenses` list or a known vulnerability; a package is exempted through `allow-dependencies-licenses` and an advisory through `allow-ghsas`, each with a comment giving the reason). A malware match is never ignored: the package is removed. |
| `.github/workflows/release.yml` | Started by hand on `main`: when `VERSION` has no release yet, GoReleaser builds crew and publishes `vX.Y.Z`, a GitHub release with the binaries, `checksums.txt` and notes made from the commits since the last release. The workflow then attests each archive and the `crew` inside it. Merging a pull request publishes nothing. |
| `.goreleaser.yaml` | What a release builds and publishes: crew for macOS and Linux on amd64 and arm64, one archive per platform, and `checksums.txt`. |
| `.github/dependabot.yml` | Weekly updates, one pull request per ecosystem, of the pinned actions and of crew's Go module and its tools, each version at least 7 days old. |
| `VERSION` | The version. A pull request bumps it; the release is started by hand once it merges. |
| `CHANGELOG.md` | What changed in each version for the people who use crew, newest first, up to 0.1.1. Later versions' notes are on their GitHub releases. |

## Symlinks on Windows

`CLAUDE.md`, `.claude/agents` and `.claude/skills` are symlinks. On Windows, clone with `git clone -c core.symlinks=true` (with Developer Mode on, or as an administrator), or they check out as small text files holding the path. crew itself does not run on Windows.
