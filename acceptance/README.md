# crew's acceptance suite

This module tests the `crew` binary that GoReleaser builds, as a user runs it. The binary finds doubles for `gh` and `claude` first on its `PATH`. The `gh` double answers from a fake GitHub that keeps state, and the `claude` double plays the Claude Code sessions a scenario scripts. Nothing reaches real GitHub or real Claude Code.

The module is its own Go module (`github.com/thatsnotmynameio/crew/acceptance`). It never imports crew's packages: the suite reaches crew only through the binary.

| Path | What it holds |
| --- | --- |
| `harness/` | Scenarios (`New`), the screen harness, masks, snapshots, and `Main`, which every test package's `TestMain` calls. |
| `fakegithub/` | The fake GitHub: a repository's state, and the `gh` calls it answers. |
| `fakeclaude/` | The fake Claude Code: session scripts and stream-json events. |
| `cmd/acceptance/` | The one command that builds crew and runs the suite. |
| `smoke/` | The developer's smoke runs. |
| `scenarios/<area>/` | The tester's scenarios, one package per area of crew. |

Two roles work here. The tester writes the scenarios and their snapshots from crew's README, never from crew's code. The developer builds and maintains the doubles and the harness. The first part of this file is the tester's; [For the developer](#for-the-developer) is the developer's.

## Running the suite

From the repository root:

```sh
go -C acceptance run ./cmd/acceptance            # every test
go -C acceptance run ./cmd/acceptance -run TestX # go test flags pass through
```

The command builds crew with the release config (a GoReleaser v2.18.2 snapshot for this machine), prints the binary's path and its `--version`, then runs `go test -race -count=1` over the module with `CREW_BIN` set to the binary. `-count=1` keeps Go from reusing a cached pass after crew changed. When a test fails, the command prints the directory that holds the tests' artifacts and keeps it.

A test that runs crew reads the binary's path from `CREW_BIN`. Without it, the test fails and names the command above; it never skips.

CI runs the same suite in the `acceptance` job on every pull request and every push to `main`, against a Linux amd64 snapshot build. Any failing test fails the job. On failure, the job uploads the artifacts as `acceptance-artifacts`.

### Artifacts

Every scenario saves, in its test's artifact directory:

| File | What it holds |
| --- | --- |
| `crew-logs/` | crew's `.crew/logs`. |
| `crew.stdout`, `crew.stderr` | What crew printed, without a screen. |
| `screen.txt` | The last screen, in screen mode. |
| `gh-calls.txt` | Every `gh` call the fake GitHub received. |
| `unknown-calls.txt` | The calls the doubles did not know. |

They are for finding out why a scenario failed. A scenario never asserts on `gh-calls.txt`.

## Writing a scenario

### Where scenarios go

Each area of crew gets a package under `acceptance/scenarios/<area>/`. Its `TestMain` must call `harness.Main`:

```go
package board

import (
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

func TestMain(m *testing.M) {
	harness.Main(m)
}
```

The test binary is the double: crew runs `gh` and `claude` through links to the test binary, and `harness.Main` plays the double when the binary runs under one of those names. A package without it cannot run a scenario. `harness.Main` also registers the `-accept-snapshots` flag.

### A scenario's life

A scenario is one run of crew in a repository of its own:

1. `harness.New(t, harness.Options{...})` builds the fake GitHub, the fake Claude Code and the repository. crew does not run yet.
2. Set the starting state on `sc.GitHub` and register a script on `sc.Claude` for every session crew is expected to start.
3. `sc.Start()` starts crew.
4. `sc.Wait(cond, timeout)` waits until the end state holds.
5. `sc.Stop()` sends SIGINT to crew, as Ctrl+C does, and `sc.Exit(timeout)` returns how crew ended. A config that limits crew's run time lets crew stop by itself instead; then call `Exit` without `Stop`.
6. Assert on the fake GitHub's state, crew's output or its screen.

```go
// config is your scenario's .crew/config.yaml.
const config = `...`

func TestSessionThatSucceeds(t *testing.T) {
	sc := harness.New(t, harness.Options{Config: config, Args: []string{"--plain"}})
	n := sc.GitHub.AddIssue(fakegithub.Issue{Title: "Add a search box", Labels: []string{"your:ready"}})
	sc.Claude.Script("a phrase from your config's prompt", fakeclaude.Succeed("Done."))

	sc.Start()
	sc.Wait(func() bool {
		issue, _ := sc.GitHub.Issue(n)
		return slices.Contains(issue.Labels, "your:done")
	}, time.Minute)
	sc.Stop()
	exited := sc.Exit(time.Minute)
	// Assert on exited.Code, exited.Stdout and exited.Stderr as crew's README describes them.
}
```

