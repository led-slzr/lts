// Package tools detects the external programs LTS drives — package
// managers, terminals, IDEs, AI CLIs. It powers the "auto" package-manager
// mode, the inline ⚠ not-found warnings in Settings, and the Diagnostics
// inventory. Detection is presence-first (cheap LookPath / app-bundle
// stats); versions are fetched only where shown.
package tools

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Has reports whether cmd resolves on PATH.
func Has(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// CommandPresent checks a full command line (e.g. an AI CLI setting like
// "claude --dangerously-skip-permissions") by its first field.
func CommandPresent(cmdline string) bool {
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return false
	}
	return Has(fields[0])
}

// FirstWord returns the executable a command line starts with ("" if empty).
func FirstWord(cmdline string) string {
	fields := strings.Fields(cmdline)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// macApp reports whether a macOS app bundle is installed.
func macApp(name string) bool {
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join("/Applications", name+".app"),
		filepath.Join(home, "Applications", name+".app"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// TerminalPresent reports whether a terminal option from Settings is
// actually available. On macOS several terminals are app bundles with no
// PATH binary (iTerm2, Terminal.app), so LookPath alone would lie.
func TerminalPresent(key string) bool {
	if runtime.GOOS == "darwin" {
		switch key {
		case "terminal":
			return true // Terminal.app ships with macOS
		case "iterm":
			return macApp("iTerm")
		case "ghostty":
			return Has("ghostty") || macApp("Ghostty")
		case "wezterm":
			return Has("wezterm") || macApp("WezTerm")
		case "alacritty":
			return Has("alacritty") || macApp("Alacritty")
		case "kitty":
			return Has("kitty") || macApp("kitty")
		}
		return Has(key)
	}
	if key == "terminal" {
		return Has("x-terminal-emulator")
	}
	return Has(key)
}

// PackageManagers is the set LTS knows how to drive.
var PackageManagers = []string{"pnpm", "npm", "yarn", "bun"}

// PMInfo is one detected package manager for the Diagnostics inventory.
type PMInfo struct {
	Name    string
	Version string // "" when not installed
}

var (
	pmOnce  sync.Once
	pmCache []PMInfo
)

// PMVersions returns every known package manager with its version ("" when
// missing). Spawns one subprocess per installed PM — cached for the process
// lifetime (versions don't change mid-session).
func PMVersions() []PMInfo {
	pmOnce.Do(func() {
		for _, pm := range PackageManagers {
			info := PMInfo{Name: pm}
			if Has(pm) {
				if out, err := exec.Command(pm, "--version").Output(); err == nil {
					info.Version = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
				}
			}
			pmCache = append(pmCache, info)
		}
	})
	return pmCache
}

// Lockfile evidence per package manager, in detection-preference order.
// pnpm outranks bun outranks yarn outranks npm when a repo carries
// multiple lockfiles (mixed-lockfile repos are usually mid-migration
// toward the more specific tool).
var pmLockfiles = []struct {
	pm    string
	files []string
}{
	{"pnpm", []string{"pnpm-lock.yaml"}},
	{"bun", []string{"bun.lockb", "bun.lock"}},
	{"yarn", []string{"yarn.lock"}},
	{"npm", []string{"package-lock.json"}},
}

// fallbackPref is the no-evidence hierarchy (matches LTS's historical
// default of pnpm-first).
var fallbackPref = []string{"pnpm", "npm", "yarn", "bun"}

// DetectPM resolves the package manager for a repo — the engine behind the
// "auto" setting. Hierarchy:
//  1. package.json's "packageManager" field (corepack) — the repo's own
//     declaration wins even if the tool is missing (Settings warns).
//  2. Lockfiles, preferring pnpm > bun > yarn > npm; among multiple
//     matches an installed one wins over a missing one.
//  3. No evidence: the first installed of pnpm, npm, yarn, bun.
func DetectPM(repoPath string) string {
	return detectPM(repoPath, Has)
}

// detectPM takes the installed-check as a parameter for testability.
func detectPM(repoPath string, installed func(string) bool) string {
	if data, err := os.ReadFile(filepath.Join(repoPath, "package.json")); err == nil {
		var pj struct {
			PackageManager string `json:"packageManager"`
		}
		if json.Unmarshal(data, &pj) == nil && pj.PackageManager != "" {
			name := strings.SplitN(pj.PackageManager, "@", 2)[0]
			for _, known := range PackageManagers {
				if name == known {
					return name
				}
			}
		}
	}

	var found []string
	for _, entry := range pmLockfiles {
		for _, f := range entry.files {
			if _, err := os.Stat(filepath.Join(repoPath, f)); err == nil {
				found = append(found, entry.pm)
				break
			}
		}
	}
	for _, pm := range found {
		if installed(pm) {
			return pm
		}
	}
	if len(found) > 0 {
		return found[0]
	}

	for _, pm := range fallbackPref {
		if installed(pm) {
			return pm
		}
	}
	return "npm"
}
