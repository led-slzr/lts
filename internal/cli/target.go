package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lts-revamp/internal/git"
)

// Target is one resolved worktree (or mono-group branch subdir).
type Target struct {
	Repo git.Repo
	WT   git.Worktree
}

// ID is the stable identifier `<lts-dir>/<folder>` accepted as a target.
func (t Target) ID() string { return worktreeID(t.Repo, t.WT) }

func worktreeID(r git.Repo, wt git.Worktree) string {
	return r.LTSDir + "/" + filepath.Base(wt.Path)
}

// allTargets flattens every worktree in the workspace.
func (e *Env) allTargets() []Target {
	var out []Target
	for _, r := range e.Repos {
		for _, wt := range r.Worktrees {
			out = append(out, Target{Repo: r, WT: wt})
		}
	}
	return out
}

// repoFilterMatches reports whether repo r satisfies a -r/--repo filter.
// A single name matches that single repo; a comma list (or the group's
// card name) matches the mono group with exactly those repos.
func repoFilterMatches(r git.Repo, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	if !r.IsMonorepo {
		return len(filter) == 1 && strings.EqualFold(filter[0], r.Name)
	}
	if len(filter) == 1 && strings.EqualFold(filter[0], r.Name) {
		return true
	}
	if len(filter) != len(r.RepoNames) {
		return false
	}
	want := map[string]bool{}
	for _, f := range filter {
		want[strings.ToLower(f)] = true
	}
	for _, n := range r.RepoNames {
		if !want[strings.ToLower(n)] {
			return false
		}
	}
	return true
}

// splitRepoFlag accepts repeated and comma-separated -r values.
func splitRepoFlag(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func looksLikePath(arg string) bool {
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "./") || strings.HasPrefix(arg, "../") ||
		strings.HasPrefix(arg, "~") || arg == "." || arg == ".." {
		return true
	}
	return false
}

// pathWithin reports whether p is dir or inside it.
func pathWithin(p, dir string) bool {
	p, dir = filepath.Clean(p), filepath.Clean(dir)
	if rp, err := filepath.EvalSymlinks(p); err == nil {
		p = rp
	}
	if rd, err := filepath.EvalSymlinks(dir); err == nil {
		dir = rd
	}
	return p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))
}

// resolveTarget finds exactly one worktree for arg (branch | folder | path
// | id), scoped by the -r filter. Empty arg means "the worktree containing
// the current directory".
func (e *Env) resolveTarget(arg string, repoFilter []string) (Target, error) {
	var pool []Target
	for _, t := range e.allTargets() {
		if repoFilterMatches(t.Repo, repoFilter) {
			pool = append(pool, t)
		}
	}
	if len(repoFilter) > 0 && len(pool) == 0 {
		if _, err := e.checkRepoFilter(repoFilter); err != nil {
			return Target{}, err
		}
	}

	if arg == "" || looksLikePath(arg) {
		p := arg
		if p == "" {
			p, _ = os.Getwd()
		}
		p = expandHome(p)
		abs, _ := filepath.Abs(p)
		// Deepest match wins (a mono constituent sits inside its subdir)
		var best *Target
		for i := range pool {
			if pathWithin(abs, pool[i].WT.Path) {
				if best == nil || len(pool[i].WT.Path) > len(best.WT.Path) {
					best = &pool[i]
				}
			}
		}
		if best != nil {
			return *best, nil
		}
		if arg == "" {
			return Target{}, usageErr("no worktree given and the current directory is not inside an LTS worktree").
				withHint("pass a branch, folder or id (see `lts ls`), or cd into a worktree")
		}
		return Target{}, notFoundErr("%s is not an LTS worktree", abs).withHint("run `lts ls` to list worktrees")
	}

	var matches []Target
	for _, t := range pool {
		if t.ID() == arg || t.WT.Branch == arg || filepath.Base(t.WT.Path) == arg {
			matches = append(matches, t)
		}
	}
	if len(matches) == 0 {
		for _, t := range pool {
			if strings.EqualFold(t.WT.Branch, arg) || strings.EqualFold(filepath.Base(t.WT.Path), arg) {
				matches = append(matches, t)
			}
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		var cands []string
		for _, t := range pool {
			cands = append(cands, t.WT.Branch, t.ID())
		}
		err := notFoundErr("no worktree matches %q", arg).withSuggestions(suggest(arg, cands, 4))
		if len(err.Suggestions) == 0 {
			for _, t := range pool {
				if len(err.Available) < 20 {
					err.Available = append(err.Available, t.ID())
				}
			}
		}
		if len(pool) == 0 {
			err.withHint("there are no LTS worktrees yet — create one with `lts create <branch> -r <repo>`")
		} else {
			err.withHint("run `lts ls` to list worktrees")
		}
		return Target{}, err
	default:
		var ids []string
		for _, t := range matches {
			ids = append(ids, t.ID())
		}
		sort.Strings(ids)
		err := newErr(ExitNotFound, CodeAmbiguous, "%q matches %d worktrees", arg, len(matches)).
			withSuggestions(ids).
			withHint("pass an id from the list, or narrow with -r <repo>")
		return Target{}, err
	}
}

// checkRepoFilter validates -r values against known repos.
func (e *Env) checkRepoFilter(filter []string) ([]git.Repo, error) {
	var out []git.Repo
	for _, f := range filter {
		r, err := e.findRepo(f)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}
