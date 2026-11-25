package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"lts-revamp/internal/core"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
)

// JSON view types — the public, versioned output shapes (SchemaVersion).

type repoView struct {
	Name           string         `json:"name"`
	Kind           string         `json:"kind"` // "repo" | "group"
	Path           string         `json:"path,omitempty"`
	Members        []string       `json:"members,omitempty"` // group: constituent repos
	BasisBranch    string         `json:"basis_branch,omitempty"`
	PackageManager string         `json:"package_manager,omitempty"`
	LTSDir         string         `json:"lts_dir,omitempty"`
	LTSPath        string         `json:"lts_path,omitempty"`
	Hidden         bool           `json:"hidden,omitempty"`
	MainCheckout   *mainCheckout  `json:"main_checkout,omitempty"`
	Worktrees      []worktreeView `json:"worktrees"`
	// Stale: worktrees git still has registered whose folder is gone —
	// clean up with `lts prune`
	Stale []git.StaleWorktree `json:"stale,omitempty"`
}

// mainCheckout flags work sitting in the repo's own folder instead of an
// LTS worktree (the TUI's "Migrate to LTS" card).
type mainCheckout struct {
	NeedsMigration bool   `json:"needs_migration"`
	Branch         string `json:"branch,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type worktreeView struct {
	ID           string           `json:"id"`
	Repo         string           `json:"repo"`
	Group        bool             `json:"group"`
	Branch       string           `json:"branch"`
	Path         string           `json:"path"`
	Members      []git.MonoMember `json:"members,omitempty"`
	CreatedAt    string           `json:"created_at,omitempty"`
	LastActivity string           `json:"last_activity,omitempty"`
	TmuxSession  bool             `json:"tmux_session"`
	Busy         bool             `json:"busy"`
	Status       *statusView      `json:"status,omitempty"`
}

type statusView struct {
	State           string         `json:"state"` // git.WTStatus.Slug()
	Text            string         `json:"text"`
	ChangedFiles    int            `json:"changed_files"`
	Ahead           int            `json:"ahead"`
	Behind          int            `json:"behind"`
	Upstream        string         `json:"upstream,omitempty"`
	AheadOfBasis    int            `json:"ahead_of_basis"`   // commits not on the basis branch
	UnpushedCommits int            `json:"unpushed_commits"` // commits that exist only locally
	BehindBasis     int            `json:"behind_basis"`     // basis-branch commits not in HEAD (rebase candidates)
	HasRemoteBranch bool           `json:"has_remote_branch"`
	DeleteRisk      string         `json:"delete_risk,omitempty"`
	DangerousDelete bool           `json:"dangerous_to_delete"`
	ProtectedBranch bool           `json:"protected_branch"`
	MemberStatuses  []memberStatus `json:"member_statuses,omitempty"`
}

type memberStatus struct {
	Repo  string `json:"repo"`
	State string `json:"state"`
	Text  string `json:"text"`
}

func isoTime(unix int64) string {
	if unix <= 0 {
		return ""
	}
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

func (e *Env) repoView(r git.Repo, tmux map[string]bool, busy func(git.Repo) bool) repoView {
	v := repoView{
		Name:    r.Name,
		Kind:    "repo",
		Path:    r.Path,
		LTSDir:  r.LTSDir,
		LTSPath: r.LTSPath,
		Hidden:  r.Hidden,
	}
	if r.IsMonorepo {
		v.Kind = "group"
		v.Members = r.RepoNames
	} else {
		v.BasisBranch = e.Cfg.GetRepoBasisBranch(r.Name)
		v.PackageManager = core.ResolvePM(&e.Cfg, r.Name)
		v.MainCheckout = &mainCheckout{NeedsMigration: r.NeedsMigration, Branch: r.MigrationBranch, Reason: r.MigrationReason}
		v.Stale = r.Stale
	}
	v.Worktrees = []worktreeView{}
	for _, wt := range r.Worktrees {
		v.Worktrees = append(v.Worktrees, e.worktreeView(r, wt, tmux, busy))
	}
	return v
}

func (e *Env) worktreeView(r git.Repo, wt git.Worktree, tmux map[string]bool, busy func(git.Repo) bool) worktreeView {
	repoName := r.Name
	w := worktreeView{
		ID:           worktreeID(r, wt),
		Repo:         repoName,
		Group:        r.IsMonorepo,
		Branch:       wt.Branch,
		Path:         wt.Path,
		CreatedAt:    isoTime(wt.CreatedAt),
		LastActivity: isoTime(wt.LastActivity),
		TmuxSession:  tmux[opener.SessionName(wt.Path)],
	}
	if busy != nil {
		w.Busy = busy(r)
	}
	if r.IsMonorepo {
		w.Members = git.MonoMembers(wt.Path)
	}
	return w
}

// fillStatus computes the full status for a worktree view.
func (e *Env) fillStatus(r git.Repo, wt git.Worktree, w *worktreeView) {
	basis := e.Cfg.GetRepoBasisBranch(r.Name)
	if r.IsMonorepo && len(r.RepoNames) > 0 {
		basis = e.Cfg.GetRepoBasisBranch(r.RepoNames[0])
	}
	sv := &statusView{}
	if r.IsMonorepo {
		// Group status: git.MonoGroupStatus (a member off the group branch
		// is "unknown", i.e. dangerous)
		worst, worstText, members := git.MonoGroupStatus(w.Members, wt.Branch, e.Cfg.GetRepoBasisBranch)
		for _, m := range members {
			sv.MemberStatuses = append(sv.MemberStatuses, memberStatus{Repo: m.Repo, State: m.Status.Slug(), Text: m.Text})
		}
		for _, m := range members {
			si := m.Sync
			if si.Branch == "" { // off-branch member: count its working tree anyway
				si = git.GetSyncInfo(m.Path)
			}
			sv.ChangedFiles += si.ChangedFiles
			sv.Ahead += si.Ahead
			sv.Behind += si.Behind
			ab := countCommits(m.Path, e.Cfg.GetRepoBasisBranch(m.Repo)+"..HEAD")
			sv.AheadOfBasis += ab
			sv.BehindBasis += countCommits(m.Path, "HEAD.."+e.Cfg.GetRepoBasisBranch(m.Repo))
			if si.Upstream == "" {
				sv.UnpushedCommits += ab
			} else {
				sv.UnpushedCommits += si.Ahead
			}
		}
		sv.State, sv.Text = worst.Slug(), worstText
		sv.HasRemoteBranch = git.HasRemoteBranch(worst)
		sv.DeleteRisk, sv.DangerousDelete = git.DeleteRisk(worst)
	} else {
		st, txt, si := git.GetWorktreeDetail(wt.Path, basis)
		sv.State, sv.Text = st.Slug(), txt
		sv.ChangedFiles, sv.Ahead, sv.Behind, sv.Upstream = si.ChangedFiles, si.Ahead, si.Behind, si.Upstream
		sv.AheadOfBasis = countCommits(wt.Path, basis+"..HEAD")
		sv.BehindBasis = countCommits(wt.Path, "HEAD.."+basis)
		sv.UnpushedCommits = si.Ahead
		if si.Upstream == "" {
			sv.UnpushedCommits = sv.AheadOfBasis
		}
		sv.HasRemoteBranch = git.HasRemoteBranch(st)
		sv.DeleteRisk, sv.DangerousDelete = git.DeleteRisk(st)
	}
	sv.ProtectedBranch = git.IsProtectedBranch(wt.Branch)
	w.Status = sv
}

func countCommits(dir, rangeSpec string) int {
	out, err := git.RunGit(dir, "rev-list", "--count", rangeSpec)
	if err != nil {
		return 0
	}
	n := 0
	fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
	return n
}

// riskNote explains in a few words what deleting would lose.
func riskNote(s *statusView) string {
	var parts []string
	if s.ChangedFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d uncommitted file(s)", s.ChangedFiles))
	}
	if s.UnpushedCommits > 0 {
		parts = append(parts, fmt.Sprintf("%d unpushed commit(s)", s.UnpushedCommits))
	}
	if len(parts) == 0 {
		return "unsaved work"
	}
	return strings.Join(parts, " + ")
}

// relToWorkspace shortens p for text output when it sits under the workspace.
func (e *Env) rel(p string) string {
	if r, err := filepath.Rel(e.WorkDir, p); err == nil && !startsWithDotDot(r) {
		return r
	}
	return p
}

func startsWithDotDot(p string) bool {
	return p == ".." || len(p) > 2 && p[:3] == "../"
}
