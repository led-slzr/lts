package git

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type WTStatus int

const (
	StatusClean WTStatus = iota
	StatusChanged
	StatusMergedCleanable
	StatusDiverged
	StatusToPush
	StatusToPull
	StatusNew
	StatusNoRemote
	StatusMissing
	StatusMergedDirty
	StatusNewDirty
)

type Worktree struct {
	Name       string // display name (branch suffix or branch name)
	Branch     string // full branch name (e.g. fix/hotfix)
	Path       string // absolute filesystem path
	Status     WTStatus
	StatusText string
	LTSDir     string // which LTS dir this came from
}

type Repo struct {
	Name       string
	Path       string // empty for monorepo cards
	MainBranch string
	LTSDir     string // e.g. "repo-lts" or "core-erp-ui-lts"
	LTSType    string // "single", "erp", "monorepo"
	IsMonorepo bool   // true for multi-repo LTS cards
	RepoNames  []string // for monorepo: the constituent repo names
	Worktrees  []Worktree

	// Migration state: true when the main repo dir has work on a non-main branch
	NeedsMigration  bool
	MigrationBranch string // current branch name (e.g. "fix/hotfix")
	MigrationReason string // human-readable reason (e.g. "3 unpushed commits, 2 uncommitted files")
}

// DiscoverRepos finds all git repositories and monorepo LTS groups in scriptDir.
// Individual repos only show their single-repo worktrees.
// Multi-repo LTS dirs get their own separate "monorepo" cards.
// BasisBranchResolver returns the basis branch for a given repo name.
type BasisBranchResolver func(repoName string) string

func DiscoverRepos(scriptDir string, getBasisBranch BasisBranchResolver) []Repo {
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		return nil
	}

	var repos []Repo

	// Phase 1: Discover individual git repos with their single-repo LTS worktrees only
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, "-lts") {
			continue
		}
		dirPath := filepath.Join(scriptDir, name)
		gitDir := filepath.Join(dirPath, ".git")

		info, err := os.Stat(gitDir)
		if err != nil || !info.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(gitDir, "config")); err != nil {
			continue
		}

		repoBasis := getBasisBranch(name)
		mainBranch := detectMainBranch(dirPath, repoBasis)
		ltsDir := name + "-lts"
		ltsType := "single"
		ltsPath := filepath.Join(scriptDir, ltsDir)

		if _, err := os.Stat(ltsPath); os.IsNotExist(err) {
			ltsDir = ""
		} else {
			ltsType = getLTSType(scriptDir, ltsDir)
		}

		repo := Repo{
			Name:       name,
			Path:       dirPath,
			MainBranch: mainBranch,
			LTSDir:     ltsDir,
			LTSType:    ltsType,
		}

		// Only collect worktrees from the repo's own single-repo LTS dir
		if ltsDir != "" && ltsType == "single" {
			wts := listWorktrees(scriptDir, ltsDir, name, repoBasis)
			repo.Worktrees = append(repo.Worktrees, wts...)
		}

		// Check if main repo needs migration to LTS worktree
		needsMigration, migBranch, migReason := checkMigrationNeeded(dirPath, mainBranch)
		repo.NeedsMigration = needsMigration
		repo.MigrationBranch = migBranch
		repo.MigrationReason = migReason

		repos = append(repos, repo)
	}

	// Phase 2: Discover multi-repo LTS directories and create monorepo cards
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "-lts") {
			continue
		}
		ltsDir := e.Name()
		ltsType := getLTSType(scriptDir, ltsDir)
		if ltsType != "monorepo" && ltsType != "erp" {
			continue
		}

		repoNames := getLTSRepos(scriptDir, ltsDir)
		if len(repoNames) < 2 {
			continue
		}

		// Card name: sorted repo names joined (e.g. "core-erp-ui")
		sorted := make([]string, len(repoNames))
		copy(sorted, repoNames)
		sort.Strings(sorted)
		cardName := strings.Join(sorted, "-")

		monoRepo := Repo{
			Name:       cardName,
			LTSDir:     ltsDir,
			LTSType:    ltsType,
			IsMonorepo: true,
			RepoNames:  sorted,
		}

		// Collect all worktrees from this LTS dir, grouped by branch
		// Use first repo's basis branch for status checking
		monoBasis := getBasisBranch(sorted[0])
		allWTs := listAllMonorepoWorktrees(scriptDir, ltsDir, sorted, monoBasis)
		monoRepo.Worktrees = allWTs

		repos = append(repos, monoRepo)
	}

	return repos
}

