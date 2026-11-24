package core

import (
	"sort"

	"lts-revamp/internal/git"
)

// RepoActivity returns a repo's newest worktree activity (for sorting).
func RepoActivity(r git.Repo) int64 {
	var newest int64
	for _, wt := range r.Worktrees {
		if wt.LastActivity > newest {
			newest = wt.LastActivity
		}
	}
	return newest
}

// RepoCreated returns a repo's newest worktree creation time (for sorting).
func RepoCreated(r git.Repo) int64 {
	var newest int64
	for _, wt := range r.Worktrees {
		if wt.CreatedAt > newest {
			newest = wt.CreatedAt
		}
	}
	return newest
}

// SortRepos orders repos (Board cards / Explorer sidebar) and each repo's
// worktrees by the configured sort: newest activity first (default), newest
// created first, or alphabetical. Name breaks ties so ordering is stable;
// unknown timestamps sink to the bottom.
func SortRepos(repos []git.Repo, order string) {
	repoKey := RepoActivity
	wtKey := func(wt git.Worktree) int64 { return wt.LastActivity }
	switch order {
	case "name":
		repoKey = nil
	case "created":
		repoKey = RepoCreated
		wtKey = func(wt git.Worktree) int64 { return wt.CreatedAt }
	}

	sort.SliceStable(repos, func(i, j int) bool {
		if repoKey != nil {
			ki, kj := repoKey(repos[i]), repoKey(repos[j])
			if ki != kj {
				return ki > kj
			}
		}
		return repos[i].Name < repos[j].Name
	})
	for ri := range repos {
		wts := repos[ri].Worktrees
		sort.SliceStable(wts, func(i, j int) bool {
			if repoKey != nil {
				ki, kj := wtKey(wts[i]), wtKey(wts[j])
				if ki != kj {
					return ki > kj
				}
			}
			return wts[i].Branch < wts[j].Branch
		})
	}
}
