package git

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Fast, process-free reads of git metadata for discovery. Spawning git for
// every worktree dominated `lts ls`, the session hooks and TUI reloads;
// these read the same files git does and fall back to git when anything
// looks unusual.

// gitDirOf resolves a checkout's git dir: <path>/.git for a main checkout,
// or the "gitdir:" target of a linked worktree's .git file.
func gitDirOf(path string) string {
	dotgit := filepath.Join(path, ".git")
	info, err := os.Lstat(dotgit)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return dotgit
	}
	data, err := os.ReadFile(dotgit)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir: ") {
		return ""
	}
	dir := strings.TrimPrefix(line, "gitdir: ")
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(path, dir)
	}
	return filepath.Clean(dir)
}

// readHeadBranch returns the checked-out branch ("" when detached).
// ok=false means HEAD couldn't be read — callers fall back to git.
func readHeadBranch(path string) (branch string, ok bool) {
	gd := gitDirOf(path)
	if gd == "" {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(gd, "HEAD"))
	if err != nil {
		return "", false
	}
	head := strings.TrimSpace(string(data))
	if strings.HasPrefix(head, "ref: refs/heads/") {
		return strings.TrimPrefix(head, "ref: refs/heads/"), true
	}
	if strings.HasPrefix(head, "ref: ") {
		return "", false // symbolic ref we don't model — let git decide
	}
	return "", true // detached (a raw SHA)
}

// currentBranch is readHeadBranch with a git fallback.
func currentBranch(path string) string {
	if b, ok := readHeadBranch(path); ok {
		return b
	}
	b, _ := RunGit(path, "branch", "--show-current")
	return b
}

// commonDirOf returns the repository's common git dir (shared by all
// worktrees): the main .git, or what a linked worktree's commondir names.
func commonDirOf(path string) string {
	gd := gitDirOf(path)
	if gd == "" {
		return ""
	}
	if data, err := os.ReadFile(filepath.Join(gd, "commondir")); err == nil {
		c := strings.TrimSpace(string(data))
		if !filepath.IsAbs(c) {
			c = filepath.Join(gd, c)
		}
		return filepath.Clean(c)
	}
	return gd
}

// refExists reports whether a fully-qualified ref (refs/heads/main) exists,
// reading loose refs and packed-refs; falls back to git when unsure.
func refExists(repoPath, ref string) bool {
	cd := commonDirOf(repoPath)
	if cd == "" {
		_, err := RunGit(repoPath, "show-ref", "--verify", "--quiet", ref)
		return err == nil
	}
	if info, err := os.Stat(filepath.Join(cd, filepath.FromSlash(ref))); err == nil && !info.IsDir() {
		return true
	}
	f, err := os.Open(filepath.Join(cd, "packed-refs"))
	if err != nil {
		if os.IsNotExist(err) {
			// No packed refs and no loose ref; reftable repos have neither
			if _, rt := os.Stat(filepath.Join(cd, "reftable")); rt == nil {
				_, gerr := RunGit(repoPath, "show-ref", "--verify", "--quiet", ref)
				return gerr == nil
			}
			return false
		}
		_, gerr := RunGit(repoPath, "show-ref", "--verify", "--quiet", ref)
		return gerr == nil
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if len(line) > 41 && line[40] == ' ' && line[41:] == ref {
			return true
		}
	}
	return false
}

// headActivity approximates "last touched" without spawning git: the
// timestamp recorded in the newest reflog entry (every commit, checkout,
// reset, rebase) and the index mtime (staging). File mtimes of the reflog
// itself are NOT used — gc/reflog-expire rewrite it without user activity.
func headActivity(path string) (int64, bool) {
	gd := gitDirOf(path)
	if gd == "" {
		return 0, false
	}
	newest, found := lastReflogTime(filepath.Join(gd, "logs", "HEAD"))
	if info, err := os.Stat(filepath.Join(gd, "index")); err == nil {
		if t := info.ModTime().Unix(); t > newest {
			newest = t
		}
	}
	return newest, found
}

// lastReflogTime parses the unix time of the last entry in a reflog file
// ("<old> <new> <name> <email> <unix> <tz>\t<msg>").
func lastReflogTime(file string) (int64, bool) {
	f, err := os.Open(file)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return 0, false
	}
	const tail = 8192
	off := info.Size() - tail
	if off < 0 {
		off = 0
	}
	buf := make([]byte, info.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil {
		return 0, false
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	last := lines[len(lines)-1]
	if i := strings.IndexByte(last, '\t'); i >= 0 {
		last = last[:i]
	}
	fields := strings.Fields(last)
	if len(fields) < 2 {
		return 0, false
	}
	var ts int64
	if _, err := fmt.Sscanf(fields[len(fields)-2], "%d", &ts); err != nil || ts <= 0 {
		return 0, false
	}
	return ts, true
}

// discoverPool bounds concurrent git work during discovery.
const discoverParallelism = 8

// parallelEach runs fn(i) for i in [0,n) on up to discoverParallelism
// goroutines and waits. fn must only write to its own index.
func parallelEach(n int, fn func(i int)) {
	if n <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	sem := make(chan struct{}, discoverParallelism)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
