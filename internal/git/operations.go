package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CreateResult holds the result of a worktree creation.
type CreateResult struct {
	WorktreePath  string
	WorktreeName  string
	WorkspaceFile string // path to .code-workspace file
	RepoName      string
	Branch        string
	IsExisting    bool // true if checked out an existing branch
}

// CreateLog collects status messages during creation for UI display.
type CreateLog struct {
	Steps []string
}

func (l *CreateLog) Add(msg string) {
	l.Steps = append(l.Steps, msg)
}

// ValidateBranchName checks if a branch name is valid.
func ValidateBranchName(branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	if strings.Contains(branch, " ") {
		return fmt.Errorf("branch name cannot contain spaces")
	}
	protected := []string{"main", "master", "develop", "development", "staging", "production"}
	for _, p := range protected {
		if branch == p {
			return fmt.Errorf("'%s' is a protected branch", branch)
		}
	}
	return nil
}

// ExtractSuffix gets the part after the last "/" in a branch name.
func ExtractSuffix(branch string) string {
	if idx := strings.LastIndex(branch, "/"); idx >= 0 {
		return branch[idx+1:]
	}
	return branch
}

// SanitizeForFilename cleans a string for safe use as a folder name.
func SanitizeForFilename(s string) string {
	s = strings.ReplaceAll(s, " ", "-")
	// Replace special characters
	for _, ch := range []string{"@", "#", "$", "%", "^", "&", "*", "(", ")", "!", "+", "=", "[", "]", "{", "}", "|", "\\", ":", ";", "\"", "'", "<", ">", ",", "?"} {
		s = strings.ReplaceAll(s, ch, "-")
	}
	// Collapse multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	return s
}

// GenerateUniqueWorktreeName creates a collision-free worktree folder name.
func GenerateUniqueWorktreeName(repoName, suffix, ltsPath string) string {
	safeSuffix := SanitizeForFilename(suffix)
	baseName := repoName + "-" + safeSuffix
	name := baseName

	counter := 2
	for {
		checkPath := filepath.Join(ltsPath, name)
		if _, err := os.Stat(checkPath); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s-%d", baseName, counter)
		counter++
	}
	return name
}

// GetExistingBranches returns local and remote-only branches for a repo.
func GetExistingBranches(repoPath string) (local []string, remoteOnly []string) {
	// Local branches (exclude main/master)
	out, err := RunGit(repoPath, "branch", "--format=%(refname:short)")
	if err == nil && out != "" {
		for _, b := range strings.Split(out, "\n") {
			b = strings.TrimSpace(b)
			if b != "" && b != "main" && b != "master" {
				local = append(local, b)
			}
		}
	}

	// Remote branches not present locally
	rOut, err := RunGit(repoPath, "branch", "-r", "--format=%(refname:short)")
	if err == nil && rOut != "" {
		for _, rb := range strings.Split(rOut, "\n") {
			rb = strings.TrimSpace(rb)
			if rb == "" || !strings.HasPrefix(rb, "origin/") {
				continue
			}
			name := strings.TrimPrefix(rb, "origin/")
			if name == "main" || name == "master" || name == "HEAD" {
				continue
			}
			// Check if exists locally
			_, err := RunGit(repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
			if err != nil {
				// Not local, it's remote-only
				remoteOnly = append(remoteOnly, name)
			}
		}
	}
	return
}

// EnsureCleanMain prepares a repo for worktree creation:
// stashes changes, switches to main, pulls latest.
func EnsureCleanMain(repoPath, basisBranch string, log *CreateLog) error {
	repoName := filepath.Base(repoPath)
	mainBranch := detectMainBranch(repoPath, basisBranch)
	if mainBranch == "" {
		return fmt.Errorf("could not detect main branch for %s", repoName)
	}

	// Check for uncommitted changes
	_, diffErr := RunGit(repoPath, "diff", "--quiet", "HEAD")
	_, cachedErr := RunGit(repoPath, "diff", "--cached", "--quiet")

	if diffErr != nil || cachedErr != nil {
		log.Add(fmt.Sprintf("Stashing uncommitted changes in %s", repoName))
		msg := fmt.Sprintf("LTS auto-stash %s", time.Now().Format("2006-01-02 15:04:05"))
		RunGit(repoPath, "stash", "push", "-m", msg)
	}

	// Switch to main branch
	currentBranch, _ := RunGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if currentBranch != mainBranch {
		log.Add(fmt.Sprintf("Switching %s to %s", repoName, mainBranch))
		_, err := RunGit(repoPath, "checkout", mainBranch)
		if err != nil {
			return fmt.Errorf("failed to checkout %s in %s", mainBranch, repoName)
		}
	}

	// Pull latest (best-effort)
	log.Add(fmt.Sprintf("Pulling latest for %s", repoName))
	_, pullErr := RunGit(repoPath, "pull", "origin", mainBranch, "--ff-only")
	if pullErr != nil {
		log.Add(fmt.Sprintf("Pull skipped for %s (continuing with local state)", repoName))
	}

	return nil
}

// CheckOngoingOperations checks for rebase/merge/cherry-pick in progress.
func CheckOngoingOperations(repoPath string) error {
	gitDir := filepath.Join(repoPath, ".git")
	// Handle worktrees where .git is a file
	info, err := os.Stat(gitDir)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		// It's a worktree .git file, read the actual git dir
		data, _ := os.ReadFile(gitDir)
		content := strings.TrimSpace(string(data))
		if strings.HasPrefix(content, "gitdir: ") {
			gitDir = strings.TrimPrefix(content, "gitdir: ")
		}
	}

	checks := map[string]string{
		"rebase-merge":  "rebase",
		"rebase-apply":  "rebase",
		"MERGE_HEAD":    "merge",
		"CHERRY_PICK_HEAD": "cherry-pick",
	}
	for file, op := range checks {
		if _, err := os.Stat(filepath.Join(gitDir, file)); err == nil {
			return fmt.Errorf("%s has an ongoing %s — resolve it first", filepath.Base(repoPath), op)
		}
	}
	return nil
}

