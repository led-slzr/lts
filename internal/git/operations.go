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

// LogFunc is a callback for streaming log messages to the UI.
// ctx is the repo/operation context shown as [ctx] in the log panel.
type LogFunc func(ctx, msg string, isError bool)

// WithContext returns a LogFunc that always prefixes the given context.
func WithContext(logFn LogFunc, ctx string) LogFunc {
	return func(_ string, msg string, isError bool) {
		logFn(ctx, msg, isError)
	}
}

// CreateLog collects status messages during creation for UI display.
type CreateLog struct {
	Steps   []string
	Stream  LogFunc // optional real-time callback
	Context string  // repo context for streaming
}

func (l *CreateLog) Add(msg string) {
	l.Steps = append(l.Steps, msg)
	if l.Stream != nil {
		l.Stream(l.Context, msg, false)
	}
}

func (l *CreateLog) AddError(msg string) {
	l.Steps = append(l.Steps, msg)
	if l.Stream != nil {
		l.Stream(l.Context, msg, true)
	}
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

// BranchToDirName converts a branch name to a directory-safe name.
// "feat/login-system" → "feat-login-system", "hotfix" → "hotfix"
func BranchToDirName(branch string) string {
	return SanitizeForFilename(strings.ReplaceAll(branch, "/", "-"))
}

// generateUniqueName returns a collision-free name inside parentDir.
func generateUniqueName(baseName, parentDir string) string {
	name := baseName
	counter := 2
	for {
		checkPath := filepath.Join(parentDir, name)
		if _, err := os.Stat(checkPath); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s-%d", baseName, counter)
		counter++
	}
	return name
}

// BranchInfo holds a branch name with its source and last commit date.
type BranchInfo struct {
	Name     string
	IsLocal  bool // true = local, false = remote-only
	Date     string // formatted date string (e.g. "2025-03-28")
	UnixTime int64  // for sorting by recency
}

// GetBranchesWithDates returns all branches (local + remote-only) for the selected repos,
// sorted: local first, then remote, each group sorted by most recent commit.
// Deduplicates: if a branch exists both locally and remotely, it's shown as local.
func GetBranchesWithDates(repoPaths []string) []BranchInfo {
	seen := make(map[string]*BranchInfo)

	for _, repoPath := range repoPaths {
		// Local branches with date
		out, err := RunGit(repoPath, "for-each-ref",
			"--sort=-committerdate",
			"--format=%(refname:short)\t%(committerdate:unix)\t%(committerdate:relative)",
			"refs/heads/")
		if err == nil && out != "" {
			for _, line := range strings.Split(out, "\n") {
				parts := strings.SplitN(strings.TrimSpace(line), "\t", 3)
				if len(parts) < 3 || parts[0] == "" {
					continue
				}
				name := parts[0]
				if name == "main" || name == "master" {
					continue
				}
				if _, exists := seen[name]; !exists {
					var unix int64
					fmt.Sscanf(parts[1], "%d", &unix)
					seen[name] = &BranchInfo{
						Name:     name,
						IsLocal:  true,
						Date:     parts[2],
						UnixTime: unix,
					}
				}
			}
		}

		// Remote branches
		rOut, err := RunGit(repoPath, "for-each-ref",
			"--sort=-committerdate",
			"--format=%(refname:short)\t%(committerdate:unix)\t%(committerdate:relative)",
			"refs/remotes/origin/")
		if err == nil && rOut != "" {
			for _, line := range strings.Split(rOut, "\n") {
				parts := strings.SplitN(strings.TrimSpace(line), "\t", 3)
				if len(parts) < 3 || parts[0] == "" {
					continue
				}
				name := strings.TrimPrefix(parts[0], "origin/")
				if name == "main" || name == "master" || name == "HEAD" {
					continue
				}
				if _, exists := seen[name]; !exists {
					var unix int64
					fmt.Sscanf(parts[1], "%d", &unix)
					seen[name] = &BranchInfo{
						Name:     name,
						IsLocal:  false,
						Date:     parts[2],
						UnixTime: unix,
					}
				}
			}
		}
	}

	// Collect and sort: local first (by recency), then remote (by recency)
	var local, remote []BranchInfo
	for _, b := range seen {
		if b.IsLocal {
			local = append(local, *b)
		} else {
			remote = append(remote, *b)
		}
	}
	sort.Slice(local, func(i, j int) bool { return local[i].UnixTime > local[j].UnixTime })
	sort.Slice(remote, func(i, j int) bool { return remote[i].UnixTime > remote[j].UnixTime })

	result := make([]BranchInfo, 0, len(local)+len(remote))
	result = append(result, local...)
	result = append(result, remote...)
	return result
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

	log.Context = repoName

	if diffErr != nil || cachedErr != nil {
		log.Add("Stashing uncommitted changes")
		msg := fmt.Sprintf("LTS auto-stash %s", time.Now().Format("2006-01-02 15:04:05"))
		RunGit(repoPath, "stash", "push", "-m", msg)
	}

	// Switch to main branch
	currentBranch, _ := RunGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if currentBranch != mainBranch {
		log.Add("Switching to " + mainBranch)
		_, err := RunGit(repoPath, "checkout", mainBranch)
		if err != nil {
			return fmt.Errorf("failed to checkout %s in %s", mainBranch, repoName)
		}
	}

	// Pull latest (best-effort)
	log.Add("Pulling latest")
	_, pullErr := RunGit(repoPath, "pull", "origin", mainBranch, "--ff-only")
	if pullErr != nil {
		log.AddError("Pull skipped (continuing with local state)")
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
func CreateSingleRepoWorktree(repoPath, scriptDir, branch, basisBranch, pkgManager, aiCliCommand, ideCommand string, openEnvInIDE bool, log *CreateLog) (*CreateResult, error) {
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
	log.Context = repoName
	log.Add("Fetching remote")
	RunGit(repoPath, "fetch", "origin")

	// Detect main branch
	mainBranch := detectMainBranch(repoPath, basisBranch)

	// Generate worktree name: feat/login → feat-login
	wtName := generateUniqueName(BranchToDirName(branch), ltsPath)
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
	wsFile := generateIndividualWorkspace(ltsPath, wtName, pkgManager, aiCliCommand, ideCommand, openEnvInIDE)
	result.WorkspaceFile = wsFile

	return result, nil
}

// CreateMonorepoWorktrees creates worktrees across multiple repos with the same branch.
// This matches mode_create_monorepo_worktrees from lts.sh.
func CreateMonorepoWorktrees(repoNames []string, scriptDir, branch, basisBranch, pkgManager, aiCliCommand, ideCommand string, openEnvInIDE bool, log *CreateLog) ([]*CreateResult, error) {
	if len(repoNames) == 0 {
		return nil, fmt.Errorf("no repositories selected")
	}

	// Single repo shortcut — use standard naming
	if len(repoNames) == 1 {
		repoPath := filepath.Join(scriptDir, repoNames[0])
		result, err := CreateSingleRepoWorktree(repoPath, scriptDir, branch, basisBranch, pkgManager, aiCliCommand, ideCommand, openEnvInIDE, log)
		if err != nil {
			return nil, err
		}
		return []*CreateResult{result}, nil
	}

	// Multi-repo monorepo mode
	branchDirName := BranchToDirName(branch)

	// Generate LTS directory name (sorted repos joined with -)
	sorted := make([]string, len(repoNames))
	copy(sorted, repoNames)
	sort.Strings(sorted)
	ltsDir := strings.Join(sorted, "-") + "-lts"
	ltsPath := filepath.Join(scriptDir, ltsDir)

	// Branch subdirectory: feat/integration → feat-integration
	branchSubdir := branchDirName
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

		log.Context = repoName
		log.Add("Processing")

		// Check for ongoing operations
		if err := CheckOngoingOperations(repoPath); err != nil {
			log.AddError("Skipping: " + err.Error())
			continue
		}

		// Prune and prepare
		RunGit(repoPath, "worktree", "prune")

		if err := EnsureCleanMain(repoPath, basisBranch, log); err != nil {
			log.AddError("Skipping: " + err.Error())
			continue
		}

		RunGit(repoPath, "fetch", "origin")

		mainBranch := detectMainBranch(repoPath, basisBranch)

		// Generate worktree name inside branch subdir: core-feat-integration
		wtName := generateUniqueName(repoName+"-"+branchDirName, branchSubdirPath)
		wtPath := filepath.Join(branchSubdirPath, wtName)

		// Check if worktree already exists
		if _, err := os.Stat(wtPath); err == nil {
			if isWorktreeDir(wtPath) {
				log.Add("Worktree already exists: " + wtName)
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
			log.AddError("Failed to create worktree: " + err.Error())
			continue
		}

		// Copy .env files
		copyEnvFilesRecursive(repoPath, wtPath)

		// Install dependencies
		runPackageInstall(wtPath, pkgManager, log)

		results = append(results, &CreateResult{
			WorktreePath: wtPath,
			WorktreeName: wtName,
			RepoName:     repoName,
			Branch:       branch,
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

	// Generate monorepo workspace (only workspace file for monorepo setups)
	wsFile := generateMonorepoWorkspace(branchSubdirPath, branchDirName, repoWTPairs, aiCliCommand, ideCommand, openEnvInIDE)
	for _, r := range results {
		r.WorkspaceFile = wsFile
	}

	return results, nil
}

// createWorktreeWithBranchHandling creates a worktree, handling the three cases:
// 1. Branch exists locally
// 2. Branch exists on remote only
// 3. Brand new branch
func createWorktreeWithBranchHandling(repoPath, wtPath, branch, mainBranch string, log *CreateLog) error {
	log.Context = filepath.Base(repoPath)

	// Check if branch exists locally
	_, localErr := RunGit(repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if localErr == nil {
		// Branch exists locally — check if already checked out
		wtList, _ := RunGit(repoPath, "worktree", "list")
		if strings.Contains(wtList, "["+branch+"]") {
			return fmt.Errorf("branch %s is already checked out in another worktree", branch)
		}
		log.Add("Checking out existing local branch " + branch)
		_, err := RunGit(repoPath, "worktree", "add", wtPath, branch)
		return err
	}

	// Check if branch exists on remote
	_, remoteErr := RunGit(repoPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	if remoteErr == nil {
		log.Add("Checking out remote branch " + branch)
		_, err := RunGit(repoPath, "worktree", "add", "--track", "-b", branch, wtPath, "origin/"+branch)
		if err != nil {
			// Fallback: maybe local branch was created between check and add
			_, err = RunGit(repoPath, "worktree", "add", wtPath, branch)
		}
		return err
	}

	// New branch from main
	log.Add("Creating new branch " + branch + " from " + mainBranch)
	_, err := RunGit(repoPath, "worktree", "add", "-b", branch, wtPath, mainBranch)
	return err
}

// findEnvFiles returns relative paths of all .env* files under root,
// skipping node_modules, .git, dist, build directories.
func findEnvFiles(root string) []string {
	var files []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "node_modules" || base == ".git" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(info.Name(), ".env") {
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
		}
		return nil
	})
	return files
}

// findExactEnvFiles returns relative paths of files named exactly ".env" under root,
// skipping node_modules, .git, dist, build directories.
// Unlike findEnvFiles, this does NOT match .env.example, .env.local, etc.
func findExactEnvFiles(root string) []string {
	var files []string
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "node_modules" || base == ".git" || base == "dist" || base == "build" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == ".env" {
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
		}
		return nil
	})
	return files
}

// shellQuotePaths single-quotes each path for safe shell interpolation,
// escaping any embedded single quotes.
func shellQuotePaths(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = "'" + strings.ReplaceAll(p, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
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
		log.AddError("Package manager '" + pkgManager + "' not found, skipping install")
		return
	}
	// Check for package.json
	if _, err := os.Stat(filepath.Join(wtPath, "package.json")); os.IsNotExist(err) {
		return
	}

	log.Add("Installing dependencies with " + pkgManager)

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
		log.AddError(pkgManager + " install completed with warnings")
		_ = out
	} else {
		log.Add("Dependencies installed")
	}
}

func generateIndividualWorkspace(ltsPath, wtName, pkgMgr, aiCliCmd, ideCmd string, openEnv bool) string {
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

	var tasks []string
	tasks = append(tasks,
		fmt.Sprintf(`      {
        "label": "%s",
        "type": "shell",
        "command": "%s",
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "always", "panel": "new" }
      }`, aiLabel, aiCliCmd),
		fmt.Sprintf(`      {
        "label": "%s",
        "type": "shell",
        "command": "echo '' && echo '📦 %s Terminal' && echo '' && exec $SHELL",
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "always", "panel": "dedicated", "group": "lts" }
      }`, pmLabel, pmLabel),
		`      {
        "label": "Git",
        "type": "shell",
        "command": "git status; echo '' && exec $SHELL",
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "always", "panel": "dedicated", "group": "lts" }
      }`)

	// Auto-open .env files
	if openEnv && ideCmd != "" {
		wtPath := filepath.Join(ltsPath, wtName)
		if envFiles := findExactEnvFiles(wtPath); len(envFiles) > 0 {
			tasks = append(tasks, fmt.Sprintf(`      {
        "label": "Open .env",
        "type": "shell",
        "command": "%s %s",
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "never", "close": true }
      }`, ideCmd, shellQuotePaths(envFiles)))
		}
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
%s
    ]
  }
}
`, wtName, strings.Join(tasks, ",\n"))

	os.WriteFile(wsPath, []byte(content), 0644)
	return wsPath
}

// generateMonorepoWorkspace creates a workspace for multi-repo monorepo-like worktrees.
// Includes: all repo folders, AI CLI task at parent dir, and one terminal per repo.
func generateMonorepoWorkspace(branchSubdirPath, suffix string, repoWTPairs []string, aiCliCmd, ideCmd string, openEnv bool) string {
	wsPath := filepath.Join(branchSubdirPath, "monorepo-"+suffix+".code-workspace")

	// Derive AI CLI label from command
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

	var folders []string
	var repoTasks []string
	firstFolder := ""
	for _, pair := range repoWTPairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) < 2 {
			continue
		}
		repo, wt := parts[0], parts[1]
		folderName := repo + " - " + suffix
		if firstFolder == "" {
			firstFolder = folderName
		}
		folders = append(folders, fmt.Sprintf(`    { "name": "%s", "path": "%s" }`, folderName, wt))

		repoTasks = append(repoTasks, fmt.Sprintf(`      {
        "label": "%s",
        "type": "shell",
        "command": "echo '' && echo '📂 %s Terminal (%s)' && echo '' && exec $SHELL",
        "options": { "cwd": "${workspaceFolder:%s}" },
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "always", "panel": "dedicated", "group": "lts" }
      }`, repo, repo, suffix, folderName))
	}

	// Set cwd to the branch subdirectory (parent of all repo folders) so the
	// AI CLI opens in e.g. feat-login/ rather than inside a single repo folder.
	var aiTask string
	if firstFolder != "" {
		aiTask = fmt.Sprintf(`      {
        "label": "%s",
        "type": "shell",
        "command": "%s",
        "options": { "cwd": "${workspaceFolder:%s}/.." },
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "always", "panel": "new" }
      }`, aiLabel, aiCliCmd, firstFolder)
	} else {
		aiTask = fmt.Sprintf(`      {
        "label": "%s",
        "type": "shell",
        "command": "%s",
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "always", "panel": "new" }
      }`, aiLabel, aiCliCmd)
	}

	allTasks := []string{aiTask}
	allTasks = append(allTasks, repoTasks...)

	// Auto-open .env files across all repo worktrees
	if openEnv && ideCmd != "" {
		var allEnvPaths []string
		for _, pair := range repoWTPairs {
			parts := strings.SplitN(pair, ":", 2)
			if len(parts) < 2 {
				continue
			}
			wt := parts[1]
			wtPath := filepath.Join(branchSubdirPath, wt)
			for _, f := range findExactEnvFiles(wtPath) {
				allEnvPaths = append(allEnvPaths, filepath.Join(wt, f))
			}
		}
		if len(allEnvPaths) > 0 {
			envCwd := ""
			if firstFolder != "" {
				envCwd = fmt.Sprintf(`
        "options": { "cwd": "${workspaceFolder:%s}/.." },`, firstFolder)
			}
			allTasks = append(allTasks, fmt.Sprintf(`      {
        "label": "Open .env",
        "type": "shell",
        "command": "%s %s",%s
        "runOptions": { "runOn": "folderOpen" },
        "presentation": { "reveal": "never", "close": true }
      }`, ideCmd, shellQuotePaths(allEnvPaths), envCwd))
		}
	}

	content := fmt.Sprintf(`{
  "folders": [
%s
  ],
  "settings": {
    "terminal.integrated.defaultProfile.osx": "zsh"
  },
  "tasks": {
    "version": "2.0.0",
    "tasks": [
%s
    ]
  }
}
`, strings.Join(folders, ",\n"), strings.Join(allTasks, ",\n"))

	os.WriteFile(wsPath, []byte(content), 0644)
	return wsPath
}

// updateWorkspaceContents replaces occurrences of oldName with newName inside a workspace file.
func updateWorkspaceContents(wsPath, oldName, newName string) {
	data, err := os.ReadFile(wsPath)
	if err != nil {
		return
	}
	updated := strings.ReplaceAll(string(data), oldName, newName)
	if updated != string(data) {
		os.WriteFile(wsPath, []byte(updated), 0644)
	}
}

// RefreshRepo fetches latest and updates the main branch ref.
func RefreshRepo(repoPath, basisBranch string, logFn ...LogFunc) error {
	log := noopLog
	if len(logFn) > 0 && logFn[0] != nil {
		log = logFn[0]
	}
	repoName := filepath.Base(repoPath)

	log(repoName, "Fetching from origin...", false)
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
		log(repoName, "Pulling "+mainBranch+" (ff-only)...", false)
		RunGit(repoPath, "pull", "origin", mainBranch, "--ff-only")
	} else {
		log(repoName, "Updating "+mainBranch+" ref...", false)
		RunGit(repoPath, "fetch", "origin", mainBranch+":"+mainBranch)
	}

	log(repoName, "Refresh complete", false)
	return nil
}

// noopLog is a no-op LogFunc for when no logger is provided.
func noopLog(_, _ string, _ bool) {}

// RefreshAllRepos refreshes all repos in the script directory.
// Returns (refreshed count, failed repo names, error).
func RefreshAllRepos(scriptDir string, getBasisBranch BasisBranchResolver, logFn ...LogFunc) (int, []string, error) {
	log := noopLog
	if len(logFn) > 0 && logFn[0] != nil {
		log = logFn[0]
	}

	repos := DiscoverRepos(scriptDir, getBasisBranch)
	refreshed := 0
	var failed []string
	var lastErr error
	total := 0
	for _, r := range repos {
		if !r.IsMonorepo {
			total++
		}
	}

	idx := 0
	for _, r := range repos {
		if r.IsMonorepo {
			continue
		}
		idx++
		log(r.Name, fmt.Sprintf("Refreshing repo %d/%d", idx, total), false)
		if err := RefreshRepo(r.Path, getBasisBranch(r.Name), log); err != nil {
			failed = append(failed, r.Name)
			lastErr = err
			log(r.Name, "Failed: "+err.Error(), true)
		} else {
			refreshed++
		}
	}
	if lastErr != nil && refreshed == 0 {
		return 0, failed, lastErr
	}
	return refreshed, failed, nil
}

// resolveWorktreeRepo finds the main repository path for a git worktree
// by reading the .git file which contains "gitdir: /path/to/repo/.git/worktrees/name".
func resolveWorktreeRepo(wtPath string) string {
	gitFile := filepath.Join(wtPath, ".git")
	data, err := os.ReadFile(gitFile)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return ""
	}
	gitDir := strings.TrimPrefix(line, "gitdir: ")
	// gitDir is like /path/to/repo/.git/worktrees/name
	// Walk up to find the .git dir, then its parent is the repo
	dir := gitDir
	for {
		base := filepath.Base(dir)
		dir = filepath.Dir(dir)
		if base == ".git" {
			return dir
		}
		if dir == filepath.Dir(dir) {
			break // reached root
		}
	}
	return ""
}

// DeleteMonorepoWorktree removes all repo worktrees inside a monorepo branch subdir,
// cleans up workspace files, and removes the branch subdir.
func DeleteMonorepoWorktree(scriptDir, branchSubdir, branch string, repoNames []string, deleteLocal, deleteRemote bool, logFn ...LogFunc) error {
	log := noopLog
	if len(logFn) > 0 && logFn[0] != nil {
		log = logFn[0]
	}

	ctx := branch
	if ctx == "" {
		ctx = filepath.Base(branchSubdir)
	}

	entries, err := os.ReadDir(branchSubdir)
	if err != nil {
		return fmt.Errorf("failed to read branch subdir: %w", err)
	}

	// Remove all worktree directories inside the branch subdir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wtPath := filepath.Join(branchSubdir, e.Name())
		if !isWorktreeDir(wtPath) {
			continue
		}

		repoPath := resolveWorktreeRepo(wtPath)
		if repoPath == "" {
			// Fallback: try to match by repo name prefix
			for _, rn := range repoNames {
				if strings.HasPrefix(e.Name(), rn+"-") {
					repoPath = filepath.Join(scriptDir, rn)
					break
				}
			}
		}

		log(ctx, "Removing worktree "+e.Name(), false)
		if repoPath != "" {
			_, gitErr := RunGit(repoPath, "worktree", "remove", "--force", wtPath)
			if gitErr != nil {
				log(ctx, "Git remove failed for "+e.Name()+", removing manually", true)
				os.RemoveAll(wtPath)
				RunGit(repoPath, "worktree", "prune")
			}
		} else {
			log(ctx, "Could not resolve repo for "+e.Name()+", removing manually", true)
			os.RemoveAll(wtPath)
		}

		// Delete the branch in this repo
		if repoPath != "" && branch != "" && !IsProtectedBranch(branch) {
			if deleteLocal {
				log(ctx, "Deleting local branch in "+filepath.Base(repoPath), false)
				RunGit(repoPath, "branch", "-D", branch)
			}

			if deleteRemote {
				log(ctx, "Deleting remote branch in "+filepath.Base(repoPath), false)
				RunGit(repoPath, "push", "origin", "--delete", branch)
			}
		}
	}

	// Remove the entire branch subdir (workspace files + any leftovers)
	os.RemoveAll(branchSubdir)

	// Clean up empty LTS parent
	log(ctx, "Cleaning up empty directories", false)
	cleanEmptyLTSDirs(filepath.Dir(branchSubdir))

	return nil
}

// DeleteWorktree removes a worktree, its workspace file, and optionally its branch.
func DeleteWorktree(repoPath, wtPath, branch string, deleteLocal, deleteRemote bool, logFn ...LogFunc) error {
	log := noopLog
	if len(logFn) > 0 && logFn[0] != nil {
		log = logFn[0]
	}

	// Safety: validate wtPath is inside an -lts directory by checking ancestors
	cleanPath := filepath.Clean(wtPath)
	inLTS := false
	for dir := filepath.Dir(cleanPath); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if strings.HasSuffix(filepath.Base(dir), "-lts") {
			inLTS = true
			break
		}
	}
	if !inLTS {
		return fmt.Errorf("refusing to delete path outside LTS directory: %s", wtPath)
	}

	// Use branch as context identifier
	ctx := branch
	if ctx == "" {
		ctx = filepath.Base(wtPath)
	}

	// Remove individual workspace file (sibling of worktree dir)
	wtName := filepath.Base(wtPath)
	wsFile := filepath.Join(filepath.Dir(wtPath), wtName+".code-workspace")
	if _, err := os.Stat(wsFile); err == nil {
		log(ctx, "Removing workspace file", false)
		os.Remove(wsFile)
	}

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		log(ctx, "Worktree directory missing — pruning git refs", false)
		RunGit(repoPath, "worktree", "prune")
	} else {
		log(ctx, "Removing worktree directory", false)
		_, err := RunGit(repoPath, "worktree", "remove", "--force", wtPath)
		if err != nil {
			log(ctx, "Force remove failed, falling back to manual cleanup", true)
			if isWorktreeDir(wtPath) {
				os.RemoveAll(wtPath)
			}
			RunGit(repoPath, "worktree", "prune")
		}
	}

	if deleteLocal && branch != "" && !IsProtectedBranch(branch) {
		log(ctx, "Deleting local branch", false)
		RunGit(repoPath, "branch", "-D", branch)
	}

	if deleteRemote && branch != "" && !IsProtectedBranch(branch) {
		log(ctx, "Deleting remote branch", false)
		RunGit(repoPath, "push", "origin", "--delete", branch)
	}

	// Clean up empty parent directories inside -lts structure
	log(ctx, "Cleaning up empty directories", false)
	cleanEmptyLTSDirs(filepath.Dir(wtPath))

	return nil
}

// cleanEmptyLTSDirs removes empty directories up to and including the -lts root.
func cleanEmptyLTSDirs(dir string) {
	for {
		if _, err := os.Stat(dir); err != nil {
			return // directory already gone
		}
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

// RebaseWorktree rebases a worktree onto its main branch and runs package install after.
func RebaseWorktree(wtPath, mainBranch, pkgManager string, logFn ...LogFunc) error {
	log := noopLog
	if len(logFn) > 0 && logFn[0] != nil {
		log = logFn[0]
	}

	ctx, _ := RunGit(wtPath, "branch", "--show-current")
	if ctx == "" {
		ctx = filepath.Base(wtPath)
	}

	log(ctx, "Checking for uncommitted changes...", false)
	status, _ := RunGit(wtPath, "status", "--porcelain")
	hasChanges := status != ""

	if hasChanges {
		log(ctx, "Stashing uncommitted changes", false)
		_, err := RunGit(wtPath, "stash", "push", "-m", "lts-rebase-auto-stash")
		if err != nil {
			return fmt.Errorf("failed to stash changes: %w", err)
		}
	}

	log(ctx, "Rebasing onto "+mainBranch+"...", false)
	_, err := RunGit(wtPath, "rebase", mainBranch)
	if err != nil {
		log(ctx, "Rebase conflict detected — aborting", true)
		_, abortErr := RunGit(wtPath, "rebase", "--abort")

		if hasChanges {
			log(ctx, "Restoring stashed changes", false)
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
		log(ctx, "Restoring stashed changes", false)
		_, popErr := RunGit(wtPath, "stash", "pop")
		if popErr != nil {
			return fmt.Errorf("rebase succeeded but failed to restore stashed changes (run 'git stash pop' manually)")
		}
	}

	log(ctx, "Rebase complete", false)

	// Run package install if configured and package.json exists
	if pkgManager != "" {
		if _, err := os.Stat(filepath.Join(wtPath, "package.json")); err == nil {
			if _, lookErr := exec.LookPath(pkgManager); lookErr == nil {
				log(ctx, "Installing dependencies with "+pkgManager, false)
				cmd := exec.Command(pkgManager, "install", "--silent")
				cmd.Dir = wtPath
				if installErr := cmd.Run(); installErr != nil {
					log(ctx, pkgManager+" install completed with warnings", true)
				} else {
					log(ctx, "Dependencies installed", false)
				}
			}
		}
	}

	return nil
}

// RenameResult holds the outcome of a worktree rename operation.
type RenameResult struct {
	NewPath string // new worktree directory path
}

// RenameWorktree fully renames a worktree: branch, directory, workspace file, and optionally remote.
// repoPath is the bare/main repo, wtPath is the current worktree directory.
func RenameWorktree(repoPath, wtPath, oldBranch, newBranch string, renameRemote bool, pkgMgr, aiCliCmd, ideCmd string, openEnvInIDE bool, logFn ...LogFunc) (*RenameResult, error) {
	log := noopLog
	if len(logFn) > 0 && logFn[0] != nil {
		log = logFn[0]
	}

	ctx := newBranch

	// 1. Rename the local git branch
	log(ctx, "Renaming branch to "+newBranch, false)
	if _, err := RunGit(wtPath, "branch", "-m", newBranch); err != nil {
		return nil, fmt.Errorf("git branch -m failed: %w", err)
	}

	// 2. Compute new directory name and move the worktree
	ltsDir := filepath.Dir(wtPath)
	oldWtName := filepath.Base(wtPath)
	idealName := BranchToDirName(newBranch)

	// If the ideal name matches the current directory, no move needed.
	// Otherwise, generate a unique name (which avoids collisions with OTHER dirs).
	var newWtName string
	if idealName == oldWtName {
		newWtName = oldWtName
	} else {
		newWtName = generateUniqueName(idealName, ltsDir)
	}
	newWtPath := filepath.Join(ltsDir, newWtName)

	if newWtPath != wtPath {
		log(ctx, "Moving worktree directory", false)
		// Use --force to allow moving dirty/locked worktrees
		if _, err := RunGit(repoPath, "worktree", "move", "--force", wtPath, newWtPath); err != nil {
			// If git worktree move fails (e.g. old git version), fall back to OS rename
			log(ctx, "git worktree move not available, using fallback", false)
			if err2 := os.Rename(wtPath, newWtPath); err2 != nil {
				// Revert branch name on failure (dir is still at wtPath)
				RunGit(wtPath, "branch", "-m", oldBranch)
				return nil, fmt.Errorf("move worktree directory failed: %w", err2)
			}
			RunGit(repoPath, "worktree", "repair")
		}
	}

	// 3. Rename the workspace file and regenerate its contents
	oldWsFile := filepath.Join(ltsDir, oldWtName+".code-workspace")
	if _, err := os.Stat(oldWsFile); err == nil {
		log(ctx, "Removing old workspace file", false)
		os.Remove(oldWsFile)
	}
	log(ctx, "Generating new workspace file", false)
	generateIndividualWorkspace(ltsDir, newWtName, pkgMgr, aiCliCmd, ideCmd, openEnvInIDE)

	// 4. Handle remote branch if requested
	if renameRemote && oldBranch != "" && !IsProtectedBranch(oldBranch) {
		// Unset stale upstream first — the old tracking (origin/oldBranch) is now wrong
		// regardless of whether the push succeeds. This prevents accidental pushes
		// to the old remote branch on partial failure.
		RunGit(newWtPath, "branch", "--unset-upstream")

		log(ctx, "Pushing new remote branch", false)
		if _, err := RunGit(newWtPath, "push", "origin", newBranch); err != nil {
			log(ctx, "Push new branch failed: "+err.Error(), true)
		} else {
			// Set upstream tracking to the new remote branch
			RunGit(newWtPath, "branch", "--set-upstream-to", "origin/"+newBranch)

			log(ctx, "Deleting old remote branch", false)
			if _, err := RunGit(newWtPath, "push", "origin", "--delete", oldBranch); err != nil {
				log(ctx, "Delete old remote branch failed: "+err.Error(), true)
			}
		}
	}

	log(ctx, "Rename complete", false)
	return &RenameResult{NewPath: newWtPath}, nil
}

// RenameMonorepoWorktrees renames all worktrees in a monorepo branch group atomically.
// branchSubdirPath is the branch subdirectory containing all repo worktrees (e.g. .../core-erp-ui-lts/core-erp-ui-feat-login/).
// scriptDir is the root working directory where repos live.
// repoNames are the constituent repo names (e.g. ["core", "erp-ui"]).
func RenameMonorepoWorktrees(scriptDir, branchSubdirPath string, repoNames []string, oldBranch, newBranch string, renameRemote bool, pkgMgr, aiCliCmd, ideCmd string, openEnvInIDE bool, logFn ...LogFunc) (*RenameResult, error) {
	log := noopLog
	if len(logFn) > 0 && logFn[0] != nil {
		log = logFn[0]
	}

	ctx := newBranch
	ltsPath := filepath.Dir(branchSubdirPath)
	newBranchDirName := BranchToDirName(newBranch)

	// Discover all worktree directories inside the branch subdir
	entries, err := os.ReadDir(branchSubdirPath)
	if err != nil {
		return nil, fmt.Errorf("read branch subdir: %w", err)
	}

	type wtInfo struct {
		path     string
		repoName string
		repoPath string
	}
	var worktrees []wtInfo

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(branchSubdirPath, e.Name())
		if !isWorktreeDir(dirPath) {
			continue
		}
		// Match directory to repo name by prefix (e.g. "core-feat-login" → repo "core")
		// Try longest repo names first to avoid "core" matching "core-api-feat-login"
		bestMatch := ""
		for _, rn := range repoNames {
			if strings.HasPrefix(e.Name(), rn+"-") && len(rn) > len(bestMatch) {
				bestMatch = rn
			}
		}
		if bestMatch != "" {
			worktrees = append(worktrees, wtInfo{
				path:     dirPath,
				repoName: bestMatch,
				repoPath: filepath.Join(scriptDir, bestMatch),
			})
		}
	}

	if len(worktrees) == 0 {
		return nil, fmt.Errorf("no worktrees found in branch subdir")
	}

	// 1. Rename git branch in each worktree
	for _, wt := range worktrees {
		log(ctx, "Renaming branch in "+wt.repoName, false)
		if _, err := RunGit(wt.path, "branch", "-m", newBranch); err != nil {
			return nil, fmt.Errorf("git branch -m failed in %s: %w", wt.repoName, err)
		}
	}

	// 2. Move each worktree directory and regenerate individual workspace files
	var repoWTPairs []string
	for i, wt := range worktrees {
		oldWtName := filepath.Base(wt.path)
		idealName := wt.repoName + "-" + newBranchDirName
		var newWtName string
		if idealName == oldWtName {
			newWtName = oldWtName
		} else {
			newWtName = generateUniqueName(idealName, branchSubdirPath)
		}
		newWtPath := filepath.Join(branchSubdirPath, newWtName)

		if newWtPath != wt.path {
			log(ctx, "Moving "+wt.repoName+" worktree directory", false)
			if _, err := RunGit(wt.repoPath, "worktree", "move", "--force", wt.path, newWtPath); err != nil {
				log(ctx, "git worktree move not available for "+wt.repoName+", using fallback", false)
				if err2 := os.Rename(wt.path, newWtPath); err2 != nil {
					log(ctx, "Move failed for "+wt.repoName+": "+err2.Error(), true)
					continue
				}
				RunGit(wt.repoPath, "worktree", "repair")
			}
			worktrees[i].path = newWtPath
		}

		// Remove old individual workspace file
		oldWsFile := filepath.Join(branchSubdirPath, oldWtName+".code-workspace")
		if _, err := os.Stat(oldWsFile); err == nil {
			os.Remove(oldWsFile)
		}
		// Generate new individual workspace
		generateIndividualWorkspace(branchSubdirPath, newWtName, pkgMgr, aiCliCmd, ideCmd, openEnvInIDE)

		repoWTPairs = append(repoWTPairs, wt.repoName+":"+newWtName)
	}

	// 3. Remove old monorepo workspace file and generate new one
	oldBranchDirName := BranchToDirName(oldBranch)
	oldMonoWs := filepath.Join(branchSubdirPath, "monorepo-"+oldBranchDirName+".code-workspace")
	if _, err := os.Stat(oldMonoWs); err == nil {
		log(ctx, "Removing old monorepo workspace", false)
		os.Remove(oldMonoWs)
	}
	log(ctx, "Generating new monorepo workspace", false)
	generateMonorepoWorkspace(branchSubdirPath, newBranchDirName, repoWTPairs, aiCliCmd, ideCmd, openEnvInIDE)

	// 4. Rename the branch subdirectory itself
	newBranchSubdirPath := filepath.Join(ltsPath, newBranchDirName)

	if newBranchSubdirPath != branchSubdirPath {
		log(ctx, "Renaming branch subdirectory", false)
		if err := os.Rename(branchSubdirPath, newBranchSubdirPath); err != nil {
			log(ctx, "Branch subdir rename failed: "+err.Error(), true)
			// Non-fatal — worktrees still work at old subdir path
			newBranchSubdirPath = branchSubdirPath
		}
		// Repair all worktrees after moving the parent directory
		for _, wt := range worktrees {
			RunGit(wt.repoPath, "worktree", "repair")
		}
	}

	// 5. Handle remote branches if requested
	if renameRemote && oldBranch != "" && !IsProtectedBranch(oldBranch) {
		for _, wt := range worktrees {
			// Re-compute path after potential subdir rename
			newWtPath := filepath.Join(newBranchSubdirPath, filepath.Base(wt.path))

			RunGit(newWtPath, "branch", "--unset-upstream")

			log(ctx, "Pushing new remote branch for "+wt.repoName, false)
			if _, err := RunGit(newWtPath, "push", "origin", newBranch); err != nil {
				log(ctx, "Push failed for "+wt.repoName+": "+err.Error(), true)
				continue
			}
			RunGit(newWtPath, "branch", "--set-upstream-to", "origin/"+newBranch)

			log(ctx, "Deleting old remote branch for "+wt.repoName, false)
			if _, err := RunGit(newWtPath, "push", "origin", "--delete", oldBranch); err != nil {
				log(ctx, "Delete old remote failed for "+wt.repoName+": "+err.Error(), true)
			}
		}
	}

	log(ctx, "Rename complete for all repos", false)
	return &RenameResult{NewPath: newBranchSubdirPath}, nil
}

// CleanupMergedCleanables finds and deletes all merged/cleanable worktrees.
// Also cleans up workspace files and empty directories.
func CleanupMergedCleanables(scriptDir string, getBasisBranch BasisBranchResolver, deleteRemote bool, logFn ...LogFunc) (int, error) {
	log := noopLog
	if len(logFn) > 0 && logFn[0] != nil {
		log = logFn[0]
	}

	log("cleanup", "Discovering repos and scanning worktree statuses...", false)
	repos := DiscoverRepos(scriptDir, getBasisBranch)
	cleaned := 0

	// Count candidates first
	candidates := 0
	for _, repo := range repos {
		for _, wt := range repo.Worktrees {
			if wt.Status == StatusMergedCleanable {
				candidates++
			}
		}
	}
	if candidates == 0 {
		log("cleanup", "No merged cleanable worktrees found", false)
		return 0, nil
	}
	log("cleanup", fmt.Sprintf("Found %d merged cleanable worktrees", candidates), false)

	for _, repo := range repos {
		for _, wt := range repo.Worktrees {
			if wt.Status == StatusMergedCleanable {
				if repo.IsMonorepo {
					log(wt.Branch, "Cleaning merged monorepo worktree in "+repo.Name, false)
					err := DeleteMonorepoWorktree(scriptDir, wt.Path, wt.Branch, repo.RepoNames, true, deleteRemote, log)
					if err == nil {
						cleaned++
					} else {
						log(wt.Branch, "Failed: "+err.Error(), true)
					}
				} else {
					if repo.Path == "" {
						continue
					}
					log(wt.Branch, "Cleaning merged worktree in "+repo.Name, false)
					err := DeleteWorktree(repo.Path, wt.Path, wt.Branch, true, deleteRemote, log)
					if err == nil {
						cleaned++
					} else {
						log(wt.Branch, "Failed: "+err.Error(), true)
					}
				}
			}
		}
	}

	return cleaned, nil
}

// MigrateToWorktree migrates existing work from the main repo directory into
// an LTS worktree. This handles the case where a user has been working directly
// in the repo (non-main branch, uncommitted changes, unpushed commits) before
// installing LTS.
//
// Steps:
// 1. Stash any uncommitted changes (including untracked, preserving staged state)
// 2. Switch main repo to the main branch (required: git forbids a branch in two worktrees)
// 3. Create an LTS worktree for the feature branch
// 4. Pop the stash into the new worktree (tries --index to preserve staging)
// 5. Copy .env files, install deps, generate workspace
//
// Every failure path restores the original state or tells the user exactly
// where their data is (commits on the branch, uncommitted work in git stash list).
func MigrateToWorktree(repoPath, scriptDir, basisBranch, pkgManager, aiCliCommand, ideCommand string, openEnvInIDE bool, logFn LogFunc) (*CreateResult, error) {
	repoName := filepath.Base(repoPath)
	ctx := repoName

	// Get current branch
	currentBranch, err := RunGit(repoPath, "branch", "--show-current")
	if err != nil {
		return nil, fmt.Errorf("could not detect current branch in %s", repoName)
	}
	if currentBranch == "" {
		return nil, fmt.Errorf("%s is in detached HEAD — check out a branch first, then retry", repoName)
	}
	logFn(ctx, "Current branch: "+currentBranch, false)

	mainBranch := detectMainBranch(repoPath, basisBranch)
	if currentBranch == mainBranch {
		return nil, fmt.Errorf("%s is already on %s — nothing to migrate", repoName, mainBranch)
	}

	// Check for ongoing operations
	if err := CheckOngoingOperations(repoPath); err != nil {
		return nil, err
	}

	// Check for uncommitted changes (staged + unstaged + untracked) and stash them
	hasChanges := false
	hasStagedChanges := false
	porcelain, _ := RunGit(repoPath, "status", "--porcelain")
	if strings.TrimSpace(porcelain) != "" {
		hasChanges = true
		// Detect if there are staged changes (lines starting with A/M/D/R/C in first column)
		for _, line := range strings.Split(strings.TrimSpace(porcelain), "\n") {
			if len(line) >= 2 && line[0] != ' ' && line[0] != '?' {
				hasStagedChanges = true
				break
			}
		}
		logFn(ctx, "Stashing uncommitted changes (including untracked files)", false)
		msg := fmt.Sprintf("LTS migration stash %s", time.Now().Format("2006-01-02 15:04:05"))
		_, err := RunGit(repoPath, "stash", "push", "--include-untracked", "-m", msg)
		if err != nil {
			return nil, fmt.Errorf("failed to stash changes: %w", err)
		}
	}

	// Helper to safely restore stash — only pops if still on the correct branch.
	// Returns true if stash was restored, false if it remains in the stash list.
	restoreStash := func(targetBranch string) bool {
		if !hasChanges {
			return true
		}
		current, _ := RunGit(repoPath, "branch", "--show-current")
		if current != targetBranch {
			logFn(ctx, "Cannot auto-restore stash: repo is on '"+current+"', expected '"+targetBranch+"'. Your changes are safely saved — run 'git stash pop' on "+targetBranch+" to recover", true)
			return false
		}
		_, popErr := RunGit(repoPath, "stash", "pop")
		if popErr != nil {
			logFn(ctx, "Could not auto-restore stash. Your changes are safely saved — run 'git stash pop' to recover", true)
			return false
		}
		return true
	}

	// CRITICAL: Switch to main branch BEFORE creating worktree.
	// Git refuses to have a branch checked out in two places simultaneously,
	// so we must release the feature branch from the main repo first.
	logFn(ctx, "Switching "+repoName+" to "+mainBranch, false)
	_, err = RunGit(repoPath, "checkout", mainBranch)
	if err != nil {
		// Checkout failed — we're still on currentBranch, safe to pop stash
		logFn(ctx, "Restoring stashed changes after checkout failure", true)
		restoreStash(currentBranch)
		return nil, fmt.Errorf("failed to switch to %s: %w — is there an ongoing merge/rebase?", mainBranch, err)
	}

	// Setup LTS directory
	ltsDir := repoName + "-lts"
	ltsPath := filepath.Join(scriptDir, ltsDir)
	if err := os.MkdirAll(ltsPath, 0755); err != nil {
		// Try to restore the original state: switch back then pop
		_, coErr := RunGit(repoPath, "checkout", currentBranch)
		if coErr != nil {
			logFn(ctx, "Could not switch back to "+currentBranch+". Your changes are safely saved in 'git stash list'", true)
		} else {
			restoreStash(currentBranch)
		}
		return nil, fmt.Errorf("failed to create LTS directory: %w", err)
	}

	// Prune orphaned worktree entries
	RunGit(repoPath, "worktree", "prune")

	// Generate worktree name: feat/login → feat-login
	wtName := generateUniqueName(BranchToDirName(currentBranch), ltsPath)
	wtPath := filepath.Join(ltsPath, wtName)

	// Create the worktree for the feature branch (now safe — branch is no longer checked out)
	logFn(ctx, "Creating worktree for "+currentBranch, false)
	_, err = RunGit(repoPath, "worktree", "add", wtPath, currentBranch)
	if err != nil {
		// Restore: try to switch back to feature branch, then pop stash
		logFn(ctx, "Worktree creation failed — restoring original state", true)
		_, coErr := RunGit(repoPath, "checkout", currentBranch)
		if coErr != nil {
			// Can't switch back — do NOT pop stash onto wrong branch
			if hasChanges {
				logFn(ctx, "Could not switch back to "+currentBranch+". Your commits are safe on the branch. Uncommitted changes are safely saved in 'git stash list'", true)
			}
		} else {
			restoreStash(currentBranch)
		}
		return nil, fmt.Errorf("failed to create worktree: %w", err)
	}

	// Pop stash into the new worktree (stashes are shared across worktrees).
	// Try --index first to preserve staging state, fall back to plain pop.
	if hasChanges {
		logFn(ctx, "Moving uncommitted changes to worktree", false)
		stashRestored := false
		if hasStagedChanges {
			// Try to preserve staged state
			_, err := RunGit(wtPath, "stash", "pop", "--index")
			if err == nil {
				stashRestored = true
			} else {
				// --index can fail two ways:
				// 1. Can't recreate index (no changes to working tree — safe to retry plain pop)
				// 2. Working tree conflict (partial changes applied — do NOT retry)
				// Check if working tree is still clean to distinguish.
				dirtyCheck, _ := RunGit(wtPath, "status", "--porcelain")
				if strings.TrimSpace(dirtyCheck) != "" {
					// Working tree has changes — conflict case, don't retry
					logFn(ctx, "Warning: stash applied with conflicts. Resolve conflicts in the worktree, then run 'git stash drop' if satisfied", true)
					stashRestored = true // prevent retry
				}
				// else: working tree is clean, --index just couldn't recreate index. Fall through.
			}
		}
		if !stashRestored {
			_, err := RunGit(wtPath, "stash", "pop")
			if err != nil {
				logFn(ctx, "Warning: stash could not be cleanly applied (possible conflicts). Your changes are safely preserved — run 'git stash pop' in the worktree to resolve manually", true)
			} else if hasStagedChanges {
				logFn(ctx, "Note: staged changes were restored as unstaged (staging state could not be preserved)", false)
			}
		}
	}

	// Copy .env files
	logFn(ctx, "Copying .env files", false)
	copyEnvFilesRecursive(repoPath, wtPath)

	// Install dependencies
	runPackageInstall(wtPath, pkgManager, &CreateLog{Stream: logFn, Context: ctx})

	// Generate workspace file
	logFn(ctx, "Generating workspace file", false)
	wsFile := generateIndividualWorkspace(ltsPath, wtName, pkgManager, aiCliCommand, ideCommand, openEnvInIDE)

	logFn(ctx, "Migration complete — "+currentBranch+" is now an LTS worktree", false)

	return &CreateResult{
		WorktreePath:  wtPath,
		WorktreeName:  wtName,
		WorkspaceFile: wsFile,
		RepoName:      repoName,
		Branch:        currentBranch,
	}, nil
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

// NeedsMigration checks if any LTS directories use the old naming convention.
// Fast: only does stat checks and reads a single branch per LTS dir.
func NeedsMigration(scriptDir string) bool {
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "-lts") {
			continue
		}
		ltsPath := filepath.Join(scriptDir, e.Name())
		ltsType := getLTSType(scriptDir, e.Name())

		if ltsType == "single" {
			repoName := strings.TrimSuffix(e.Name(), "-lts")
			if hasMismatchedSingleDirs(ltsPath, repoName) {
				return true
			}
		} else {
			if hasMismatchedMonorepoDirs(ltsPath, scriptDir, e.Name()) {
				return true
			}
		}
	}
	return false
}

// hasMismatchedSingleDirs checks if any worktree dir in an LTS dir doesn't match the new naming.
func hasMismatchedSingleDirs(ltsPath, repoName string) bool {
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(ltsPath, e.Name())
		if !isWorktreeDir(dirPath) {
			continue
		}
		branch, _ := RunGit(dirPath, "branch", "--show-current")
		if branch == "" {
			continue
		}
		expectedName := BranchToDirName(branch)
		if e.Name() != expectedName {
			return true
		}
	}
	return false
}

// hasMismatchedMonorepoDirs checks monorepo LTS dirs for old naming.
func hasMismatchedMonorepoDirs(ltsPath, scriptDir, ltsDirName string) bool {
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return false
	}
	repoNames := getLTSRepos(scriptDir, ltsDirName)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		branchSubdirPath := filepath.Join(ltsPath, e.Name())
		subEntries, err := os.ReadDir(branchSubdirPath)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			wtPath := filepath.Join(branchSubdirPath, se.Name())
			if !isWorktreeDir(wtPath) {
				continue
			}
			branch, _ := RunGit(wtPath, "branch", "--show-current")
			if branch == "" {
				continue
			}
			// Check branch subdir name
			expectedSubdir := BranchToDirName(branch)
			if e.Name() != expectedSubdir {
				return true
			}
			// Check worktree dir name (match longest repo prefix)
			bestRepo := ""
			for _, rn := range repoNames {
				if strings.HasPrefix(se.Name(), rn+"-") && len(rn) > len(bestRepo) {
					bestRepo = rn
				}
			}
			if bestRepo != "" {
				expectedWtName := bestRepo + "-" + expectedSubdir
				if se.Name() != expectedWtName {
					return true
				}
			}
			// One branch check per subdir is enough
			break
		}
	}
	return false
}

// MigrateDirectoryStructure renames old-style LTS directories to the new convention.
// Returns the number of directories migrated.
func MigrateDirectoryStructure(scriptDir string) int {
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		return 0
	}

	migrated := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "-lts") {
			continue
		}
		ltsPath := filepath.Join(scriptDir, e.Name())
		ltsType := getLTSType(scriptDir, e.Name())

		if ltsType == "single" {
			repoName := strings.TrimSuffix(e.Name(), "-lts")
			repoPath := filepath.Join(scriptDir, repoName)
			migrated += migrateSingleLTS(ltsPath, repoPath, repoName)
		} else {
			repoNames := getLTSRepos(scriptDir, e.Name())
			migrated += migrateMonorepoLTS(ltsPath, scriptDir, repoNames)
		}
	}
	return migrated
}

// migrateSingleLTS migrates worktree dirs inside a single-repo LTS dir.
func migrateSingleLTS(ltsPath, repoPath, repoName string) int {
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return 0
	}

	migrated := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(ltsPath, e.Name())
		if !isWorktreeDir(dirPath) {
			continue
		}
		branch, _ := RunGit(dirPath, "branch", "--show-current")
		if branch == "" {
			continue
		}
		expectedName := BranchToDirName(branch)
		if e.Name() == expectedName {
			continue
		}

		newPath := filepath.Join(ltsPath, expectedName)
		// Avoid collision
		if _, err := os.Stat(newPath); err == nil {
			expectedName = generateUniqueName(expectedName, ltsPath)
			newPath = filepath.Join(ltsPath, expectedName)
		}

		// Move worktree directory
		if _, err := RunGit(repoPath, "worktree", "move", "--force", dirPath, newPath); err != nil {
			if err2 := os.Rename(dirPath, newPath); err2 != nil {
				continue
			}
			RunGit(repoPath, "worktree", "repair")
		}

		// Rename workspace file and update its contents
		oldWs := filepath.Join(ltsPath, e.Name()+".code-workspace")
		newWs := filepath.Join(ltsPath, expectedName+".code-workspace")
		if _, err := os.Stat(oldWs); err == nil {
			os.Rename(oldWs, newWs)
			updateWorkspaceContents(newWs, e.Name(), expectedName)
		}

		migrated++
	}
	return migrated
}

// migrateMonorepoLTS migrates worktree dirs inside a monorepo LTS dir.
func migrateMonorepoLTS(ltsPath, scriptDir string, repoNames []string) int {
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return 0
	}

	migrated := 0
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		branchSubdirPath := filepath.Join(ltsPath, e.Name())

		// Find a worktree inside to get the branch name
		branch := ""
		subEntries, err := os.ReadDir(branchSubdirPath)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			wtPath := filepath.Join(branchSubdirPath, se.Name())
			if isWorktreeDir(wtPath) {
				branch, _ = RunGit(wtPath, "branch", "--show-current")
				break
			}
		}
		if branch == "" {
			continue
		}

		expectedSubdir := BranchToDirName(branch)
		branchDirName := expectedSubdir

		// Rename worktree dirs inside the branch subdir
		type wtRename struct{ oldName, newName string }
		var wtRenames []wtRename
		allWorktreesMigrated := true
		for _, se := range subEntries {
			if !se.IsDir() {
				continue
			}
			wtPath := filepath.Join(branchSubdirPath, se.Name())
			if !isWorktreeDir(wtPath) {
				continue
			}

			// Find the repo name by longest prefix match
			bestRepo := ""
			for _, rn := range repoNames {
				if strings.HasPrefix(se.Name(), rn+"-") && len(rn) > len(bestRepo) {
					bestRepo = rn
				}
			}
			if bestRepo == "" {
				continue
			}

			expectedWtName := bestRepo + "-" + branchDirName
			if se.Name() == expectedWtName {
				continue
			}

			newWtPath := filepath.Join(branchSubdirPath, expectedWtName)
			if _, err := os.Stat(newWtPath); err == nil {
				expectedWtName = generateUniqueName(expectedWtName, branchSubdirPath)
				newWtPath = filepath.Join(branchSubdirPath, expectedWtName)
			}

			repoPath := filepath.Join(scriptDir, bestRepo)
			if _, err := RunGit(repoPath, "worktree", "move", "--force", wtPath, newWtPath); err != nil {
				if err2 := os.Rename(wtPath, newWtPath); err2 != nil {
					allWorktreesMigrated = false
					continue
				}
				RunGit(repoPath, "worktree", "repair")
			}

			// Rename individual workspace and update its contents
			oldWs := filepath.Join(branchSubdirPath, se.Name()+".code-workspace")
			newWs := filepath.Join(branchSubdirPath, expectedWtName+".code-workspace")
			if _, err := os.Stat(oldWs); err == nil {
				os.Rename(oldWs, newWs)
				updateWorkspaceContents(newWs, se.Name(), expectedWtName)
			}

			wtRenames = append(wtRenames, wtRename{oldName: se.Name(), newName: expectedWtName})
			migrated++
		}

		// Only rename the branch subdirectory if all worktrees inside were migrated
		if !allWorktreesMigrated {
			continue
		}
		if e.Name() != expectedSubdir {
			newSubdirPath := filepath.Join(ltsPath, expectedSubdir)
			if _, err := os.Stat(newSubdirPath); err != nil {
				if os.Rename(branchSubdirPath, newSubdirPath) == nil {
					branchSubdirPath = newSubdirPath
					// Repair all worktrees after parent move
					for _, rn := range repoNames {
						RunGit(filepath.Join(scriptDir, rn), "worktree", "repair")
					}
					migrated++
				}
			}
		}

		// Rename monorepo workspace if suffix changed, and update its contents
		oldSuffix := ExtractSuffix(branch)
		oldSafeSuffix := SanitizeForFilename(oldSuffix)
		oldMonoWs := filepath.Join(branchSubdirPath, "monorepo-"+oldSafeSuffix+".code-workspace")
		newMonoWs := filepath.Join(branchSubdirPath, "monorepo-"+branchDirName+".code-workspace")
		if oldMonoWs != newMonoWs {
			if _, err := os.Stat(oldMonoWs); err == nil {
				os.Rename(oldMonoWs, newMonoWs)
			}
		}
		// Update folder paths and names inside the monorepo workspace
		monoWsPath := newMonoWs
		if _, err := os.Stat(monoWsPath); err != nil {
			monoWsPath = oldMonoWs // file wasn't renamed (same name)
		}
		if _, err := os.Stat(monoWsPath); err == nil {
			for _, r := range wtRenames {
				updateWorkspaceContents(monoWsPath, r.oldName, r.newName)
			}
			// Also update the suffix in display names (e.g. "core - feat-login" → "core - feat-login")
			if oldSafeSuffix != branchDirName {
				updateWorkspaceContents(monoWsPath, oldSafeSuffix, branchDirName)
			}
		}
	}
	return migrated
}

// NeedsWorkspaceRepair quickly checks if any .code-workspace files have stale paths.
func NeedsWorkspaceRepair(scriptDir string) bool {
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "-lts") {
			continue
		}
		ltsPath := filepath.Join(scriptDir, e.Name())
		ltsType := getLTSType(scriptDir, e.Name())

		if ltsType == "single" {
			if hasStaleWorkspaces(ltsPath) {
				return true
			}
		} else {
			if hasStaleMonorepoWorkspaces(ltsPath) {
				return true
			}
		}
	}
	return false
}

// hasStaleWorkspaces checks if any single-repo workspace file has a mismatched path.
func hasStaleWorkspaces(ltsPath string) bool {
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".code-workspace") {
			continue
		}
		expectedDir := strings.TrimSuffix(e.Name(), ".code-workspace")
		dirPath := filepath.Join(ltsPath, expectedDir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(ltsPath, e.Name()))
		if err != nil {
			continue
		}
		if !strings.Contains(string(data), `"path": "`+expectedDir+`"`) {
			return true
		}
	}
	return false
}

// hasStaleMonorepoWorkspaces checks if any monorepo workspace file has mismatched paths.
func hasStaleMonorepoWorkspaces(ltsPath string) bool {
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		branchSubdirPath := filepath.Join(ltsPath, e.Name())
		subEntries, err := os.ReadDir(branchSubdirPath)
		if err != nil {
			continue
		}
		for _, se := range subEntries {
			if se.IsDir() || !strings.HasSuffix(se.Name(), ".code-workspace") || strings.HasPrefix(se.Name(), "monorepo-") {
				continue
			}
			expectedDir := strings.TrimSuffix(se.Name(), ".code-workspace")
			dirPath := filepath.Join(branchSubdirPath, expectedDir)
			if _, err := os.Stat(dirPath); os.IsNotExist(err) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(branchSubdirPath, se.Name()))
			if err != nil {
				continue
			}
			if !strings.Contains(string(data), `"path": "`+expectedDir+`"`) {
				return true
			}
		}
	}
	return false
}

// RepairWorkspaceContents fixes .code-workspace files whose internal paths
// don't match the current directory names. This repairs workspaces left stale
// by a previous migration that renamed directories without updating file contents.
// Returns the number of workspace files repaired.
func RepairWorkspaceContents(scriptDir string) int {
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		return 0
	}

	repaired := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), "-lts") {
			continue
		}
		ltsPath := filepath.Join(scriptDir, e.Name())
		ltsType := getLTSType(scriptDir, e.Name())

		if ltsType == "single" {
			repaired += repairSingleWorkspaces(ltsPath)
		} else {
			repaired += repairMonorepoWorkspaces(ltsPath)
		}
	}
	return repaired
}

// repairSingleWorkspaces fixes workspace files in a single-repo LTS directory.
func repairSingleWorkspaces(ltsPath string) int {
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return 0
	}

	repaired := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".code-workspace") {
			continue
		}
		wsPath := filepath.Join(ltsPath, e.Name())
		expectedDir := strings.TrimSuffix(e.Name(), ".code-workspace")

		// Check the directory this workspace should point to exists
		dirPath := filepath.Join(ltsPath, expectedDir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}

		data, err := os.ReadFile(wsPath)
		if err != nil {
			continue
		}
		content := string(data)

		// If the path already matches, nothing to repair
		if strings.Contains(content, `"path": "`+expectedDir+`"`) {
			continue
		}

		// Replace the stale path value with the correct one
		updated := replaceJSONPath(content, expectedDir)
		if updated != content {
			os.WriteFile(wsPath, []byte(updated), 0644)
			repaired++
		}
	}
	return repaired
}

// repairMonorepoWorkspaces fixes workspace files in a monorepo LTS directory.
func repairMonorepoWorkspaces(ltsPath string) int {
	entries, err := os.ReadDir(ltsPath)
	if err != nil {
		return 0
	}

	repaired := 0
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		branchSubdirPath := filepath.Join(ltsPath, e.Name())
		subEntries, err := os.ReadDir(branchSubdirPath)
		if err != nil {
			continue
		}

		// Repair individual worktree workspace files
		for _, se := range subEntries {
			if se.IsDir() || !strings.HasSuffix(se.Name(), ".code-workspace") || strings.HasPrefix(se.Name(), "monorepo-") {
				continue
			}

			wsPath := filepath.Join(branchSubdirPath, se.Name())
			expectedDir := strings.TrimSuffix(se.Name(), ".code-workspace")

			dirPath := filepath.Join(branchSubdirPath, expectedDir)
			if _, err := os.Stat(dirPath); os.IsNotExist(err) {
				continue
			}

			data, err := os.ReadFile(wsPath)
			if err != nil {
				continue
			}
			content := string(data)
			if strings.Contains(content, `"path": "`+expectedDir+`"`) {
				continue
			}
			updated := replaceJSONPath(content, expectedDir)
			if updated != content {
				os.WriteFile(wsPath, []byte(updated), 0644)
				repaired++
			}
		}

		// Repair monorepo aggregate workspace — match each "path" to actual dirs
		for _, se := range subEntries {
			if se.IsDir() || !strings.HasPrefix(se.Name(), "monorepo-") || !strings.HasSuffix(se.Name(), ".code-workspace") {
				continue
			}
			wsPath := filepath.Join(branchSubdirPath, se.Name())
			data, err := os.ReadFile(wsPath)
			if err != nil {
				continue
			}
			content := string(data)
			updated := content

			// Build a map of actual worktree dirs in this branch subdir
			for _, de := range subEntries {
				if !de.IsDir() || !isWorktreeDir(filepath.Join(branchSubdirPath, de.Name())) {
					continue
				}
				if strings.Contains(updated, `"path": "`+de.Name()+`"`) {
					continue
				}
				// Find stale path entries that share a repo prefix with this dir
				// and replace them with the current directory name
				for _, stalePath := range extractWSPaths(updated) {
					if stalePath == de.Name() {
						continue
					}
					// Match by shared repo prefix (longest common prefix up to a dash)
					staleRepo := longestRepoPrefix(stalePath)
					currentRepo := longestRepoPrefix(de.Name())
					if staleRepo != "" && staleRepo == currentRepo {
						updated = strings.ReplaceAll(updated, stalePath, de.Name())
					}
				}
			}
			if updated != content {
				os.WriteFile(wsPath, []byte(updated), 0644)
				repaired++
			}
		}
	}
	return repaired
}

// replaceJSONPath replaces the first "path": "..." value in workspace JSON with newPath.
func replaceJSONPath(content, newPath string) string {
	const prefix = `"path": "`
	idx := strings.Index(content, prefix)
	if idx < 0 {
		return content
	}
	start := idx + len(prefix)
	end := strings.Index(content[start:], `"`)
	if end < 0 {
		return content
	}
	return content[:start] + newPath + content[start+end:]
}

