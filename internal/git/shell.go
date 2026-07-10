package git

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func RunGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// Oldest supported git: `git worktree move` (used by rename and directory
// migration) requires 2.17.
const minGitMajor, minGitMinor = 2, 17

// CheckPrerequisites verifies git is installed and recent enough.
// Called once at startup so failures surface as one clear message instead
// of every operation failing confusingly.
func CheckPrerequisites() error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required but was not found — install git and retry")
	}
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return fmt.Errorf("git is installed but not runnable: %w", err)
	}
	major, minor, ok := parseGitVersion(string(out))
	if !ok {
		return nil // unrecognized version output — don't block
	}
	if major > minGitMajor || (major == minGitMajor && minor >= minGitMinor) {
		return nil
	}
	return fmt.Errorf("git %d.%d is too old — LTS needs git %d.%d+ for worktree support",
		major, minor, minGitMajor, minGitMinor)
}

// GitVersion returns the installed git version string and whether it meets
// the minimum requirement. Empty version means git is missing entirely.
func GitVersion() (version string, ok bool) {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return "", false
	}
	major, minor, parsed := parseGitVersion(string(out))
	if !parsed {
		return strings.TrimSpace(string(out)), true
	}
	// Show the full numeric token (e.g. "2.39.5"), judge on major.minor
	version = fmt.Sprintf("%d.%d", major, minor)
	for _, f := range strings.Fields(string(out)) {
		if strings.HasPrefix(f, fmt.Sprintf("%d.%d", major, minor)) {
			version = f
			break
		}
	}
	ok = major > minGitMajor || (major == minGitMajor && minor >= minGitMinor)
	return version, ok
}

// parseGitVersion extracts major.minor from `git --version` output,
// e.g. "git version 2.39.5 (Apple Git-154)".
func parseGitVersion(s string) (major, minor int, ok bool) {
	for _, f := range strings.Fields(s) {
		parts := strings.Split(f, ".")
		if len(parts) < 2 {
			continue
		}
		maj, err1 := strconv.Atoi(parts[0])
		min, err2 := strconv.Atoi(parts[1])
		if err1 == nil && err2 == nil {
			return maj, min, true
		}
	}
	return 0, 0, false
}
