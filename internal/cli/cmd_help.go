package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"lts-revamp/internal/version"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func init() {
	register(addAgentsTopic)
	register(addHelp)
}

const agentsGuide = `LTS for AI agents and scripts — the contract

LTS manages git worktrees for every repo in a workspace folder (the folder
that contains the repos). Work on a branch happens in an isolated worktree,
never in a repo's main checkout.

WHERE COMMANDS RUN
  The workspace is --dir, else $LTS_DIR, else found from the current
  directory — running from inside any repo or LTS worktree works.
  Layout:   <ws>/<repo>/                       main checkout (don't edit here)
            <ws>/<repo>-lts/<branch-dir>/      a worktree (or <LTS_ROOT>/<repo>-lts/)
            <ws>/<a>-<b>-lts/<branch-dir>/     a mono group (one dir per member repo)
  Every worktree has a stable id "<lts-dir>/<folder>" (e.g. core-lts/feat-login)
  accepted wherever a target is.

OUTPUT CONTRACT
  • stdout carries only the result; progress, logs and warnings go to stderr.
  • --json on any command → one JSON object on stdout:
      {"schema_version":1,"ok":true, ...fields}
      {"schema_version":1,"ok":false,"error":{"code","message","hint",
                                              "suggestions":[],"available":[]}}
    Partial results ride along on failure (e.g. .results, .candidates).
  • Text output is stable for the first line: create / path / rename print
    exactly one path, so cd "$(lts path feat/x)" and cd "$(lts create ...)" work.
  • Never parse stderr; use --json. -q silences progress.

EXIT CODES
  0   ok
  1   error — including code "busy": another lts process (TUI or CLI)
      holds the repo; retry or add --wait=2m
  2   usage — bad flag/argument/value (see .error.suggestions)
  3   not found / ambiguous — unknown repo, branch, worktree or setting.
      .error.suggestions holds the closest matches; ambiguous matches list
      every candidate id. Suggestions are never auto-applied: pick one and re-run.
  4   refused by a safety guard — needs --yes and/or --force, or the action
      is unsupported (e.g. rebase of a mono group). Treat as "ask the user".
  5   partial failure — some repos/worktrees failed (details in JSON)
  6   rebase conflict — the rebase was aborted and changes restored
  7   already exists — create found the worktree; --reuse returns it with exit 0
  130 interrupted

PROMPTS
  Nothing prompts when stdin is not a terminal, --no-input is set, or $CI is
  set. Instead the command exits 4 and the hint names the exact flag needed.
  Git credential prompts are disabled in that mode too (GIT_TERMINAL_PROMPT=0).

SAFETY RULES (follow these)
  • Run destructive commands with --dry-run first and show the result:
      lts delete X --dry-run   lts cleanup-merged --dry-run
  • Never pass --force (destroys uncommitted/unpushed work) or --remote
    (deletes branches on origin) unless the user explicitly asked for it.
  • Exit 4 means a human decision is needed — don't escalate flags to get past it.
  • Protected branches (main, master, develop, development, staging,
    production, release/*) are never deleted by LTS.
  • create changes each repo's MAIN checkout: auto-stashes its uncommitted
    changes, switches it to the basis branch, pulls --ff-only. --dry-run
    lists these side effects ("side_effect": true).

COMMON WORKFLOWS
  Orient:        lts ls --json                     what exists (fast)
                 lts status --all --json           state of every worktree
  Start a task:  lts create feat/x -r core --dry-run
                 cd "$(lts create feat/x -r core --reuse -q)"
  Multi-repo:    lts create feat/x -r core,erp     mono group (one branch, all repos)
  Stay current:  lts rebase --refresh              (inside the worktree)
  Ship:          git push -u origin feat/x && lts pr --print
  Finish:        lts delete feat/x --dry-run, then --yes once merged
  Tidy:          lts cleanup-merged --dry-run, then --yes
  Settings:      lts config --json; lts config set KEY VALUE [-r repo]

ORCHESTRATING PARALLEL WORKERS
  • One worktree per worker: lts create <task-branch> -r <repo> --reuse.
    --reuse makes retries idempotent (exit 0 with the existing path).
  • Work spanning repos: one mono group per task (-r a,b) — every member
    shares the branch name and one workspace.
  • Workers on different repos run concurrently; operations on the same
    repo serialize through per-repo locks — pass --wait=5m instead of
    failing on "busy".
  • Fleet view: lts status --all --json (state, ahead/behind, busy,
    dangerous_to_delete per worktree).
  • After merge: lts cleanup-merged --yes removes finished worktrees.

ENVIRONMENT
  LTS_DIR          default workspace folder
  LTS_CONFIG_DIR   global config dir (default ~/.config/lts) — use a temp
                   dir for sandboxes so tests never touch real settings
  NO_COLOR, CI     disable color / disable prompts

DISCOVERY
  lts help <command>   full help with examples and exit codes
  lts help --json      every command, flag and default as JSON
`