// extractWSPaths returns all "path" values from a workspace JSON string.
func extractWSPaths(content string) []string {
	var paths []string
	remaining := content
	for {
		idx := strings.Index(remaining, `"path": "`)
		if idx < 0 {
			break
		}
		start := idx + len(`"path": "`)
		remaining = remaining[start:]
		end := strings.Index(remaining, `"`)
		if end < 0 {
			break
		}
		paths = append(paths, remaining[:end])
		remaining = remaining[end:]
	}
	return paths
}

// longestRepoPrefix extracts the repo name prefix from a worktree dir name.
// e.g. "core-feat-login" → "core", "erp-ui-feat-login" → "erp-ui"
// It finds the prefix before the branch portion by looking for known branch prefixes.
func longestRepoPrefix(name string) string {
	// Common branch prefixes that indicate where the repo name ends
	branchPrefixes := []string{"-feat-", "-fix-", "-hotfix-", "-release-", "-chore-", "-refactor-", "-docs-", "-test-", "-ci-", "-build-", "-perf-", "-style-"}
	bestIdx := -1
	for _, bp := range branchPrefixes {
		idx := strings.Index(name, bp)
		if idx > 0 && (bestIdx < 0 || idx > bestIdx) {
			bestIdx = idx
		}
	}
	if bestIdx > 0 {
		return name[:bestIdx]
	}
	// Fallback: take everything before the last dash
	if idx := strings.LastIndex(name, "-"); idx > 0 {
		return name[:idx]
	}
	return ""
}
