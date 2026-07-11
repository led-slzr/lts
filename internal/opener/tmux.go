package opener

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// TmuxAvailable reports whether tmux is installed.
func TmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// TmuxVersion returns the installed tmux version (e.g. "3.5a") for diagnostics.
func TmuxVersion() (string, bool) {
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "tmux ")), true
}

// SessionName derives the tmux session name for a repo or worktree path.
// Worktrees live under a *-lts dir, so the group name is folded in:
//
//	~/w/core-lts/feat-login        → lts-core-feat-login
//	~/w/core-goforms-lts/feat-x    → lts-core-goforms-feat-x
//	~/w/core (main repo)           → lts-core
//
// The lts- prefix keeps LTS-managed sessions apart from the user's own.
func SessionName(path string) string {
	base := filepath.Base(path)
	parent := filepath.Base(filepath.Dir(path))
	name := base
	if strings.HasSuffix(parent, "-lts") {
		name = strings.TrimSuffix(parent, "-lts") + "-" + base
	}
	return "lts-" + sanitizeSessionName(name)
}

// sanitizeSessionName keeps session names within tmux's allowed characters
// (no "." or ":" — they're target separators).
func sanitizeSessionName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// openTmux opens the worktree's tmux session in the configured terminal,
// creating it first if needed (left pane: AI CLI, right pane: shell).
// focusAI selects the AI pane; otherwise the shell pane is focused.
func openTmux(path string, o Options, focusAI bool) error {
	name := SessionName(path)
	if err := ensureTmuxSession(name, path, o.AICliCommand); err != nil {
		return err
	}
	focusTmuxPane(name, focusAI)

	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	return openTerminalWithCommand(path, o.Terminal, fmt.Sprintf("'%s' attach-session -t '%s'", tmuxPath, name))
}

// ensureTmuxSession creates the session if it doesn't exist yet: pane 0
// (left) runs the AI CLI, the horizontal split (right) is a plain shell.
// The commands talk to the tmux server directly — no terminal needed.
func ensureTmuxSession(name, dir, aiCliCommand string) error {
	// "=" forces an exact-name match (plain -t does prefix matching)
	if exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil {
		return nil
	}
	out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, "-P", "-F", "#{window_id}").Output()
	if err != nil {
		return fmt.Errorf("tmux new-session failed: %w", err)
	}
	win := strings.TrimSpace(string(out))
	if aiCliCommand != "" && win != "" {
		// Split creates the right pane; the AI CLI types into the left one,
		// running inside the user's interactive shell (full PATH, aliases).
		exec.Command("tmux", "split-window", "-h", "-t", win, "-c", dir).Run()
		exec.Command("tmux", "send-keys", "-t", win+".{left}", aiCliCommand, "Enter").Run()
	}
	return nil
}

// focusTmuxPane selects the AI pane ({left}) or the shell pane ({right}) of
// the session's current window. Explicit mnemonic targets, not select-pane
// -L/-R — the directional forms wrap around and aren't deterministic.
func focusTmuxPane(name string, left bool) {
	out, err := exec.Command("tmux", "display-message", "-t", name+":", "-p", "#{window_id}").Output()
	if err != nil {
		return
	}
	win := strings.TrimSpace(string(out))
	pane := "{right}"
	if left {
		pane = "{left}"
	}
	exec.Command("tmux", "select-pane", "-t", win+"."+pane).Run()
}

// KillSession removes the tmux session of a deleted worktree (best-effort).
func KillSession(path string) {
	if !TmuxAvailable() {
		return
	}
	exec.Command("tmux", "kill-session", "-t", "="+SessionName(path)).Run()
}

// RenameSession follows a worktree rename so the session keeps matching its
// path (best-effort; no-op when the session doesn't exist).
func RenameSession(oldPath, newPath string) {
	if !TmuxAvailable() || oldPath == "" || newPath == "" {
		return
	}
	exec.Command("tmux", "rename-session", "-t", "="+SessionName(oldPath), SessionName(newPath)).Run()
}