// listAllMonorepoWorktrees collects worktrees from a monorepo LTS dir.
// For display, we show branch names (not per-repo worktree names).
// A branch like feat/login that exists across core and erp-ui shows as one entry.
func listAllMonorepoWorktrees(scriptDir, ltsDir string, repoNames []string, basisBranch string) []Worktree {
	ltsPath := filepath.Join(scriptDir, ltsDir)
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return nil
	}

	var wts []Worktree
	seen := make(map[string]bool)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Each subdirectory is a branch group (e.g. core-erp-ui-login)
		branchSubdir := filepath.Join(ltsPath, e.Name())
		subEntries, err := os.ReadDir(branchSubdir)
		if err != nil {
			continue
		}

		// Find worktrees inside the branch subdir
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			wtPath := filepath.Join(branchSubdir, se.Name())
			if !isWorktreeDir(wtPath) {
				continue
			}

			// Get the branch name from git
			branch, _ := RunGit(wtPath, "branch", "--show-current")
			if branch == "" {
				continue
			}

			// Deduplicate: show each branch once (not once per repo)
			if seen[branch] {
				continue
			}
			seen[branch] = true

			status, statusText := GetWorktreeStatus(wtPath, basisBranch)

			wts = append(wts, Worktree{
				Name:       branch,
				Branch:     branch,
				Path:       branchSubdir, // point to the branch subdir (contains all repo worktrees)
				Status:     status,
				StatusText: statusText,
				LTSDir:     ltsDir,
			})
		}
	}

	return wts
}

// checkMigrationNeeded checks if the main repo directory has work on a non-main
// branch that should be migrated to an LTS worktree.
func checkMigrationNeeded(repoPath, mainBranch string) (needsMigration bool, branch, reason string) {
	currentBranch, err := RunGit(repoPath, "branch", "--show-current")
	if err != nil {
		return false, "", ""
	}

	// Detached HEAD: check for uncommitted changes that need rescuing
	if currentBranch == "" {
		porcelain, _ := RunGit(repoPath, "status", "--porcelain")
		porcelain = strings.TrimSpace(porcelain)
		if porcelain != "" {
			count := len(strings.Split(porcelain, "\n"))
			detachedLabel := "HEAD (detached)"
			if count == 1 {
				return true, detachedLabel, "1 uncommitted file"
			}
			return true, detachedLabel, fmt.Sprintf("%d uncommitted files", count)
		}
		return false, "", ""
	}

	// If on the main branch, no migration needed
	if currentBranch == mainBranch {
		return false, "", ""
	}

	var reasons []string

	// Check uncommitted changes
	porcelain, _ := RunGit(repoPath, "status", "--porcelain")
	porcelain = strings.TrimSpace(porcelain)
	if porcelain != "" {
		count := len(strings.Split(porcelain, "\n"))
		if count == 1 {
			reasons = append(reasons, "1 uncommitted file")
		} else {
			reasons = append(reasons, fmt.Sprintf("%d uncommitted files", count))
		}
	}

	// Check unpushed commits
	// Try upstream first, then origin/<branch>
	remoteRef := ""
	if _, err := RunGit(repoPath, "rev-parse", "--verify", "@{upstream}"); err == nil {
		remoteRef = "@{upstream}"
	} else if _, err := RunGit(repoPath, "rev-parse", "--verify", "origin/"+currentBranch); err == nil {
		remoteRef = "origin/" + currentBranch
	}

	if remoteRef != "" {
		aheadStr, _ := RunGit(repoPath, "rev-list", "--count", remoteRef+"..HEAD")
		ahead := 0
		fmt.Sscanf(aheadStr, "%d", &ahead)
		if ahead > 0 {
			if ahead == 1 {
				reasons = append(reasons, "1 unpushed commit")
			} else {
				reasons = append(reasons, fmt.Sprintf("%d unpushed commits", ahead))
			}
		}
	} else {
		// No remote tracking — count commits ahead of main branch.
		// Verify main branch exists first; if not, count all commits on HEAD.
		refSpec := mainBranch + "..HEAD"
		if _, err := RunGit(repoPath, "rev-parse", "--verify", "refs/heads/"+mainBranch); err != nil {
			// Main branch doesn't exist locally — count all commits as local
			refSpec = "HEAD"
		}
		aheadStr, _ := RunGit(repoPath, "rev-list", "--count", refSpec)
		ahead := 0
		fmt.Sscanf(aheadStr, "%d", &ahead)
		if ahead > 0 {
			if ahead == 1 {
				reasons = append(reasons, "1 unpushed commit")
			} else {
				reasons = append(reasons, fmt.Sprintf("%d unpushed commits", ahead))
			}
		}
	}

	// Only flag for migration if there are actual uncommitted or unpushed changes.
	// Being on a non-main branch with everything clean and pushed doesn't need migration.
	if len(reasons) == 0 {
		return false, "", ""
	}

	return true, currentBranch, strings.Join(reasons, ", ")
}

