package app

import (
	"sync"
	"testing"

	"lts-revamp/internal/config"
)

// Command goroutines resolve per-repo config through snapshots taken at
// construction — settings can write cfg.Local while an operation is running,
// and reading the live map from a goroutine would be a concurrent map access.
// Run with -race: the old resolver (reading cfg.Local directly) fails this.
func TestResolversAreSafeAgainstConcurrentConfigWrites(t *testing.T) {
	cfg := &config.Config{
		Global:  config.DefaultGlobal(),
		Local:   map[string]config.RepoLocalConfig{"CORE": {BasisBranch: "dev", PackageManager: "bun"}},
		WorkDir: t.TempDir(),
	}

	// Snapshots taken on the "Update thread"
	basis := basisResolver(cfg)
	opts := workspaceOpts(cfg)

	var wg sync.WaitGroup
	wg.Add(2)
	// "Operation goroutine" resolving repeatedly
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			if b := basis("core"); b != "dev" {
				t.Errorf("snapshot changed mid-flight: got %q", b)
				return
			}
			if pm := opts.PkgManager("core"); pm != "bun" {
				t.Errorf("pkg snapshot changed mid-flight: got %q", pm)
				return
			}
			_ = basis("other") // fallback path
		}
	}()
	// "Settings on the Update thread" writing concurrently
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			cfg.SetRepoBasisBranch("core", "main")
			cfg.SetRepoPackageManager("newrepo", "npm")
			cfg.SetRepoBasisBranch("core", "dev")
		}
	}()
	wg.Wait()
}
