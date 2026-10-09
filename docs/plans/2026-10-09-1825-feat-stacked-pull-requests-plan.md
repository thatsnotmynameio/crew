---
title: Stacked pull requests for blocked issues - Plan
type: feat
date: 2026-10-09
topic: stacked-pull-requests
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
origin: GitHub issue #425
execution: code
---

# Stacked pull requests for blocked issues - Plan

## Goal Capsule

- **Objective:** an issue that another issue blocks starts as soon as its blocker's pull request is open and crew's work on that blocker has ended, instead of waiting for that pull request to merge, so a chain of dependent issues is developed while the pull requests below it are still in review.
- **Means:** a rule that opts in takes such an issue and gives it a workspace made from the branch of the pull request it stacks on; the session opens its pull request on that branch and links it into a GitHub stack, and GitHub's stacked pull requests keep the chain rebased as it merges.
- **Product authority:** the boss, through the brainstorm of #425. The Product Contract wins on behaviour.
- **Open blockers:** none.

---

## Product Contract

### Summary

A rule can opt in to stacking. It then takes an issue whose open blockers all have an open pull request and no run left in crew, and crew makes the issue's workspace from the branch of the pull request at the top of that chain, telling the session which base it is on. Sub-issues of one parent keep stacking on each other; an independent issue that would compete for the same top waits for the merge, as today. Opening the pull request on that base and linking it into a GitHub stack is the prompt's work, and GitHub rebases the rest of the stack when a pull request in it merges.

### Problem Frame

crew never takes an issue that an open issue blocks (`internal/core/scheduler.go:200`), and a blocker closes only when its pull request merges. Each issue in a chain therefore waits for the review and merge of the one before it. A split plan is the common case: refinement turns a large plan into sub-issues linked by `blocked_by`, and the parts run one at a time, each idle until the boss reviews and merges the previous one. The feature's lead time becomes the sum of every review in the chain, and the developer queue sits empty while a ready part waits.

A session could already rebase its own branch and open a pull request on another base: it has the permissions and the bot's credentials. The scheduler never lets it run, so no prompt can stack, and a hand-made stack would leave the boss to rebase each pull request after the one below it merges.

GitHub now ships stacked pull requests natively, with the `gh stack` extension (`github/gh-stack`). A pull request linked into a stack targets the branch of the one below it. When a pull request in the stack merges, squash merges included, GitHub retargets the next one to the trunk and rebases the rest on the server with `git rebase --onto`. This repository merges by squash only and deletes merged branches.

### Key Decisions

- **crew picks the base when it takes the issue, from the issue's dependencies.** Governs R3, R4, R5. (session-settled: user-approved — chosen over the issue's original design, where an action writes the base into `.crew-branch-base.local` and crew rebases the worktree between actions: the dependencies already hold the same information, and deciding once at the take avoids a conflict mid-run and a force push.)
- **GitHub's stacked pull requests do the restack after a merge; crew does nothing when a pull request in a stack merges.** Governs R14. (session-settled: user-directed — chosen over crew noticing the merge and moving the issue to a restack rule, a GitHub workflow that rebases the stack, or a person restacking by hand: the boss pointed to `gh stack`, and GitHub rebases the stack on the server.)
- **Sub-issues of one parent stack on each other; independent issues do not.** Governs R6, R7. (session-settled: user-directed — chosen over stacking every competing issue on top of the one before it, which ties issues that do not depend on each other into one merge, and over a plain pull request on the blocker's branch outside a stack, which brings back the manual restack: parts of one split are meant to land together, independent issues are not.)
- **Among independent issues competing for one top, the first in the scheduler's order stacks and the others wait for the merge.** Governs R7. (session-settled: user-directed — chosen over the alternatives above: the waiting issue is no worse off than today.)
- **crew reads the parent link from GitHub, whoever created it.** Governs R6. (session-settled: user-directed — chosen over tying the rule to the split skill that creates the parts: crew does not need to know who created a sub-issue.)
- **Opening the pull request on the base and linking it into a stack is the prompt's work.** Governs R11, R12. (session-settled: user-approved — chosen over crew calling `gh stack` itself: opening pull requests is already the prompts' job in crew, and a repository can stack by base alone without the extension.)
- **Stacking is a rule's opt-in, off by default.** Governs R1. (session-settled: user-approved — chosen over stacking in every rule: it changes when an issue starts, so a repository turns it on per rule.)
- **An issue stacks only on a pull request whose run has ended, never on a branch still being worked.** Governs R2, R8. A session goes on pushing CI and review fixes after it opens its pull request, so an issue stacked earlier would start from a base about to move and break the stack's linear history. (session-settled: user-approved — chosen over stacking as soon as the blocker's pull request opens: the `lfg` session keeps pushing to its branch after opening the pull request.)

### Requirements

**Taking a blocked issue**

