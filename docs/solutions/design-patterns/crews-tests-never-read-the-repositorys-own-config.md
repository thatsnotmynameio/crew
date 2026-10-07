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
  - "Changing this repository's .crew/config.yaml: its rules, queues, bots, prompts or shell actions"
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

- **crew's behaviour: Go tests over invented fixtures.** A test that needs a full config loads one from `internal/config/testdata/`. `TestAFullConfigLoads` (`internal/config/config_full_test.go:32`) loads `testdata/translation` (`config_full_test.go:15`), a translation team's config written for the test. It covers every kind of key: queues, a tracker bot and an agent bot, both harnesses, shell actions defined as a string and as a mapping with `verdicts` and `resume`, sessions with and without a name, `on` maps, routes as a label and as a list of steps, rules with and without actions, notify set both ways and multi-line prompts. A fixture copies nothing of this repository's config: no rule, queue, bot or label name, prompt, shell action or skill.
- **This repository's setup: `tools/crew_config_test.sh`, run by hand.** The refine prompt's agreement with `/cw-split-plan` and `/cw-rank-blockers`, the 17 cases of the `session-finished` judge and its request shape, and the wiring of the shell actions after the sessions live there. It is POSIX sh, like `rank_test.sh` next to the skills, and runs after a change to the config (`sh tools/crew_config_test.sh`; the config's header and `AGENTS.md` say so). It is not in CI, so the repository's config changes without failing crew's build.

The shell script reads the config with awk, not through crew's loader. `definition NAME` (`tools/crew_config_test.sh:36`) prints a shell action's definition under the top-level `actions:`, a string's script under `script: |-` as a mapping's would be. `rule NAME` (`tools/crew_config_test.sh:49`) prints a rule's keys, and `sequence RULE` (`tools/crew_config_test.sh:77`) its actions in order, each with its `on`. All match fixed indents, so another valid YAML style fails the script loudly rather than letting it pass. The judge runs under `env -i` with stub `curl` and `sleep` first on `PATH` (`tools/crew_config_test.sh:255`), as the shell adapter would run it, without TypeSafe.

## Why This Matters

A test that reads the repository's own config fails on every edit of that config, so whoever edits it must also edit crew's tests. Then the tests describe the config instead of guarding the loader. Moving those tests into a shell script also loses some coverage, and two gaps are easy to miss:

- **Wiring.** The Go judge test failed when development's `lfg` action named no `session-finished` check (in the format before #254), because `judgeScript` called `t.Fatal` on an empty script. The first shell port only read the `checks:` map, so the judge could be unwired from every action and the script still printed `ok`. Review caught it. The script now checks that development and fix run `lfg`, then `session-finished` with its `needs_person` verdict sent to a `needs-person` route, then `pr-closes-issue` (`tools/crew_config_test.sh:207`), as it checks that refinement runs `refine`, then `split-finished`.
- **Validity.** Nothing runs `config.Load` (`internal/config/config.go:121`) on the repository's config any more. A misspelled shell action name merges green. crew still refuses that config when it starts, with exit 2, so the failure is loud but late.

## When to Apply

- A new config key or feature needs a loader test: extend `testdata/translation` or add another invented fixture. Never point a test at the repository's own `.crew/config.yaml`.
- A change to this repository's prompts, shell actions or skills: run `sh tools/crew_config_test.sh`. It needs `jq`, as the judge does.
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
want='session lfg
shell session-finished needs_person=needs-person
shell pr-closes-issue'
for name in development fix; do
	got=$(sequence "$name")
	[ "$got" = "$want" ] || fail "the $name rule runs \"$got\", want \"$want\""
	rule "$name" | block "routes:" | grep -qx 'needs-person:' || fail "the $name rule has no needs-person route"
done
```

## Related

- #277, the issue this fixed.
- `docs/solutions/design-patterns/global-config-path-is-one-rule-kept-in-two-places.md`: the same module's other test-isolation rule (a developer's own config files never reach a test).