// CreateSingleRepoWorktree creates a worktree for a single repository.
// This matches mode_create_worktrees from lts.sh (for 1 worktree).
func CreateSingleRepoWorktree(repoPath, scriptDir, branch, basisBranch, pkgManager, aiCliCommand string, log *CreateLog) (*CreateResult, error) {
	repoName := filepath.Base(repoPath)
	ltsDir := repoName + "-lts"
	ltsPath := filepath.Join(scriptDir, ltsDir)

	// Create LTS directory
	if err := os.MkdirAll(ltsPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create LTS directory: %w", err)
	}

	// Check for ongoing operations
	if err := CheckOngoingOperations(repoPath); err != nil {
		return nil, err
	}

	// Prune orphaned worktree entries
	RunGit(repoPath, "worktree", "prune")

	// Ensure clean main
	if err := EnsureCleanMain(repoPath, basisBranch, log); err != nil {
		return nil, err
	}

	// Fetch remote
	log.Add(fmt.Sprintf("Fetching remote for %s", repoName))
	RunGit(repoPath, "fetch", "origin")

	// Detect main branch
	mainBranch := detectMainBranch(repoPath, basisBranch)

	// Generate worktree name
	suffix := ExtractSuffix(branch)
	wtName := GenerateUniqueWorktreeName(repoName, suffix, ltsPath)
	wtPath := filepath.Join(ltsPath, wtName)

	// Create worktree based on branch existence
	result := &CreateResult{
		WorktreePath: wtPath,
		WorktreeName: wtName,
		RepoName:     repoName,
		Branch:       branch,
	}

	err := createWorktreeWithBranchHandling(repoPath, wtPath, branch, mainBranch, log)
	if err != nil {
		return nil, err
	}

	// Copy .env files
	log.Add("Copying .env files")
	copyEnvFilesRecursive(repoPath, wtPath)

	// Install dependencies
	runPackageInstall(wtPath, pkgManager, log)

	// Generate individual workspace
	log.Add("Generating workspace file")
	wsFile := generateIndividualWorkspace(ltsPath, wtName, pkgManager, aiCliCommand)
	result.WorkspaceFile = wsFile

	return result, nil
}

