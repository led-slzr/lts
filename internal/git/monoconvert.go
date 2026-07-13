package git

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Monorepo conversion: promote a single-repo worktree into a mono-group
// worktree (creating or adopting partner worktrees on the same branch), and
// split a group worktree back into singles. Both restructure directories
// via `git worktree move`, so git's own metadata stays correct — the same
// layouts creation would have produced, just arrived at later.

// ConvertPartner is one repo joining the group: either an existing
// same-branch worktree to adopt (moved in), or a fresh worktree to create.
type ConvertPartner struct {
	Name      string
	AdoptPath string // existing single worktree to move in; "" = create fresh
}

// ConvertResult reports what the conversion produced.
type ConvertResult struct {
	GroupDir      string // e.g. "core-erp-ui-lts"
	BranchSubdir  string // absolute path of the branch subdir
	WorkspaceFile string
	Created       []string // partners that got fresh worktrees
	Adopted       []string // partners whose existing worktrees moved in
}

// ConvertToMono moves the worktree at wtPath (repo repoName, branch branch)
// into a monorepo group with partners. The old single-repo workspace file
// and metadata are cleaned up; the group gets the exact layout
// CreateMonorepoWorktrees would have produced.
func ConvertToMono(scriptDir, repoName, wtPath, branch string, partners []ConvertPartner,
	getBasis BasisBranchResolver, opts WorkspaceOptions, installNew bool, logFn ...LogFunc) (*ConvertResult, error) {
	log := &CreateLog{Context: repoName}
	if len(logFn) > 0 {
		log.Stream = logFn[0]
	}
	if len(partners) == 0 {
		return nil, fmt.Errorf("no partner repos selected")
	}
	if !isWorktreeDir(wtPath) {
		return nil, fmt.Errorf("%s is not a worktree", wtPath)
	}

	// Validate everything before the first move — a failed partner check
	// must not leave the initiator half-relocated.
	names := []string{repoName}
	for _, p := range partners {
		repoPath := filepath.Join(scriptDir, p.Name)
		if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
			return nil, fmt.Errorf("repo %s not found", p.Name)
		}
		if p.AdoptPath != "" && !isWorktreeDir(p.AdoptPath) {
			return nil, fmt.Errorf("%s: adoption target is not a worktree", p.Name)
		}
		if p.AdoptPath == "" {
			if err := CheckOngoingOperations(repoPath); err != nil {
				return nil, err
			}
		}
		names = append(names, p.Name)
	}
	sort.Strings(names)

	groupDir := strings.Join(names, "-") + "-lts"
	ltsPath := filepath.Join(scriptDir, groupDir)
	branchDirName := BranchToDirName(branch)
	branchSubdirPath := filepath.Join(ltsPath, branchDirName)
	if _, err := os.Stat(branchSubdirPath); err == nil {
		return nil, fmt.Errorf("%s already has a %s worktree group", groupDir, branchDirName)
	}
	_, statErr := os.Stat(ltsPath)
	groupPreExisted := statErr == nil
	if groupPreExisted {
		// Hyphenated repo names make dir names ambiguous ({a, b-c} and
		// {a-b, c} both join to a-b-c-lts) — never merge into a group
		// whose recorded repo set differs from ours.
		existing := getLTSRepos(scriptDir, groupDir)
		if !sameRepoSet(existing, names) {
			return nil, fmt.Errorf("%s already exists for repos %s — not %s",
				groupDir, strings.Join(existing, "+"), strings.Join(names, "+"))
		}
	}
	if err := os.MkdirAll(branchSubdirPath, 0755); err != nil {
		return nil, err
	}
	os.WriteFile(filepath.Join(ltsPath, ".lts-type"), []byte("monorepo\n"), 0644)
	writeReposMetadata(ltsPath, names)

	res := &ConvertResult{GroupDir: groupDir, BranchSubdir: branchSubdirPath}
	createdAt := time.Now().Unix()
	var pairs []string

	// Move the initiator in (its original home is remembered for rollback).
	oldParent, oldName := filepath.Dir(wtPath), filepath.Base(wtPath)
	log.Add("Moving " + oldName + " into " + groupDir)
	movedName, prevCreated, err := moveWorktreeIntoDir(scriptDir, repoName, wtPath, branchSubdirPath, branchDirName)
	if err != nil {
		if !groupPreExisted {
			os.RemoveAll(ltsPath)
		} else {
			os.RemoveAll(branchSubdirPath)
		}
		return nil, err
	}
	if prevCreated != 0 {
		createdAt = prevCreated
	}
	pairs = append(pairs, repoName+":"+movedName)

	// Partners: adopt existing same-branch worktrees, create the rest.
	for _, p := range partners {
		log.Context = p.Name
		repoPath := filepath.Join(scriptDir, p.Name)
		if p.AdoptPath != "" {
			log.Add("Adopting existing " + branch + " worktree")
			name, _, err := moveWorktreeIntoDir(scriptDir, p.Name, p.AdoptPath, branchSubdirPath, branchDirName)
			if err != nil {
				log.AddError("Adopt failed: " + err.Error())
				continue
			}
			pairs = append(pairs, p.Name+":"+name)
			res.Adopted = append(res.Adopted, p.Name)
			continue
		}

		basis := getBasis(p.Name)
		RunGit(repoPath, "worktree", "prune")
		if err := EnsureCleanMain(repoPath, basis, log); err != nil {
			log.AddError("Skipping: " + err.Error())
			continue
		}
		RunGit(repoPath, "fetch", "origin")
		mainBranch := detectMainBranch(repoPath, basis)
		wtName := generateUniqueName(p.Name+"-"+branchDirName, branchSubdirPath)
		newPath := filepath.Join(branchSubdirPath, wtName)
		if err := createWorktreeWithBranchHandling(repoPath, newPath, branch, mainBranch, log); err != nil {
			log.AddError("Create failed: " + err.Error())
			continue
		}
		copySupportFiles(repoPath, newPath, opts.CopyEnv, opts.CopyMCP)
		if installNew {
			runPackageInstall(newPath, opts.pkgFor(p.Name), log)
		}
		runSetupScript(newPath, opts.setupFor(p.Name), log)
		pairs = append(pairs, p.Name+":"+wtName)
		res.Created = append(res.Created, p.Name)
	}

	if len(pairs) < 2 {
		// Total partner failure: put the initiator back where it was and
		// dissolve whatever the conversion created — the user's worktree
		// must never be stranded in a half-born group.
		log.Context = repoName
		log.AddError("No partner joined the group — rolling back")
		movedPath := filepath.Join(branchSubdirPath, movedName)
		os.MkdirAll(oldParent, 0755)
		if out, mvErr := RunGit(filepath.Join(scriptDir, repoName), "worktree", "move", movedPath, filepath.Join(oldParent, oldName)); mvErr != nil {
			// Rollback itself failed — leave the group intact (Split Mono
			// Group can still recover it) and say where everything is.
			return res, fmt.Errorf("no partner joined and rollback failed (%s) — the worktree is at %s",
				strings.TrimSpace(out), movedPath)
		}
		recordWorktreeCreated(oldParent, oldName, createdAt)
		generateIndividualWorkspace(oldParent, oldName, opts.pkgFor(repoName),
			opts.AICliCommand, opts.IDECommand, opts.OpenEnvInIDE)
		if !groupPreExisted {
			os.RemoveAll(ltsPath)
		} else {
			os.RemoveAll(branchSubdirPath)
		}
		return nil, fmt.Errorf("no partner could join the group — conversion rolled back")
	}

	recordWorktreeCreated(ltsPath, branchDirName, createdAt)
	log.Context = repoName
	log.Add("Generating monorepo workspace")
	res.WorkspaceFile = generateMonorepoWorkspace(branchSubdirPath, branchDirName, pairs,
		opts.AICliCommand, opts.IDECommand, opts.OpenEnvInIDE)
	return res, nil
}

