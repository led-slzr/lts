package git

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Hibernate: safety-audited local deletion of a GitHub-synced repo. The
// audit proves nothing local-only would be lost (every branch pushed or
// merged, no stashes, every checkout clean); the one thing git can't vouch
// for — untracked .env files — is backed up with its directory structure
// preserved so a later re-clone can restore each file where it lived.

type CheckState int

const (
	CheckOK CheckState = iota
	CheckWarn
	CheckFail
)

// HibernateCheck is one line of the audit checklist.
type HibernateCheck struct {
	Label   string
	State   CheckState
	Details []string // per-item specifics (branch names, dirty files, mono dirs)
}

// EnvFile is an untracked env file scheduled for backup. Rel is the path
// inside the backup dir: repo-root-relative for main-repo files, and
// worktrees/<wtdir>/<rel> for files living in a worktree.
type EnvFile struct {
	Src string // absolute source path
	Rel string // destination-relative path inside the backup
}

// HibernateAudit is the full audit result the confirmation dialog renders.
type HibernateAudit struct {
	RepoName   string
	Checks     []HibernateCheck
	EnvFiles   []EnvFile
	FreedBytes int64
}

// Blockers counts failed checks — any blocker disables confirmation.
func (a HibernateAudit) Blockers() int {
	n := 0
	for _, c := range a.Checks {
		if c.State == CheckFail {
			n++
		}
	}
	return n
}

// AuditHibernate runs every safety check against fresh remote state.
// It never mutates anything besides the fetch.
func AuditHibernate(scriptDir string, repo Repo, basisBranch string, logFn ...LogFunc) HibernateAudit {
	log := noopLog
	if len(logFn) > 0 {
		log = logFn[0]
	}
	ctx := "hibernate:" + repo.Name
	audit := HibernateAudit{RepoName: repo.Name}
	add := func(c HibernateCheck) { audit.Checks = append(audit.Checks, c) }

	// 1. Fresh remote state — the audit judges against what GitHub has now.
	log(ctx, "Fetching origin...", false)
	if _, err := RunGit(repo.Path, "fetch", "--prune", "origin"); err != nil {
		add(HibernateCheck{Label: "origin fetched", State: CheckFail,
			Details: []string{"fetch failed — can't verify GitHub has everything"}})
	} else {
		add(HibernateCheck{Label: "origin fetched", State: CheckOK})
	}
	remoteBase := remoteBaseRef(repo.Path, basisBranch)

	// 2. Monorepo entanglement: live mono worktrees contain this repo's
	// checkouts — hibernating underneath them corrupts the group.
	if dirs := monoEntanglements(scriptDir, repo.Name); len(dirs) > 0 {
		add(HibernateCheck{Label: "not part of a live monorepo group", State: CheckFail,
			Details: append([]string{"delete these monorepo worktrees first:"}, dirs...)})
	} else {
		add(HibernateCheck{Label: "no monorepo group entanglement", State: CheckOK})
	}

	// 3. Every local branch pushed (synced upstream) or merged into the base.
	log(ctx, "Checking local branches...", false)
	total, violations := auditBranches(repo.Path, remoteBase)
	if len(violations) > 0 {
		add(HibernateCheck{Label: fmt.Sprintf("%d/%d branches synced or merged", total-len(violations), total),
			State: CheckFail, Details: violations})
	} else {
		add(HibernateCheck{Label: fmt.Sprintf("%d/%d branches synced or merged", total, total), State: CheckOK})
	}

	// 4. Stashes are pure local state — the classic silent-loss vector.
	if out, _ := RunGit(repo.Path, "stash", "list"); strings.TrimSpace(out) != "" {
		n := len(strings.Split(strings.TrimSpace(out), "\n"))
		add(HibernateCheck{Label: fmt.Sprintf("%d stash(es) would be lost", n), State: CheckFail,
			Details: []string{"apply or drop them (git stash list)"}})
	} else {
		add(HibernateCheck{Label: "no stashes", State: CheckOK})
	}

	// 5. Main repo dir clean (porcelain includes untracked files, which are
	// not on GitHub). A detached HEAD needs its own ancestry check — its
	// commits may sit on no branch at all.
	log(ctx, "Checking working trees...", false)
	if det := detachedHeadRisk(repo.Path, remoteBase); det != "" {
		add(HibernateCheck{Label: "main checkout has unpushed work", State: CheckFail, Details: []string{det}})
	} else if out, _ := RunGit(repo.Path, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		n := len(strings.Split(strings.TrimSpace(out), "\n"))
		add(HibernateCheck{Label: fmt.Sprintf("main checkout has %d uncommitted change(s)", n), State: CheckFail,
			Details: []string{"commit and push, or discard"}})
	} else {
		add(HibernateCheck{Label: "main checkout clean", State: CheckOK})
	}

	// 6. Every worktree clean. Branch sync is already covered by the branch
	// loop (refs/heads spans all checkouts) — this catches uncommitted work.
	total, violations = auditWorktrees(repo.Worktrees)
	if len(violations) > 0 {
		add(HibernateCheck{Label: fmt.Sprintf("%d/%d worktrees clean", total-len(violations), total),
			State: CheckFail, Details: violations})
	} else {
		add(HibernateCheck{Label: fmt.Sprintf("%d/%d worktrees clean", total, total), State: CheckOK})
	}

	// 7. Untracked env files: not blockers (they get backed up), but the
	// user must see them — they are the one thing GitHub doesn't have.
	log(ctx, "Scanning for .env files...", false)
	audit.EnvFiles = scanHibernateEnvs(repo)

	// Freed estimate: repo dir + the whole LTS dir (worktrees, modules, meta).
	audit.FreedBytes = dirSize(repo.Path)
	if repo.LTSDir != "" {
		audit.FreedBytes += dirSize(filepath.Join(scriptDir, repo.LTSDir))
	}
	log(ctx, fmt.Sprintf("Audit complete — %d blocker(s), %d .env file(s), ~%s",
		audit.Blockers(), len(audit.EnvFiles), HumanBytes(audit.FreedBytes)), false)
	return audit
}

