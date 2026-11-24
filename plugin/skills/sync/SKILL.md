---
name: sync
description: Bring an LTS workspace up to date — "sync", "update everything", "rebase my branches", "pull latest". Refreshes every repo's basis branch and rebases worktrees onto it, reporting conflicts without leaving anything half-done.
---

# Sync an LTS workspace

1. `lts refresh --json` — fetch + fast-forward every repo's basis branch.
   Exit 5 lists failed repos in `.results` (usually network/auth): report them.
2. `lts status --all --json` — pick single-repo worktrees (`.group` false) with
   `.status.behind_basis > 0` (commits on the basis branch they don't have yet;
   `behind` is about their own remote). Mono groups: rebase members manually.
3. For each, `lts rebase <id> --no-install` (drop `--no-install` to reinstall deps):
   - exit 0 → rebased (or already up to date: `.changed` false)
   - exit 6 → conflict; the rebase was aborted and changes restored. Record it and
     continue with the next one.
4. Report: rebased, already current, conflicted (with the hint path), failed.
   Do not resolve conflicts unless the user asks.
