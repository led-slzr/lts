# GitHub integration

LTS talks to GitHub through the [GitHub CLI](https://cli.github.com): install `gh` and run `gh auth login`. Credentials stay in `gh`; LTS never stores any. Without `gh` everything else works, and **Settings → Diagnostics** shows what's missing. Turning **GitHub Integration** off in Settings removes every GitHub feature, and LTS then makes no `gh` calls at all.

## Clone browser

`c`, the **Clone a Repo** tile (Board) or the sidebar entry (Explorer) lists every repo you can access, newest activity first, filtered as you type. Cloning lands in the workspace, ready for worktrees.

## Pull requests

For a pushed, unmerged branch on a GitHub repo, **Create PR** (menu, or `g` in Explorer) opens the PR if one exists, or a compare page prefilled against the repo's basis branch.

From the shell, `lts pr [target]` prints the same URL and opens a browser on a terminal. `--print` only prints it, and `--member <repo>` picks one repo of a mono group.

## Hibernate

Free disk space by deleting a repo's local copy and all its worktrees, after LTS proves GitHub has everything. It ships disabled: enable **Settings → Preferences → Enable Hibernate** (or `lts config set ENABLE_HIBERNATE true`), then use **Hibernate Repo** from the repo menu or `lts hibernate <repo>`.

**The audit** fetches origin and checks that:

- every local branch is pushed or merged;
- there are no stashes;
- the main checkout and every worktree are clean;
- the repo isn't part of a live mono group.

Any failed check names the problem and blocks the hibernate; there is no force. Fix it and re-audit (`r`).

**`.env` files**, the one thing git can't vouch for, are backed up first to `~/.config/lts/env-backup/<repo>/<timestamp>/`, folder structure included. If the backup fails, nothing is deleted. When you re-clone the repo through LTS, it offers to put them back.

**Confirmation:** type `DELETE` in the TUI, or pass `--confirm <repo>` without a terminal. Run `lts hibernate <repo> --dry-run` to see the audit first.

**Manual mode** (`Hibernate Repo (manual)`, or `--manual`) covers repos without `gh`, without a GitHub remote, or without any remote. The same audit runs as warnings, and the `.env` backup still happens. It's only allowed for repos without worktrees, confirmed by typing the repo's name. Responsibility for unpushed work is yours.
