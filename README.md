# template

The starting point of thatsnotmynameio's repositories: GitHub setup, releases, docs, and instructions for coding agents. It is language-agnostic: add your stack.

> Starting a repository from this template? Replace this README with the project's own.

## Start a repository

1. Create it from the template:

   ```sh
   gh repo create thatsnotmynameio/<name> --template thatsnotmynameio/template --public --clone
   ```

2. Apply the GitHub settings a template doesn't copy (merge, security, the `checks` ruleset):

   ```sh
   gh repo clone thatsnotmynameio/.github /tmp/thatsnotmynameio-github -- --depth 1
   /tmp/thatsnotmynameio-github/scripts/bootstrap.sh thatsnotmynameio/<name>
   ```

   (Cloned outside the repository: a clone named `.github` inside it would collide with its own `.github/`.)

3. Fill in:
   - `README.md`;
   - `AGENTS.md` (commands, architecture, tests);
   - `docs.json` and `docs/`.

4. Add the project's build and test jobs to `.github/workflows/ci.yml`, and their check names to the ruleset:

   ```sh
   /tmp/thatsnotmynameio-github/scripts/bootstrap.sh thatsnotmynameio/<name> --checks "version,actionlint / actionlint,docs / docs.page check,<your jobs>"
   ```

5. For SonarQube Cloud:
   - create the project there;
   - set `sonar.projectKey` in `sonar-project.properties`;
   - run the bootstrap with `--sonar`.

## What's inside

| Path | What it does |
| --- | --- |
| `.github/workflows/ci.yml` | Pull requests: `version` (the release rule on `VERSION`) and `actionlint`. Add the project's jobs. |
| `.github/workflows/release.yml` | Pushes to `main`: publishes `VERSION` as `vX.Y.Z` and a GitHub release when it is new. |
| `.github/workflows/docs.yml` | Pull requests: docs.page's check of `docs.json` and `docs/`. |
| `.github/workflows/sonar.yml` | SonarQube Cloud, when the variable `SONAR_ENABLED` is `true`. |
| `.github/workflows/claude.yml` | `@claude` in issues, pull requests and reviews. |
| `.github/dependabot.yml` | Weekly updates of the pinned actions, the shared workflows and the docs CLI. |
| `AGENTS.md` (`CLAUDE.md`) | Instructions for coding agents. |
| `.agents/agents/acceptance-tester.md` | The acceptance tester: writes behavior tests from a plan's acceptance examples, without reading the implementation. See the Guide. |
| `docs.json`, `docs/` | The docs.page site (Guide and Develop tabs). |
| `VERSION` | The version; a pull request that bumps it is a release. Starts at `0.1.0`. |

The workflows call [thatsnotmynameio/.github](https://github.com/thatsnotmynameio/.github), pinned by SHA: shared behaviour changes there, once.

## Secrets

`CLAUDE_CODE_OAUTH_TOKEN` (for `@claude`) and `SONAR_TOKEN` (for Sonar) are organization secrets. A new repository needs access to them: an organization owner grants it in the organization's settings.

## Symlinks on Windows

`CLAUDE.md`, `.claude/skills` and `.claude/agents` are symlinks. On Windows, clone with `git clone -c core.symlinks=true` (with Developer Mode on, or as an administrator), or they check out as small text files holding the path.
