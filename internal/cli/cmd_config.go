package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"lts-revamp/internal/config"

	"github.com/spf13/cobra"
)

func init() { register(addConfig) }

type configEntry struct {
	Key         string   `json:"key"`
	Value       string   `json:"value"`
	Default     string   `json:"default"`
	Source      string   `json:"source"` // "default" | "global" | "local"
	Kind        string   `json:"kind"`
	Options     []string `json:"options,omitempty"`
	Description string   `json:"description"`
}

func globalEntries() []configEntry {
	raw := config.RawGlobalFile()
	def := config.DefaultGlobal()
	cur := config.LoadReadOnly("").Global
	var out []configEntry
	for _, d := range config.GlobalKeys() {
		v, _ := cur.Get(d.Key)
		dv, _ := def.Get(d.Key)
		src := "default"
		if _, ok := raw[d.Key]; ok {
			src = "global"
		}
		out = append(out, configEntry{Key: d.Key, Value: v, Default: dv, Source: src, Kind: string(d.Kind), Options: cleanOpts(d.Options), Description: d.Description})
	}
	return out
}

func cleanOpts(opts []string) []string {
	var out []string
	for _, o := range opts {
		if o != "" {
			out = append(out, o)
		}
	}
	return out
}

func repoEntries(env *Env, repo string) []configEntry {
	raw := config.RawLocalFile(env.WorkDir)
	rc, ok := env.Cfg.Local[strings.ToUpper(repo)]
	if !ok {
		rc = config.DefaultRepoLocal()
	}
	defRC := config.DefaultRepoLocal()
	var out []configEntry
	for _, d := range config.RepoKeys() {
		v, _ := rc.Get(d.Key)
		dv, _ := defRC.Get(d.Key)
		src := "default"
		if _, ok := raw[strings.ToUpper(repo)+"_"+d.Key]; ok {
			src = "local"
		}
		// Effective values make more sense to readers than "" for overrides
		if d.Key == "PACKAGE_MANAGER" && v == "" {
			v = env.Cfg.Global.PackageManager
			src = "global"
		}
		if d.Key == "BASIS_BRANCH" && v == "" {
			v = "main"
		}
		if d.Key == "LTS_ROOT" && v == "" {
			v = env.WorkDir
		}
		out = append(out, configEntry{Key: d.Key, Value: v, Default: dv, Source: src, Kind: string(d.Kind), Options: cleanOpts(d.Options), Description: d.Description})
	}
	return out
}

func addConfig(a *app, root *cobra.Command) {
	var repoFlag string
	cfgCmd := &cobra.Command{
		Use:     "config",
		Short:   "Show or change settings (list, get, set, path)",
		GroupID: groupChange,
		Long: `Show LTS settings with their effective value and where each comes from.

Global settings live in ~/.config/lts/config ($LTS_CONFIG_DIR overrides the
directory); per-repo settings live in <workspace>/.lts.conf. Both are the
same files the TUI's Settings screen edits.

  lts config [list]           every setting (global + each repo)
  lts config get KEY [-r R]   one value, printed alone
  lts config set KEY VAL [-r R]  validate and save
  lts config path             config file locations

Per-repo keys (with -r): ` + strings.Join(config.RepoKeyNames(), ", ") + exitCodesHelp(ExitNotFound),
		Example: `  lts config
  lts config get PACKAGE_MANAGER
  lts config get BASIS_BRANCH -r core
  lts config set COPY_MCP_JSON true
  lts config set BASIS_BRANCH dev -r erp`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return a.configList(repoFlag) },
	}
	cfgCmd.PersistentFlags().StringVarP(&repoFlag, "repo", "r", "", "per-repo setting for this repo")

	cfgCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List every setting with value, default and source",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.configList(repoFlag) },
	})
	cfgCmd.AddCommand(&cobra.Command{
		Use:   "get KEY",
		Short: "Print one setting's effective value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.configGet(args[0], repoFlag)
		},
	})
	cfgCmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the config file locations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := a.loadEnv(envOpts{})
			if err != nil {
				return err
			}
			res := map[string]any{"global": config.GlobalConfigPath(), "local": config.LocalConfigPath(env.WorkDir), "workspace": env.WorkDir}
			a.out.Result(res, func(w io.Writer) {
				fmt.Fprintf(w, "global: %s\nlocal:  %s\n", res["global"], res["local"])
			})
			return nil
		},
	})
	addConfigSet(a, cfgCmd, &repoFlag)
	root.AddCommand(cfgCmd)
}

func (a *app) configList(repo string) error {
	env, err := a.loadEnv(envOpts{discover: true})
	if err != nil {
		return err
	}
	repos := map[string][]configEntry{}
	var names []string
	if repo != "" {
		r, err := env.findRepo(repo)
		if err != nil {
			return err
		}
		names = []string{r.Name}
	} else {
		for _, r := range env.realRepos(true) {
			names = append(names, r.Name)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		repos[n] = repoEntries(env, n)
	}
	global := globalEntries()
	res := map[string]any{
		"global_config_path": config.GlobalConfigPath(),
		"local_config_path":  config.LocalConfigPath(env.WorkDir),
		"global":             global,
		"repos":              repos,
	}
	a.out.Result(res, func(w io.Writer) {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Global  (%s)\n", config.GlobalConfigPath())
		for _, e := range global {
			mark := ""
			if e.Source == "default" {
				mark = "(default)"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", e.Key, displayVal(e.Value), mark)
		}
		for _, n := range names {
			fmt.Fprintf(tw, "\n%s  (%s)\n", n, config.LocalConfigPath(env.WorkDir))
			for _, e := range repos[n] {
				mark := ""
				if e.Source != "local" {
					mark = "(" + e.Source + ")"
				}
				fmt.Fprintf(tw, "  %s\t%s\t%s\n", e.Key, displayVal(e.Value), mark)
			}
		}
		tw.Flush()
	})
	return nil
}

func displayVal(v string) string {
	if v == "" {
		return `""`
	}
	return v
}

func (a *app) configGet(key, repo string) error {
	if repo != "" {
		env, err := a.loadEnv(envOpts{discover: true})
		if err != nil {
			return err
		}
		r, err := env.findRepo(repo)
		if err != nil {
			return err
		}
		if _, ok := config.LookupRepoKey(key); !ok {
			return unknownKeyErr(key, true)
		}
		for _, e := range repoEntries(env, r.Name) {
			if e.Key == strings.ToUpper(key) {
				a.out.Result(map[string]any{"repo": r.Name, "key": e.Key, "value": e.Value, "source": e.Source},
					func(w io.Writer) { fmt.Fprintln(w, e.Value) })
				return nil
			}
		}
	}
	if _, ok := config.LookupGlobalKey(key); !ok {
		e := unknownKeyErr(key, false)
		if _, isRepoKey := config.LookupRepoKey(key); isRepoKey {
			e.withHint("%s is a per-repo setting — add -r <repo>", strings.ToUpper(key))
		}
		return e
	}
	for _, e := range globalEntries() {
		if e.Key == strings.ToUpper(key) {
			a.out.Result(map[string]any{"key": e.Key, "value": e.Value, "source": e.Source},
				func(w io.Writer) { fmt.Fprintln(w, e.Value) })
		}
	}
	return nil
}

func unknownKeyErr(key string, repo bool) *Error {
	names := config.GlobalKeyNames()
	scope := "global"
	if repo {
		names = config.RepoKeyNames()
		scope = "per-repo"
	}
	return notFoundErr("unknown %s setting %q", scope, key).
		withSuggestions(suggest(strings.ToUpper(key), names, 3)).
		withHint("run `lts config` to list settings")
}
