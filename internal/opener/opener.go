package opener

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type ClickUsage int

const (
	ClickIDE ClickUsage = iota
	ClickAICli
	ClickTerminal
)

func (c ClickUsage) String() string {
	switch c {
	case ClickIDE:
		return "IDE"
	case ClickAICli:
		return "AI CLI"
	case ClickTerminal:
		return "Terminal"
	}
	return "IDE"
}

func (c ClickUsage) Next() ClickUsage {
	return (c + 1) % 3
}

// Options carries the configured commands used to open repos and worktrees.
type Options struct {
	IDECommand      string
	AICliCommand    string
	Terminal        string
	Multiplexer     string // "tmux" = session per worktree; anything else = plain
	TmuxAIPaneWidth int    // AI pane width % (20–90; 0 = default 50)
	TmuxRightPanes  int    // stacked panes in the right column (1–3; 0 = 1)
}

// tmuxEnabled reports whether opens should go through tmux sessions.
func (o Options) tmuxEnabled() bool {
	return o.Multiplexer == "tmux" && TmuxAvailable()
}

// OpenRepo opens the main repo path using the specified click usage mode.
// For IDE mode, it opens the directory directly without searching for workspace files.
func OpenRepo(path string, mode ClickUsage, o Options) error {
	switch mode {
	case ClickIDE:
		cmd := exec.Command(o.IDECommand, path)
		return cmd.Start()
	case ClickAICli:
		if o.tmuxEnabled() {
			return openTmux(path, o, true)
		}
		return openAICli(path, o.AICliCommand, o.Terminal)
	case ClickTerminal:
		if o.tmuxEnabled() {
			return openTmux(path, o, false)
		}
		return openTerminal(path, o.Terminal)
	}
	return nil
}

// OpenWorktree opens a worktree path using the specified click usage mode.
// With tmux enabled, AI CLI and Terminal modes converge on one session per
// worktree (left pane: AI CLI, right pane: shell) — the mode only decides
// which pane gets focus, so the AI CLI conversation survives reopening.
func OpenWorktree(path string, mode ClickUsage, o Options) error {
	switch mode {
	case ClickIDE:
		return openIDE(path, o.IDECommand)
	case ClickAICli:
		if o.tmuxEnabled() {
			return openTmux(path, o, true)
		}
		return openAICli(path, o.AICliCommand, o.Terminal)
	case ClickTerminal:
		if o.tmuxEnabled() {
			return openTmux(path, o, false)
		}
		return openTerminal(path, o.Terminal)
	}
	return nil
}

func openIDE(wtPath, ideCommand string) error {
	if strings.HasSuffix(wtPath, ".code-workspace") {
		cmd := exec.Command(ideCommand, wtPath)
		return cmd.Start()
	}

	wtName := filepath.Base(wtPath)
	parentDir := filepath.Dir(wtPath)

	// Try: parentDir/wtName.code-workspace
	wsFile := filepath.Join(parentDir, wtName+".code-workspace")
	if _, err := os.Stat(wsFile); err == nil {
		cmd := exec.Command(ideCommand, wsFile)
		return cmd.Start()
	}

	// Try: monorepo workspace in the path itself
	matches, _ := filepath.Glob(filepath.Join(wtPath, "monorepo-*.code-workspace"))
	if len(matches) > 0 {
		cmd := exec.Command(ideCommand, matches[0])
		return cmd.Start()
	}

	// Try: any .code-workspace in the path
	matches, _ = filepath.Glob(filepath.Join(wtPath, "*.code-workspace"))
	if len(matches) > 0 {
		cmd := exec.Command(ideCommand, matches[0])
		return cmd.Start()
	}

	// Fallback: open directory
	cmd := exec.Command(ideCommand, wtPath)
	return cmd.Start()
}

func openAICli(path, aiCliCommand, terminal string) error {
	if aiCliCommand == "" {
		return fmt.Errorf("no AI CLI configured — set one in Settings")
	}
	parts := strings.Fields(aiCliCommand)
	if len(parts) == 0 {
		return fmt.Errorf("empty AI CLI command")
	}
	fullCmd := strings.Join(parts, " ")

	// Open a new tab in the configured terminal and run the AI CLI command
	// (the working directory is handled per-terminal)
	return openTerminalWithCommand(path, terminal, fullCmd)
}

func openTerminal(path, terminal string) error {
	return openTerminalWithCommand(path, terminal, "")
}

// OpenURL opens a URL in the default browser (macOS open, Linux xdg-open).
func OpenURL(url string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", url).Start()
	}
	return exec.Command("xdg-open", url).Start()
}

// userShell returns the user's login shell for running commands with a full
// environment (PATH from profile — homebrew tools, etc).
func userShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/sh"
}

// openTerminalWithCommand opens a new window/tab of the configured terminal
// at path. With an empty command it's a plain interactive shell; otherwise
// the command runs in the user's login shell and the window lives as long as
// it does. AppleScript-driven terminals (macOS Ghostty, iTerm2, Terminal.app)
// instead type the command into an interactive shell.
func openTerminalWithCommand(path, terminal, command string) error {
	if terminal == "" {
		terminal = "terminal"
	}

	// AppleScript flows type into a fresh shell, so they need the cd; the
	// native-flag flows below set the working directory themselves.
	typed := command
	if typed == "" {
		typed = "clear"
	}
	typed = fmt.Sprintf("cd '%s' && %s", path, typed)

	switch terminal {
	case "ghostty":
		if runtime.GOOS == "darwin" {
			script := fmt.Sprintf(`tell application "Ghostty"
				activate
			end tell
			delay 0.1
			tell application "System Events"
				tell process "Ghostty"
					keystroke "t" using command down
					delay 0.2
					keystroke "%s"
					key code 36
				end tell
			end tell`, typed)
			cmd := exec.Command("osascript", "-e", script)
			return cmd.Start()
		}
		args := []string{fmt.Sprintf("--working-directory=%s", path)}
		if command != "" {
			args = append(args, "-e", userShell(), "-lc", command)
		}
		return exec.Command("ghostty", args...).Start()

	case "iterm":
		script := fmt.Sprintf(`tell application "iTerm2"
			activate
			tell current window
				create tab with default profile
				tell current session
					write text "%s"
				end tell
			end tell
		end tell`, typed)
		cmd := exec.Command("osascript", "-e", script)
		return cmd.Start()

	case "wezterm":
		args := []string{"start", "--cwd", path}
		if command != "" {
			args = append(args, "--", userShell(), "-lc", command)
		}
		return exec.Command("wezterm", args...).Start()

	case "alacritty":
		args := []string{"--working-directory", path}
		if command != "" {
			args = append(args, "-e", userShell(), "-lc", command)
		}
		return exec.Command("alacritty", args...).Start()

	case "kitty":
		args := []string{"--directory", path}
		if command != "" {
			args = append(args, userShell(), "-lc", command)
		}
		return exec.Command("kitty", args...).Start()

	case "terminal":
		if runtime.GOOS == "darwin" {
			script := fmt.Sprintf(`tell application "Terminal"
				activate
				tell application "System Events" to tell process "Terminal" to keystroke "t" using command down
				delay 0.2
				do script "%s" in front window
			end tell`, typed)
			cmd := exec.Command("osascript", "-e", script)
			return cmd.Start()
		}
		args := []string{}
		if command != "" {
			args = append(args, "-e", userShell(), "-lc", command)
		} else {
			args = append(args, "--working-directory", path)
		}
		return exec.Command("x-terminal-emulator", args...).Start()

	default:
		cmd := exec.Command(terminal, path)
		return cmd.Start()
	}
}
