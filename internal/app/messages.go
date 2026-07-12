package app

import (
	"lts-revamp/internal/gh"
	"lts-revamp/internal/git"
	"lts-revamp/internal/ui"
	"lts-revamp/internal/update"
)

// ReposLoadedMsg is sent when repo discovery completes.
type ReposLoadedMsg struct {
	Repos         []git.Repo
	TmuxLive      map[string]bool // live LTS tmux sessions (piggybacked on discovery)
	GhState       ui.CloneAvail   // gh install/auth state (piggybacked on discovery)
	DiskUsed      uint64          // workdir volume usage (piggybacked on discovery)
	DiskTotal     uint64
	GithubRemotes map[string]bool // repo name → origin points at github.com
	Err           error
}

// SizesScannedMsg carries freshly scanned worktree sizes (background walker).
type SizesScannedMsg struct {
	Sizes map[string]ui.WTSize
}

// RefreshDoneMsg is sent when refresh completes.
type RefreshDoneMsg struct {
	Count  int
	Failed []string // repo names that failed
	Locked []string // repo locks to release
	Err    error
}

// SingleRefreshDoneMsg is sent when a single repo refresh completes.
// Operations identify their target by name, not by index into m.repos —
// the repo list can be reloaded while an operation is in flight.
type SingleRefreshDoneMsg struct {
	RepoName string
	Locked   []string // repo locks to release
	Err      error
}

// RebaseDoneMsg is sent when a rebase completes.
type RebaseDoneMsg struct {
	Branch string
	Locked []string // repo locks to release
	Err    error
}

// DeleteDoneMsg is sent when a worktree deletion completes.
type DeleteDoneMsg struct {
	Branch string
	Locked []string // repo locks to release
	Err    error
}

// CreateDoneMsg is sent when worktree creation completes.
type CreateDoneMsg struct {
	Results []*git.CreateResult
	Branch  string
	Log     *git.CreateLog
	Locked  []string // repo locks to release
	Err     error
}

// CleanupMergedDoneMsg is sent when cleanup completes.
type CleanupMergedDoneMsg struct {
	Cleaned int
	Locked  []string // repo locks to release
	Err     error
}

// CleanModulesDoneMsg is sent when a node_modules cleanup completes.
type CleanModulesDoneMsg struct {
	Branch  string
	WtPath  string // for size-cache invalidation
	Removed int
	Freed   int64
	Locked  []string // repo locks to release
	Err     error
}

// GhUserMsg carries the authenticated GitHub login (fetched once at startup).
type GhUserMsg struct {
	Login string
}

// PRDoneMsg is sent after attempting to open a PR page.
type PRDoneMsg struct {
	Branch string
	Err    error
}

// GhRepoListMsg carries the cloneable-repo list fetched from GitHub.
type GhRepoListMsg struct {
	Repos []gh.Repo
	Err   error
}

// CloneDoneMsg is sent when a gh repo clone completes.
type CloneDoneMsg struct {
	RepoName string
	Locked   []string
	Err      error
}

// HibernateAuditMsg carries the completed hibernate safety audit. Locked
// travels with it: when the dialog was closed mid-audit, the handler
// releases the locks the audit was still holding.
type HibernateAuditMsg struct {
	RepoName string
	Audit    git.HibernateAudit
	Locked   []string
}

// HibernateDoneMsg is sent when a hibernate (backup + delete) completes.
type HibernateDoneMsg struct {
	RepoName  string
	Freed     int64
	EnvBacked int
	Locked    []string
	Err       error
}

// MaintenanceTickMsg re-evaluates auto-maintenance hourly so long-running
// instances sweep worktrees/sessions that cross their age threshold after
// launch (the startup run only covers launch time).
type MaintenanceTickMsg struct{}

// MaintenanceDoneMsg is sent when startup auto-maintenance completes.
type MaintenanceDoneMsg struct {
	Cleaned int      // node_modules directories removed
	Freed   int64    // bytes freed
	Killed  []string // idle tmux sessions killed
	Locked  []string // repo locks to release
}

// LogEntryMsg is sent when an async operation produces a log line.
type LogEntryMsg struct {
	Context string // repo/operation context
	Message string
	IsError bool
}

// StatusClearMsg clears the status bar.
type StatusClearMsg struct {
	Gen int // only clear if this matches the current generation
}

// LoaderTickMsg advances the loading animation frame.
type LoaderTickMsg struct{}

// RenameDoneMsg is sent when a rename completes.
type RenameDoneMsg struct {
	NewBranch string
	Locked    []string // repo locks to release
	Err       error
}

// MigrateDoneMsg is sent when a migration to LTS worktree completes.
type MigrateDoneMsg struct {
	Result *git.CreateResult
	Locked []string // repo locks to release
	Err    error
}

// UpdateCheckMsg is sent when a background update check completes.
type UpdateCheckMsg struct {
	Result update.Result
}

// MigrationCheckMsg is sent after checking if directory migration is needed.
type MigrationCheckMsg struct {
	Needed bool
}

// MigrationDoneMsg is sent after directory migration completes.
type MigrationDoneMsg struct {
	Count int
}
