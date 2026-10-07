---
title: A Prepare step is a startup line the acceptance snapshots record, so new Prepare work reports no new step
date: 2026-10-06
category: design-patterns
module: internal/adapter/github, internal/port, acceptance
problem_type: design_pattern
component: tracker_adapter
related_components:
  - testing_framework
severity: medium
applies_when:
  - "Adding a call, query or check to an adapter's Prepare that must not change what users see"
  - "Deciding whether new startup work deserves its own port.Step"
  - "Adding a repository-level read to the github tracker, such as its node id"
tags: [prepare, port-step, startup, acceptance, snapshots, github-tracker, repository-id]
---

# A Prepare step is a startup line the acceptance snapshots record, so new Prepare work reports no new step

## Context

Issue #238 needed the github tracker to read its repository's GraphQL node id at startup (plan: `docs/plans/2026-10-06-2221-refactor-typed-global-identities-plan.md`, KTD6), with nothing users see changing (R20). The first draft of the plan ran the query "as a `Prepare` step after the code owners", with its own step, as the label read has one. Document review caught that this breaks the screen before any code was written.

Each `port.Step(ctx, text)` a preparer reports (`internal/port/step.go:19`) becomes a visible startup line, `HH:MM:SS crew: <text>`. The acceptance screen scenarios record those lines exactly: `acceptance/scenarios/screen/testdata/issue-box.snapshot` opens with "checking the gh login", "finding the code owners" and "reading the repository's labels", and seven snapshots under `acceptance/scenarios/` hold that last line. Only the tester rewrites snapshots (`-accept-snapshots`, AGENTS.md and `acceptance/README.md`), so a developer's new step leaves the acceptance suite red with no fix the developer may make.

## Guidance

Treat the list of startup steps as part of the screen. Work added to `Prepare` that should not change the screen runs inside an existing step's window and reports no step of its own:

```go
port.Step(ctx, "reading the repository's labels")
if err := t.readRepository(ctx); err != nil {
	return fmt.Errorf("tracker github: read the repository: %w", err)
}
// then gh label list, as before
```

(`internal/adapter/github/tracker.go:456`.) The error text names the new work, so a failure still says which read failed, while the step line stays the one users already see. The adapter's tests pin the step list unchanged (`prepareSteps` in `internal/adapter/github/prepare_test.go`).

A new step is right only when the screen is meant to change. Then the tester accepts the new snapshots in the same change.

The same read shows which repository value each GitHub API wants. The node id (`repository(owner:, name:) { id nameWithOwner }` in `internal/adapter/github/repository.go:13`) is crew's identity for the repository, because it survives a rename. `owner/name` stays the display name and what the REST calls use (`gh api repos/{owner}/{repo}/...`). The closing pull request filter compares `nameWithOwner` on both sides (`internal/adapter/github/pullrequest.go:155`), so it must never receive the node id.

## Why This Matters

A step added "for symmetry" looks harmless and passes every unit test, then fails seven screen scenarios only in the acceptance job. The developer cannot rewrite those snapshots, so the change stalls until the tester runs. The same trap waits in every later part of #237 that adds startup work.

## When to Apply

- Any new gh, git, harness or workspace call in a `Prepare`.
- Any new engine work between the ports' `Prepare` and the first poll. The engine's own "reading the run journal" step is a startup line too, so the repository read in `internal/engine/engine.go` reports none.

## Examples

Rejected (changes every screen snapshot):

```go
port.Step(ctx, "reading the repository")
if err := t.readRepository(ctx); err != nil { ... }
port.Step(ctx, "reading the repository's labels")
```

Kept (screen unchanged, failure still named): the block under Guidance.
