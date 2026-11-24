---
name: finish
description: Finish work in an LTS worktree — "I'm done", "ship it", "open a PR", "wrap this up", "clean up after merge". Checks status, pushes, opens or prints the pull request, and removes the worktree once merged — with dry-runs before anything destructive.
---

# Finish work in an LTS worktree

1. **Check state.** `lts status --json` (inside the worktree) — look at
   `status.state`, `changed_files`, `ahead`, `behind`.
2. **Commit** any remaining changes the user wants kept (ask if unsure what to include).
3. **Stay current (optional).** `lts rebase --refresh`. Exit 6 = conflict: the rebase
   was aborted and the worktree restored — tell the user and stop.
4. **Push.** `git push -u origin "$(git branch --show-current)"`.
5. **PR.** `lts pr --print --json` → `.url` (the existing PR, or a prefilled compare
   page). Share the URL. For a mono group: `lts pr --member <repo>` per repo.
6. **After merge** (only when the user confirms it merged):
   ```bash
   lts delete --dry-run        # shows state + what will be removed
   lts delete --yes            # state must be merged_cleanable / clean
   ```
   If `delete` exits 4 with a risk message, there is unpushed or uncommitted
   work — report it; never add `--force` on your own. Add `--remote` only if the
   user asks to delete the remote branch too.
