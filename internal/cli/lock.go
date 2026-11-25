package cli

import (
	"errors"
	"os"
	"strings"

	"lts-revamp/internal/git"
	"lts-revamp/internal/lock"
)

// lockRepos takes the cross-process locks for repos (shared with the TUI).
// Without --wait a busy repo fails fast (exit 1, code "busy"); with
// --wait=DUR it retries until DUR elapses.
func (a *app) lockRepos(env *Env, repos []string) (func(), error) {
	owner := "lts " + strings.Join(os.Args[1:], " ")
	h, err := lock.Acquire(env.WorkDir, repos, owner, a.flags.wait, func(be *lock.BusyError) {
		a.out.Notice("waiting for %s (up to %s)...", be.Error(), a.flags.wait)
	})
	if err != nil {
		var be *lock.BusyError
		if errors.As(err, &be) {
			e := newErr(ExitError, CodeBusy, "%s", be.Error())
			if a.flags.wait > 0 {
				return nil, e.withHint("still busy after waiting %s; retry later or raise --wait", a.flags.wait)
			}
			return nil, e.withHint("another lts process (TUI or CLI) is working on it; retry, or pass --wait=2m to wait")
		}
		return nil, err
	}
	return h.Release, nil
}

// busyFn reports whether another process holds a repo's lock.
func (a *app) busyFn(env *Env) func(git.Repo) bool {
	cache := map[string]bool{}
	held := func(name string) bool {
		if v, ok := cache[name]; ok {
			return v
		}
		locked, _ := lock.Holder(env.WorkDir, name)
		cache[name] = locked
		return locked
	}
	return func(r git.Repo) bool {
		if !r.IsMonorepo {
			return held(r.Name)
		}
		for _, n := range r.RepoNames {
			if held(n) {
				return true
			}
		}
		return false
	}
}
