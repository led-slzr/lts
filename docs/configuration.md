# Configuration

Everything here is editable in the TUI (**Settings**, `s`) or from the shell:

```bash
lts config                        # every setting: value, default, source
lts config get PACKAGE_MANAGER
lts config set COPY_MCP_JSON true
lts config set BASIS_BRANCH dev -r web
```

Changes save immediately and reach a running TUI. Saves merge with what other LTS processes wrote, so the TUI never reverts a `config set`.

- [Files](#files)
- [Global settings](#global-settings)
- [Per-repo settings](#per-repo-settings)
- [Workspace layout](#workspace-layout)
- [Environment variables](#environment-variables)

## Files

| File | Holds |
|---|---|
| `~/.config/lts/config` | global settings (`$LTS_CONFIG_DIR` moves the whole folder) |
| `<workspace>/.lts.conf` | per-repo settings, `KEY="value"` lines |
| `~/.config/lts/env-backup/` | `.env` backups made by [hibernate](github.md#hibernate) |
| `<workspace>/.lts-locks/` | per-repo locks shared by the TUI and CLI (released automatically) |

## Global settings

| Key | Default | Values | What it does |
|---|---|---|---|
| `AI_CLI_COMMAND` | `claude` | `claude`, `opencode` or any command | AI CLI started in AI-CLI click mode and workspace tasks (empty disables) |
| `AUTO_CLEAN_MODULES` | `OFF` | `OFF`, `1D`, `3D`, `7D`, `14D`, `30D` | Remove node_modules from worktrees idle longer than this (TUI startup/hourly) |
| `AUTO_KILL_TMUX` | `OFF` | `OFF`, `8H`, `1D`, `3D`, `7D` | Kill unattached LTS tmux sessions idle longer than this |
| `AUTO_REFRESH` | `OFF` | `OFF`, `15M`, `30M`, `1H`, `6H`, `12H`, `24H` | TUI: fetch + fast-forward each repo's basis branch in the background once its last refresh is older than this |
| `AUTO_UPDATE_NEW_RELEASE` | `false` | `true` / `false` | Install new releases automatically in the background (TUI) |
| `CLICK_USAGE` | `ide` | `ide`, `ai`, `terminal` | TUI click mode restored on launch |
| `COPY_ENV_FILES` | `true` | `true` / `false` | Copy .env* files into new worktrees |
| `COPY_MCP_JSON` | `false` | `true` / `false` | Copy .mcp.json files into new worktrees |
| `DAILY_CHECK_FOR_UPDATES` | `true` | `true` / `false` | Check for new releases once a day (TUI) |
| `DONE_SOUND` | `off` | `off`, `glass`, `submarine`, `ping`, `pop`, `hero`, `bell` | Sound played when a worktree finishes creating |
| `ENABLE_HIBERNATE` | `false` | `true` / `false` | Show the destructive Hibernate Repo action in the TUI |
| `GITHUB_INTEGRATION` | `true` | `true` / `false` | gh-powered features: clone browser, PRs, hibernate |
| `IDE_COMMAND` | `windsurf` | `windsurf`, `code`, `cursor`, `zed` or any command | Command that opens a worktree / workspace file in your IDE |
| `LAUNCH_GREETING` | `true` | `true` / `false` | Greeting line in the TUI header |
| `LAYOUT` | `board` | `board`, `explorer` | TUI main view layout |
| `NEW_WT_PACKAGE_INSTALL` | `true` | `true` / `false` | Install dependencies when creating worktrees (default) |
| `OPEN_ENV_IDE` | `true` | `true` / `false` | Workspace file auto-opens .env files in the IDE |
| `PACKAGE_MANAGER` | `auto` | `auto`, `pnpm`, `npm`, `yarn`, `bun` | Default package manager (auto = detect per repo) |
| `SHOW_HIDDEN_REPOS` | `false` | `true` / `false` | Show hidden repos grayed out in the TUI |
| `SIZE_SCANNING` | `true` | `true` / `false` | Background worktree size scan (TUI SIZE column) |
| `SORT_ORDER` | `activity` | `activity`, `created`, `name` | Ordering of repos and worktrees |
| `TERMINAL` | `terminal` | `ghostty`, `iterm`, `terminal`, `wezterm`, `alacritty`, `kitty` or any command | Terminal emulator used for AI CLI / terminal opens |
| `TERMINAL_MULTIPLEXER` | `none` | `none`, `tmux` | tmux = one persistent session per worktree |
| `THEME` | `classic-lts` | a theme key | TUI color theme key |
| `TMUX_AI_PANE_WIDTH` | `50` | `20`–`90` | AI pane width as percent of the tmux window |
| `TMUX_RIGHT_PANES` | `1` | `1`, `2`, `3` | Stacked shell panes in the right tmux column |

**Package manager `auto`** reads each repo's corepack `packageManager` field, then its lockfiles (pnpm › bun › yarn › npm), so mixed workspaces need no setup.

**tmux.** With `TERMINAL_MULTIPLEXER=tmux`, AI CLI and Terminal opens share one session per worktree: the AI CLI on the left, `TMUX_RIGHT_PANES` shell panes on the right, sized by `TMUX_AI_PANE_WIDTH`. Layout changes apply on the next open, without touching running processes.

**Background care** (TUI only). `AUTO_REFRESH` refreshes overdue repos every minute; `lts refresh --due` does the same once, for cron. `AUTO_CLEAN_MODULES` and `AUTO_KILL_TMUX` sweep at startup and hourly. Busy repos are always skipped.

## Per-repo settings

Stored in `.lts.conf` as `<REPO>_KEY` (the repo name upper-cased); set them with `lts config set KEY VALUE -r <repo>` or in **Settings → Worktrees**.

| Key | Default | Values | What it does |
|---|---|---|---|
| `<REPO>_BASIS_BRANCH` | `main` | a branch | Branch new worktrees are created from and rebased onto |
| `<REPO>_HIDDEN` | `false` | `true` / `false` | Hide this repo from the main view and all-repo operations |
| `<REPO>_LTS_ROOT` | — | an absolute directory | Directory holding this repo's `<repo>-lts` folder (empty = next to the repos) |
| `<REPO>_PACKAGE_MANAGER` | — | `default`, `pnpm`, `npm`, `yarn`, `bun` | Per-repo package manager override (default/empty = global) |
| `<REPO>_SETUP_SCRIPT` | — | a shell command | Command run inside every new worktree after install |

```
API_BASIS_BRANCH="main"
API_PACKAGE_MANAGER="pnpm"
API_SETUP_SCRIPT="pnpm --filter @acme/shared build"
WEB_BASIS_BRANCH="dev"
WEB_LTS_ROOT="/Volumes/fast/worktrees"
```

The setup script runs in every new worktree after the install; chain commands with `&&`. A failure is logged but never blocks the worktree.

## Workspace layout

```
~/projects/                          the workspace: run lts here
├── api/                             a repo's main checkout
├── api-lts/
│   ├── feat-login/                  a worktree (branch feat/login)
│   └── feat-login.code-workspace
├── api+web-lts/                     a mono group: api and web
│   └── feat-billing/
│       ├── api-feat-billing/
│       ├── web-feat-billing/
│       └── monorepo-feat-billing.code-workspace
└── .lts.conf
```

- **Worktree folders** are the branch with `/` → `-`, kept unique.
- **Mono group folders** join the sorted repo names with `+`. A `+` or `%` inside a repo name is escaped (`%2B`, `%25`), so every repo set has exactly one folder.
- **`LTS_ROOT`** puts a repo's `-lts` folder elsewhere (for example a faster disk). It's set with `lts create --lts-root DIR` or `config set LTS_ROOT`; groups always stay in the workspace.

**Automatic upgrades.** When LTS finds an older layout (pre-3.2 group names joined with `-`, or old worktree folder names), the TUI upgrades it at startup, as does the next CLI command that changes something. Read-only commands just mention it. Each upgrade is all-or-nothing per group: folders are moved, git is re-linked, and tmux sessions are renamed. Workspace files use relative paths and need no change.

## Environment variables

| Variable | Effect |
|---|---|
| `LTS_DIR` | default workspace for CLI commands |
| `LTS_CONFIG_DIR` | global config folder; use a temp dir for sandboxes and tests |
| `NO_COLOR` | disable colored output |
| `CI` | never prompt (same as `--no-input`) |
| `CLAUDE_CONFIG_DIR` | where `lts skill install` writes for Claude Code |
