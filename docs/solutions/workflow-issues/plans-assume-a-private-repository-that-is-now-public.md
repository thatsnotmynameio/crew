---
title: crew's plans assume a private repository, and the repository is public now
date: 2026-10-08
category: workflow-issues
module: README.md, internal/upgrade, cmd/crew, docs/plans
problem_type: workflow_issue
component: documentation
severity: medium
applies_when:
  - "A plan in docs/plans asks for a step, a README form or a manual check only while thatsnotmynameio/crew is private"
  - "Writing or changing the README's install, Quick start or crew upgrade text"
  - "Verifying crew upgrade's private-repository messages against the real repository"
  - "Picking up #318 or another issue parked until the repository is public"
tags: [repository-visibility, private-repository, public-repository, crew-upgrade, install, readme, stale-plan, gh-release-download]
retire_when: "thatsnotmynameio/crew becomes private again; check with gh repo view thatsnotmynameio/crew --json isPrivate"
---

# crew's plans assume a private repository, and the repository is public now

## Context

The plans for `crew upgrade` and the install were written while `thatsnotmynameio/crew` was private, and they say so as a fact:

- `docs/plans/2026-10-08-2149-issue-355-plan.md` (#355): "The repository is private today and will be public", with R6 and AE9 covering both states.
- The #357 plan (`docs/plans/2026-10-09-0227-issue-357-plan.md`) lists "The repository is still private" under Dependencies / Assumptions. The #356 plan (`docs/plans/2026-10-08-2329-issue-356-plan.md`) assumes it without saying so: its R7 and its 404 table name a missing login on a private repository.
- `docs/plans/2026-10-07-1154-docs-install-local-bin-plan.md` accepted that the README's unauthenticated `curl` snippet "fails while the repository is private".
- `docs/plans/2026-10-08-1217-feat-manual-release-workflow-plan.md` left out a protected-environment approval "while the repository is private on GitHub Team", and moved that gate to #318.

From that assumption, the #357 plan asked for three things. The README was to carry, "for as long as the repository is private", a second form of the Quick start snippet that downloads with `gh release download --repo thatsnotmynameio/crew --pattern <archive> --pattern checksums.txt` instead of `curl`. The README was to say `crew upgrade` "uses the gh login while the repository is private". And its Verification Contract asked for a manual check where a logged-out `gh` makes `crew upgrade` exit 1 with the private-repository login message.

During #357's implementation the repository turned out to be public: `gh repo view thatsnotmynameio/crew --json isPrivate` gives `false`, an anonymous `GET /repos/thatsnotmynameio/crew/releases/latest` answers 200, and so do the Quick start's `curl` downloads of `crew_linux_amd64.tar.gz` and `checksums.txt` from `https://github.com/thatsnotmynameio/crew/releases/latest/download`. The session first wrote the README section with the private-period form, as the plan asked. Checking that form showed the anonymous downloads worked, so the form came out again, along with "no `gh` login" among the failure causes (session history).

Plans are not updated after they ship, so every one of these still describes a private repository. So do the bodies of the open issues that carry the rest of the work: #268, the parent, says the curl snippet "fails while the repository is private, which it is today"; #358 makes the acceptance fake GitHub "private by default, as today"; #359 asks for scenarios in both states (AE9).

## Guidance

- **Check the repository's visibility before you follow a plan's private-repository step.** Run `gh repo view thatsnotmynameio/crew --json isPrivate`, or request `releases/latest` anonymously. If the repository is public, the step is obsolete: skip it and record the deviation in the work report, as #357 did.
- **The README has no private-period text.** `## Upgrading crew` sends crew v0.1.1 and earlier to the Quick start's own command (R15). It says `crew upgrade` needs no login and that a logged-in `gh`'s token is sent only because it raises GitHub's rate limit (`README.md:88-90`). Do not add the `gh release download` form or a "while private" sentence back.
- **Keep the private-repository code path, and test it only through fakes.** `internal/upgrade` still tells a private repository without a login from a missing release, because GitHub answers 404 to both (`internal/upgrade/release.go:288-298`), and its package doc still says it reaches a private repository as well as a public one (`internal/upgrade/doc.go:12-16`). That is #355's decision that the command works in both states, so it stays. The real repository can no longer reach it: with a logged-out `gh`, `crew upgrade` against it now upgrades and exits 0. The fake GitHub in `internal/upgrade/fakegithub_test.go` (its `private` field) covers it, and the acceptance doubles that serve releases (#268, part 5) will too. Do not plan a manual private-repository check against the real repository.
- **Work parked until the repository is public can start.** #318, "Gate releases on a protected environment once the repository is public", is open. Its precondition holds now, and the reason the manual release workflow plan gives for leaving the gate out no longer applies.

## Why This Matters

The plans read as current requirements, and the next agent to implement the remaining parts of #268, or to edit the README, will meet "the repository is still private" in a plan it is told to follow. If it takes that at face value, it puts back a README form that nobody needs and that documents a login requirement crew no longer has. Or it plans a manual check whose expected result, exit 1 with the login message, the real repository cannot produce, and then reports a failure that is not one. The code holds no sign of the change: the private-repository branch is still there by design, so reading the code does not show that the real repository has stopped reaching it.

## When to Apply

- Before following any instruction in `docs/plans/` that depends on the repository being private.
- Before writing install, Quick start or upgrade text in `README.md`.
- Before writing acceptance scenarios or manual checks for `crew upgrade`'s login and 404 messages.
- When triaging issues parked until the repository goes public.

## Examples

The #357 plan's Verification Contract expected, against the real repository, exit 1 with the private-repository login message, which `internal/upgrade/release.go:295-296` writes as:

```text
logged-out gh on PATH, crew upgrade  ->  exit 1, "GitHub found no thatsnotmynameio/crew: the repository is private and no gh login was found (...); log in with gh auth login"
```

What the real repository gives now, as recorded in #357's work report:

```text
logged-out gh on PATH, crew upgrade  ->  crew: upgraded v0.1.0 to v0.1.1 at <path>, exit 0
```

The check that tells the two states apart, before you follow a private-period step:

```sh
gh repo view thatsnotmynameio/crew --json isPrivate      # {"isPrivate":false}
curl -fsS -o /dev/null -w '%{http_code}\n' \
  https://api.github.com/repos/thatsnotmynameio/crew/releases/latest   # 200 without a token
```
