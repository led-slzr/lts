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

// OpenWorktree opens a worktree path using the specified click usage mode.
func OpenWorktree(path string, mode ClickUsage, ideCommand, aiCliCommand, terminal string) error {
	switch mode {
	case ClickIDE:
		return openIDE(path, ideCommand)
	case ClickAICli:
		return openAICli(path, aiCliCommand)
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

func openAICli(path, aiCliCommand string) error {
	if aiCliCommand == "" {
		return fmt.Errorf("no AI CLI configured — set one in Settings")
	}
	// Split command and flags: "claude --dangerously-skip-permissions" → ["claude", "--dangerously-skip-permissions"]
	parts := strings.Fields(aiCliCommand)
	if len(parts) == 0 {
		return fmt.Errorf("empty AI CLI command")
	}

	// Open terminal at path, then run AI CLI
	switch runtime.GOOS {
	case "darwin":
		fullCmd := strings.Join(parts, " ")
		script := fmt.Sprintf(`tell application "Terminal"
			do script "cd '%s' && %s"
			activate
		end tell`, path, fullCmd)
		cmd := exec.Command("osascript", "-e", script)
		return cmd.Start()
	default:
		args := append(parts[1:], path)
		cmd := exec.Command(parts[0], args...)
		cmd.Dir = path
		return cmd.Start()
	}
}

func openTerminal(path, terminal string) error {
	if terminal == "" {
		terminal = "terminal"
	}

	switch terminal {
	case "ghostty":
		cmd := exec.Command("ghostty", fmt.Sprintf("--working-directory=%s", path))
		return cmd.Start()

	case "iterm":
		script := fmt.Sprintf(`tell application "iTerm2"
			create window with default profile
			tell current session of current window
				write text "cd '%s'"
			end tell
			activate
		end tell`, path)
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
				do script "cd '%s'"
				activate
			end tell`, path)
			cmd := exec.Command("osascript", "-e", script)
			return cmd.Start()
		}
		cmd := exec.Command("x-terminal-emulator", "--working-directory", path)
		return cmd.Start()

	default:
		// Try running terminal name directly
		cmd := exec.Command(terminal, path)
		return cmd.Start()
	}
}
