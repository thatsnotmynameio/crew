---
name: multi-role-claude-code-sessions
category: workflow
description: When running multiple roles: use `--worktree` per session.
---

Each crew role runs in its own interactive Claude Code session with a dedicated git worktree to avoid checkout conflicts. Sessions discover and message each other by name.

**Setup:**

1. **Create a named agent definition** in `.agents/agents/<role>.md` with frontmatter fields: `name`, `description`, `tools`, `skills`. (See `.agents/agents/acceptance-tester.md` as an example.)

2. **Start a role session** with a dedicated worktree:
   ```bash
   claude --agent <role> --worktree <role> --name <role>
   ```
   This creates a git worktree at `.claude/worktrees/<role>/` on a new branch, `worktree-<role>`, cut from the default branch, and names the session so peers can address it. Files that exist only on your feature branch or only in your main checkout (an uncommitted plan, for example) are not in that worktree: pass them by absolute path.

3. **Discover and message other roles** from within a session by name (e.g., "message the acceptance-tester session about this disagreement").

**Disagreement routing:** When roles disagree on a requirement or test, they address each other directly by session name. Only the repo owner / user escalates if they cannot reach agreement. See `agent-test-isolation` for the operational pattern.

**Branch coordination:** Each role session works on its own branch. Merge each role's branch into the feature branch and validate together before opening a pull request.