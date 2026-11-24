package main

import (
	"fmt"
	"os"
	"syscall"

	"lts-revamp/internal/app"
	"lts-revamp/internal/cli"
	"lts-revamp/internal/config"
	"lts-revamp/internal/git"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if err := git.CheckPrerequisites(); err != nil && !infoOnly(os.Args[1:]) {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	// Subcommands are the scriptable CLI; bare `lts` (and `lts --dir X`)
	// opens the TUI via runTUI.
	os.Exit(cli.Execute(os.Args[1:], runTUI))
}

// infoOnly reports invocations that need no git: version and help.
func infoOnly(args []string) bool {
	for _, a := range args {
		switch a {
		case "--version", "-v", "version", "help", "--help", "-h":
			return true
		}
	}
	return false
}

// runTUI launches the interactive UI in workDir (first-run wizard included).
func runTUI(workDir string) int {
	info, err := os.Stat(workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: directory does not exist: %s\n", workDir)
		return 1
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: not a directory: %s\n", workDir)
		return 1
	}

	// First-run setup wizard
	if config.IsFirstRun() {
		setupModel := ui.NewSetup()
		p := tea.NewProgram(setupModel, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
	}

	cfg := config.Load(workDir)

	model := app.NewModel(cfg)
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),
	)

	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	if m, ok := finalModel.(app.Model); ok {
		// Reset requested from Diagnostics: rerun the setup wizard, then relaunch
		if m.RelaunchSetup {
			setupModel := ui.NewSetup()
			sp := tea.NewProgram(setupModel, tea.WithAltScreen())
			if _, err := sp.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return 1
			}
			relaunch(workDir)
		}
		// Relaunch in a different directory (history suggestion)
		if m.RelaunchDir != "" {
			relaunch(m.RelaunchDir)
		}
	}
	return 0
}

// relaunch replaces the current process with a fresh LTS instance in dir.
func relaunch(dir string) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not find executable: %v\n", err)
		os.Exit(1)
	}
	err = syscall.Exec(exe, []string{exe, "--dir", dir}, os.Environ())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not relaunch: %v\n", err)
		os.Exit(1)
	}
}