- R1. A rule takes blocked issues only when its config turns stacking on; a rule without it waits for every blocker to close, exactly as today.
- R2. A rule with stacking on takes a blocked issue only when each of its open blockers has exactly one open pull request of its own, in the same repository, crew holds no run of that blocker, and those pull requests lie on one chain, each based on the branch of the one below it. crew finds an issue's pull request without GitHub's closing link, which a stacked pull request does not get.
- R3. The issue stacks where a walk up the chain ends. The walk starts at the highest pull request of the chain R2 establishes; R6 moves it up, and R7 and R8 stop it with a wait.
- R4. A blocked issue that R2 rejects waits in its label as a blocked issue does today, and crew checks it again at each listing.
- R5. An issue with no open blocker is taken as today, with its workspace made from the default branch.

**Siblings and competitors**

- R6. When the one open pull request based on the pull request the walk has reached is the pull request of an issue with the same GitHub parent issue as the issue being taken, the walk moves up to it.
- R7. When the open pull request based on the pull request the walk has reached belongs to an issue that is not a sibling, or two or more open pull requests are based on it, the issue being taken waits, as R4 says.
- R8. An issue that crew holds stacked on a pull request holds the top of that chain until its run ends with its own pull request open: a sibling of it waits until then and stacks on that pull request, and any other issue waits as R7 says.
- R9. When several waiting issues could stack on the same top, they are taken in the scheduler's order: priority, then the rule's place in the file, then age.

**The workspace and the session**

- R10. A stacked issue's workspace is made from the latest branch of the pull request it stacks on, on a new branch of its own named as today.
- R11. Every session and shell action of a run can read the run's base branch and, when stacked, the issue and pull request it stacks on, both in its prompt and in its environment; an unstacked run's base is the default branch.
- R12. crew opens no pull request, sets no pull request's base, and links nothing into a GitHub stack.
- R13. A resumed stacked run reopens its workspace as the earlier run left it; when that workspace is gone, the new run chooses its base again by R2 to R9, from the default branch when the issue is no longer blocked.
- R14. crew does nothing to a stacked issue, its branch or its pull request when a pull request below it merges or closes.

**Closing and reporting**

- R15. A stacked issue closes when its pull request lands in the default branch, whether alone after GitHub retargets it or in a merge of the stack from higher up.
- R16. crew's existing reports on an issue's pull requests reach a stacked issue's pull request, found as R2 finds it: the state label crew mirrors from the issue, and the label removal and stop comment of a `close`.

**Showing it**

- R17. The issue's card and its box in the live view say which issue a held stacked issue stacks on.
- R18. Events, and the `--plain` lines, say when crew takes an issue stacked, naming the issue and the pull request it stacks on.

**This repository's rules**

- R19. `development` turns stacking on; `fix` keeps waiting for merges.
- R20. The `development` prompt tells the session to open its pull request against the run's base and, when stacked, to link it into a GitHub stack on top of the pull request it stacks on with `gh stack link`.
- R21. The `development` prompt tells the session what to do when the base branch is gone because the pull request below merged before it opened its own: rebase its own commits onto `main`, leaving out the merged blocker's, and open its pull request against `main` outside any stack.
- R22. The machine that runs crew's sessions for this repository has the `gh stack` extension installed.

### Key Flows

- F1. A split's parts stack as each pull request opens
  - **Trigger:** refinement splits #P into sub-issues #A, #B and #C, where #B is blocked by #A and #C by #B, and moves them to `crew:development:ready`.
  - **Steps:** `development` takes #A from `main`, as today. Its session opens #A's pull request and the run ends in `crew:development:waiting review`. At the next listing, #B's only blocker has an open pull request and no run, so `development` takes #B with a workspace made from #A's branch, and #B's session opens its pull request on #A's branch and links it into a stack. #C follows the same way once #B's run ends with its pull request open.
  - **Outcome:** the stack is `main ← #A ← #B ← #C`, all in review at once. When the boss merges #A's pull request, GitHub retargets #B's to `main` and rebases #C's on the server.
  - **Covered by:** R2, R3, R10, R11, R14, R15, R20.
- F2. Siblings with one external blocker
  - **Trigger:** #B and #C, sub-issues of #P, are both blocked only by #A, which is not a sub-issue of #P, and #A's pull request is open.
  - **Steps:** `development` takes #B first by R9 and stacks it on #A. #C waits while crew holds #B's run (R8). Once #B's run ends with its pull request open, #C stacks on #B.
  - **Outcome:** the stack is `main ← #A ← #B ← #C`.
  - **Covered by:** R6, R8, R9.

### Acceptance Examples