func detectMainBranch(repoPath, basisBranch string) string {
	candidates := []string{basisBranch}
	if basisBranch != "main" {
		candidates = append(candidates, "main")
	}
	if basisBranch != "master" {
		candidates = append(candidates, "master")
	}

	for _, c := range candidates {
		if _, err := RunGit(repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+c); err == nil {
			return c
		}
	}
	return basisBranch
}

func getLTSType(scriptDir, ltsDir string) string {
	typeFile := filepath.Join(scriptDir, ltsDir, ".lts-type")
	if data, err := os.ReadFile(typeFile); err == nil {
		t := strings.TrimSpace(string(data))
		if t != "" {
			return t
		}
	}

	reposFile := filepath.Join(scriptDir, ltsDir, ".lts-repos")
	if _, err := os.Stat(reposFile); err == nil {
		name := strings.TrimSuffix(ltsDir, "-lts")
		if name == "erp" {
			return "erp"
		}
		return "monorepo"
	}
	return "single"
}

func getLTSRepos(scriptDir, ltsDir string) []string {
	reposFile := filepath.Join(scriptDir, ltsDir, ".lts-repos")
	data, err := os.ReadFile(reposFile)
	if err != nil {
		return []string{strings.TrimSuffix(ltsDir, "-lts")}
	}
	var repos []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			repos = append(repos, line)
		}
	}
	return repos
}

func getMultiRepoLTSDirs(scriptDir, repoName string) []string {
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		return nil
	}
	var results []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "-lts") {
			continue
		}
		reposFile := filepath.Join(scriptDir, e.Name(), ".lts-repos")
		data, err := os.ReadFile(reposFile)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.TrimSpace(line) == repoName {
				results = append(results, e.Name())
				break
			}
		}
	}
	return results
}

func listWorktrees(scriptDir, ltsDir, repoName, basisBranch string) []Worktree {
	ltsPath := filepath.Join(scriptDir, ltsDir)
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return nil
	}

	var wts []Worktree
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(ltsPath, e.Name())

		if isWorktreeDir(dirPath) {
			wt := buildWorktree(dirPath, e.Name(), repoName, ltsDir, basisBranch)
			wts = append(wts, wt)
			continue
		}

		// Check one level deeper (for ERP/monorepo subdirs)
		subEntries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			subPath := filepath.Join(dirPath, se.Name())
			if isWorktreeDir(subPath) {
				wt := buildWorktree(subPath, se.Name(), repoName, ltsDir, basisBranch)
				wts = append(wts, wt)
			}
		}
	}
	return wts
}

func listWorktreesForRepo(scriptDir, ltsDir, repoName, basisBranch string) []Worktree {
	all := listWorktrees(scriptDir, ltsDir, repoName, basisBranch)
	var filtered []Worktree
	for _, wt := range all {
		baseName := filepath.Base(wt.Path)
		if strings.HasPrefix(baseName, repoName+"-") {
			filtered = append(filtered, wt)
		}
	}
	return filtered
}

func isWorktreeDir(path string) bool {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func buildWorktree(path, dirName, repoName, ltsDir, basisBranch string) Worktree {
	branch, _ := RunGit(path, "branch", "--show-current")
	if branch == "" {
		branch = dirName
	}

	// Use actual git branch name for display (not folder name)
	// This ensures rename is reflected immediately
	displayName := branch

	status, statusText := GetWorktreeStatus(path, basisBranch)

	return Worktree{
		Name:       displayName,
		Branch:     branch,
		Path:       path,
		Status:     status,
		StatusText: statusText,
		LTSDir:     ltsDir,
	}
}
