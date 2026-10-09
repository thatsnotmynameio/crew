---
title: The release plans describe one publish job that GoReleaser publishes from, and the Release workflow is draft-first in three jobs now
date: 2026-10-09
category: workflow-issues
module: .github/workflows/release.yml, .goreleaser.yaml, tools/release, docs/plans
problem_type: workflow_issue
component: tooling
severity: medium
applies_when:
  - "Changing .github/workflows/release.yml, .goreleaser.yaml's release section or tools/release check"
  - "Picking up #318 (a protected environment on releases) or #320 (wait for the acceptance suite, a v* tag ruleset)"
  - "Reading docs/plans/2026-10-03-0209-feat-release-binaries-plan.md KTD2 or docs/plans/2026-10-08-1217-feat-manual-release-workflow-plan.md KTD1 and KTD3"
  - "Changing the README's gh attestation verify command or its flags"
tags: [release, goreleaser, draft-release, artifact-attestations, github-actions, stale-plan, oidc, release-gate]
---

# The release plans describe one publish job that GoReleaser publishes from, and the Release workflow is draft-first in three jobs now

## Context

#382 made every release binary carry a signed build-provenance attestation. To do that without ever publishing an unattested release, it changed the workflow's shape. The two plans that built the Release workflow still describe the old shape, and plans are not updated after they ship:

- `docs/plans/2026-10-03-0209-feat-release-binaries-plan.md`, KTD2 (line 136): GoReleaser's release "starts as a draft while assets upload and is published last", by GoReleaser itself. Its U-steps keep workflow permissions at `contents: read` with `contents: write` on the one job (line 231).
- `docs/plans/2026-10-08-1217-feat-manual-release-workflow-plan.md`, KTD1 (line 96): "One `publish` job", and "#320 splits the job when it adds the wait". KTD3 (line 98): a draft does not count as released because "a failed upload leaves one".

