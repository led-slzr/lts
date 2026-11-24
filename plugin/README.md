# LTS plugin for Claude Code

Teaches Claude Code (and any subagents or orchestrators it runs) to use
[LTS](https://github.com/led-slzr/lts), the git worktree manager, safely.

## Install

```
/plugin marketplace add led-slzr/lts
/plugin install lts@lts
```

Requires the `lts` CLI v3.2+ on your PATH:

```bash
curl -fsSL https://raw.githubusercontent.com/led-slzr/lts/main/install.sh | bash
```

No marketplace? `lts skill install` writes the same skills to `~/.claude/skills`
and adds the hooks to `~/.claude/settings.json`.

## What you get

| Skill | Use it for |
|---|---|
| `/lts:context` | Loaded automatically in LTS workspaces: layout, CLI contract, exit codes, safety rules |
| `/lts:start` | Start a task in a new worktree (single repo or mono group) |
| `/lts:finish` | Push, open/print the PR, delete the worktree after merge |
| `/lts:tidy` | Find merged/stale worktrees and clean them up — dry-run first |
| `/lts:sync` | Refresh every repo and rebase worktrees, reporting conflicts |

**Hooks:** `SessionStart` and `SubagentStart` run `lts hook session-start` /
`subagent-start`, adding a short summary (which worktree you're in, its status,
other worktrees, safety rules) to every session and subagent. They print nothing
outside LTS workspaces and are a no-op when `lts` isn't installed.

**Optional:** route Claude Code's own isolated worktrees through LTS (env files,
dependency install, setup scripts, LTS layout) with
`lts skill install --worktree-hook` — off by default.

The skills defer to `lts help agents` for exact flags, so they stay correct as
the CLI evolves.
