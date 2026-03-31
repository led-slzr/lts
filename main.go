package main

import (
	"fmt"
	"os"
	"syscall"

	"lts-revamp/internal/app"
	"lts-revamp/internal/config"
	"lts-revamp/internal/ui"
	"lts-revamp/internal/version"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Handle --version flag
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-v" {
			fmt.Println(version.Full)
			os.Exit(0)
		}
	}

	workDir := getWorkDir()

	// Validate workDir exists and is a directory
	info, err := os.Stat(workDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: directory does not exist: %s\n", workDir)
		os.Exit(1)
	}
	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Error: not a directory: %s\n", workDir)
		os.Exit(1)
	}

	// First-run setup wizard
	if config.IsFirstRun() {
		setupModel := ui.NewSetup()
		p := tea.NewProgram(
			setupModel,
			tea.WithAltScreen(),
		)
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
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
		os.Exit(1)
	}

	// Check if we need to relaunch in a different directory
	if m, ok := finalModel.(app.Model); ok && m.RelaunchDir != "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: could not find executable: %v\n", err)
			os.Exit(1)
		}
		// Replace the current process with a new LTS instance
		err = syscall.Exec(exe, []string{exe, "--dir", m.RelaunchDir}, os.Environ())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: could not relaunch: %v\n", err)
			os.Exit(1)
		}
	}
}

func getWorkDir() string {
	// Check --dir flag
	for i, arg := range os.Args {
		if arg == "--dir" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}

	// Default: current working directory
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not determine working directory: %v\n", err)
		os.Exit(1)
	}
	return cwd
}
