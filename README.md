# LTS — Led's Tree Script

A modern terminal UI for managing git worktrees. Built with Go, Bubble Tea, and Lip Gloss.

![LTS v2.0.0](https://img.shields.io/badge/version-2.0.0-green)

```
██╗     ████████╗███████╗
██║     ╚══██╔══╝██╔════╝
██║        ██║   ███████╗
██║        ██║   ╚════██║
███████╗   ██║   ███████║
╚══════╝   ╚═╝   ╚══════╝
```

## Features

- **Interactive grid UI** — Repo cards with mouse hover, click, and context menus
- **Single & multi-repo worktrees** — Create worktrees for one repo or across multiple repos (monorepo-like)
- **Full creation pipeline** — Stash, checkout main, pull, create branch, copy `.env` files, install dependencies, generate VS Code workspace
- **Click-to-open** — Click a worktree to open in your IDE, AI CLI, or terminal
- **Per-repo config** — Each repo gets its own basis branch and refresh timestamp
- **Status at a glance** — Branch names colored by status (clean, changed, diverged, merged, new)
- **Context menu actions** — Rebase, rename, delete per worktree; refresh, change basis per repo
- **Settings UI** — Configure IDE, AI CLI, package manager, terminal, auto-refresh from within the TUI
- **Black background** — Full-screen black with no terminal bleed-through
- **Tree loading animation** — A growing worktree animation on startup

## Installation

Requires [Go 1.21+](https://go.dev/dl/) and Git.

```bash
curl -fsSL https://raw.githubusercontent.com/led-slzr/lts/main/install.sh | bash
```

Or build manually:

```bash
git clone https://github.com/led-slzr/lts.git
cd lts
go build -o lts .
mv lts ~/.local/bin/
```

## Usage

```bash
lts                    # Run in current directory
lts --dir ~/projects   # Run in specific directory
lts --version          # Print version
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Tab` | Cycle click usage: IDE → AI CLI → Terminal |
| `r` | Refresh all repos |
| `q` / `Ctrl+C` | Quit |
| `Esc` | Close modal / clear selection |

## Mouse

| Action | Result |
|--------|--------|
| Hover card | Border highlights white |
| Hover worktree | Row highlights, `[▸]` button appears |
| Click `[▸]` | Context menu (Rebase / Rename / Delete) |
| Click worktree | Opens in active click usage mode |
| Click footer buttons | Refresh All, Cleanup Merged, Settings, Exit |

## Config

**Global** (`~/.config/lts/config`) — applies everywhere:

```
IDE_COMMAND="windsurf"
AI_CLI_COMMAND="claude"
PACKAGE_MANAGER="pnpm"
AUTO_REFRESH="24H"
TERMINAL="ghostty"
```

**Local** (`.lts.conf` in your project directory) — per-repo:

```
CORE_BASIS_BRANCH="main"
CORE_LAST_REFRESH="1711612800"
ERP_BASIS_BRANCH="dev"
ERP_LAST_REFRESH="1711612800"
```

Both configs are editable from the Settings UI inside LTS.

## License

MIT
