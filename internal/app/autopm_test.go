package app

import (
	"os"
	"path/filepath"
	"testing"

	"lts-revamp/internal/config"
)

// The "auto" package manager resolves per repo from lockfile evidence, at
// every resolution layer: the goroutine-safe snapshot resolver and the
// Update-thread helper.
func TestAutoPackageManagerResolvesPerRepo(t *testing.T) {
	work := t.TempDir()
	os.MkdirAll(filepath.Join(work, "pnpm-repo"), 0755)
	os.WriteFile(filepath.Join(work, "pnpm-repo", "pnpm-lock.yaml"), []byte{}, 0644)
	os.MkdirAll(filepath.Join(work, "npm-repo"), 0755)
	os.WriteFile(filepath.Join(work, "npm-repo", "package-lock.json"), []byte{}, 0644)

	cfg := config.Config{Global: config.DefaultGlobal(), Local: map[string]config.RepoLocalConfig{}, WorkDir: work}
	if cfg.Global.PackageManager != "auto" {
		t.Fatal("auto should be the fresh-install default")
	}

	resolve := pkgResolver(&cfg)
	if got := resolve("pnpm-repo"); got != "pnpm" {
		t.Errorf("resolver: pnpm-repo → %q", got)
	}
	if got := resolve("npm-repo"); got != "npm" {
		t.Errorf("resolver: npm-repo → %q", got)
	}

	// Per-repo override still beats auto.
	cfg.Local["NPM-REPO"] = config.RepoLocalConfig{PackageManager: "bun"}
	if got := pkgResolver(&cfg)("npm-repo"); got != "bun" {
		t.Errorf("override should beat auto, got %q", got)
	}

	// The Update-thread helper agrees.
	if got := resolvePMWith(&cfg, "pnpm-repo"); got != "pnpm" {
		t.Errorf("resolvePMWith: pnpm-repo → %q", got)
	}
}
