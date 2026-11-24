// Package cli implements LTS's scriptable subcommands (`lts ls`, `lts
// create`, ...). Bare `lts` still launches the TUI. Every command follows
// one contract, documented in `lts help agents`: results on stdout, logs on
// stderr, `--json` everywhere, stable exit codes, no prompts without a TTY.
package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"lts-revamp/internal/version"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// TUIFunc launches the interactive UI in dir and returns an exit code.
type TUIFunc func(dir string) int

type globalFlags struct {
	dir     string
	json    bool
	quiet   bool
	verbose bool
	noColor bool
	noInput bool
	wait    time.Duration
}

type app struct {
	flags       globalFlags
	out         *Output
	tui         TUIFunc
	root        *cobra.Command
	interrupted atomic.Bool
	// streaming commands (watch) end normally on Ctrl+C
	streaming bool
}

const (
	groupInspect = "inspect"
	groupChange  = "change"
	groupRemove  = "remove"
	groupAgents  = "agents"
	groupMeta    = "meta"
)

// Execute runs the CLI and returns the process exit code.
func Execute(args []string, tui TUIFunc) int {
	a := &app{tui: tui}
	root := a.newRoot()
	a.root = root
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetIn(stdin)

	// Early output so errors raised before flag parsing still honour --json
	a.out = newOutput(hasFlag(args, "--json"), false, false, hasFlag(args, "--no-color"), hasFlag(args, "--no-input"))

	cmd, err := root.ExecuteC()
	if err != nil {
		e := a.translateCobraError(cmd, err)
		a.out.Failure(e)
		return e.Exit
	}
	if a.interrupted.Load() && a.streaming {
		return ExitOK
	}
	if a.interrupted.Load() {
		// The command finished its work and already printed its one
		// result; don't add a second JSON document — just the exit code
		if !a.out.wrote {
			a.out.Failure(newErr(ExitInterrupted, CodeInterrupted, "interrupted"))
		} else {
			fmt.Fprintln(stderr, "interrupted (the command had already finished)")
		}
		return ExitInterrupted
	}
	return ExitOK
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

func (a *app) newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "lts",
		Short: "LTS — git worktree manager (TUI + scriptable CLI)",
		Long: `LTS manages git worktrees for every repo in a workspace folder.

Run ` + "`lts`" + ` with no command to open the interactive TUI. Use the commands
below from scripts, CI, or AI agents: results go to stdout, progress to
stderr, every command accepts --json, and nothing ever prompts when stdin
is not a terminal.

Layout: worktrees of <repo> live in <repo>-lts/<branch-dir>/ next to the
repos (or under a per-repo LTS_ROOT); multi-repo "mono groups" live in
<a>-<b>-lts/<branch-dir>/<repo>-<branch-dir>/.

New here (human or agent)? Start with:
  lts help agents        contract, exit codes, JSON shapes, workflows
  lts ls                 what repos and worktrees exist`,
		Version:       version.Full(),
		SilenceErrors: true,
		SilenceUsage:  true,
		// Bare `lts` (no subcommand) opens the TUI, exactly like before v3.2
		RunE: func(cmd *cobra.Command, args []string) error {
			// An agent or script running bare `lts` must not hang in a TUI
			if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
				return usageErr("the interactive TUI needs a terminal (stdin/stdout are not a TTY)").
					withHint("use a subcommand instead, e.g. `lts ls` or `lts status --all`; see `lts help agents`")
			}
			dir := a.flags.dir
			if dir == "" {
				if wd, err := os.Getwd(); err == nil {
					dir = wd
				}
			}
			if code := a.tui(dir); code != 0 {
				return &Error{Exit: code, Code: CodeError, Message: "TUI exited with an error"}
			}
			return nil
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			a.out = newOutput(a.flags.json, a.flags.quiet, a.flags.verbose, a.flags.noColor, a.flags.noInput)
			if cmd != cmd.Root() {
				a.trapInterrupts()
				// Never let git block on a credential prompt nobody can answer
				if !a.out.Interactive && os.Getenv("GIT_TERMINAL_PROMPT") == "" {
					os.Setenv("GIT_TERMINAL_PROMPT", "0")
				}
			}
			return nil
		},
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.SuggestionsMinimumDistance = 2

	pf := root.PersistentFlags()
	pf.StringVar(&a.flags.dir, "dir", "", "workspace folder containing your repos (commands: default $LTS_DIR, else found from the current directory; the TUI: the current directory)")
	pf.BoolVar(&a.flags.json, "json", false, "machine-readable JSON on stdout (errors too)")
	pf.BoolVarP(&a.flags.quiet, "quiet", "q", false, "suppress progress output on stderr")
	pf.BoolVar(&a.flags.verbose, "verbose", false, "extra diagnostics on stderr")
	pf.BoolVar(&a.flags.noColor, "no-color", false, "disable colored output (also: NO_COLOR env)")
	pf.DurationVar(&a.flags.wait, "wait", 0, "when a repo is busy (another lts process), wait up to this long (e.g. 2m; bare --wait = 10m) instead of failing")
	pf.Lookup("wait").NoOptDefVal = "10m"
	pf.BoolVar(&a.flags.noInput, "no-input", false, "never prompt; fail with exit 4 when confirmation is needed (implied when stdin is not a TTY or CI is set)")

	root.AddGroup(
		&cobra.Group{ID: groupInspect, Title: "Inspect:"},
		&cobra.Group{ID: groupChange, Title: "Create & change:"},
		&cobra.Group{ID: groupRemove, Title: "Remove:"},
		&cobra.Group{ID: groupAgents, Title: "Agents & automation:"},
		&cobra.Group{ID: groupMeta, Title: "Other:"},
	)
	root.SetHelpCommandGroupID(groupMeta)
	root.SetCompletionCommandGroupID(groupMeta)
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return a.flagError(cmd, err)
	})

	root.AddCommand(&cobra.Command{
		Use:     "tui",
		Short:   "Open the interactive TUI (same as bare `lts`)",
		GroupID: groupMeta,
		Args:    cobra.NoArgs,
		RunE:    root.RunE,
	})
	root.AddCommand(&cobra.Command{
		Use:     "version",
		Short:   "Print the LTS version",
		GroupID: groupMeta,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a.out.Result(map[string]any{"version": version.Version, "full": version.Full()}, func(w io.Writer) {
				fmt.Fprintln(w, version.Full())
			})
			return nil
		},
	})

	for _, add := range commandRegistry {
		add(a, root)
	}
	addCompletions(a, root) // after every command exists
	return root
}

