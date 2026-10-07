---
title: crew's tests never read the repository's own config
date: 2026-10-07
category: design-patterns
module: internal/config, .crew
problem_type: design_pattern
component: config_loader
severity: medium
applies_when:
  - "Writing a test in internal/config, or anywhere in crew, that needs a full config"
  - "Changing this repository's .crew/config.yaml: its rules, queues, bots, prompts or checks"
  - "Changing the /cw-split-plan or /cw-rank-blockers skills, whose outcomes and markers the refine prompt names"
  - "Reading the #220 plan (rule sequences), which still names config_own_test.go and judge_test.go"
tags: [config, test-fixture, testdata, own-config, self-hosting, session-finished, judge, config-test-sh]
---

# crew's tests never read the repository's own config

## Context

crew runs on its own repository, with the rules in `.crew/config.yaml`. Until #277, crew's unit tests loaded that file: `config_own_test.go` symlinked it into a temporary root (`loadOwn`) and pinned its rule, queue, bot, label and check names, the refine prompt's wording and the skills it runs. `judge_test.go` took the `session-finished` check script from `loadOwn(t).Rules[3]`, and `schema_test.go` checked its modeline.

So renaming the `clerk` queue in the repository's config failed `TestTheRepositorysOwnConfigLoads`, though crew's code had not changed. The tests pinned how this repository uses crew, not what crew does.

Plans are not updated after they ship. `docs/plans/2026-10-07-0724-feat-rule-sequences-and-routes-plan.md` (#220) still says `config_own_test.go` pins the `lfg` and `refine` session names (KTD16), and lists `config_own_test.go` and `judge_test.go` among the files its unit changes. Both files are gone: read that unit against this doc.

## Guidance

Split the two kinds of test by what they test.

- **crew's behaviour: Go tests over invented fixtures.** A test that needs a full config loads one from `internal/config/testdata/`. `TestAFullConfigLoads` (`internal/config/config_full_test.go:32`) loads `testdata/translation` (`config_full_test.go:15`), a translation team's config written for the test. It covers every kind of key: queues, a tracker bot and an agent bot, both harnesses, a single check and a list of checks, rules with and without actions, notify set both ways and multi-line prompts. A fixture copies nothing of this repository's config: no rule, queue, bot or label name, prompt, check or skill.
- **This repository's setup: `.crew/config_test.sh`, run by hand.** The refine prompt's agreement with `/cw-split-plan` and `/cw-rank-blockers`, the 17 cases of the `session-finished` judge and its request shape, and the wiring of the checks to the actions live there. It is POSIX sh, like `rank_test.sh` next to the skills, and runs after a change to the config (`sh .crew/config_test.sh`; the config's header and `AGENTS.md` say so). It is not in CI, so the repository's config changes without failing crew's build.

The shell script reads the config with awk, not through crew's loader. `check NAME` (`.crew/config_test.sh:33`) prints the `|-` block under `checks:`. `action RULE NAME` (`.crew/config_test.sh:44`) prints an action's keys. Both match fixed indents, so another valid YAML style fails the script loudly rather than letting it pass. The judge runs under `env -i` with stub `curl` and `sleep` first on `PATH` (`.crew/config_test.sh:202`), as the shell adapter would run it, without TypeSafe.

## Why This Matters

A test that reads the repository's own config fails on every edit of that config, so whoever edits it must also edit crew's tests. Then the tests describe the config instead of guarding the loader. Moving those tests into a shell script also loses some coverage, and two gaps are easy to miss:

- **Wiring.** The Go judge test failed when development's `lfg` action named no `session-finished` check, because `judgeScript` called `t.Fatal` on an empty script. The first shell port only read the `checks:` map, so the judge could be unwired from every action and the script still printed `ok`. Review caught it. The script now checks that the `lfg` actions of development and fix run `session-finished` (`.crew/config_test.sh:155`), as it checks that refine runs `split-finished`.
- **Validity.** Nothing runs `config.Load` (`internal/config/config.go:121`) on the repository's config any more. A misspelled check name merges green. crew still refuses that config when it starts, with exit 2, so the failure is loud but late.

## When to Apply

- A new config key or feature needs a loader test: extend `testdata/translation` or add another invented fixture. Never point a test at the repository's own `.crew/config.yaml`.
- A change to this repository's prompts, checks or skills: run `sh .crew/config_test.sh`. It needs `jq`, as the judge does.
- Porting a test from Go to shell: keep every assertion the Go test made, including the ones its helpers made implicitly with `t.Fatal`, and break the config on purpose to see each one fail.

## Examples

Before, a test pinned this repository's queue names:

```go
clerk, developer := crew.Queue{Name: "clerk", Slots: 1}, crew.Queue{Name: "developer", Slots: 2}
```

After, the same check runs on the invented fixture:

```go
cfg, err := config.Load(translation, "")
// ...
name: "accept request", queue: crew.Queue{Name: "desk", Slots: 1},
```

The wiring check the shell port first missed:

```sh
for rule in development fix; do
	action "$rule" lfg | grep '^check:' | grep -q session-finished ||
		fail "the lfg action of $rule does not run session-finished"
done
```

## Related

- #277, the issue this fixed.
- `docs/solutions/design-patterns/global-config-path-is-one-rule-kept-in-two-places.md`: the same module's other test-isolation rule (a developer's own config files never reach a test).