// CreateMonorepoWorktrees creates worktrees across multiple repos with the same branch.
// This matches mode_create_monorepo_worktrees from lts.sh.
func CreateMonorepoWorktrees(repoNames []string, scriptDir, branch, basisBranch, pkgManager, aiCliCommand string, log *CreateLog) ([]*CreateResult, error) {
	if len(repoNames) == 0 {
		return nil, fmt.Errorf("no repositories selected")
	}

	// Single repo shortcut — use standard naming
	if len(repoNames) == 1 {
		repoPath := filepath.Join(scriptDir, repoNames[0])
		result, err := CreateSingleRepoWorktree(repoPath, scriptDir, branch, basisBranch, pkgManager, aiCliCommand, log)
		if err != nil {
			return nil, err
		}
		return []*CreateResult{result}, nil
	}

	// Multi-repo monorepo mode
	suffix := ExtractSuffix(branch)
	safeSuffix := SanitizeForFilename(suffix)

	// Generate LTS directory name (sorted repos joined with -)
	sorted := make([]string, len(repoNames))
	copy(sorted, repoNames)
	sort.Strings(sorted)
	ltsDir := strings.Join(sorted, "-") + "-lts"
	ltsPath := filepath.Join(scriptDir, ltsDir)

	// Branch subdirectory
	ltsPrefix := strings.TrimSuffix(ltsDir, "-lts")
	branchSubdir := ltsPrefix + "-" + safeSuffix
	branchSubdirPath := filepath.Join(ltsPath, branchSubdir)

	// Create directories
	if err := os.MkdirAll(branchSubdirPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create monorepo directory: %w", err)
	}

	// Write metadata
	os.WriteFile(filepath.Join(ltsPath, ".lts-type"), []byte("monorepo\n"), 0644)

	var results []*CreateResult
	var repoWTPairs []string

	for _, repoName := range sorted {
		repoPath := filepath.Join(scriptDir, repoName)

		log.Add(fmt.Sprintf("Processing %s", repoName))

		// Check for ongoing operations
		if err := CheckOngoingOperations(repoPath); err != nil {
			log.Add(fmt.Sprintf("Skipping %s: %s", repoName, err.Error()))
			continue
		}

		// Prune and prepare
		RunGit(repoPath, "worktree", "prune")

		if err := EnsureCleanMain(repoPath, basisBranch, log); err != nil {
			log.Add(fmt.Sprintf("Skipping %s: %s", repoName, err.Error()))
			continue
		}

		RunGit(repoPath, "fetch", "origin")

		mainBranch := detectMainBranch(repoPath, basisBranch)

		// Generate worktree name inside branch subdir
		wtName := GenerateUniqueWorktreeName(repoName, safeSuffix, branchSubdirPath)
		wtPath := filepath.Join(branchSubdirPath, wtName)

		// Check if worktree already exists
		if _, err := os.Stat(wtPath); err == nil {
			if isWorktreeDir(wtPath) {
				log.Add(fmt.Sprintf("Worktree already exists: %s", wtName))
				results = append(results, &CreateResult{
					WorktreePath: wtPath,
					WorktreeName: wtName,
					RepoName:     repoName,
					Branch:       branch,
					IsExisting:   true,
				})
				repoWTPairs = append(repoWTPairs, repoName+":"+wtName)
				continue
			}
		}

		err := createWorktreeWithBranchHandling(repoPath, wtPath, branch, mainBranch, log)
		if err != nil {
			log.Add(fmt.Sprintf("Failed to create worktree for %s: %s", repoName, err.Error()))
			continue
		}

		// Copy .env files
		copyEnvFilesRecursive(repoPath, wtPath)

		// Install dependencies
		runPackageInstall(wtPath, pkgManager, log)

		// Generate individual workspace
		wsFile := generateIndividualWorkspace(branchSubdirPath, wtName, pkgManager, aiCliCommand)

		results = append(results, &CreateResult{
			WorktreePath:  wtPath,
			WorktreeName:  wtName,
			WorkspaceFile: wsFile,
			RepoName:      repoName,
			Branch:        branch,
		})
		repoWTPairs = append(repoWTPairs, repoName+":"+wtName)
	}

	if len(results) == 0 {
		// Cleanup empty dirs
		os.Remove(branchSubdirPath)
		entries, _ := os.ReadDir(ltsPath)
		if len(entries) <= 2 { // only .lts-type and maybe .lts-repos
			os.RemoveAll(ltsPath)
		}
		return nil, fmt.Errorf("failed to create any worktrees")
	}

	// Write/merge .lts-repos metadata
	writeReposMetadata(ltsPath, sorted)

	// Generate monorepo workspace if 2+ succeeded
	if len(results) >= 2 {
		generateMonorepoWorkspace(branchSubdirPath, safeSuffix, repoWTPairs)
	}

	return results, nil
}

