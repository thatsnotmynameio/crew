---
title: crew mates - Plan
type: feat
date: 2026-10-03
topic: crew-mates
artifact_contract: ce-unified-plan/v1
product_contract_source: ce-brainstorm
execution: code
---

# crew mates - Plan

## Goal Capsule

- **Objective:** the boss can give crew GitHub identities of its own, called mates, each ready to act on the boss's repository, while crew keeps behaving exactly as it does today.
- **Means:** a new subcommand, `crew mates create <name>`, which creates a private GitHub App through GitHub's manifest flow and installs it on the current repository.
- **Product authority:** the boss, through the brainstorm of #60. Making crew act as its mates is #80, not this work.
- **Stop conditions:** stop and report if GitHub's manifest flow cannot hand the result back to a command running on the boss's machine (R3), or if a mate's saved key cannot mint an installation token for the repository (R7).
- **Open blockers:** none.

---

## Product Contract

### Summary

`crew mates create <name>` becomes crew's first subcommand. It creates a mate, a private GitHub App owned by the account that owns the current repository, through GitHub's manifest flow in the browser, and then has the boss install it on that repository. crew keeps the mate's key on the boss's machine and confirms the mate can act on the repository. Nothing in crew acts as a mate yet.

### Problem Frame

crew reaches GitHub through `gh`, logged in as the boss (`docs/guide/crew.mdx:56`). So every label, comment, pull request and commit crew or its sessions make appears as the boss. The boss cannot tell their own actions from crew's. GitHub does not notify you in its web inbox about your own activity, so crew's failure reports, which exist to notify the boss, reach them only if they opted in to email for their own updates. And GitHub does not let a pull request's author approve it, so the boss cannot approve a pull request crew opened.

A GitHub App gives crew an identity of its own (`<slug>[bot]`). GitHub's rules shape which kind:

- A public app installed by other people would need its private key, which grants access to every installation, on each boss's machine. GitHub forbids shipping the key in a client: *"If your app is a native client, client-side app, or runs on a user device (as opposed to running on your servers), you must never ship your private key with your app."* A public crew app therefore needs a server, which crew does not have.
- A private app can be installed only on the account that owns it. Each boss creates their own, and the key never leaves the machine of the person who owns the app.

The brainstorm checked that crew can later use such an identity inside a session that runs longer than an installation token's one hour. See Sources.

### Key Decisions

