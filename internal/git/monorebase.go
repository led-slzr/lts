package git

import (
	"fmt"
	"strings"
)

// MemberRebase is one mono-group member's rebase outcome.
type MemberRebase struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Onto   string `json:"onto"`
	Result string `json:"result"` // rebased | up_to_date | conflict | failed | skipped
	Error  string `json:"error,omitempty"`
}

// RebaseMonoGroup rebases every member of a mono group (the branch subdir
// at branchSubdir) onto its own repo's basis branch, one member at a time.
// It refuses up front if any member is off the group branch, detached, or
// mid-rebase/merge. On the first conflict that member's rebase is aborted
// and restored (see RebaseWorktree) and the remaining members are skipped;
// members already rebased keep their result — each repo's history is
// independent, so nothing half-done is left in any single repo.
func RebaseMonoGroup(branchSubdir, branch string, basisFor func(repo string) string, pkgFor func(repo string) string, logFn ...LogFunc) ([]MemberRebase, error) {
	log := noopLog
	if len(logFn) > 0 && logFn[0] != nil {
		log = logFn[0]
	}
	members := MonoMembers(branchSubdir)
	if len(members) == 0 {
		return nil, fmt.Errorf("no member worktrees in %s", branchSubdir)
	}
	// Preflight: every member must be on the group branch and idle
	for _, m := range members {
		if cur := currentBranch(m.Path); cur != branch {
			return nil, fmt.Errorf("%s is on %q, not the group branch %s — fix it before rebasing the group", m.Repo, cur, branch)
		}
		if err := CheckOngoingOperations(m.Path); err != nil {
			return nil, err
		}
	}
	var out []MemberRebase
	stopped := false
	for _, m := range members {
		onto := detectMainBranch(m.Path, basisFor(m.Repo))
		r := MemberRebase{Repo: m.Repo, Path: m.Path, Onto: onto}
		if stopped {
			r.Result = "skipped"
			out = append(out, r)
			continue
		}
		before, _ := RunGit(m.Path, "rev-parse", "HEAD")
		err := RebaseWorktree(m.Path, onto, pkgFor(m.Repo), WithContext(log, m.Repo))
		after, _ := RunGit(m.Path, "rev-parse", "HEAD")
		switch {
		case err == nil && before == after:
			r.Result = "up_to_date"
		case err == nil:
			r.Result = "rebased"
		case strings.HasPrefix(err.Error(), "rebase conflict"):
			r.Result, r.Error, stopped = "conflict", err.Error(), true
		default:
			r.Result, r.Error, stopped = "failed", err.Error(), true
		}
		out = append(out, r)
	}
	if stopped {
		for _, r := range out {
			if r.Result == "conflict" {
				return out, fmt.Errorf("rebase conflict in %s — aborted and restored there; later members skipped", r.Repo)
			}
		}
		for _, r := range out {
			if r.Result == "failed" {
				return out, fmt.Errorf("rebase failed in %s: %s", r.Repo, r.Error)
			}
		}
	}
	return out, nil
}