// createWorktreeWithBranchHandling creates a worktree, handling the three cases:
// 1. Branch exists locally
// 2. Branch exists on remote only
// 3. Brand new branch
func createWorktreeWithBranchHandling(repoPath, wtPath, branch, mainBranch string, log *CreateLog) error {
	repoName := filepath.Base(repoPath)

	// Check if branch exists locally
	_, localErr := RunGit(repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if localErr == nil {
		// Branch exists locally — check if already checked out
		wtList, _ := RunGit(repoPath, "worktree", "list")
		if strings.Contains(wtList, "["+branch+"]") {
			return fmt.Errorf("branch %s is already checked out in another worktree", branch)
		}
		log.Add(fmt.Sprintf("Checking out existing local branch %s for %s", branch, repoName))
		_, err := RunGit(repoPath, "worktree", "add", wtPath, branch)
		return err
	}

	// Check if branch exists on remote
	_, remoteErr := RunGit(repoPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	if remoteErr == nil {
		log.Add(fmt.Sprintf("Checking out remote branch %s for %s", branch, repoName))
		_, err := RunGit(repoPath, "worktree", "add", "--track", "-b", branch, wtPath, "origin/"+branch)
		if err != nil {
			// Fallback: maybe local branch was created between check and add
			_, err = RunGit(repoPath, "worktree", "add", wtPath, branch)
		}
		return err
	}

	// New branch from main
	log.Add(fmt.Sprintf("Creating new branch %s for %s from %s", branch, repoName, mainBranch))
	_, err := RunGit(repoPath, "worktree", "add", "-b", branch, wtPath, mainBranch)
	return err
}

// copyEnvFilesRecursive copies .env* files preserving directory structure.
func copyEnvFilesRecursive(srcRoot, dstRoot string) {
	filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Skip directories we don't want to traverse
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "node_modules" || base == ".git" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		// Match .env* files
		if strings.HasPrefix(info.Name(), ".env") {
			relPath, _ := filepath.Rel(srcRoot, path)
			dstPath := filepath.Join(dstRoot, relPath)
			os.MkdirAll(filepath.Dir(dstPath), 0755)
			if data, err := os.ReadFile(path); err == nil {
				os.WriteFile(dstPath, data, 0644)
			}
		}
		return nil
	})
}

// writeReposMetadata writes/merges the .lts-repos file.
func writeReposMetadata(ltsPath string, repos []string) {
	reposFile := filepath.Join(ltsPath, ".lts-repos")

	// Read existing repos if file exists
	existing := make(map[string]bool)
	if data, err := os.ReadFile(reposFile); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				existing[line] = true
			}
		}
	}

	// Merge
	for _, r := range repos {
		existing[r] = true
	}

	// Sort and write
	var all []string
	for r := range existing {
		all = append(all, r)
	}
	sort.Strings(all)
	os.WriteFile(reposFile, []byte(strings.Join(all, "\n")+"\n"), 0644)
}

// generateIndividualWorkspace creates a .code-workspace file for a single worktree.
// runPackageInstall runs the package manager install in a worktree.
func runPackageInstall(wtPath, pkgManager string, log *CreateLog) {
	if pkgManager == "" {
		return
	}
	// Check if package manager is available
	if _, err := exec.LookPath(pkgManager); err != nil {
		log.Add(fmt.Sprintf("Package manager '%s' not found, skipping install", pkgManager))
		return
	}
	// Check for package.json
	if _, err := os.Stat(filepath.Join(wtPath, "package.json")); os.IsNotExist(err) {
		return
	}

	log.Add(fmt.Sprintf("Installing dependencies with %s", pkgManager))

	var args []string
	switch pkgManager {
	case "pnpm":
		args = []string{"install", "--silent"}
	case "npm":
		args = []string{"install", "--silent"}
	case "yarn":
		args = []string{"install", "--silent"}
	case "bun":
		args = []string{"install", "--silent"}
	default:
		args = []string{"install"}
	}

	cmd := exec.Command(pkgManager, args...)
	cmd.Dir = wtPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Add(fmt.Sprintf("%s install completed with warnings", pkgManager))
		_ = out
	} else {
		log.Add("Dependencies installed")
	}
}

