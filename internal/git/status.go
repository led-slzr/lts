package git

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// GetWorktreeStatus replicates the get_worktree_status() logic from lts.sh.
func GetWorktreeStatus(wtPath, basisBranch string) (WTStatus, string) {
	porcelain, err := RunGit(wtPath, "status", "--porcelain")
	if err != nil {
		// Only a directory that is really gone is "missing" (safe to prune)
		if _, serr := os.Stat(wtPath); os.IsNotExist(serr) {
			return StatusMissing, "missing"
		}
		return StatusUnknown, "unreadable by git"
	}

	// Check uncommitted changes
	var localStatus string
	var changedCount int
	porcelain = strings.TrimSpace(porcelain)
	if porcelain != "" {
		changedCount = len(strings.Split(porcelain, "\n"))
		localStatus = fmt.Sprintf("%d changed", changedCount)
	}

	// Check ahead/behind
	currentBranch, _ := RunGit(wtPath, "branch", "--show-current")

	// Detached HEAD: no branch to sync or merge. What matters is whether
	// HEAD's commits are reachable from any branch; if not, deleting the
	// worktree orphans them.
	if currentBranch == "" {
		if refs, _ := RunGit(wtPath, "for-each-ref", "--contains", "HEAD", "--count=1", "refs/heads", "refs/remotes"); strings.TrimSpace(refs) == "" {
			return StatusUnknown, "detached HEAD with commits on no branch"
		}
		if localStatus != "" {
			return StatusChanged, localStatus + " | detached"
		}
		return StatusClean, "detached"
	}

	var syncStatus string
	var ahead, behind int

	// Try upstream first, then origin/<branch>
	hasUpstream := false
	if _, err := RunGit(wtPath, "rev-parse", "--verify", "@{upstream}"); err == nil {
		hasUpstream = true
	}

	var remoteRef string
	if hasUpstream {
		remoteRef = "@{upstream}"
	} else if currentBranch != "" {
		if _, err := RunGit(wtPath, "rev-parse", "--verify", "origin/"+currentBranch); err == nil {
			remoteRef = "origin/" + currentBranch
		}
	}

	if remoteRef != "" {
		aheadStr, _ := RunGit(wtPath, "rev-list", "--count", remoteRef+"..HEAD")
		behindStr, _ := RunGit(wtPath, "rev-list", "--count", "HEAD.."+remoteRef)
		ahead, _ = strconv.Atoi(aheadStr)
		behind, _ = strconv.Atoi(behindStr)

		if ahead > 0 && behind > 0 {
			syncStatus = "diverged"
		} else if ahead > 0 {
			syncStatus = fmt.Sprintf("%d to push", ahead)
		} else if behind > 0 {
			syncStatus = fmt.Sprintf("%d to pull", behind)
		} else {
			syncStatus = "synced"
		}
	} else {
		syncStatus = "no remote"
	}

	// Detect main branch for merge/new detection
	mainBranch := ""
	candidates := []string{basisBranch}
	if basisBranch != "main" {
		candidates = append(candidates, "main")
	}
	if basisBranch != "master" {
		candidates = append(candidates, "master")
	}
	for _, c := range candidates {
		if _, err := RunGit(wtPath, "show-ref", "--verify", "--quiet", "refs/heads/"+c); err == nil {
			mainBranch = c
			break
		}
	}

	// Check merged/new
	isMerged := false
	isNew := false

	if mainBranch != "" && currentBranch != mainBranch {
		uniqueStr, _ := RunGit(wtPath, "rev-list", "--count", mainBranch+"..HEAD")
		uniqueCommits, _ := strconv.Atoi(uniqueStr)

		if uniqueCommits == 0 {
			if syncStatus == "no remote" {
				isNew = true
			} else {
				isMerged = true
			}
		} else {
			// Check squash/rebase merge via git cherry
			cherryOut, _ := RunGit(wtPath, "cherry", mainBranch, "HEAD")
			unmergedCount := 0
			for _, line := range strings.Split(cherryOut, "\n") {
				if strings.HasPrefix(line, "+") {
					unmergedCount++
				}
			}
			if unmergedCount == 0 {
				isMerged = true
			}
		}
		// Merged locally isn't enough: commits that exist only on the
		// branch's remote (pushed by someone else, not pulled) are work a
		// cleanup with --remote would destroy.
		if isMerged && remoteRef != "" && behind > 0 {
			if n, _ := strconv.Atoi(strings.TrimSpace(mustGit(wtPath, "rev-list", "--count", remoteRef, "^HEAD", "^"+mainBranch))); n > 0 {
				isMerged = false
			}
		}
	}

	// Build final status
	if isNew {
		if localStatus != "" {
			return StatusNewDirty, localStatus + " | new"
		}
		return StatusNew, "new"
	}
	if isMerged {
		if localStatus != "" {
			return StatusMergedDirty, localStatus + " | merged"
		}
		return StatusMergedCleanable, "merged · cleanable"
	}
	if localStatus != "" {
		return StatusChanged, localStatus + " | " + syncStatus
	}
	if syncStatus == "synced" {
		return StatusClean, "clean"
	}
	if syncStatus == "diverged" {
		return StatusDiverged, syncStatus
	}
	if strings.Contains(syncStatus, "to push") {
		return StatusToPush, syncStatus
	}
	if strings.Contains(syncStatus, "to pull") {
		return StatusToPull, syncStatus
	}
	if syncStatus == "no remote" {
		return StatusNoRemote, syncStatus
	}

	return StatusClean, syncStatus
}

