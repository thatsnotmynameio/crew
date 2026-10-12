---
title: The prompt's load-time check skipped the empty base every run has
date: 2026-10-12
category: logic-errors
module: internal/crew, internal/config
problem_type: logic_error
component: config_loader
related_components: [core_reducer]
symptoms:
  - "A prompt with a typo in the branch of `{{if .Base}}...{{else}}...{{end}}` that an empty base takes passes config load"
  - "Every session of that action then fails with CausePrompt, since every run renders its prompt with an empty .Base"
root_cause: missing_validation
resolution_type: code_fix
severity: medium
tags: [prompt, text-template, sample-render, config-validation, parse-prompt, stacking, base-branch, stale-plan]
---

# The prompt's load-time check skipped the empty base every run has

## Problem

crew checks a session's prompt when it loads the config by rendering it for sample data (`ParsePrompt` in `internal/crew/prompt.go`). Go's `text/template` resolves a field only on the branch it executes. So a typo on a branch that no sample takes passes the check. #442 added `.Base` and `.Stack` to the prompt's data. Its samples set `.Base` to `"main"` or to the stack's branch, but every real run currently has an empty `.Base`.

## Symptoms

- `{{if .Base}}on {{.Base}}{{else}}{{.Issue.Nmber}}{{end}}` loads without an error.
- Once crew takes an issue, the run renders the prompt with `.Base` empty, takes the `else` branch, and the action fails with `CausePrompt` (`internal/crew/decide.go:117`). This happens for every session of the action, not once.

## What Didn't Work

- **Two samples, as the plan says.** `docs/plans/2026-10-12-1825-issue-442-plan.md` (KTD8's "Rendering" and U3's "Approach") says `ParsePrompt` renders "a stacked and an unstacked sample". The implementation followed it to the letter: an unstacked sample with `Base: "main"` and a stacked one with the stack's branch. Mutation tests showed that the stacked sample catches a typo inside `{{if .Stack.Stacked}}`. Both samples have a non-empty `.Base`, though, so neither takes the empty-base branch (session history).
- **The simplify pass did not catch it.** It judged the samples sound. The correctness reviewer and the Codex adversarial reviewer of `ce-code-review` each found it on their own, at confidence 100 (session history).
- **The plan still lists two samples.** Plans are not updated after they ship. Treat its sample list as incomplete: the code has three.

## Solution

`samplePromptData` gives three samples, and `ParsePrompt` renders the prompt for each of them (`internal/crew/prompt.go:99`):

```go
return []PromptData{
	{Issue: sampleIssue()},                                  // no base yet: every run today
	{Issue: sampleIssue(), Base: "main"},                    // unstacked, default branch known
	{Issue: sampleIssue(), Base: stack.Branch, Stack: Some(stack)}, // stacked
}
```

`TestParsePromptRefusesABadTemplate` in `internal/crew/prompt_test.go` gets a "typo without a base" row with the template above. The row fails without the first sample and passes with it.

## Why This Works

A run's `.Base` comes from `RuleRun.PromptData` (`internal/crew/run.go:62`): the stack's branch when the run is stacked, otherwise its workspace's `DefaultBranch`. Nothing sets `DefaultBranch` yet. `WorkspaceOpened` is built without it (`internal/crew/fact.go:198`), and #444 (open as of this writing) has the workspace report origin's default branch. Until then every run reaches the prompt with `.Base` empty. After #444, `PromptData` still documents `.Base` as empty whenever the run does not know it (`internal/crew/prompt.go:27`), so the empty-base sample stays.

The samples now cover every combination of values a run can render with. A typo on any branch of a conditional over `.Base` or `.Stack.Stacked` stops crew at load, not at the first session.

## Prevention

- **One sample for each state the runtime can reach, zero values included.** When prompt data gains a field that a template can branch on, add a sample for each value that leads down a different branch. The zero value matters most: it is what the field holds until the code that fills it ships, and also whenever the fill fails.
- **Derive the samples from the runtime, not from the plan.** Read the function that builds the real data (`RuleRun.PromptData`) and list what it can return today, not what it will return once later parts land.
- **The other load-time templates have the same gap.** Comment templates render one `sampleCommentData()` (`internal/crew/template.go:51`), and function parameter templates render `sampleIssue()` (`internal/crew/parameter.go:37`). If either gains a field that can be empty or optional, it needs more than one sample.
- **Test the branch, not just the field.** A `ParsePrompt` refusal row for each conditional branch (`typo in a stacked branch`, `typo without a base`) fails as soon as a sample goes missing.
- **Known gap.** No core test asserts that the prompt crew sends to the harness carries the run's base and stack. The checked render (`internal/crew/decide.go:116`) and the started render (`internal/core/runs.go:330`) agree only because both call `RuleRun.PromptData()`. Keep both on that one function.