`harness.Options` has four fields:

- `Config`: the text of `.crew/config.yaml`, committed in the repository's first commit. `""` leaves the repository without one.
- `Args`: crew's command-line arguments. Nothing is added for you: pass `--plain` yourself for plain output.
- `Screen`: run crew in a pseudo-terminal, as a user runs it in a terminal (see [Screens](#screens)).
- `Size`: the terminal's size in screen mode; the default is 120 columns by 50 rows.

A scenario's methods fail the test they were built for, so call them from that test's goroutine. Choose generous timeouts, such as a minute: a wait returns as soon as its condition holds.

### The repository and crew's environment

crew runs in a clone, in a directory named `widgets` (`harness.RepositoryName`), of a local bare origin. On the fake GitHub the repository is `acme/widgets` (`harness.RepositoryOwner`, `harness.RepositoryName`). Its default branch `main` holds one commit, with a `README.md` and your config. `sc.Repo` is the clone's path.

crew's environment is built from a short list, never from the test's environment:

- `PATH`: the doubles' directory first, then the directories of `git` and `sh`.
- `HOME` and `XDG_CONFIG_HOME`: empty temporary directories.
- `TZ=UTC` and `LANG=C.UTF-8`.
- Git reads no system config, and a global config with a test identity.
- `TERM=xterm-256color`, in screen mode only.

No GitHub token is set, and the user config directory is empty. Before crew starts, the harness checks that `gh` and `claude` resolve to the doubles, and fails otherwise.

**Bots that act are out of reach.** With an empty config directory, crew finds no bot of its own, so a scenario cannot cover a bot acting or creating a bot.

### The fake GitHub

`sc.GitHub` (a `*fakegithub.GitHub`) is one repository and the account `gh` is logged in as. It starts empty: `gh` is logged in as `boss`, and there are no labels, files, teams, issues or pull requests. Issues and pull requests share one number sequence, as on GitHub.

Set the starting state before `Start`:

| Method | What it sets |
| --- | --- |
| `AddIssue(fakegithub.Issue{...}) int` | An issue: number (0 takes the next), title, author (the viewer when `""`; a GitHub App's login ends in `[bot]`), labels, creation time, state (`Open` or `Closed`), the option of its single-select `Priority` field, and the issues that block it (only the open ones block). Returns its number. |
| `AddPullRequest(fakegithub.PullRequest{...}) int` | A pull request: number, title, head branch, state (`Open`, `Closed` or `Merged`), author, labels, whether its head is in a fork, the issues it closes, creation time. Returns its number. |
| `AddComment(number, author, body) int64` | A comment on an issue or pull request. Returns its id. |
| `AddLabel(names...)` | Labels the repository lacks, compared ignoring case. `AddIssue`, `AddPullRequest` and `SetLabels` create missing labels too. |
| `SetLabels(number, labels...)` | Replaces an issue's or pull request's labels. |
| `SetState(number, state)` | Closes, reopens or merges. |
| `SetFile(path, content)` | A file on the default branch, as GitHub's contents API serves it (such as `.github/CODEOWNERS`). |
| `AddTeam(org, slug, members...)` | An organization team. A team under the repository's owner makes the owner an organization. |
| `SetViewer(login)` | The account `gh` is logged in as. |
| `SetPriorityOptions(options...)` | The `Priority` field's options, most urgent first. The default is `Urgent`, `High`, `Medium`, `Low`. |

A setup method panics when the setup is wrong, such as a number already taken.

Read the state at any time, and in a `Wait` condition:

| Method | What it returns |
| --- | --- |
| `Issue(number) (Issue, bool)` | The issue as it is now; false when there is no issue with that number. |
| `PullRequest(number) (PullRequest, bool)` | The pull request as it is now. |
| `PullRequests() []PullRequest` | Every pull request, in number order. |
| `Comments(number) []Comment` | The comments on an issue or pull request, oldest first: id, author, body. |
| `Labels() []string` | The repository's labels, in the order they were created. |

Answers are deterministic: comment ids count up from 1001, and every time GitHub would stamp comes from a fixed base time.

**Scripted errors.** `Fail(call, status, message, times)` makes the next `times` `gh` calls that match `call` fail with that GitHub error, as `gh` prints an HTTP error. `call` is a list of `gh` arguments separated by spaces, such as `"issue edit 3"` or `"api --method PATCH"`. A call matches when its arguments hold these words in this order, not necessarily next to each other. `{owner}` and `{repo}` stand for the repository's owner and name.

```go
sc.GitHub.Fail("issue edit 3", 502, "Bad Gateway", 1)
```

**Missing objects.** A known call on an object that does not exist fails as GitHub does, with `gh`'s error text (such as `HTTP 404`). That is not a violation.

### The `gh` subset the fake emulates

Everything in a scenario that runs `gh` goes through the fake: crew, and a check command in your scenario's config. The fake knows these calls, in `gh`'s own terms. Any other subcommand, endpoint, flag, header or GraphQL field is a [violation](#violations).

| Call | What it does |
| --- | --- |
| `gh auth status` | Logged in as the viewer. |
| `gh label list --json name [--limit N]` | The labels, oldest first. |
| `gh label create NAME` | Creates a label; fails when one of that name exists, ignoring case. |
| `gh issue view N --json FIELDS [--jq EXPR]` | An issue or pull request. Fields: `labels`, `number`, `state`, `title`, `url`. |
| `gh issue edit N`, `gh pr edit N` with `--add-label` and `--remove-label` | Changes labels. A label the repository lacks fails, as on GitHub. |
| `gh pr list --json FIELDS [--head BRANCH] [--state open\|closed\|merged\|all] [--limit N] [--jq EXPR]` | Pull requests, newest first, open by default. Fields: `createdAt`, `headRefName`, `isCrossRepository`, `number`, `state`, `title`, `url`. |
| `gh api user` | The viewer. |
| `gh api repos/{owner}/{repo}` | The repository; its owner is an organization when it has teams. |
| `gh api repos/{owner}/{repo}/contents/PATH` | A file's raw text. |
| `gh api orgs/ORG/teams/TEAM/members` | A team's members. |
| `gh api repos/{owner}/{repo}/issues/N/comments` | Lists comments (`per_page`, `--paginate`); with `-f body=...`, adds one as the account `gh` acts as. |
| `gh api -X PATCH repos/{owner}/{repo}/issues/comments/ID -f body=...` | Edits a comment. |
| `gh api graphql -f query=... [-F name=value]` | One query, resolved from the state (below). |

`gh api` takes `-X`/`--method`, `-H`/`--header` (only `Accept: application/vnd.github.raw+json`), `-f`/`--raw-field`, `-F`/`--field`, `-q`/`--jq` and `--paginate` (on the listing endpoints). `--jq` is evaluated by gojq, the evaluator `gh` uses, and strings print unquoted. `--paginate` returns the whole list.

The GraphQL queries the fake resolves start at `repository(owner, name)`, with:

- `nameWithOwner`;
- `issues(first, states, filterBy: {createdBy, labels}, orderBy: {field: CREATED_AT, direction}, labels)` and `pullRequests(first, states, labels, orderBy, headRefName)`, as connections with `nodes`;
- `issueOrPullRequest(number)`.

An issue or pull request has `number`, `title`, `url`, `createdAt`, `state`, `repository` and `labels(first)`. A pull request also has `author { login }`, `headRefName` and `isCrossRepository`. An issue also has `issueDependenciesSummary { blockedBy }`, `issueFieldValues(first)` (the `Priority` option) and `closedByPullRequestsReferences(first, includeClosedPrs)`. `__typename`, aliases and fragments work.

### Scripting a Claude Code session

`sc.Claude` (a `*fakeclaude.Claude`) answers Claude Code's headless print mode with stream-json output: `-p`, `--verbose`, `--output-format stream-json`, `--model`, `--permission-mode`, and the prompt after `--`. A session prints one JSON event per line and ends with a result event that says whether it succeeded; then the process exits with a code.

Register one script for every session crew is expected to start:

```go
sc.Claude.Script(key, fakeclaude.Succeed("Done."))
```

The key is text that the session's prompt must contain. Each invocation runs the first script not yet used whose key its prompt contains, so a key registered twice answers two sessions, in order. An invocation that no script matches is a violation. The prompt comes from your scenario's config: give each action's prompt a phrase of its own and key on it. To tell apart the issues that go through the same action, use a template field that the prompt fills in from the issue, as `schema/config.schema.json` and `.crew/config.example.yaml` show.

Three scripts cover the common sessions:

| Script | The session |
| --- | --- |
| `fakeclaude.Succeed(text)` | Says `text`, succeeds with `text` as its result and exits 0. |
| `fakeclaude.Fail(message)` | Fails with `message` as its result and exits 1. |
| `fakeclaude.NoResult(text, code)` | Says `text` and exits with `code` before any result event, as a session cut short does. |

Every result event carries a fixed usage, so screens that show it stay the same: `fakeclaude.Cost` (US$0.25), `Turns` (3), `InputTokens`, `OutputTokens`, `CacheReadTokens` and `CacheCreationTokens`.

A script of your own is a `fakeclaude.ScriptFunc`: `func(ctx context.Context, s *fakeclaude.Session) int`. It writes events with `s.Emit` as the session goes and returns the exit code. The `Session` has:

- `Dir`: the directory the session runs in, where the script may write files;
- `Prompt`, `Model` and `PermissionMode`, as given on the command line;
- `Env`: the session's `GH_CONFIG_DIR` and `CREW_*` variables;
- `GitHub`: the fake GitHub, which the script may change as a session that runs `gh` would.

Its event builders are `s.Init()` (the system init event that opens a session), `s.Said(text)` (an assistant message; the last one is what a reader shows), `s.Success(result)` and `s.Failure(message)`.

```go
release := make(chan struct{})
sc.Claude.Script("a phrase from your config's prompt", func(ctx context.Context, s *fakeclaude.Session) int {
	_ = s.Emit(s.Init(), s.Said("Writing the fix."))
	if err := os.WriteFile(filepath.Join(s.Dir, "fix.txt"), []byte("fixed\n"), 0o600); err != nil {
		_ = s.Emit(s.Failure(err.Error()))
		return 1
	}
	s.GitHub.AddComment(n, "boss", "Fixed.")
	select {
	case <-release: // the test closes release when the session may end
		_ = s.Emit(s.Success("Fixed."))
		return 0
	case <-ctx.Done(): // crew stopped the session
		return 1
	}
})
```

`ctx` is cancelled when crew stops the session, for example with SIGTERM: a script that blocks must give up then. Scripts of parallel sessions run at the same time, in the test process. A script that runs `git` gets the test process's environment, not crew's: give its command an environment of its own (such as `GIT_CONFIG_NOSYSTEM=1` and a `GIT_CONFIG_GLOBAL` with an identity) so your own git config does not leak in.

### Waiting and reading the end state

- `sc.Wait(cond, timeout)` checks `cond` whenever the fake GitHub's state changes and every few milliseconds. It fails the test when `timeout` passes first.
- `sc.GitHub.Changed()` returns a channel that closes at the next change of the state, for a wait of your own.
- `sc.Exit(timeout)` returns `harness.Exited`: `Code` (-1 when a signal ended crew), `Killed` (crew still ran at the deadline, so the harness killed it and failed the test), `Stdout` and `Stderr`.
- `sc.Stdout()` and `sc.Stderr()` return what crew printed so far, without a screen.

A scenario asserts on the end state: issues, labels, comments, pull requests, crew's output, its exit code and its screen. It never asserts on which `gh` calls crew made.

### Screens

With `Options{Screen: true}`, crew runs in a pseudo-terminal of a fixed size, and a terminal emulator draws its output. `sc.Screen()` returns the `*harness.Screen` after `Start`:

| Method | What it does |
| --- | --- |
| `WaitForText(t, substr, timeout)` | Waits until the screen shows `substr`; returns the text. |
| `WaitFor(t, cond, timeout)` | Waits until `cond(text)` holds for the unmasked text; returns it. |
| `WaitStable(t, settle, timeout, masks...)` | Waits until the masked text has not changed for `settle`; returns it. |
| `Text()` | The screen as plain text: one line per row, without styles or trailing spaces. |
| `Masked(masks...)` | The text with masks applied. |
| `Send(t, keys)` | Types keys. It fails before the first frame, while the terminal still echoes keys and turns `^C` into SIGINT. |
| `Signal(t, sig)` | Sends a signal to crew's process group, as a terminal does. |

A wait fails with the last screen when its timeout passes first. In screen mode crew's output is on the screen: `Exited.Stdout` and `Exited.Stderr` are empty.

A screen with a clock or a spinner never stops changing, so `WaitStable` compares the masked text: give it a `settle` of a few seconds and the masks below.

### Masks and snapshots

A mask hides a part of the screen that changes on every run. `harness.Mask{Pattern, Placeholder}` turns each whole match of `Pattern` into `Placeholder`, at the placeholder's width: a wider match is clipped and a narrower one padded, so `9s` becoming `10s` does not shift the text after it. `harness.DefaultMasks()` applies, in order:

| What | Example | Placeholder |
| --- | --- | --- |
| Clocks | `14:30:05` | `HH:MM:SS` |
| Costs | `$0.42` | `$#.##` |
| Durations | `9s`, `1m5s`, `2h03m` | `##` |
| Spinner frames | the braille dots `⠋⠙⠹…` | `⠿` |
| Runs of the fill `╱` whose width depends on the rest of the line | `╱╱╱╱╱` | `╱╱` |

Add your own masks after the defaults, and apply them to any text with `harness.MaskText(text, masks...)`.

`harness.MatchSnapshot(t, name, text)` compares masked text with `testdata/<name>.snapshot` in the scenario's package. A mismatch fails with the lines that differ. A missing snapshot fails and names `-accept-snapshots`.

```go
masks := harness.DefaultMasks()
text := sc.Screen().WaitStable(t, 3*time.Second, time.Minute, masks...)
harness.MatchSnapshot(t, "board-with-one-issue", text)
```

**Only the tester writes snapshots,** with `-accept-snapshots`, and reviews the diff before committing it. CI never passes the flag. Every package of the suite defines it, so pass it through the local command, with `-run` naming the scenarios whose snapshots you accept:

```sh
go -C acceptance run ./cmd/acceptance -run TestBoard -accept-snapshots
```

### Violations

A violation is a call the doubles do not know: a `gh` subcommand, endpoint, flag, header or GraphQL field the fake GitHub does not emulate, or a `claude` invocation that no script matches. The double exits 1 and the server journals the call. The scenario fails, naming the call as a shell-quoted command line with the reason:

```text
acceptance: unknown call: gh issue comment 3 --body Done. (unknown command)
```

It fails at the first wait after the call, when crew exits (before `Exit` returns its code), and at the end of the test. crew retries a failing `gh` call, so its exit code alone would not show the violation; the journal does.

An unscripted `claude` call means the scenario is missing a script, or its key does not match the prompt. An unknown `gh` call means the fake lacks something crew uses: that is the developer's to add (see below). Do not work around it in the scenario.

## For the developer

The developer builds and maintains the doubles and the harness, and may read crew's code. The developer never edits scenarios (`scenarios/`) or their snapshots.

### Teaching the fake a new call

When a pull request changes how crew calls `gh` or `claude`, the same pull request teaches the doubles the new call. The suite tells you what is missing: run it, and each violation names the call and what the fake did not know (`unknown command`, `unknown flag --x`, `unknown endpoint GET ...`, `unknown GraphQL field Issue.x`, ...). `unknown-calls.txt` in the artifacts lists them all.

- **A `gh` subcommand or flag:** an entry in `commands()` (`fakegithub/issues.go`): its flags (`spec`), a check that returns what the fake does not know about a call, and the handler.
- **A REST endpoint:** a `route` in `routes()` (`fakegithub/rest.go`): method, path pattern, accepted query parameters and body fields, and whether it serves raw content or paginates.
- **A GraphQL field or argument:** the node that holds it in `fakegithub/schema.go`.
- **New state:** a field in the state model (`fakegithub/state.go`), with a setup method and a read method. Document both in the tester's part of this file.
- **A `claude` flag:** `parseArgs` in `fakeclaude/script.go`. A new event shape goes in `fakeclaude/stream.go`, following the recorded stream-json fixtures of crew's Claude Code adapter tests.

Model GitHub, not crew. Answer as GitHub and `gh` do, with their output shapes and error texts: a known call on a missing object fails as GitHub does, not as a violation. Keep the fake strict, so a call it does not fully understand stays a violation. Cover each new call with a test in the package's `_test.go` files.

### Checks

The `acceptance` CI job runs vet, golangci-lint and govulncheck in this module before the suite. Locally, from the repository root:

```sh
go -C acceptance vet ./...
go -C acceptance run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run
go -C acceptance run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
gofmt -l acceptance
```

golangci-lint reads the repository's `.golangci.yml`, whose depguard rule stops this module from importing crew's packages.

`harness`, `fakegithub` and `fakeclaude` test themselves without a crew binary: `go -C acceptance test -race ./harness ./fakegithub ./fakeclaude`. The harness's own snapshot, `harness/testdata/probe.snapshot`, is the developer's; rewrite it with `go -C acceptance test ./harness -accept-snapshots`.

### The smoke runs

`smoke/` runs the release build against the doubles with one rule whose action starts a Claude Code session, and two issues: one session succeeds, one fails. `TestSmokePlain` runs crew with `--plain`; `TestSmokeScreen` runs it in a pseudo-terminal. Each waits until both sessions ran, then checks that crew exits 0 with no violation. They show that the doubles fit the binary. They assert nothing that crew's README promises; the scenarios do.