func addAgentsTopic(a *app, root *cobra.Command) {
	root.AddCommand(&cobra.Command{
		Use:     "agents",
		Short:   "Guide for AI agents and scripts: contract, exit codes, JSON, workflows",
		GroupID: groupAgents,
		Long:    agentsGuide,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a.out.Result(map[string]any{"guide": agentsGuide}, func(w io.Writer) { fmt.Fprint(w, agentsGuide) })
			return nil
		},
	})
}

type flagSpec struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default"`
	Usage     string `json:"usage"`
}

type cmdSpec struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Aliases  []string   `json:"aliases,omitempty"`
	Group    string     `json:"group,omitempty"`
	Usage    string     `json:"usage"`
	Short    string     `json:"short"`
	Long     string     `json:"long,omitempty"`
	Example  string     `json:"example,omitempty"`
	Flags    []flagSpec `json:"flags"`
	Commands []cmdSpec  `json:"commands,omitempty"`
}

func specFor(c *cobra.Command) cmdSpec {
	s := cmdSpec{Name: c.Name(), Path: c.CommandPath(), Aliases: c.Aliases, Group: c.GroupID,
		Usage: c.UseLine(), Short: c.Short, Long: c.Long, Example: c.Example, Flags: []flagSpec{}}
	c.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "help" {
			return
		}
		s.Flags = append(s.Flags, flagSpec{Name: f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: f.DefValue, Usage: f.Usage})
	})
	for _, sub := range c.Commands() {
		if sub.Hidden || sub.Name() == "__complete" || sub.Name() == "__completeNoDesc" {
			continue
		}
		s.Commands = append(s.Commands, specFor(sub))
	}
	sort.Slice(s.Commands, func(i, j int) bool { return s.Commands[i].Name < s.Commands[j].Name })
	return s
}

func addHelp(a *app, root *cobra.Command) {
	help := &cobra.Command{
		Use:     "help [command]",
		GroupID: groupMeta,
		Short:   "Help about any command (help agents: guide for AI agents; --json: full spec)",
		Long: `Show help for a command. ` + "`lts help agents`" + ` explains the output contract,
exit codes and workflows; ` + "`lts help --json`" + ` dumps every command and flag.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.flags.json {
				target := root
				if len(args) > 0 {
					if c, _, err := root.Find(args); err == nil {
						target = c
					}
				}
				spec := specFor(target)
				exits := map[string]string{}
				for code, meaning := range exitMeanings {
					exits[fmt.Sprint(code)] = meaning
				}
				a.out.Result(map[string]any{"version": version.Version, "exit_codes": exits, "command": spec}, nil)
				return nil
			}
			if len(args) == 0 {
				return root.Help()
			}
			if len(args) == 1 && args[0] == "agents" {
				fmt.Fprint(a.out.Out, agentsGuide)
				return nil
			}
			c, rest, err := root.Find(args)
			if err != nil || c == root || len(rest) > 0 {
				var names []string
				for _, sc := range root.Commands() {
					names = append(names, sc.Name())
				}
				return usageErr("unknown help topic %q", strings.Join(args, " ")).
					withSuggestions(suggest(args[0], names, 3)).withHint("run `lts help` for the command list")
			}
			return c.Help()
		},
	}
	root.SetHelpCommand(help)
}

// Shell completion for repos (-r) and worktree targets.
func addCompletions(a *app, root *cobra.Command) {
	for _, c := range root.Commands() {
		c := c
		if c.Flags().Lookup("repo") != nil {
			c.RegisterFlagCompletionFunc("repo", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				return a.completeRepos(), cobra.ShellCompDirectiveNoFileComp
			})
		}
		switch c.Name() {
		case "status", "path", "rebase", "rename", "delete", "clean-modules", "pr":
			c.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				if len(args) > 0 {
					return nil, cobra.ShellCompDirectiveNoFileComp
				}
				return a.completeTargets(), cobra.ShellCompDirectiveNoFileComp
			}
		case "ls", "refresh":
			c.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
				return a.completeRepos(), cobra.ShellCompDirectiveNoFileComp
			}
		}
	}
}

func (a *app) completionEnv() *Env {
	a.out = newOutput(false, true, false, true, true)
	env, err := a.loadEnv(envOpts{discover: true, dryRun: true}) // never migrate from a completion
	if err != nil {
		return nil
	}
	return env
}

func (a *app) completeRepos() []string {
	env := a.completionEnv()
	if env == nil {
		return nil
	}
	var out []string
	for _, r := range env.realRepos(false) {
		out = append(out, r.Name)
	}
	return out
}

func (a *app) completeTargets() []string {
	env := a.completionEnv()
	if env == nil {
		return nil
	}
	var out []string
	for _, t := range env.allTargets() {
		out = append(out, t.WT.Branch+"\t"+t.ID())
	}
	return out
}
