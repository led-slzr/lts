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

// OpenRepo opens the main repo path using the specified click usage mode.
// For IDE mode, it opens the directory directly without searching for workspace files.
func OpenRepo(path string, mode ClickUsage, ideCommand, aiCliCommand, terminal string) error {
	switch mode {
	case ClickIDE:
		cmd := exec.Command(ideCommand, path)
		return cmd.Start()
	case ClickAICli:
		return openAICli(path, aiCliCommand, terminal)
	case ClickTerminal:
		return openTerminal(path, terminal)
	}
	return nil
}

// OpenWorktree opens a worktree path using the specified click usage mode.
func OpenWorktree(path string, mode ClickUsage, ideCommand, aiCliCommand, terminal string) error {
	switch mode {
	case ClickIDE:
		return openIDE(path, ideCommand)
	case ClickAICli:
		return openAICli(path, aiCliCommand, terminal)
	case ClickTerminal:
		return openTerminal(path, terminal)
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
	return openTerminalWithCommand(path, terminal, fmt.Sprintf("cd '%s' && %s", path, fullCmd))
}

func openTerminal(path, terminal string) error {
	return openTerminalWithCommand(path, terminal, fmt.Sprintf("cd '%s' && clear", path))
}

func openTerminalWithCommand(path, terminal, command string) error {
	if terminal == "" {
		terminal = "terminal"
	}

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
			end tell`, command)
			cmd := exec.Command("osascript", "-e", script)
			return cmd.Start()
		}
		cmd := exec.Command("ghostty", fmt.Sprintf("--working-directory=%s", path))
		return cmd.Start()

	case "iterm":
		script := fmt.Sprintf(`tell application "iTerm2"
			activate
			tell current window
				create tab with default profile
				tell current session
					write text "%s"
				end tell
			end tell
		end tell`, command)
		cmd := exec.Command("osascript", "-e", script)
		return cmd.Start()

	case "wezterm":
		cmd := exec.Command("wezterm", "start", "--cwd", path)
		return cmd.Start()

	case "alacritty":
		cmd := exec.Command("alacritty", "--working-directory", path)
		return cmd.Start()

	case "kitty":
		cmd := exec.Command("kitty", "--directory", path)
		return cmd.Start()

	case "terminal":
		if runtime.GOOS == "darwin" {
			script := fmt.Sprintf(`tell application "Terminal"
				activate
				tell application "System Events" to tell process "Terminal" to keystroke "t" using command down
				delay 0.2
				do script "%s" in front window
			end tell`, command)
			cmd := exec.Command("osascript", "-e", script)
			return cmd.Start()
		}
		cmd := exec.Command("x-terminal-emulator", "--working-directory", path)
		return cmd.Start()

	default:
		cmd := exec.Command(terminal, path)
		return cmd.Start()
	}
}