// unknownSubcommand is the RunE of command groups (hook, skill): bare →
// help; anything else → usage error with suggestions (cobra would print
// help and exit 0).
func unknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	var names []string
	for _, c := range cmd.Commands() {
		names = append(names, c.Name())
	}
	e := usageErr("unknown %s subcommand %q", cmd.Name(), args[0]).withSuggestions(suggest(args[0], names, 3))
	e.Available = names
	return e
}

// commandRegistry is filled by each command file's init().
var commandRegistry []func(a *app, root *cobra.Command)

func register(f func(a *app, root *cobra.Command)) { commandRegistry = append(commandRegistry, f) }

var unknownCmdRe = regexp.MustCompile(`^unknown command "([^"]*)" for "([^"]*)"`)

// translateCobraError turns cobra's errors into *Error with suggestions.
func (a *app) translateCobraError(cmd *cobra.Command, err error) *Error {
	if ce, ok := err.(*Error); ok {
		return ce
	}
	msg := err.Error()
	if m := unknownCmdRe.FindStringSubmatch(msg); m != nil {
		bad := m[1]
		var names []string
		for _, c := range a.root.Commands() {
			if c.IsAvailableCommand() || c.Name() == "help" {
				names = append(names, c.Name()) // canonical names only
			}
		}
		var sugg []string
		for _, s := range a.root.SuggestionsFor(bad) {
			if c, _, err := a.root.Find([]string{s}); err == nil && !contains(sugg, c.Name()) {
				sugg = append(sugg, c.Name())
			}
		}
		for _, s := range suggest(bad, names, 3) {
			if !contains(sugg, s) {
				sugg = append(sugg, s)
			}
		}
		return usageErr("unknown command %q", bad).withSuggestions(sugg).
			withHint("run `lts help` for the command list")
	}
	if strings.HasPrefix(msg, "accepts ") || strings.HasPrefix(msg, "requires ") ||
		strings.HasPrefix(msg, "unknown shorthand") || strings.HasPrefix(msg, "invalid argument") {
		name, use := "lts", ""
		if cmd != nil {
			name, use = cmd.CommandPath(), cmd.UseLine()
		}
		if strings.HasPrefix(msg, "requires at least") || (strings.HasPrefix(msg, "accepts ") && strings.Contains(msg, "received 0")) {
			if args := missingArgNames(use); args != "" {
				msg = "missing " + args
			}
		}
		return usageErr("%s", msg).withHint("usage: %s — run `%s --help` for details", use, name)
	}
	return asCLIError(err)
}

var argNameRe = regexp.MustCompile(`<[^>]+>`)

// missingArgNames lists the required <args> of a usage line.
func missingArgNames(use string) string {
	return strings.Join(argNameRe.FindAllString(use, -1), " ")
}

var unknownFlagRe = regexp.MustCompile(`unknown flag: --([^\s]+)`)

// flagError adds did-you-mean suggestions for mistyped flags.
func (a *app) flagError(cmd *cobra.Command, err error) error {
	e := usageErr("%s", err.Error()).withHint("run `%s --help` for usage", cmd.CommandPath())
	if m := unknownFlagRe.FindStringSubmatch(err.Error()); m != nil {
		var names []string
		visit := func(f *pflag.Flag) { names = append(names, f.Name) }
		cmd.Flags().VisitAll(visit)
		cmd.InheritedFlags().VisitAll(visit)
		for _, s := range suggest(m[1], names, 3) {
			e.Suggestions = append(e.Suggestions, "--"+s)
		}
	}
	return e
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// trapInterrupts makes the first Ctrl+C stop multi-step commands before
// their next step and exit 130; a second Ctrl+C exits immediately. A
// terminal Ctrl+C also reaches the running git/install child, which may
// cut that one step short — re-running (e.g. create --reuse) completes it.
func (a *app) trapInterrupts() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ch
		a.interrupted.Store(true)
		if !a.streaming {
			fmt.Fprintln(os.Stderr, "\ninterrupt received — stopping after the current step (Ctrl+C again to force quit)")
		}
		<-ch
		os.Exit(ExitInterrupted)
	}()
}