// sameRepoSet compares two repo-name lists as sets.
func sameRepoSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, n := range a {
		set[n] = true
	}
	for _, n := range b {
		if !set[n] {
			return false
		}
	}
	return true
}

// moveWorktreeIntoDir relocates a worktree via git (metadata stays valid),
// cleans the old location's workspace file and creation record, and returns
// the new directory name plus the preserved creation timestamp.
func moveWorktreeIntoDir(scriptDir, repoName, oldPath, targetDir, branchDirName string) (string, int64, error) {
	oldParent := filepath.Dir(oldPath)
	oldName := filepath.Base(oldPath)
	created := worktreeCreatedAt(oldParent, oldName)

	newName := generateUniqueName(repoName+"-"+branchDirName, targetDir)
	newPath := filepath.Join(targetDir, newName)
	repoPath := filepath.Join(scriptDir, repoName)
	if out, err := RunGit(repoPath, "worktree", "move", oldPath, newPath); err != nil {
		return "", 0, fmt.Errorf("worktree move failed: %s", strings.TrimSpace(out))
	}

	os.Remove(filepath.Join(oldParent, oldName+".code-workspace"))
	removeWorktreeMeta(oldParent, oldName)
	cleanEmptyLTSDirs(oldParent)
	return newName, created, nil
}

