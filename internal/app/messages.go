package app

import (
	"lts-revamp/internal/git"
	"lts-revamp/internal/update"
)

// ReposLoadedMsg is sent when repo discovery completes.
type ReposLoadedMsg struct {
	Repos []git.Repo
	Err   error
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
