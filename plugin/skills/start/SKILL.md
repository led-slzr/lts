---
name: start
description: Start work on a task in a fresh LTS worktree — "start working on X", "new branch for", "fix ticket ...", "spin up a worktree", or any multi-repo change. Picks the repo(s) and branch name, previews the side effects, creates the worktree, and moves into it.
---

# Start a task in an LTS worktree

1. **Orient.** `lts ls --json`. Identify the repo(s) the task touches. If it spans
   several repos (e.g. API + UI), use one mono group: `-r core,erp`.
2. **Name the branch** from the task: `feat/<slug>`, `fix/<slug>`, `chore/<slug>`
   (lowercase, hyphens). Reuse the user's ticket id if they gave one.
3. **Preview.** `lts create <branch> -r <repo> --dry-run`. Lines marked as side
   effects change the repo's main checkout (stash, switch to the basis branch,
   pull). If the main checkout holds uncommitted work, tell the user before going on.
4. **Create** (idempotent, safe to retry):
   ```bash
   WT="$(lts create <branch> -r <repo> --reuse -q)" && cd "$WT"
   ```
   - exit 3 → read `.error.suggestions` (typo in a repo or basis branch)
   - exit 4 → guard (e.g. the branch is checked out in the main checkout): ask the user
   - exit 1 `busy` → re-run with `--wait=2m`
   Useful flags: `--basis dev` (or `--basis core=dev,erp=main`), `--no-install`.
5. **Work only inside `$WT`.** For a mono group `$WT` holds one folder per repo;
   `lts path <branch> --member <repo>` gives a member's folder.
6. Report the worktree id and path to the user.

When orchestrating several workers, give each its own branch/worktree; workers
on the same repo serialize automatically (pass `--wait`).
