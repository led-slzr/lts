package git

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Per-repo LTS roots: a single-repo LTS dir normally lives next to the repos
// (<scriptDir>/<repo>-lts), but a repo can be configured (`<REPO>_LTS_ROOT`
// in .lts.conf) to keep it elsewhere. Callers register the configured roots
// once per config load via SetLTSRoots; every path computation for a
// single-repo LTS dir goes through SingleLTSPath so discovery, creation,
// migration and hibernate all agree. Mono groups always live in scriptDir.
var (
	rootsMu  sync.RWMutex
	ltsRoots = map[string]string{} // upper-cased repo name → absolute root dir
)

// SetLTSRoots replaces the registered roots (keys: repo names, any case).
// Empty values are ignored (= default location).
func SetLTSRoots(roots map[string]string) {
	m := make(map[string]string, len(roots))
	for k, v := range roots {
		// Only absolute roots: a hand-edited relative value would resolve
		// against whatever directory lts happens to run from
		if v = strings.TrimSpace(v); v != "" && filepath.IsAbs(v) {
			m[strings.ToUpper(k)] = filepath.Clean(v)
		}
	}
	rootsMu.Lock()
	ltsRoots = m
	rootsMu.Unlock()
}

// LTSRootFor returns the directory that holds repoName's single-repo LTS dir.
func LTSRootFor(scriptDir, repoName string) string {
	rootsMu.RLock()
	r, ok := ltsRoots[strings.ToUpper(repoName)]
	rootsMu.RUnlock()
	if ok {
		return r
	}
	return scriptDir
}

// SingleLTSPath returns the absolute path of repoName's single-repo LTS dir.
func SingleLTSPath(scriptDir, repoName string) string {
	return filepath.Join(LTSRootFor(scriptDir, repoName), repoName+"-lts")
}

// ltsDirEntry is one *-lts directory to scan: root is its parent dir.
type ltsDirEntry struct {
	root, name string
}

// allLTSDirs lists every *-lts dir LTS manages: those in scriptDir plus the
// single-repo dirs that live under a custom root. Each appears once.
func allLTSDirs(scriptDir string) []ltsDirEntry {
	var out []ltsDirEntry
	seen := map[string]bool{}
	if entries, err := os.ReadDir(scriptDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.HasSuffix(e.Name(), "-lts") {
				p := filepath.Join(scriptDir, e.Name())
				seen[p] = true
				out = append(out, ltsDirEntry{root: scriptDir, name: e.Name()})
			}
		}
	}
	rootsMu.RLock()
	defer rootsMu.RUnlock()
	for upper, root := range ltsRoots {
		repo := repoDirName(scriptDir, upper)
		if repo == "" {
			continue
		}
		p := filepath.Join(root, repo+"-lts")
		if seen[p] {
			continue
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			seen[p] = true
			out = append(out, ltsDirEntry{root: root, name: repo + "-lts"})
		}
	}
	return out
}

// repoDirName maps an upper-cased config key back to the repo's real
// directory name in scriptDir ("" when no such repo exists).
func repoDirName(scriptDir, upper string) string {
	entries, err := os.ReadDir(scriptDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && strings.ToUpper(e.Name()) == upper && !strings.HasSuffix(e.Name(), "-lts") {
			return e.Name()
		}
	}
	return ""
}