// DeleteRisk describes what deleting a worktree in this status would lose.
// dangerous = true means the deletion destroys work that exists nowhere
// else, so callers must demand an explicit, typed / forced confirmation.
func DeleteRisk(status WTStatus) (warning string, dangerous bool) {
	switch status {
	case StatusChanged:
		return "Uncommitted changes will be LOST", true
	case StatusDiverged:
		return "CRITICAL: Unpushed commits will be permanently lost", true
	case StatusToPush:
		return "Unpushed commits will be LOST", true
	case StatusNoRemote:
		return "Branch was never pushed — ALL work will be LOST", true
	case StatusMergedDirty:
		return "Uncommitted changes will be LOST (branch is merged)", true
	case StatusNewDirty:
		return "Uncommitted changes will be LOST (new branch)", true
	case StatusMissing:
		return "Worktree directory is missing — will clean up git references", false
	case StatusUnknown:
		return "Git can't read this worktree, so its contents can't be checked — everything in the folder will be LOST", true
	default:
		return "", false
	}
}

// HasRemoteBranch reports whether a worktree status implies its branch
// exists on the remote (so deleting / renaming the remote is meaningful).
func HasRemoteBranch(status WTStatus) bool {
	switch status {
	case StatusNew, StatusNewDirty, StatusNoRemote:
		return false
	default:
		return true
	}
}

// Slug returns the stable machine name of a status (CLI JSON output).
func (s WTStatus) Slug() string {
	switch s {
	case StatusClean:
		return "clean"
	case StatusChanged:
		return "changed"
	case StatusMergedCleanable:
		return "merged_cleanable"
	case StatusDiverged:
		return "diverged"
	case StatusToPush:
		return "to_push"
	case StatusToPull:
		return "to_pull"
	case StatusNew:
		return "new"
	case StatusNoRemote:
		return "no_remote"
	case StatusMissing:
		return "missing"
	case StatusMergedDirty:
		return "merged_dirty"
	case StatusNewDirty:
		return "new_dirty"
	case StatusUnknown:
		return "unknown"
	}
	return "unknown"
}

// SyncInfo is the raw sync state of a worktree checkout.
type SyncInfo struct {
	Branch       string `json:"branch"`
	ChangedFiles int    `json:"changed_files"`
	Upstream     string `json:"upstream,omitempty"` // "" = no remote branch
	Ahead        int    `json:"ahead"`
	Behind       int    `json:"behind"`
}

// GetSyncInfo reports changed-file count and ahead/behind against the
// upstream (or origin/<branch>), mirroring GetWorktreeStatus's rules.
func GetSyncInfo(wtPath string) SyncInfo {
	var si SyncInfo
	if out, err := RunGit(wtPath, "status", "--porcelain"); err == nil {
		if out = strings.TrimSpace(out); out != "" {
			si.ChangedFiles = len(strings.Split(out, "\n"))
		}
	}
	si.Branch, _ = RunGit(wtPath, "branch", "--show-current")
	if up, err := RunGit(wtPath, "rev-parse", "--abbrev-ref", "@{upstream}"); err == nil && up != "" {
		si.Upstream = up
	} else if si.Branch != "" {
		if _, err := RunGit(wtPath, "rev-parse", "--verify", "origin/"+si.Branch); err == nil {
			si.Upstream = "origin/" + si.Branch
		}
	}
	if si.Upstream != "" {
		a, _ := RunGit(wtPath, "rev-list", "--count", si.Upstream+"..HEAD")
		b, _ := RunGit(wtPath, "rev-list", "--count", "HEAD.."+si.Upstream)
		si.Ahead, _ = strconv.Atoi(a)
		si.Behind, _ = strconv.Atoi(b)
	}
	return si
}

// StatusSeverity orders statuses by how much unrecoverable work they hold.
func StatusSeverity(s WTStatus) int {
	switch s {
	case StatusMergedCleanable, StatusClean:
		return 0
	case StatusNew, StatusToPull, StatusMissing:
		return 1
	case StatusMergedDirty, StatusNewDirty, StatusChanged:
		return 3
	case StatusToPush, StatusNoRemote:
		return 4
	case StatusDiverged:
		return 5
	case StatusUnknown:
		return 6
	}
	return 2
}

// AggregateStatus combines member statuses of a mono group: merged ·
// cleanable only when every member is; otherwise the most severe member
// (so one member with unpushed work protects the whole group).
func AggregateStatus(statuses []WTStatus, texts []string) (WTStatus, string) {
	if len(statuses) == 0 {
		return StatusMissing, "missing"
	}
	allCleanable := true
	worst, worstText := -1, ""
	var worstSt WTStatus
	for i, st := range statuses {
		if st != StatusMergedCleanable {
			allCleanable = false
		}
		sev := StatusSeverity(st)
		if st == StatusMergedCleanable {
			sev = -1 // never the representative unless all members are
		}
		if sev > worst {
			worst, worstSt = sev, st
			if i < len(texts) {
				worstText = texts[i]
			}
		}
	}
	if allCleanable {
		return StatusMergedCleanable, "merged · cleanable"
	}
	return worstSt, worstText
}

func mustGit(dir string, args ...string) string {
	out, _ := RunGit(dir, args...)
	return out
}

// RemoteOnlyCommits counts commits on origin/<branch> that are neither in
// localRef nor in the repo's main branch — work that exists only on the
// remote. Deleting or replacing origin/<branch> would destroy it. Returns
// 0 when the remote branch doesn't exist.
func RemoteOnlyCommits(repoPath, branch, localRef string) int {
	remote := "refs/remotes/origin/" + branch
	if _, err := RunGit(repoPath, "show-ref", "--verify", "--quiet", remote); err != nil {
		return 0
	}
	args := []string{"rev-list", "--count", remote, "^" + localRef}
	if main := detectMainBranch(repoPath, "main"); main != "" {
		args = append(args, "^"+main)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(mustGit(repoPath, args...)))
	return n
}
