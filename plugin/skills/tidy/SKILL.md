---
name: tidy
description: Tidy an LTS workspace — "clean up worktrees", "what's stale", "free disk space", "remove merged branches". Reports merged, stale and risky worktrees, previews cleanup, and acts only after the user confirms.
---

# Tidy an LTS workspace

1. **Survey.** `lts status --all --fetch --json`. Group worktrees by
   `.worktrees[].status.state`:
   - `merged_cleanable` — safe to remove
   - `dangerous_to_delete: true` — holds unpushed/uncommitted work; never remove
   - old `last_activity` — stale; ask the user
2. **Report** a short table (id, branch, state, last activity) to the user.
3. **Preview** the safe batch: `lts cleanup-merged --dry-run --json` → `.candidates`.
4. **Act only after the user says yes:** `lts cleanup-merged --yes`
   (`--remote` only if they also want the branches deleted on origin).
   Exit 5 = some failed; report `.failed`.
5. **Missing worktrees:** `lts ls --json` → `.repos[].stale` lists worktrees whose
   folder was deleted by hand; `lts prune` forgets them (branches are kept).
6. **Disk space:** `lts clean-modules <target> --dry-run` shows reclaimable
   `node_modules`; removing them is safe (reinstall any time).
7. Anything else (stale but unmerged worktrees) needs a per-item decision from the
   user: `lts delete <id> --dry-run` first, then `--yes`. Never `--force` unasked.
