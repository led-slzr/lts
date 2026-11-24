package cli

import (
	"fmt"
	"io"
	"strings"

	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/tools"

	"github.com/spf13/cobra"
)

func init() { register(addOpen) }

func addOpen(a *app, root *cobra.Command) {
	var ide, ai, term, dryRun bool
	var with, member string
	var repoFlag []string
	cmd := &cobra.Command{
		Use:     "open [target] [--ide | --ai | --terminal]",
		Short:   "Open a worktree in your IDE, AI CLI or terminal (like clicking it in the TUI)",
		GroupID: groupChange,
		Long: `Open a worktree exactly as clicking it in the TUI does:

  --ide        the IDE (IDE_COMMAND) on the worktree's .code-workspace file —
               a mono group opens its shared workspace with every member
  --ai         the AI CLI (AI_CLI_COMMAND) in a new terminal (TERMINAL)
  --terminal   a shell in a new terminal

Without a mode flag the TUI's current click mode (CLICK_USAGE) is used.
With TERMINAL_MULTIPLEXER=tmux, --ai and --terminal attach the worktree's
persistent tmux session (created on first open), so an AI conversation
survives closing the window. --member <repo> opens one mono-group member
instead of the whole group (--ai / --terminal start in that member).

Useful for agents handing work back: "the fix is ready — lts open feat/x --ide".` + targetHelp + exitCodesHelp(ExitNotFound),
		Example: `  lts open --ide                    # the worktree you're in
  lts open feat/login --ai
  lts open feat/integration -r core,erp --terminal --member erp
  lts open feat/login --with terminal --dry-run --json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := 0
			for _, b := range []bool{ide, ai, term} {
				if b {
					n++
				}
			}
			if n > 1 || (n == 1 && with != "") {
				return usageErr("pick one of --ide, --ai, --terminal (or --with MODE)")
			}
			env, err := a.loadEnv(envOpts{discover: true})
			if err != nil {
				return err
			}
			mode := with
			switch {
			case ide:
				mode = "ide"
			case ai:
				mode = "ai"
			case term:
				mode = "terminal"
			case mode == "":
				mode = env.Cfg.Global.ClickUsage
			}
			usage, err := parseUsage(mode)
			if err != nil {
				return err
			}
			t, err := env.resolveTarget(firstArg(args), splitRepoFlag(repoFlag))
			if err != nil {
				return err
			}
			path := t.WT.Path
			if member != "" {
				if !t.Repo.IsMonorepo {
					return usageErr("--member only applies to mono-group worktrees; %s is a single-repo worktree", t.ID())
				}
				var names []string
				found := false
				for _, m := range git.MonoMembers(t.WT.Path) {
					names = append(names, m.Repo)
					if strings.EqualFold(m.Repo, member) {
						path, found = m.Path, true
					}
				}
				if !found {
					e := notFoundErr("%s has no member %q", t.ID(), member).withSuggestions(suggest(member, names, 3))
					e.Available = names
					return e
				}
			}
			o := openerOpts(&env.Cfg)
			program, key := "", ""
			switch usage {
			case opener.ClickIDE:
				program, key = o.IDECommand, "IDE_COMMAND"
				if program == "" || !tools.CommandPresent(program) {
					return newErr(ExitError, CodeError, "IDE command %q is not installed", tools.FirstWord(program)).
						withHint("set it: lts config set IDE_COMMAND <code|cursor|windsurf|zed|...>")
				}
			case opener.ClickAICli:
				program, key = o.AICliCommand, "AI_CLI_COMMAND"
				if program == "" {
					return usageErr("no AI CLI configured").withHint("set it: lts config set AI_CLI_COMMAND claude")
				}
				if !tools.CommandPresent(program) {
					return newErr(ExitError, CodeError, "AI CLI %q is not installed", tools.FirstWord(program)).withHint("lts config set AI_CLI_COMMAND <command>")
				}
			}
			if usage != opener.ClickIDE && !tools.TerminalPresent(o.Terminal) {
				return newErr(ExitError, CodeError, "terminal %q was not found", o.Terminal).withHint("lts config set TERMINAL <ghostty|iterm|terminal|wezterm|alacritty|kitty>")
			}
			tmux := o.Multiplexer == "tmux" && usage != opener.ClickIDE
			if tmux && !opener.TmuxAvailable() {
				a.out.Warn("TERMINAL_MULTIPLEXER is tmux but tmux isn't installed — opening a plain terminal")
				tmux = false
			}
			res := map[string]any{"id": t.ID(), "path": path, "mode": mode, "program": program, "config_key": key, "tmux": tmux, "dry_run": dryRun}
			if tmux {
				res["tmux_session"] = opener.SessionName(path)
			}
			if !dryRun {
				if err := opener.OpenWorktree(path, usage, o); err != nil {
					return newErr(ExitError, CodeError, "could not open %s: %s", path, err.Error())
				}
			}
			a.out.Result(res, func(w io.Writer) {
				verb := "Opened"
				if dryRun {
					verb = "Would open"
				}
				extra := ""
				if tmux {
					extra = " (tmux session " + opener.SessionName(path) + ")"
				}
				fmt.Fprintf(w, "%s %s in %s%s\n", verb, t.ID(), usage.String(), extra)
			})
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&ide, "ide", false, "open in the IDE (workspace file)")
	fl.BoolVar(&ai, "ai", false, "open the AI CLI in a terminal")
	fl.BoolVar(&term, "terminal", false, "open a shell in a terminal")
	fl.StringVar(&with, "with", "", "mode by name: ide | ai | terminal (default: CLICK_USAGE)")
	fl.StringVar(&member, "member", "", "mono group: open just this member repo's worktree")
	fl.BoolVar(&dryRun, "dry-run", false, "show what would be opened")
	fl.StringSliceVarP(&repoFlag, "repo", "r", nil, "disambiguate by repo")
	root.AddCommand(cmd)
}
