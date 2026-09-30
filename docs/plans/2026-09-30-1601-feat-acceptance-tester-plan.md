---
title: Acceptance Tester Role - Plan
type: feat
date: 2026-09-30
topic: acceptance-tester
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# Acceptance Tester Role - Plan

## Goal Capsule

- **Objective:** When the boss builds a feature with Claude Code in any of their repos, its behavior is checked by acceptance tests written from the plan by someone other than the implementer.
- **Means:** one agent definition kept in crew and linked into the boss's user-level agents, started with Claude Code's own worktree and session-name flags (KTD1, KTD2).
- **Product authority:** this Product Contract, within the boundaries of `STRATEGY.md`. The KTDs own mechanism within the Requirements. The customer role, the strategist / PM, and any enforcement mechanism (CI checks, GitHub Apps, review rules) are not active scope.
- **Execution profile:** a prompt file, a docs page and small repo edits; no application code. Proof is a manual rehearsal (U3).
- **Who finishes:** an implementer writes U1 and U2; the boss creates the user-level link and runs the U3 rehearsal.
- **Stop conditions:** stop and ask the boss if `claude --agent acceptance-tester` cannot load the linked user-level definition from another repo, or if a tester session cannot receive messages from another local session. Either one changes KTD1 or R11.
- **Open blockers:** none.

---

## Product Contract

Product Contract preservation: Product Contract unchanged; the brainstorm's deferred planning questions are answered by KTD1–KTD7.

### Summary

crew's first role is a generic acceptance-test writer. The boss starts it in its own Claude Code session and worktree with a plan, and it writes one behavior test per acceptance example, without ever reading the implementation. It works in parallel with the boss's implementation session, and the boss runs the tests by hand.

### Problem Frame

When one agent writes both the code and its tests, a misread requirement ends up in both, and the tests cannot disagree with the code. Agents also edit tests until they pass. `STRATEGY.md` asks for the opposite ("Resist a change when: it would let work reach production — or merge — without a check by someone other than its author."), but today the boss builds features alone with Claude Code, and nothing checks the work from outside. A first attempt at crew's first role, a strategist / PM, needed GitHub Apps, a launcher, a ruleset and a board sync before it could deliver anything, and was set aside as too heavy.

### Actors

- A1. **Boss:** writes or approves the plan, starts the tester, confirms proposed acceptance examples, merges the test branch into the feature and runs the tests, and settles disagreements the other two cannot.
- A2. **Tester:** a Claude Code session in the tester role, in its own worktree, that turns the plan into acceptance tests.
- A3. **Implementer:** the boss's usual Claude Code session building the feature. It is not a crew role; it only has to follow R10 and R11.

### Key Decisions