// ReduceDecision is one constituent's fate when splitting a group.
type ReduceDecision struct {
	Name   string
	WtPath string // constituent worktree inside the branch subdir
	Branch string
	Delete bool // false = keep, moved out as a single-repo worktree
}

// ReduceMono splits a monorepo branch subdir into per-repo outcomes: kept
// constituents move to their repo's own LTS dir (with a fresh individual
// workspace), deleted ones go through the normal worktree deletion (local
// branch removed, remote untouched). The branch subdir and — when it was
// the group's last — the group dir dissolve.
func ReduceMono(scriptDir, branchSubdirPath string, decisions []ReduceDecision,
	opts WorkspaceOptions, logFn ...LogFunc) (kept, deleted int, err error) {
	log := &CreateLog{}
	if len(logFn) > 0 {
		log.Stream = logFn[0]
	}
	groupLts := filepath.Dir(branchSubdirPath)
	subdirName := filepath.Base(branchSubdirPath)
	created := worktreeCreatedAt(groupLts, subdirName)
	if created == 0 {
		created = time.Now().Unix()
	}

	var firstErr error
	for _, d := range decisions {
		log.Context = d.Name
		repoPath := filepath.Join(scriptDir, d.Name)
		if !isWorktreeDir(d.WtPath) {
			log.AddError("not a worktree, skipping: " + d.WtPath)
			continue
		}

		if d.Delete {
			log.Add("Deleting worktree + local branch " + d.Branch)
			if err := DeleteWorktree(repoPath, d.WtPath, d.Branch, true, false, logFn...); err != nil {
				log.AddError("Delete failed: " + err.Error())
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			deleted++
			continue
		}

		singleLts := filepath.Join(scriptDir, d.Name+"-lts")
		if err := os.MkdirAll(singleLts, 0755); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		newName := generateUniqueName(filepath.Base(d.WtPath), singleLts)
		newPath := filepath.Join(singleLts, newName)
		log.Add("Moving out to " + d.Name + "-lts/" + newName)
		if out, err := RunGit(repoPath, "worktree", "move", d.WtPath, newPath); err != nil {
			log.AddError("Move failed: " + strings.TrimSpace(out))
			if firstErr == nil {
				firstErr = fmt.Errorf("move %s: %s", d.Name, strings.TrimSpace(out))
			}
			continue
		}
		recordWorktreeCreated(singleLts, newName, created)
		generateIndividualWorkspace(singleLts, newName, opts.pkgFor(d.Name),
			opts.AICliCommand, opts.IDECommand, opts.OpenEnvInIDE)
		kept++
	}

	// Dissolve the branch subdir (workspace file + junk only now) and, when
	// it was the group's last, the group dir itself.
	os.Remove(filepath.Join(branchSubdirPath, "monorepo-"+subdirName+".code-workspace"))
	removeWorktreeMeta(groupLts, subdirName)
	if entries, err := os.ReadDir(branchSubdirPath); err == nil {
		onlyJunk := true
		for _, e := range entries {
			if !isJunkFile(e.Name()) {
				onlyJunk = false
				break
			}
		}
		if onlyJunk {
			os.RemoveAll(branchSubdirPath)
		}
	}
	cleanEmptyLTSDirs(groupLts)
	return kept, deleted, firstErr
}