// remoteBaseRef picks the remote ref merged-ness is judged against.
func remoteBaseRef(repoPath, basisBranch string) string {
	for _, c := range []string{basisBranch, "main", "master"} {
		if _, err := RunGit(repoPath, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+c); err == nil {
			return "origin/" + c
		}
	}
	return ""
}

// auditBranches verifies every local branch is pushed or merged. Returns the
// branch count and one violation line per unsafe branch.
func auditBranches(repoPath, remoteBase string) (total int, violations []string) {
	out, err := RunGit(repoPath, "for-each-ref", "refs/heads",
		"--format=%(refname:short)\t%(upstream:short)\t%(upstream:track)")
	if err != nil {
		return 0, []string{"could not list branches"}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		total++
		parts := strings.SplitN(line, "\t", 3)
		branch := parts[0]
		upstream, track := "", ""
		if len(parts) > 1 {
			upstream = parts[1]
		}
		if len(parts) > 2 {
			track = parts[2]
		}

		if upstream != "" && track != "[gone]" {
			if n := parseAhead(track); n > 0 {
				violations = append(violations, fmt.Sprintf("%s — %d unpushed commit(s)", branch, n))
			}
			continue
		}
		// No upstream (or upstream deleted): safe only if merged into the base.
		if remoteBase != "" {
			if _, err := RunGit(repoPath, "merge-base", "--is-ancestor", branch, remoteBase); err == nil {
				continue
			}
		}
		reason := "never pushed"
		if track == "[gone]" {
			reason = "upstream deleted, not merged"
		}
		violations = append(violations, branch+" — "+reason)
	}
	return total, violations
}

// parseAhead extracts N from track strings like "[ahead 2]" / "[ahead 1, behind 3]".
func parseAhead(track string) int {
	idx := strings.Index(track, "ahead ")
	if idx < 0 {
		return 0
	}
	n := 0
	fmt.Sscanf(track[idx+len("ahead "):], "%d", &n)
	return n
}

// detachedHeadRisk reports unpushed detached-HEAD work in the main checkout.
func detachedHeadRisk(repoPath, remoteBase string) string {
	branch, err := RunGit(repoPath, "branch", "--show-current")
	if err != nil || strings.TrimSpace(branch) != "" {
		return ""
	}
	if remoteBase == "" {
		return "detached HEAD and no remote base to verify against"
	}
	if _, err := RunGit(repoPath, "merge-base", "--is-ancestor", "HEAD", remoteBase); err != nil {
		return "detached HEAD with commits not on " + remoteBase
	}
	return ""
}

// auditWorktrees checks each worktree checkout for uncommitted changes.
func auditWorktrees(wts []Worktree) (total int, violations []string) {
	for _, wt := range wts {
		if _, err := os.Stat(wt.Path); err != nil {
			continue // directory already gone — nothing to lose
		}
		total++
		out, err := RunGit(wt.Path, "status", "--porcelain")
		if err != nil {
			violations = append(violations, wt.Branch+" — status check failed")
			continue
		}
		if s := strings.TrimSpace(out); s != "" {
			n := len(strings.Split(s, "\n"))
			violations = append(violations, fmt.Sprintf("%s — %d uncommitted change(s)", wt.Branch, n))
		}
	}
	return total, violations
}

// monoEntanglements lists multi-repo LTS dirs that include this repo AND
// still have branch subdirs — their worktrees embed this repo's checkouts.
func monoEntanglements(scriptDir, repoName string) []string {
	var out []string
	for _, ltsDir := range getMultiRepoLTSDirs(scriptDir, repoName) {
		if len(getLTSRepos(scriptDir, ltsDir)) < 2 {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(scriptDir, ltsDir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				out = append(out, ltsDir+"/"+e.Name())
			}
		}
	}
	return out
}

