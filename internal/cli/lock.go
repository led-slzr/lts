package cli

// lockRepos acquires the cross-process locks for repos (see step 7).
func (a *app) lockRepos(env *Env, repos []string) (func(), error) {
	return func() {}, nil
}
