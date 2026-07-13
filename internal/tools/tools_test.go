package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func all(pms ...string) func(string) bool {
	set := map[string]bool{}
	for _, p := range pms {
		set[p] = true
	}
	return func(pm string) bool { return set[pm] }
}

func TestDetectPMCorepackFieldWins(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "package.json", `{"packageManager":"yarn@4.1.0"}`)
	write(t, dir, "pnpm-lock.yaml", "") // lockfile disagrees — declaration wins
	if got := detectPM(dir, all("pnpm", "yarn")); got != "yarn" {
		t.Fatalf("packageManager field must win, got %s", got)
	}
	// Even when the declared PM isn't installed — the repo said so;
	// Settings warns rather than silently mixing lockfiles.
	if got := detectPM(dir, all("pnpm")); got != "yarn" {
		t.Fatalf("declaration wins even uninstalled, got %s", got)
	}
}

func TestDetectPMLockfileHierarchy(t *testing.T) {
	// Both pnpm and npm lockfiles: pnpm outranks when installed.
	dir := t.TempDir()
	write(t, dir, "package.json", `{}`)
	write(t, dir, "pnpm-lock.yaml", "")
	write(t, dir, "package-lock.json", "")
	if got := detectPM(dir, all("pnpm", "npm")); got != "pnpm" {
		t.Fatalf("pnpm should outrank npm, got %s", got)
	}
	// pnpm missing → the installed npm wins over the preferred-but-absent pnpm.
	if got := detectPM(dir, all("npm")); got != "npm" {
		t.Fatalf("installed npm should beat missing pnpm, got %s", got)
	}
	// Neither installed → preference order still decides.
	if got := detectPM(dir, all()); got != "pnpm" {
		t.Fatalf("preference order decides among missing, got %s", got)
	}
}

func TestDetectPMSingleLockfiles(t *testing.T) {
	for lock, want := range map[string]string{
		"pnpm-lock.yaml": "pnpm", "yarn.lock": "yarn",
		"bun.lockb": "bun", "bun.lock": "bun", "package-lock.json": "npm",
	} {
		dir := t.TempDir()
		write(t, dir, lock, "")
		if got := detectPM(dir, all("pnpm", "npm", "yarn", "bun")); got != want {
			t.Errorf("%s → %s, want %s", lock, got, want)
		}
	}
}

func TestDetectPMNoEvidenceFallsBackToInstalled(t *testing.T) {
	dir := t.TempDir()
	if got := detectPM(dir, all("yarn", "bun")); got != "yarn" {
		t.Fatalf("fallback should prefer yarn over bun, got %s", got)
	}
	if got := detectPM(dir, all()); got != "npm" {
		t.Fatalf("nothing installed falls back to npm, got %s", got)
	}
}

func TestCommandPresent(t *testing.T) {
	if !CommandPresent("git --version") {
		t.Error("git with args should be found by its first word")
	}
	if CommandPresent("definitely-not-a-real-binary-xyz --flag") {
		t.Error("missing binary must not be present")
	}
	if CommandPresent("  ") {
		t.Error("blank command line must not be present")
	}
	if got := FirstWord("claude --dangerously-skip-permissions"); got != "claude" {
		t.Errorf("FirstWord = %q", got)
	}
}

func TestPMVersionsCoversAllKnown(t *testing.T) {
	infos := PMVersions()
	if len(infos) != len(PackageManagers) {
		t.Fatalf("expected %d entries, got %d", len(PackageManagers), len(infos))
	}
	for i, info := range infos {
		if info.Name != PackageManagers[i] {
			t.Errorf("entry %d = %s, want %s", i, info.Name, PackageManagers[i])
		}
	}
}