// scanHibernateEnvs finds every untracked .env* file in the main repo dir
// and each worktree. Tracked files (e.g. a committed .env.example) are on
// GitHub already and skipped. Relative paths are preserved so a monorepo's
// apps/web/.env restores exactly where it lived.
func scanHibernateEnvs(repo Repo) []EnvFile {
	var out []EnvFile
	out = append(out, untrackedEnvsUnder(repo.Path, "")...)
	for _, wt := range repo.Worktrees {
		if _, err := os.Stat(wt.Path); err != nil {
			continue
		}
		prefix := filepath.Join("worktrees", filepath.Base(wt.Path))
		out = append(out, untrackedEnvsUnder(wt.Path, prefix)...)
	}
	return out
}

func untrackedEnvsUnder(root, destPrefix string) []EnvFile {
	var out []EnvFile
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
		if !strings.HasPrefix(info.Name(), ".env") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		// Tracked files live on GitHub — only untracked ones need rescuing.
		if _, err := RunGit(root, "ls-files", "--error-unmatch", rel); err == nil {
			return nil
		}
		out = append(out, EnvFile{Src: path, Rel: filepath.Join(destPrefix, rel)})
		return nil
	})
	return out
}

const envBackupKeep = 5

// BackupEnvs copies the audited env files into
// backupRoot/<repoName>/<timestamp>/, pruning old backups beyond the newest
// few. Returns the created backup dir and file count ("" and 0 when there
// was nothing to back up).
func BackupEnvs(files []EnvFile, backupRoot, repoName string) (string, int, error) {
	if len(files) == 0 {
		return "", 0, nil
	}
	dest := filepath.Join(backupRoot, repoName, time.Now().Format("20060102-150405"))
	for _, f := range files {
		data, err := os.ReadFile(f.Src)
		if err != nil {
			return "", 0, fmt.Errorf("read %s: %w", f.Src, err)
		}
		target := filepath.Join(dest, f.Rel)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", 0, err
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			return "", 0, err
		}
	}
	pruneEnvBackups(filepath.Join(backupRoot, repoName))
	return dest, len(files), nil
}

func pruneEnvBackups(repoBackupDir string) {
	entries, err := os.ReadDir(repoBackupDir)
	if err != nil {
		return
	}
	var stamps []string
	for _, e := range entries {
		if e.IsDir() {
			stamps = append(stamps, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(stamps))) // stamp format sorts newest-first
	for _, s := range stamps[minInt(len(stamps), envBackupKeep):] {
		os.RemoveAll(filepath.Join(repoBackupDir, s))
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// LatestEnvBackup finds the newest backup for repoName and counts its files:
// main-repo files (restorable into a fresh clone) vs worktree files (their
// worktrees no longer exist — they stay in the backup for manual retrieval).
func LatestEnvBackup(backupRoot, repoName string) (dir string, mainCount, wtCount int, ok bool) {
	repoDir := filepath.Join(backupRoot, repoName)
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return "", 0, 0, false
	}
	var stamps []string
	for _, e := range entries {
		if e.IsDir() {
			stamps = append(stamps, e.Name())
		}
	}
	if len(stamps) == 0 {
		return "", 0, 0, false
	}
	sort.Sort(sort.Reverse(sort.StringSlice(stamps)))
	dir = filepath.Join(repoDir, stamps[0])
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if strings.HasPrefix(rel, "worktrees"+string(filepath.Separator)) {
			wtCount++
		} else {
			mainCount++
		}
		return nil
	})
	return dir, mainCount, wtCount, mainCount+wtCount > 0
}

// RestoreEnvBackup copies a backup's main-repo env files into repoPath,
// preserving relative paths. Worktree files are skipped (their worktrees
// don't exist after a fresh clone). Existing files are never overwritten.
func RestoreEnvBackup(backupDir, repoPath string) (int, error) {
	restored := 0
	err := filepath.Walk(backupDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(backupDir, path)
		if info.IsDir() {
			if rel == "worktrees" {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(repoPath, rel)
		if _, err := os.Stat(target); err == nil {
			return nil // never clobber a file the fresh clone already has
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			return err
		}
		restored++
		return nil
	})
	return restored, err
}

// HibernateRepo deletes the repo dir and its LTS dir. Both paths are
// verified to sit directly inside scriptDir — this is the only place LTS
// removes a whole repo, so it refuses anything that looks displaced.
func HibernateRepo(scriptDir string, repo Repo, logFn ...LogFunc) error {
	log := noopLog
	if len(logFn) > 0 {
		log = logFn[0]
	}
	ctx := "hibernate:" + repo.Name
	if repo.Path == "" || filepath.Dir(repo.Path) != filepath.Clean(scriptDir) {
		return fmt.Errorf("refusing to remove %q — not directly inside %q", repo.Path, scriptDir)
	}
	if repo.LTSDir != "" {
		ltsPath := filepath.Join(scriptDir, repo.LTSDir)
		log(ctx, "Removing "+ltsPath, false)
		if err := os.RemoveAll(ltsPath); err != nil {
			return fmt.Errorf("remove LTS dir: %w", err)
		}
	}
	log(ctx, "Removing "+repo.Path, false)
	if err := os.RemoveAll(repo.Path); err != nil {
		return fmt.Errorf("remove repo dir: %w", err)
	}
	return nil
}