func generateIndividualWorkspace(ltsPath, wtName, pkgMgr, aiCliCmd string) string {
	wsPath := filepath.Join(ltsPath, wtName+".code-workspace")
	pmLabel := strings.ToUpper(pkgMgr)

	// Derive AI CLI label from command (first word, title-cased)
	aiLabel := "Claude"
	if aiCliCmd != "" {
		parts := strings.Fields(aiCliCmd)
		if len(parts) > 0 {
			name := parts[0]
			if len(name) > 0 {
				aiLabel = strings.ToUpper(name[:1]) + name[1:]
			}
		}
	}
	if aiCliCmd == "" {
		aiCliCmd = "claude"
	}

	content := fmt.Sprintf(`{
  "folders": [
    { "path": "%s" }
  ],
  "settings": {
    "terminal.integrated.defaultProfile.osx": "zsh"
  },
  "tasks": {
    "version": "2.0.0",
    "tasks": [
      {
        "label": "%s",
        "type": "shell",
        "command": "%s",
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "always", "panel": "new" }
      },
      {
        "label": "%s",
        "type": "shell",
        "command": "echo '' && echo '📦 %s Terminal' && echo '' && exec $SHELL",
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "always", "panel": "dedicated", "group": "lts" }
      },
      {
        "label": "Git",
        "type": "shell",
        "command": "git status; echo '' && exec $SHELL",
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "always", "panel": "dedicated", "group": "lts" }
      }
    ]
  }
}
`, wtName, aiLabel, aiCliCmd, pmLabel, pmLabel)

	os.WriteFile(wsPath, []byte(content), 0644)
	return wsPath
}

// generateMonorepoWorkspace creates a workspace for multi-repo monorepo-like worktrees.
func generateMonorepoWorkspace(branchSubdirPath, suffix string, repoWTPairs []string) {
	wsPath := filepath.Join(branchSubdirPath, "monorepo-"+suffix+".code-workspace")

	var folders []string
	for _, pair := range repoWTPairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) < 2 {
			continue
		}
		repo, wt := parts[0], parts[1]
		folders = append(folders, fmt.Sprintf(`    { "name": "%s - %s", "path": "%s" }`, repo, suffix, wt))
	}

	content := fmt.Sprintf(`{
  "folders": [
%s
  ],
  "settings": {
    "terminal.integrated.defaultProfile.osx": "zsh"
  }
}
`, strings.Join(folders, ",\n"))

	os.WriteFile(wsPath, []byte(content), 0644)
}

// RefreshRepo fetches latest and updates the main branch ref.
func RefreshRepo(repoPath, basisBranch string) error {
	_, err := RunGit(repoPath, "fetch", "origin")
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	mainBranch := detectMainBranch(repoPath, basisBranch)
	if mainBranch == "" {
		return nil
	}

	currentBranch, _ := RunGit(repoPath, "branch", "--show-current")
	if currentBranch == mainBranch {
		RunGit(repoPath, "pull", "origin", mainBranch, "--ff-only")
	} else {
		RunGit(repoPath, "fetch", "origin", mainBranch+":"+mainBranch)
	}

	return nil
}

// RefreshAllRepos refreshes all repos in the script directory.
// Returns (refreshed count, failed repo names, error).
func RefreshAllRepos(scriptDir string, getBasisBranch BasisBranchResolver) (int, []string, error) {
	repos := DiscoverRepos(scriptDir, getBasisBranch)
	refreshed := 0
	var failed []string
	var lastErr error
	for _, r := range repos {
		if r.IsMonorepo {
			continue
		}
		if err := RefreshRepo(r.Path, getBasisBranch(r.Name)); err != nil {
			failed = append(failed, r.Name)
			lastErr = err
		} else {
			refreshed++
		}
	}
	if lastErr != nil && refreshed == 0 {
		return 0, failed, lastErr
	}
	return refreshed, failed, nil
}