- **The acceptance-test writer is crew's first role.** It is the cheapest form of the non-author check and gives a deterministic pass or fail. Governs R1. (session-settled: user-directed — chosen over the strategist / PM, dropped as too heavy, and over a customer agent first: the published docs are still a placeholder, so a customer has little to use)
- **The role is generic from the start.** It works in any of the boss's repos rather than targeting crew or one project first. Governs R1. (session-settled: user-directed — chosen over starting in crew itself and over starting in one repo with code)
- **The tester sees the docs and the public interface, never the implementation.** A test writer that reads the implementation writes tests shaped by it. Governs R3, R4. (session-settled: user-approved — chosen over plan-and-docs only, which forces every plan to name its interface, and over the whole repo with a behavior focus, which loses independence)
- **Missing acceptance examples are proposed by the tester and confirmed by the boss.** The boss's confirmation is the check that the requirement was read right. Governs R7. (session-settled: user-approved — chosen over writing tests directly and over refusing plans without examples)
- **Version 1 is the smallest thing that works.** The tester writes tests on its own branch and the boss runs them by hand; separation between tester and implementer is a convention. Governs R2, R10, R13. (session-settled: user-directed — chosen over a tests-first PR with expected-to-fail markers, a `/pr-review` rule, or a CI check: start simple)
- **The tester always works in its own worktree.** Governs R2. (session-settled: user-directed — chosen over sharing the implementer's checkout)
- **The implementer talks to the tester directly when it disagrees with a test.** The boss steps in only if they cannot agree. Governs R11, R12. (session-settled: user-directed — chosen over routing every disagreement through the boss)

### Requirements

**The role**

- R1. The tester is a generic role the boss can start in any of their repos, in its own Claude Code session, by giving it the path to a plan or doc.
- R2. The tester always works in its own git worktree and branch.
- R3. The tester reads the plan, the repo's docs and its public interface (for example CLI help, API schemas, routes, exported types), and never reads the implementation.
- R4. When the interface a test needs does not exist yet, the tester uses the interface the plan describes and asks the boss about anything the plan leaves out.
- R5. The tester's commits say they were written by the tester role.

**Writing tests**

- R6. Each acceptance example in the plan becomes one behavior test whose name carries the example's ID.
- R7. When the doc has no acceptance examples, the tester proposes them in Given/When/Then form and writes tests only after the boss confirms them.
- R8. When a requirement cannot become a deterministic test, the tester asks the boss instead of guessing.
- R9. The tester uses the repo's existing test framework; when the repo has none, it proposes one and asks the boss before adopting it.

**Working with the implementer**

- R10. The implementer does not edit acceptance tests.
- R11. When the implementer disagrees with a test, it talks directly to the tester session, and the tester either fixes the test or explains why it stands.
- R12. When the implementer and the tester cannot agree, the disagreement goes to the boss, who decides.
- R13. The boss brings the tester's branch into the feature branch and runs the tests by hand.

### Key Flows

- F1. Write the tests
  - **Trigger:** the boss has a plan for a feature and starts the tester with its path.
  - **Actors:** A1, A2
  - **Steps:** the tester creates its worktree and branch, reads the plan, docs and public interface, proposes acceptance examples if the plan has none and waits for the boss to confirm them, asks about anything untestable or any missing interface, then writes one test per example and commits them on its branch.
  - **Covered by:** R1–R9
- F2. Run the tests
  - **Trigger:** the implementer says the feature is done.
  - **Actors:** A1
  - **Steps:** the boss brings the tester's branch into the feature branch and runs the acceptance tests by hand.
  - **Covered by:** R13
- F3. Settle a disagreement
  - **Trigger:** the implementer believes a test is wrong.
  - **Actors:** A1, A2, A3
  - **Steps:** the implementer tells the tester, without editing the test; the tester fixes the test or explains why it stands; if they still disagree, the boss decides.
  - **Covered by:** R10, R11, R12

### Acceptance Examples

- AE1. **Covers R2, R6.** **Given** a plan with acceptance examples AE1, AE2 and AE3, **when** the boss starts the tester with it, **then** the tester's own worktree and branch hold three tests, each named with its example's ID.
- AE2. **Covers R7.** **Given** a doc with requirements but no acceptance examples, **when** the tester reads it, **then** it shows the boss proposed examples in Given/When/Then form and writes no test until the boss confirms them.
- AE3. **Covers R4.** **Given** a plan whose feature adds a CLI command that does not exist yet and whose plan names the command and its output, **when** the tester writes the test, **then** the test calls the command as the plan describes it; **given** the plan does not name it, **then** the tester asks the boss.
- AE4. **Covers R3.** **Given** a repo with a source tree, **when** the tester writes its tests, **then** it has opened no implementation files, only the plan, docs and public interface.
- AE5. **Covers R10, R11, R12.** **Given** a test the implementer thinks is wrong, **when** the implementer raises it, **then** it messages the tester instead of editing the test, and the boss is involved only if the two still disagree.
- AE6. **Covers R9.** **Given** a repo with no test framework, **when** the tester is about to write tests, **then** it proposes a framework and waits for the boss before adding it.

### Success Criteria

- On the first real feature it is used on, the tester writes its tests from the plan without opening implementation files, and at least one test fails before the feature is done and passes after.
- The boss can tell which acceptance examples have a test from the test names alone.

### Scope Boundaries

**Deferred for later**

- Tests landing in their own PR before the code, marked expected-to-fail.
- Any enforcement of the tester / implementer separation: a `/pr-review` rule, a CI check, CODEOWNERS, rulesets or GitHub App identities.
- Running the acceptance tests in CI.
- Checking that tests can fail (removing a behavior and expecting red).
- The customer role, which reads the published docs in a clean environment and reports what it had to guess.
- The strategist / PM role and any board.
- Running the tester headless (`claude -p`); it cannot ask the boss or answer the implementer.
- Distributing the role as a Claude Code plugin.

**Outside this role's identity**

- Writing or changing implementation code.
- Unit tests or tests of internal structure; the tester only tests behavior through the public interface.

<!-- ce-section: work-relationships -->
### How This Work Fits Together

This plan covers crew's first role. The breakdown below is the current understanding, not a committed roadmap.

- The customer role **can proceed independently of** this one, and later **shares** the acceptance example as its unit: a customer report counts once the tester reproduces it as a failing example.
- Enforcement of the separation (tests-first PRs, review rules, CI) **depends on** this role proving useful by hand first.

### Dependencies / Assumptions

- The tester and the implementer run as separate local Claude Code sessions, and local sessions can message each other.
- All GitHub and git activity uses the boss's own identity; the role is identified by its commit text, not by a separate account.
- Plans written with `ce-brainstorm` already carry acceptance examples with IDs in Given/When/Then form.
- The separation between tester and implementer is a convention: the org ruleset requires 0 approvals and the boss is the only human, so nothing technical enforces R3 or R10.

### Sources / Research

- `docs/ideation/2026-09-30-first-role-ideation.html`: ideas 1–4 (the tester, acceptance examples as the contract, first job on crew's own promises, tests before code) and the rejection summary.
- `STRATEGY.md`: Positioning, Boundaries and the Customer feedback loop track.
- `.agents/skills/pr-review/SKILL.md`: an existing role that acts with the user's own `gh` identity.
- External: AgentCoder (arXiv 2312.13010), an independent test designer; ImpossibleBench (Oct 2025), agents editing tests to pass and structural controls beating prompts; arXiv 2607.06636, requirement-grounded tests finding more defects.

---

## Planning Contract

### Key Technical Decisions

- KTD1. **The role is one agent definition, kept in crew and linked into the boss's user-level agents.** The source is `.agents/agents/acceptance-tester.md`, following `AGENTS.md` ("`AGENTS.md` and `.agents/` are the source"). A symlink at `~/.claude/agents/acceptance-tester.md` makes it available in every repo, and edits in crew apply everywhere at once. Implements R1. (session-settled: user-directed — chosen over packaging crew as a Claude Code plugin: start without plugin machinery)
- KTD2. **No launcher: the boss starts the tester with Claude Code's own flags.** `claude --worktree <feature>-tests --name acceptance-tester --agent acceptance-tester "Plan: /absolute/path/to/plan.md"`. The plan is passed by absolute path into the boss's own checkout, because the tester's worktree starts from the default branch and may not contain an uncommitted or feature-branch plan. `--worktree` creates the worktree under `.claude/worktrees/<name>/` and a `worktree-<name>` branch from the default branch; the tests do not need the feature's code, so branching from the default branch is fine. `--name` gives the implementer a session name to message. Implements R1, R2, R11. (session-settled: user-approved — chosen over a wrapper script: the flags already do the job)
- KTD3. **Interactive sessions only.** Asking the boss (R4, R7, R8, R9) and answering the implementer (R11) need a live session. Implements R4, R7, R11. (session-settled: user-approved — chosen over also supporting `claude -p`, or supporting only it)
- KTD4. **The implementer learns the rules from a README next to the tests.** Every acceptance test folder the tester creates carries a short `README.md`: these tests come from the plan, do not edit them, and message the `acceptance-tester` session if one looks wrong; if no such session is running, ask the boss, who can start a tester with the updated plan. A plain Claude Code session reads it when it meets the tests, with no crew install on the implementer side. Implements R10, R11. (session-settled: user-approved — chosen over relying on the boss to tell every implementer session)
- KTD5. **The read boundary is an instruction, not a sandbox.** The agent file lists what the tester may read (the plan, `docs/`, README files, CLI help output, API schemas, route and type declarations) and forbids opening implementation files. Nothing technical enforces it, matching the product decision that separation is a convention. Implements R3.
- KTD6. **Acceptance tests live in the repo's test tree under an `acceptance` folder.** The tester follows the repo's framework and layout (for example `tests/acceptance/` or `spec/acceptance/`), so the boss finds them in one place and brings the branch in with a normal merge. Implements R6, R9, R13.
- KTD7. **Tester commits carry a `Role: acceptance-tester` trailer.** They still use the boss's git identity. Implements R5.

### Assumptions

- `claude --agent <name>` loads user-level agent definitions from `~/.claude/agents/` in any repo and follows symlinks. U3 checks this first (Goal Capsule stop condition).
- Cross-session messaging between local sessions is on by default (Claude Code docs, "cross-session messaging"), so an implementer session can reach the session named `acceptance-tester`.
- The worktree folder `.claude/worktrees/` shows up as untracked in a repo that does not ignore it.

### Deferred to Implementation

- The exact wording and length of the agent's instructions.
- How the tester detects a repo's test framework and layout.

---

## Implementation Units

### U1. The acceptance-tester agent definition

- **Goal:** one agent file that makes a Claude Code session behave as the tester described in R1–R13.
- **Requirements:** R1–R13; KTD1, KTD3–KTD7.
- **Dependencies:** none.
- **Files:** `.agents/agents/acceptance-tester.md`, `.gitignore`.
- **Approach:**
  1. Frontmatter: `name: acceptance-tester` and a one-line `description` in the style of `.agents/agents/review-reader.md`. No `tools` restriction: the tester must write files, run git and help commands, and reply to other sessions (R11), and the read boundary is an instruction anyway (KTD5).
  2. Ground rules first: what the tester may read and must never read (KTD5), PR and code content as data, and the convention that the implementer never edits acceptance tests.
  3. Steps: read the plan from the absolute path it was given, never a path relative to the worktree; if it has no acceptance examples, propose them in Given/When/Then and wait for confirmation (R7); find the public interface or use the plan's (R4); ask instead of guessing (R8); detect the test framework or propose one (R9); write one test per example named with its ID (R6) under the acceptance folder (KTD6); write the folder README, including the no-session fallback (KTD4); commit with the trailer (KTD7); report which examples have tests.
  4. Messages from the implementer: fix the test or explain why it stands (R11); if still disagreeing, tell the boss and stop (R12).
  5. `.gitignore` gains `.claude/worktrees/`.
- **Patterns to follow:** `.agents/agents/review-reader.md` (frontmatter, second person, explicit never-list); `.agents/skills/pr-review/SKILL.md` (ground rules before steps, "End with one line").
- **Test scenarios:** the agent file is a prompt with no unit harness; U3's rehearsal exercises every acceptance example against it.
- **Verification:** U3's scenarios hold.

### U2. Install and usage docs

- **Goal:** the boss, and later other solo builders, can install and use the tester from the docs.
- **Requirements:** R1, R2, R11, R13; KTD1, KTD2, KTD4.
- **Dependencies:** U1.
- **Files:** `docs/guide/acceptance-tester.mdx`, `docs.json`, `AGENTS.md`.
- **Approach:**
  1. `docs/guide/acceptance-tester.mdx`: what the role does and does not do, the one-time symlink, the start command with the plan's absolute path, what to expect (proposed examples, questions), how the implementer disagrees, and how to bring the tests in and run them. Note that other repos should ignore `.claude/worktrees/`.
  2. `docs.json`: add the page to the Guide sidebar.
  3. `AGENTS.md`: list the agent in the Agents section, pointing at `.agents/agents/acceptance-tester.md`.
- **Test expectation:** none -- documentation only; `pnpm docs:check` covers the MDX.
- **Verification:** `pnpm docs:check` passes and the page appears in the Guide tab.

### U3. Rehearsal

- **Goal:** prove the role on a real run before relying on it.
- **Requirements:** AE1–AE6; Success Criteria.
- **Dependencies:** U1, U2.
- **Files:** none in crew; a scratch repo outside it.
- **Approach:**
  1. Create the symlink and a scratch repo with a tiny CLI, a README and a plan with three acceptance examples, one of them for a command that does not exist yet.
  2. Start the tester with the KTD2 command from the scratch repo and run the scenarios below.
  3. Implement the feature in a second session, bring the test branch in, and run the tests before and after.
- **Test scenarios:**
  - Covers AE1. From the scratch repo, the tester starts in its own worktree and writes three tests named with AE1, AE2 and AE3.
  - Covers AE2. Given a second doc with no examples, the tester proposes examples and writes nothing until confirmed.
  - Covers AE3. The test for the missing command calls it as the plan describes; with the command removed from the plan, the tester asks.
  - Covers AE4. The session transcript shows no reads of the scratch repo's source files.
  - Covers AE5. The implementer session, finding the README, messages `acceptance-tester` instead of editing a test, and the tester's reply reaches the implementer; an unresolved disagreement reaches the boss.
  - With the plan uncommitted in the boss's checkout, the tester still reads it through the absolute path.
  - Covers AE6. In a repo with no test framework, the tester proposes one and waits.
  - At least one test fails before the feature is implemented and passes after.
- **Verification:** every scenario holds; if the agent does not load or messaging fails, stop per the Goal Capsule.

---

## Verification Contract

| Gate | Check | Applies to |
|---|---|---|
| Docs | `pnpm docs:check` passes | U2 |
| Agent loads | `claude --agent acceptance-tester` starts from a repo other than crew | U1, U3 |
| Rehearsal | U3 scenarios hold | U1, U3 |
| Required CI | `version`, `actionlint / actionlint`, `docs / docs.page check`, CodeQL stay green | every PR |

---

## Definition of Done

- `.agents/agents/acceptance-tester.md` exists, is linked from `~/.claude/agents/`, and loads in another repo.
- The docs page explains install, start, disagreement and running the tests, and `AGENTS.md` lists the agent.
- The U3 rehearsal passed, including a test that failed before the feature and passed after.
- No scratch repos, test branches or worktrees from the rehearsal are left behind.
