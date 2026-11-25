#!/usr/bin/env bash
# Builds a throwaway LTS demo workspace (fictional repos, local bare
# origins) with worktrees in every status, for README screenshots.
# Usage: scripts/screenshots/demo-workspace.sh [dir]   (default /tmp/lts-demo)
set -euo pipefail
WS=${1:-/tmp/lts-demo}
ORIGINS="$WS/.origins"
export GIT_AUTHOR_NAME="Demo" GIT_AUTHOR_EMAIL="demo@example.com"
export GIT_COMMITTER_NAME="Demo" GIT_COMMITTER_EMAIL="demo@example.com"
rm -rf "$WS"; mkdir -p "$ORIGINS"

repo() { # name
  git init -q --bare "$ORIGINS/$1.git"
  git clone -q "$ORIGINS/$1.git" "$WS/$1" 2>/dev/null
  ( cd "$WS/$1" && git checkout -q -b main && echo "# $1" > README.md &&
    printf 'node_modules\n.env\n' > .gitignore && echo "PORT=3000" > .env &&
    git add README.md .gitignore && git commit -qm "chore: init $1" && git push -q -u origin main )
}
commit() { # dir msg
  ( cd "$1" && echo "$2" >> CHANGES.md && git add CHANGES.md && git commit -qm "$2" )
}
wt() { # repo branch → path
  local d="$WS/$1-lts/$(echo "$2" | tr '/' '-')"
  mkdir -p "$WS/$1-lts"; git -C "$WS/$1" worktree add -q -b "$2" "$d" main; echo "$d"
}
other_clone() { # repo → scratch clone used to push "someone else's" commits
  local d; d=$(mktemp -d); git clone -q "$ORIGINS/$1.git" "$d/c" 2>/dev/null; echo "$d/c"
}

for r in api web mobile shared-ui infra docs; do repo "$r"; done

# api
d=$(wt api feat/oauth-login); commit "$d" "feat: oauth scaffolding"; ( cd "$d" && git push -q -u origin HEAD ); echo "wip" > "$d/auth.ts"; echo "wip" > "$d/session.ts"
d=$(wt api fix/rate-limiter);  commit "$d" "fix: token bucket"; ( cd "$d" && git push -q -u origin HEAD ); commit "$d" "fix: burst handling"
d=$(wt api chore/upgrade-deps); commit "$d" "chore: bump deps"; ( cd "$d" && git push -q -u origin HEAD )
( cd "$WS/api" && git merge -q --no-ff chore/upgrade-deps -m "Merge chore/upgrade-deps" && git push -q )
# web
d=$(wt web feat/dark-mode); commit "$d" "feat: theme tokens"; ( cd "$d" && git push -q -u origin HEAD )
d=$(wt web feat/checkout-v2)
d=$(wt web fix/navbar-overflow); commit "$d" "fix: clamp width"; ( cd "$d" && git push -q -u origin HEAD )
c=$(other_clone web); ( cd "$c" && git checkout -q fix/navbar-overflow && commit . "fix: mobile breakpoint" && git push -q )
git -C "$d" fetch -q
# mobile
d=$(wt mobile fix/crash-on-launch); commit "$d" "fix: null guard"; ( cd "$d" && git push -q -u origin HEAD )
c=$(other_clone mobile); ( cd "$c" && git checkout -q fix/crash-on-launch && commit . "fix: from CI" && git push -q )
commit "$d" "fix: local retry"; git -C "$d" fetch -q
d=$(wt mobile feat/offline-sync); commit "$d" "feat: sync queue"
# a mono group: one branch across api + web
g="$WS/api+web-lts/feat-billing-portal"; mkdir -p "$g"
printf 'monorepo\n' > "$WS/api+web-lts/.lts-type"; printf 'api\nweb\n' > "$WS/api+web-lts/.lts-repos"
git -C "$WS/api" worktree add -q -b feat/billing-portal "$g/api-feat-billing-portal" main
git -C "$WS/web" worktree add -q -b feat/billing-portal "$g/web-feat-billing-portal" main
commit "$g/web-feat-billing-portal" "feat: billing page"
printf 'API_BASIS_BRANCH="main"\nWEB_BASIS_BRANCH="main"\nMOBILE_BASIS_BRANCH="main"\n' > "$WS/.lts.conf"
echo "$WS"