// DeleteWorktree removes a worktree, its workspace file, and optionally its branch.
func DeleteWorktree(repoPath, wtPath, branch string, deleteRemote bool) error {
	// Safety: validate wtPath is inside an -lts directory
	if !strings.Contains(wtPath, "-lts") {
		return fmt.Errorf("refusing to delete path outside LTS directory: %s", wtPath)
	}

	// Remove individual workspace file (sibling of worktree dir)
	wtName := filepath.Base(wtPath)
	wsFile := filepath.Join(filepath.Dir(wtPath), wtName+".code-workspace")
	os.Remove(wsFile) // best-effort

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		// Missing worktree — just prune git refs
		RunGit(repoPath, "worktree", "prune")
	} else {
		_, err := RunGit(repoPath, "worktree", "remove", "--force", wtPath)
		if err != nil {
			// Fallback: manual removal only if it's a valid worktree dir
			if isWorktreeDir(wtPath) {
				os.RemoveAll(wtPath)
			}
			RunGit(repoPath, "worktree", "prune")
		}
	}

	if branch != "" && !IsProtectedBranch(branch) {
		RunGit(repoPath, "branch", "-D", branch)
	}

	if deleteRemote && branch != "" && !IsProtectedBranch(branch) {
		RunGit(repoPath, "push", "origin", "--delete", branch)
	}

	// Clean up empty parent directories inside -lts structure
	cleanEmptyLTSDirs(filepath.Dir(wtPath))

	return nil
}

// cleanEmptyLTSDirs removes empty directories up to (but not including) the -lts root.
func cleanEmptyLTSDirs(dir string) {
	for {
		base := filepath.Base(dir)
		if strings.HasSuffix(base, "-lts") {
			// Check if the LTS dir itself is now empty (ignoring metadata files)
			entries, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			hasContent := false
			for _, e := range entries {
				name := e.Name()
				if name == ".lts-type" || name == ".lts-repos" {
					continue
				}
				if strings.HasSuffix(name, ".code-workspace") {
					continue
				}
				hasContent = true
				break
			}
			if !hasContent {
				os.RemoveAll(dir)
			}
			return
		}
		// Not the -lts root — remove if empty
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}

// RebaseWorktree rebases a worktree onto its main branch.
func RebaseWorktree(wtPath, mainBranch string) error {
	status, _ := RunGit(wtPath, "status", "--porcelain")
	hasChanges := status != ""

	if hasChanges {
		_, err := RunGit(wtPath, "stash", "push", "-m", "lts-rebase-auto-stash")
		if err != nil {
			return fmt.Errorf("failed to stash changes: %w", err)
		}
	}

	_, err := RunGit(wtPath, "rebase", mainBranch)
	if err != nil {
		// Abort the failed rebase
		_, abortErr := RunGit(wtPath, "rebase", "--abort")

		if hasChanges {
			// Restore stashed changes
			_, popErr := RunGit(wtPath, "stash", "pop")
			if popErr != nil {
				return fmt.Errorf("rebase conflict — aborted, but failed to restore stashed changes (run 'git stash pop' manually)")
			}
		}

		if abortErr != nil {
			return fmt.Errorf("rebase conflict — abort may have failed, check repo state manually")
		}
		return fmt.Errorf("rebase conflict — aborted, changes restored")
	}

	if hasChanges {
		_, popErr := RunGit(wtPath, "stash", "pop")
		if popErr != nil {
			return fmt.Errorf("rebase succeeded but failed to restore stashed changes (run 'git stash pop' manually)")
		}
	}

	return nil
}

// RenameWorktreeBranch renames the branch in a worktree.
func RenameWorktreeBranch(wtPath, newBranch string) error {
	_, err := RunGit(wtPath, "branch", "-m", newBranch)
	return err
}

// CleanupMergedCleanables finds and deletes all merged/cleanable worktrees.
// Also cleans up workspace files and empty directories.
func CleanupMergedCleanables(scriptDir string, getBasisBranch BasisBranchResolver) (int, error) {
	repos := DiscoverRepos(scriptDir, getBasisBranch)
	cleaned := 0

	for _, repo := range repos {
		for _, wt := range repo.Worktrees {
			if wt.Status == StatusMergedCleanable {
				repoPath := repo.Path
				// For monorepo cards, resolve the actual repo path
				if repo.IsMonorepo && len(repo.RepoNames) > 0 {
					repoPath = filepath.Join(scriptDir, repo.RepoNames[0])
				}
				if repoPath == "" {
					continue
				}
				err := DeleteWorktree(repoPath, wt.Path, wt.Branch, false)
				if err == nil {
					cleaned++
				}
			}
		}
	}

	return cleaned, nil
}

func IsProtectedBranch(branch string) bool {
	protected := []string{"main", "master", "develop", "development", "staging", "production"}
	for _, p := range protected {
		if branch == p {
			return true
		}
	}
	if strings.HasPrefix(branch, "release/") {
		return true
	}
	return false
}
