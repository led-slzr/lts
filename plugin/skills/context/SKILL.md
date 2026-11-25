---
name: context
description: How to work in a folder managed by LTS (Led's Tree Script), the git worktree manager. Use whenever the task involves branches, worktrees, starting/finishing work on a repo, multiple repos at once, or a folder containing `*-lts` directories or a `.lts.conf` — and whenever an `lts` command is mentioned. Covers the CLI contract, exit codes and safety rules.
---

# Working with LTS

LTS manages git worktrees for every repo in a **workspace** folder (the folder
that contains the repos). Branch work happens in an isolated worktree, never in a
repo's main checkout.

```
<ws>/<repo>/                    main checkout — do NOT do branch work here
<ws>/<repo>-lts/<branch-dir>/   a worktree for one repo
<ws>/<a>-<b>-lts/<branch-dir>/  a "mono group": one branch across repos a and b
```

Every worktree has a stable **id** like `core-lts/feat-login`; any command accepts
a branch, folder, id or path as its target (or the worktree you are inside).

## First move

Run `lts help agents` once per session if you haven't — it is the authoritative,
version-matched contract. Flags change between versions; `lts <cmd> --help` is
always current. Then orient with:

```bash
lts ls --json                 # repos and worktrees (fast)
lts status --all --json       # state, ahead/behind, dangerous_to_delete
```

## Contract (summary)

- Results on **stdout**, progress on stderr. Add `--json` and parse stdout only.
- `create`, `path`, `rename` print exactly one path: `cd "$(lts path feat/x)"`.
- Nothing prompts without a terminal; a missing confirmation exits **4**.
- Exit codes: 0 ok · 1 error/busy · 2 usage · 3 not found (see `.error.suggestions`)
  · 4 guard refused · 5 partial · 6 rebase conflict (restored) · 7 exists (`--reuse`).
- Typos are never auto-corrected: read `.error.suggestions`, pick one, re-run.
- "busy" (exit 1) = another lts process holds the repo; retry with `--wait=2m`.

## Safety rules — always

1. Run `lts delete` and `lts cleanup-merged` with `--dry-run` first and show the
   user what would be removed.
2. Never pass `--force` (destroys uncommitted/unpushed work) or `--remote`
   (deletes branches on origin) unless the user explicitly asked for exactly that.
3. Exit code 4 means a human decision is needed. Ask; don't escalate flags.
4. `lts create` stashes and switches the repo's **main checkout** to its basis
   branch. If the main checkout has the user's work in progress, say so first
   (`--dry-run` lists these side effects).
5. Never hibernate repos or edit `.lts.conf` / `~/.config/lts/config` by hand —
   use `lts config set`.

## Related skills

- `start` — begin a task in a new worktree
- `finish` — push, open a PR, clean up after merge
- `tidy` — find and remove stale or merged worktrees
- `sync` — refresh repos and rebase worktrees
