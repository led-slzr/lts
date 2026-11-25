package cli

import (
	"fmt"
	"path/filepath"
	"strings"
)

const targetHelp = `

Targets: a branch (feat/login), folder (feat-login), id (core-lts/feat-login)
or path. Omit it to use the worktree containing the current directory. When
a branch exists in several repos, add -r <repo> (or -r a,b for a mono group).`

var exitMeanings = map[int]string{
	ExitOK:          "success",
	ExitError:       "error (also: repo busy)",
	ExitUsage:       "usage error (bad flag/argument; suggestions included)",
	ExitNotFound:    "target or repo not found / ambiguous (suggestions included)",
	ExitGuard:       "refused by a safety guard — needs confirmation (--yes) or --force",
	ExitPartial:     "partial failure — some repos/worktrees failed",
	ExitConflict:    "rebase conflict — rebase aborted, changes restored",
	ExitExists:      "already exists (use --reuse to accept the existing one)",
	ExitInterrupted: "interrupted (Ctrl+C) — re-run to finish (create --reuse is idempotent)",
}

// exitCodesHelp renders the exit-code section for a command's help.
func exitCodesHelp(codes ...int) string {
	all := append([]int{ExitOK, ExitError, ExitUsage}, codes...)
	var b strings.Builder
	b.WriteString("\n\nExit codes:\n")
	seen := map[int]bool{}
	for _, c := range all {
		if seen[c] {
			continue
		}
		seen[c] = true
		fmt.Fprintf(&b, "  %-4d %s\n", c, exitMeanings[c])
	}
	b.WriteString("\nAdd --json for machine-readable output; errors then include .error.code,\n.error.hint and .error.suggestions.")
	return b.String()
}

func filepathBase(p string) string { return filepath.Base(p) }
