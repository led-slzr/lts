# AI agents

LTS is designed to be driven by agents that have never seen it, and to share a workspace with you while they work. Three layers:

1. **The CLI contract.** JSON output, stable exit codes, dry-runs and no prompts ([Command line](cli.md)). `lts help agents` teaches all of it in one page.
2. **Skills** that turn common workflows into safe, repeatable procedures.
3. **Hooks** that tell every session and subagent where it is.

## Claude Code plugin

```
/plugin marketplace add led-slzr/lts
/plugin install lts@lts
```

Requires the `lts` CLI (v3.2+) on your `PATH`. The plugin updates with the repo.

| Skill | For |
|---|---|
| `lts:context` | loaded automatically in LTS workspaces: layout, contract, exit codes, safety rules |
| `lts:start` | start a task in a new worktree, single repo or mono group |
| `lts:finish` | push, open or print the PR, hand back with `lts open`, delete after merge |
| `lts:tidy` | find merged, stale and missing worktrees; dry-run before every cleanup |
| `lts:sync` | refresh every repo and rebase worktrees (groups included), reporting conflicts |

The skills defer to `lts help agents` and `--help` for exact flags, so they stay correct as the CLI evolves.

## Hooks

| Hook | What it does |
|---|---|
| `SessionStart`, `SubagentStart` | add a short summary to every session and subagent: which worktree it's in (or that it's in a main checkout or a non-LTS worktree), its status, the other worktrees, and the safety rules. Silent outside LTS workspaces; a no-op if `lts` isn't installed. |
| `WorktreeCreate` *(opt-in)* | Claude Code's isolated worktrees become LTS worktrees (`.env` files, dependencies, setup script, LTS layout). Steps aside, letting Claude Code create its own, when the repo's main checkout is dirty or off its basis branch. |
| `WorktreeRemove` *(opt-in)* | removes those worktrees when Claude Code is done, but keeps any that hold unsaved work and says why |

## Without the marketplace

```bash
lts skill install                  # skills → ~/.claude/skills/lts-*, hooks → ~/.claude/settings.json
lts skill install --worktree-hook  # also WorktreeCreate / WorktreeRemove
lts skill install --no-hooks       # skills only
lts skill print > AGENTS.md        # Markdown for other agents (Codex, OpenCode, …)
lts skill uninstall
```

`install` is idempotent, preserves your existing settings and hooks, and backs up `settings.json` once to `settings.json.lts-backup`. `$CLAUDE_CONFIG_DIR` is respected.

## Safety rules the skills follow

- Run `lts delete` and `lts cleanup-merged` with `--dry-run` first and show the result.
- Never pass `--force` (destroys uncommitted or unpushed work), `--remote` (deletes branches on origin) or `hibernate` unless the user asked for exactly that.
- Exit code `4` means a person must decide: ask them, don't add flags to get past it.
- `lts create` stashes and switches the repo's main checkout to its basis branch. If that checkout holds work in progress, say so first (`--dry-run` lists it).

## Orchestrating parallel workers

- **One worktree per worker:** `lts create <task-branch> -r <repo> --reuse`. `--reuse` makes retries idempotent.
- **Work across repos:** one mono group per task (`-r api,web`).
- **Concurrency:** workers on different repos run concurrently, while work on the same repo is serialized by per-repo locks; pass `--wait=5m` rather than failing on `busy`.
- **React, don't poll:** `lts watch --json` streams `worktree_added`, `worktree_removed`, `busy`, `idle` and `missing` events (`state_changed` with `--status`).
- **Fleet view:** `lts status --all --json`.
- **Hand results to the human:** `lts open <branch> --ide`.
- **After merge:** `lts cleanup-merged --yes`.

## Human and agent in one workspace

The TUI and the CLI take the same per-repo locks. While an agent changes `api`, the TUI refuses actions on `api` and names the holder; while you rebase `api` in the TUI, the agent's command gets `busy`. Locks are released automatically if a process dies, and `lts watch` reports both sides as they happen.

## Sandboxes and CI

Point `LTS_CONFIG_DIR` at a temporary directory so tests never touch your real settings, and `LTS_DIR` at the workspace. Without a TTY, LTS never prompts, and git credential prompts are disabled too.