- AE1. **Covers R1.** Given a rule without stacking on and an issue blocked by #A whose pull request is open, the rule does not take the issue.
- AE2. **Covers R2, R4.** Given an issue blocked by #A and #B, where #A's pull request is open and #B has none, the issue waits. Once #B's run ends with its pull request open on #A's branch, the issue stacks on #B's pull request.
- AE3. **Covers R2, R4.** Given an issue blocked by #A and #B whose open pull requests are both based on `main`, the issue waits: there is no single chain.
- AE4. **Covers R7, R8, R9.** Given #X and #Y, not sub-issues of one parent, both blocked only by #A with its pull request open, the first by the scheduler's order stacks on #A. The other waits, both before and after the first opens its pull request, until #A closes, and is then taken from `main`.
- AE5. **Covers R6, R8.** Given siblings #B and #C both blocked only by #A, and crew holding #B's run stacked on #A, #C waits, even after #B's pull request opens; when #B's run ends with its pull request open, #C stacks on it.
- AE6. **Covers R2.** Given an issue blocked by #A, where #A has two open pull requests of its own, the issue waits.
- AE7. **Covers R13.** Given a stacked run of #B that failed, whose workspace still exists, and #A's pull request merged since, the resumed run reopens #B's workspace unchanged; given the same run with its workspace gone, the new run starts from `main`.
- AE8. **Covers R17, R18.** Given crew takes #B stacked on #A's pull request, Events shows a line naming #B, #A and that pull request, and #B's card and box say it stacks on #A.

### Success Criteria

- In this repository, a split into a chain of parts has every part's pull request open in one GitHub stack without any part waiting for a merge: each part starts at the first listing after the run of the part below it ends with its pull request open.
- After the boss merges the bottom pull request of such a stack, the next one targets `main` and stays mergeable with no rebase by a person or a session.

### Scope Boundaries

- The issue's file-based design is dropped: no `.crew-branch-base.local`, no `.crew-stack-pr-from.local`, no `.crew/running-data/`, and no rebase between actions.
- No restack by crew after a merge, and no rebase of a stacked issue when the pull request below gets new commits in review: GitHub's Rebase Stack button and `gh stack rebase` cover it.
- A blocker's pull request closed without merging leaves the issue stacked on it unmergeable; a person unstacks it.
- Fan-in (blockers on separate chains), a fork between independent issues, and pull requests from forks are not stacked: those issues wait for the merge.
- The session's sync before a push in a resumed run whose branch GitHub rebased on the server is the prompt's work.
- Auto-merge and rule bypass, which GitHub does not offer for stacked pull requests yet.

### Dependencies / Assumptions

- GitHub's stacked pull requests: a stack links pull requests of one repository, retargets and rebases the remaining ones when one merges, and requires review and checks on every pull request below the one merged, since merging a stacked pull request merges the ones below it too.
- GitHub links a pull request to the issues its closing keywords name, and closes them when it merges, only while the pull request targets the default branch. A stacked pull request above the bottom of its stack gets no closing link, so crew cannot find it through the issue's closing references, and its issue may stay open when its code lands (R2, R15, R16).
- GitHub's issue dependencies (`blocked_by`), which refinement already records, and GitHub's sub-issue parent link, which splits set.
- This repository's settings: squash merge only, merged branches deleted, auto-merge off.

### Outstanding Questions

**Deferred to Planning**

- How crew finds an issue's pull request without GitHub's closing link, such as by the branch of crew's latest run of that issue, and the chain above it, through pull requests' base branches or GitHub's stack API.
- Whether GitHub closes a stacked issue by itself, by experiment on this repository: when its pull request is retargeted to the default branch and merged alone, and when it lands in a merge of the stack from higher up. Where GitHub does not, planning decides how R15 is met.
- The names and shapes of the rule's stacking key, the prompt fields and the environment variables of R11.
- Whether a held stacked issue keeps the `blocked` chip in its box beside the stacked-on text of R17.
- Whether `tools/crew_config_test.sh` needs a case for the new `development` prompt.

### Sources / Research

- `internal/core/scheduler.go:200`: a rule takes only unblocked items.
- `internal/adapter/github/tracker.go:74` and `:524`: the tracker reads only an issue's count of open blockers, as one boolean.
- `internal/adapter/git/workspace.go:87-97`: every workspace is made from origin's default branch; `internal/port/port.go:169`: `Create` takes no base.
- `internal/adapter/git/workspace.go:103-113`: `Reopen` changes nothing in a reopened workspace.
- `internal/crew/prompt.go:21-26`: a prompt's fields today are `.Issue.Ref`, `.Issue.Key`, `.Issue.Title` and `.Issue.URL`.
- `internal/adapter/github/pullrequest.go:27`: crew finds an issue's pull requests, for its label mirror and stop comments, through GitHub's `closedByPullRequestsReferences`.
- GitHub's "Linking a pull request to an issue" docs: closing keywords count only on a pull request that targets the default branch.
- `internal/ui/tui/card.go:124` and `internal/ui/tui/detail.go:74-90`: where the card and the box show `blocked`.
- README.md: "Opening pull requests, reviewing and merging are your prompts' job and yours."
- `github/gh-stack`: the README and `docs/src/content/docs/faq.md` on `gh stack link`, merging a stack, squash merges and the server-side cascading rebase.
