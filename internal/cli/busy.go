package cli

import "lts-revamp/internal/git"

// busyFn reports whether another process holds a repo's lock (see lock.go).
// Until cross-process locking lands this always reports false.
func (a *app) busyFn(env *Env) func(git.Repo) bool {
	return func(git.Repo) bool { return false }
}