What the workflow does now (#382's plan, `docs/plans/2026-10-09-1039-issue-382-plan.md`, KTD1 to KTD5):

- Workflow permissions are `{}` (`.github/workflows/release.yml:29`). Each job names its own.
- `build` (`release.yml:37`), with `contents: write`, runs `tools/release check`, tags locally and runs GoReleaser. GoReleaser leaves a draft because `.goreleaser.yaml:38` sets `release.draft: true`. The job then writes a subjects file, `checksums.txt` plus one `sha256sum` line per `dist/<build>/crew` (GoReleaser's git-ignored output folder), and uploads it as the `subjects` artifact.
- `attest` (`release.yml:106`) is the only job with `id-token: write`. It has no checkout and runs `actions/attest` on the subjects file.
- `publish` (`release.yml:126`), with no checkout, re-checks the draft and the version, matches each downloaded archive against the subjects, runs `gh attestation verify` on each archive and on the `crew` extracted from it, and only then runs `gh release edit "$TAG" --draft=false --latest` (`release.yml:189`).

So the draft is no longer only the leftover of a failed upload: every run makes one, and only `publish` turns it into a release.

## Guidance

- **Read the 2026-10-03 and 2026-10-08 plans' publishing steps as history.** GoReleaser does not publish the release. `release.draft: true` stays, and the release becomes public only in `publish`. Do not remove `draft: true` to "let GoReleaser finish the job": the release would go public before it is attested, and a failed `attest` would leave a published release with no provenance that no new run can repair, because `tools/release check` refuses a version with a published release.
- **`tools/release check` passing over a draft is what the workflow runs on, not only a re-run convenience.** Its package comment now says so (`tools/release/main.go:8-10`), and `TestCheckPassesOverADraft` guards it. A change that makes `check` refuse a draft, such as #320's question of refusing a draft the workflow did not create, has to let through the draft this workflow's own `build` made, or a re-run of a failed `attest` or `publish` can never finish.
- **The job is already split. #320 adds its wait to the three jobs, it does not split one.** The acceptance-suite wait has to finish before `publish`. It can go in `build` before GoReleaser or in a job between `attest` and `publish`, as long as `publish` `needs` it.
- **#318's protected environment goes on `publish`.** It is the job that makes the release public, and it kept the name `publish` for that reason (plan KTD4). On `build`, the approval would come before the bytes exist. On `attest`, it would not stop a re-run of `publish`.
- **#320's `v*` tag ruleset has to let `publish`'s token create the tag.** The tag stays local in `build` (`release.yml`, "Tag the release locally"). GitHub creates `vX.Y.Z` when `publish` runs `gh release edit --draft=false` with the job's `GITHUB_TOKEN`. That token, not GoReleaser's in `build`, is the identity the ruleset's bypass has to cover.
- **Keep `id-token: write` on `attest` alone, and keep `attest` free of a checkout.** Otherwise GoReleaser, `go build` and every module they download could mint Sigstore certificates under `release.yml`'s identity. `TestOnlyAttestCanMintAnOIDCToken` in `tools/releaseworkflow` fails if another job gets the scope.
- **Keep `publish`'s version re-check, even though `build` already checks the version.** "Re-run failed jobs" on an old run skips `build` and CI's `version` check. Without the re-check, a stale `publish` could mark an old version Latest after a newer release shipped.
- **The verify flags are `--source-ref refs/heads/main`, never the tag.** The attestation records the ref the run was dispatched on, `main`. The tag does not exist on GitHub yet when `attest` runs. `--source-ref refs/tags/vX.Y.Z` fails. The README's two commands (`README.md:64-65`) and `release.yml:166` use the same flags, with the repository written out in the README. Change them together.

## Why This Matters

The older plans read as the workflow's design, and their decisions are plausible. An agent picking up #318 or #320 from them would split a job that is already split, gate a job that does not publish, or give the tag ruleset's bypass to the wrong identity. The last mistake only shows on the first release run under the ruleset, because no pull request can run the Release workflow (`tools/release check` refuses every ref but `main`). An agent simplifying the workflow from the 2026-10-03 plan would make GoReleaser publish again, and nothing in a pull request's CI would catch that the release now goes public before it is attested.

These facts were settled during #382's implementation and are not in the code (session history):

- `actions/attest` at the commit `release.yml` pins (v4.2.2, a SHA of the `actions/attest` repository, not of crew) accepts subject names that contain `/`, such as `crew_linux_amd64_v1/crew`: its checksum parser (`src/subject.ts` in the `actions/attest` repository) takes everything after the first space, minus a leading `*`, and rejects only names with a newline. A flat naming scheme is not needed. `gh` matches subjects by digest, never by name.
- `gh release view`, `download` and `edit` find a draft by its pending tag (`shared.FetchRelease` looks up the published release and the draft in parallel), so `publish` needs no release id.
- The subjects script was run against the `dist/` of a real `goreleaser release --snapshot --clean` at v2.18.2, and each listed binary's digest equalled the `crew` inside its archive. GoReleaser's folder suffixes (`_v1`, `_v8.0`) vary, which is why the script reads the paths from `artifacts.json`.

## When to Apply

- Before editing `release.yml`, `.goreleaser.yaml`'s `release:` section or `tools/release`.
- When brainstorming or planning #318, #320, #321 or #317's release dry run.
- When a plan or issue body says the Release workflow has one job, or that GoReleaser publishes the release.

## Examples

Two known gaps at the time of writing:

- **No release has run the new workflow yet.** The proof of R16 is the boss's next release after #382 merges: `attest` and `publish` pass, and the README's `gh attestation verify` passes on a downloaded archive and on an installed `crew`. Releases up to v0.1.1 have no attestation.
- **Some of `publish`'s refusals have no test.** `tools/releaseworkflow` covers a release that is not a draft, a draft below a published release, an archive whose digest differs from its subject, a `crew` whose attestation fails and a download with no archive. It does not cover a partial download (want `Verified 6 files; want 8`), an archive absent from the subjects, a tag already published, a failing attestation on the archive itself, an archive without `crew`, or a subjects file that lists no archive. #382's review raised this as one P2 finding (one reviewer, confidence 75) and the run left it unapplied. A change that weakens the count guard to `[ "$verified" -eq 0 ]` still passes the suite. Add those cases before changing the guards in `publish`.

Related: `docs/solutions/workflow-issues/plans-assume-a-private-repository-that-is-now-public.md`, the same kind of stale plan, which also unparks #318.
