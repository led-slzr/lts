package git

import (
	"fmt"
	"strconv"
	"strings"
)

// GetWorktreeStatus replicates the get_worktree_status() logic from lts.sh.
func GetWorktreeStatus(wtPath, basisBranch string) (WTStatus, string) {
	porcelain, err := RunGit(wtPath, "status", "--porcelain")
	if err != nil {
		return StatusMissing, "missing"
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
