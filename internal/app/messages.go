package app

import "lts-revamp/internal/git"

// ReposLoadedMsg is sent when repo discovery completes.
type ReposLoadedMsg struct {
	Repos []git.Repo
	Err   error
}

// RefreshDoneMsg is sent when refresh completes.
type RefreshDoneMsg struct {
	Count int
	Err   error
}

// SingleRefreshDoneMsg is sent when a single repo refresh completes.
type SingleRefreshDoneMsg struct {
	RepoIdx int
	Err     error
}

// RebaseDoneMsg is sent when a rebase completes.
type RebaseDoneMsg struct {
	RepoIdx int
	WTIdx   int
	Err     error
}

// DeleteDoneMsg is sent when a worktree deletion completes.
type DeleteDoneMsg struct {
	RepoIdx int
	WTIdx   int
	Err     error
}

// CreateDoneMsg is sent when worktree creation completes.
type CreateDoneMsg struct {
	Results []*git.CreateResult
	Branch  string
	Log     *git.CreateLog
	Err     error
}

// CleanupMergedDoneMsg is sent when cleanup completes.
type CleanupMergedDoneMsg struct {
	Cleaned int
	Err     error
}

// StatusClearMsg clears the status bar.
type StatusClearMsg struct{}

// LoaderTickMsg advances the loading animation frame.
type LoaderTickMsg struct{}

// RenameDoneMsg is sent when a rename completes.
type RenameDoneMsg struct {
	RepoIdx int
	WTIdx   int
	Err     error
}
