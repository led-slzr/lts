package cli

import (
	"fmt"
	"io"
	"strings"

	"lts-revamp/internal/agentskills"

	"github.com/spf13/cobra"
)

func init() { register(addSkill) }

func addSkill(a *app, root *cobra.Command) {
	skill := &cobra.Command{
		Use:     "skill",
		Short:   "Install / print the LTS agent skills (Claude Code, AGENTS.md)",
		GroupID: groupAgents,
		Long: `LTS ships agent skills that teach AI agents to use it safely:
` + strings.Join(prefixed(agentskills.Names()), ", ") + `.

Claude Code users can instead install the plugin (skills + hooks, auto-updating):
  /plugin marketplace add led-slzr/lts
  /plugin install lts@lts

  lts skill install     write the skills to ~/.claude/skills and add the
                        SessionStart/SubagentStart context hooks to
                        ~/.claude/settings.json ($CLAUDE_CONFIG_DIR respected)
  lts skill uninstall   remove them again
  lts skill print       print the skills (paste into AGENTS.md for other agents)
  lts skill list        list bundled skills`,
	}

	var noHooks, worktreeHook, dryRun bool
	var dir string
	install := &cobra.Command{
		Use:   "install",
		Short: "Install the skills (and context hooks) for Claude Code",
		Long: `Write the bundled skills to <claude-dir>/skills/lts-*/SKILL.md and merge
LTS hook entries into <claude-dir>/settings.json. Existing settings and
hooks are preserved; the original file is backed up once to
settings.json.lts-backup; re-running is a no-op.

Hooks added: SessionStart + SubagentStart (lts hook session-start /
subagent-start — a short LTS summary in every session; silent outside LTS
workspaces). --worktree-hook also adds WorktreeCreate so Claude Code's
isolated worktrees become LTS worktrees. --no-hooks installs skills only.` + exitCodesHelp(),
		Example: `  lts skill install
  lts skill install --no-hooks
  lts skill install --worktree-hook --dry-run --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := agentskills.Install(agentskills.Options{ClaudeDir: dir, Hooks: !noHooks, WorktreeHook: worktreeHook, DryRun: dryRun})
			if err != nil {
				return newErr(ExitError, CodeError, "%s", err.Error())
			}
			a.out.Result(res, func(w io.Writer) { renderSkillResult(w, res, "Installed", "Would install") })
			return nil
		},
	}
	install.Flags().BoolVar(&noHooks, "no-hooks", false, "skills only; don't touch settings.json")
	install.Flags().BoolVar(&worktreeHook, "worktree-hook", false, "also route Claude Code's WorktreeCreate through LTS")
	install.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written")
	install.Flags().StringVar(&dir, "claude-dir", "", "Claude config dir (default $CLAUDE_CONFIG_DIR or ~/.claude)")
	skill.AddCommand(install)

	var udry bool
	var udir string
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the LTS skills and hook entries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := agentskills.Uninstall(agentskills.Options{ClaudeDir: udir, DryRun: udry})
			if err != nil {
				return newErr(ExitError, CodeError, "%s", err.Error())
			}
			a.out.Result(res, func(w io.Writer) { renderSkillResult(w, res, "Removed", "Would remove") })
			return nil
		},
	}
	uninstall.Flags().BoolVar(&udry, "dry-run", false, "show what would be removed")
	uninstall.Flags().StringVar(&udir, "claude-dir", "", "Claude config dir (default $CLAUDE_CONFIG_DIR or ~/.claude)")
	skill.AddCommand(uninstall)

	skill.AddCommand(&cobra.Command{
		Use:   "print [name...]",
		Short: "Print skills as Markdown (for AGENTS.md / other agents)",
		RunE: func(cmd *cobra.Command, args []string) error {
			all := agentskills.Names()
			want := map[string]bool{}
			for _, n := range args {
				n = strings.TrimPrefix(n, agentskills.Prefix)
				if !contains(all, n) {
					return notFoundErr("no skill named %q", n).withSuggestions(suggest(n, all, 3))
				}
				want[n] = true
			}
			var parts []string
			var names []string
			for _, s := range agentskills.Skills() {
				if len(want) > 0 && !want[s.Name] {
					continue
				}
				names = append(names, s.Name)
				parts = append(parts, agentskills.Standalone(s, all))
			}
			text := strings.Join(parts, "\n\n")
			a.out.Result(map[string]any{"skills": names, "markdown": text}, func(w io.Writer) { fmt.Fprint(w, text) })
			return nil
		},
	})
	skill.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List bundled skills",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			names := prefixed(agentskills.Names())
			a.out.Result(map[string]any{"skills": names}, func(w io.Writer) { fmt.Fprintln(w, strings.Join(names, "\n")) })
			return nil
		},
	})
	root.AddCommand(skill)
}

func prefixed(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = agentskills.Prefix + n
	}
	return out
}

func renderSkillResult(w io.Writer, r agentskills.Result, done, would string) {
	verb := done
	if r.DryRun {
		verb = would
	}
	fmt.Fprintf(w, "%s skills in %s: %s\n", verb, r.SkillsDir, strings.Join(r.Skills, ", "))
	if len(r.Hooks) > 0 {
		fmt.Fprintf(w, "%s hooks in %s: %s\n", verb, r.SettingsPath, strings.Join(r.Hooks, ", "))
	}
	if r.Backup != "" {
		fmt.Fprintf(w, "Original settings backed up to %s\n", r.Backup)
	}
}