- **A mate is a private GitHub App, owned by the account that owns the repository.** (session-settled: user-directed — chosen over one official public crew app: GitHub forbids shipping its key to other people's machines, so a public app needs a server holding the key.) Governs R1, R2.
- **A boss can have many mates.** Later, each part of crew can act as a different mate, and a mate is only an identity: no model, prompt or settings of its own. (session-settled: user-directed — chosen over a single app for all of crew: the boss wants one identity per role, such as a developer and a tester.) Governs R1, R9.
- **The name is "mate", and the command is `crew mates create <name>`.** (session-settled: user-directed — chosen over `crew hire <name>`, `crew members create <name>` and `crew hands add <name>`.) Governs R1.
- **A crew command creates the app, rather than manual steps in the docs.** (session-settled: user-directed — chosen over documenting how to create the app by hand in GitHub's settings: less work for the boss.) Governs R2, R3.
- **The command creates the mate and installs it on the current repository.** (session-settled: user-approved — chosen over only creating the app and leaving installation to the boss: the mate is ready for when crew starts using it.) Governs R6, R7.
- **This work only creates identities; crew does not act as them.** Today's behaviour must not change. (session-settled: user-directed — chosen over wiring mates into sessions, checks and crew's own GitHub writes now: that is #80, after its own feasibility work.) Governs R13.
- **The suggested app name is `crew-<name>`, and GitHub's page lets the boss change it.** App names are unique across GitHub, at most 34 characters, and lose characters other than letters, digits and hyphens. `crew`, `crew-dev` and `crewdev` are already taken by GitHub accounts. A `crew_` or `crew:` prefix would not survive. Governs R4, R5.
- **Every mate asks for the same permissions, and none for pushing code.** Under #80, commits stay authored and pushed by the boss. Governs R2.

### Requirements

**Creating a mate**

- R1. `crew mates create <name>` creates a mate called `<name>` for the GitHub repository of the git repository it runs in, owned by that repository's owner, whether an organization or a user.
- R2. The mate is a private GitHub App with no webhook. It requests the permissions crew and its sessions will need on GitHub once they act as mates, without write access to code.
- R3. crew opens the boss's browser on GitHub's page for creating the app, with everything filled in, and the boss confirms the creation there. crew also prints the URL it opens.
- R4. The page suggests the name `crew-<name>`. crew records the name and bot login GitHub actually created, including when the boss changed the name.
- R5. crew rejects a `<name>` that cannot form a valid app name before opening the browser, saying why.

**Installing and confirming**

- R6. Once the app exists, crew opens the page to install it, and the boss installs it on the current repository. crew prints that URL too.
- R7. crew reports success only once the mate's saved key has minted an installation token for the current repository. Its last message names the mate and its bot login, such as `crew-tester[bot]`.
- R8. When the app was created but the installation did not cover the repository, crew keeps the mate, exits with a failure, and prints the URL to install it.

**Keeping mates**

- R9. crew saves each mate's identity and private key on the boss's machine, outside any repository, readable only by the boss's own OS user, keyed by owner account and mate name.
- R10. crew never prints, logs or writes into a repository a mate's private key.
- R11. `crew mates create` refuses a name the same owner already has a mate for on this machine, before opening the browser, and changes nothing.
- R12. When the boss cancels on GitHub's page, or the creation does not finish, crew saves nothing and exits with a failure.

**Today's behaviour**

- R13. Running `crew`, `crew --plain` and `crew --version` behaves exactly as before: crew, its sessions and its checks keep acting on GitHub as the boss's `gh` login, whether or not mates exist.

**Docs**

- R14. The guide documents mates:
  - what a mate is, and what creating one needs (rights to create and install apps for the owner account);
  - `crew mates create`, and where the key lives;
  - that crew does not act as its mates yet.

### Key Flows

- F1. Creating a mate
  - **Trigger:** the boss runs `crew mates create tester` in a clone of `thatsnotmynameio/crew`.
  - **Steps:**
    - crew checks the name (R5, R11).
    - crew opens GitHub's create-app page for the `thatsnotmynameio` organization with `crew-tester` suggested (R3, R4).
    - The boss confirms, and crew saves the mate (R9).
    - crew opens the install page, and the boss installs the app on the repository (R6).
    - crew mints a token with the saved key and reports `crew-tester[bot]` (R7).
  - **Covers R1–R7, R9.**

### Acceptance Examples

- AE1. **Covers R1.** Given a repository owned by the organization `thatsnotmynameio`, when the boss creates a mate, the app belongs to `thatsnotmynameio`. Given a repository on the boss's personal account, the app belongs to that account.
- AE2. **Covers R4.** Given GitHub already has an account named `crew-dev`, when the boss runs `crew mates create dev` and renames the app to `thatsnotmyname-crew-dev` on GitHub's page, crew records the mate `dev` with the bot login `thatsnotmyname-crew-dev[bot]`.
- AE3. **Covers R8.** Given the boss created the app but closed the install page, crew exits with a failure, keeps the mate, and prints the install URL.
- AE4. **Covers R11.** Given the boss already has a mate `tester` for `thatsnotmynameio` on this machine, `crew mates create tester` in a `thatsnotmynameio` repository exits with a failure before opening the browser. In a repository of the boss's personal account, it proceeds.
- AE5. **Covers R12.** Given the boss closes GitHub's create-app page without creating the app, crew saves nothing and exits with a failure.
- AE6. **Covers R13.** Given the boss has a mate installed on the repository, running `crew` labels, comments and starts sessions as the boss's `gh` login, as before.

### Scope Boundaries

- **Acting as mates is #80.** That covers sessions, checks and crew's own GitHub writes acting as a mate; declaring in `.crew/config.yaml` which mate acts where; refreshing tokens during a session; and adding the mate as co-author of commits.
- **No other mate commands.** Listing, removing or renaming mates, installing an existing mate on another repository, and copying a mate to another machine.
- **No permissions per mate.**
- **No official public crew app,** no server holding a key, and no hosted crew.

<!-- ce-section: work-relationships -->
### How This Work Fits Together

This plan covers creating mates. The breakdown below is the current understanding, not a committed roadmap.

- #80, crew acting as its mates. Depends on this work.
  - Its rule, from this brainstorm: once a mate exists, everything crew does on GitHub goes through a mate, and the boss's own login is used only when there are none.
  - Its split: on GitHub the mate acts (comments, labels, issues, pull requests); in git the boss stays the author, with the mate as co-author.
  - Still to decide there: how crew finds "the boss's issues" once `@me` means a mate, probably from `CODEOWNERS`.
- An official public crew app with a server that holds its key, so people can install crew the way they install Codacy or Claude. Not planned; mates do not block it.

### Dependencies / Assumptions

- The boss can create and install GitHub Apps for the repository's owner account. On an organization, that means an owner or an app manager.
- GitHub's manifest flow can return its result to a command running on the boss's machine. Probot's local setup does this through a `localhost` redirect, but GitHub's docs do not state that `localhost` is accepted.

### Outstanding Questions

**Deferred to Planning**

- The exact permission set (R2): at least issues and pull requests, read and write, and metadata. Check what the organization's `Priority` issue field and issue dependencies need.
- How crew learns that the boss finished creating the app and installing it: the manifest's redirect and setup URLs, or polling the repository's installation.
- Where and in what form crew keeps a mate on disk (R9).
- Whether `crew mates create` needs `.crew/config.yaml`, or only a git repository with a GitHub remote.
- crew's exit code for each failure, within the existing 0, 1 and 2.

### Sources / Research

- GitHub Apps:
  - [Best practices](https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/best-practices-for-creating-a-github-app): never ship the private key with a client; installation tokens last one hour.
  - [Public or private](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/making-a-github-app-public-or-private): a private app installs only on its owner's account.
  - [Registering a GitHub App](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app): up to 100 apps per account; names unique, at most 34 characters.
  - [Manifest flow](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest): the name is editable on GitHub's page; `public`; the code exchanged within one hour returns `id` and `pem`.
  - [Installation tokens](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation): `GET /repos/{owner}/{repo}/installation`; tokens limited to repositories and permissions.
  - [Probot development](https://probot.github.io/docs/development/): the manifest flow run locally, with a `localhost` redirect.
- Feasibility for #80, tested in the brainstorm:
  - `gh` reads its token on every call from `GH_CONFIG_DIR/hosts.yml`, written in its multi-account format next to `config.yml` with `version: "1"`. In a headless `claude -p` session, rewriting the file mid-session changed the token of the session's next `gh` call.
  - `GH_TOKEN` and `GITHUB_TOKEN` take precedence over the file ([gh environment](https://cli.github.com/manual/gh_help_environment)).
- Code today:
  - `cmd/crew/main.go:6` and `cmd/crew/main.go:56-67`: crew has only `--plain` and `--version`, and rejects any argument with exit 2.
  - `internal/proc/proc.go:80`: child processes inherit crew's environment.
  - `internal/adapter/github/tracker.go:271-275`: startup requires `gh auth status`.
