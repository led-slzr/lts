package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// KeyKind describes how a setting's value is validated.
type KeyKind string

const (
	KindBool KeyKind = "bool"
	KindEnum KeyKind = "enum" // value must be one of Options
	KindText KeyKind = "text" // free-form (Options, if any, are suggestions)
	KindInt  KeyKind = "int"
)

// KeyDef documents one settable key. It is the single source of truth for
// validation shared by the Settings UI and `lts config set`.
type KeyDef struct {
	Key         string   `json:"key"`
	Kind        KeyKind  `json:"kind"`
	Options     []string `json:"options,omitempty"`
	AllowEmpty  bool     `json:"allow_empty,omitempty"`
	Min, Max    int      `json:"-"`
	Description string   `json:"description"`
}

var globalKeys = []KeyDef{
	{Key: "IDE_COMMAND", Kind: KindText, Options: []string{"windsurf", "code", "cursor", "zed"}, Description: "Command that opens a worktree / workspace file in your IDE"},
	{Key: "AI_CLI_COMMAND", Kind: KindText, AllowEmpty: true, Options: []string{"claude", "opencode"}, Description: "AI CLI started in AI-CLI click mode and workspace tasks (empty disables)"},
	{Key: "TERMINAL", Kind: KindText, Options: []string{"ghostty", "iterm", "terminal", "wezterm", "alacritty", "kitty"}, Description: "Terminal emulator used for AI CLI / terminal opens"},
	{Key: "TERMINAL_MULTIPLEXER", Kind: KindEnum, Options: []string{"none", "tmux"}, Description: "tmux = one persistent session per worktree"},
	{Key: "TMUX_AI_PANE_WIDTH", Kind: KindInt, Min: 20, Max: 90, Description: "AI pane width as percent of the tmux window"},
	{Key: "TMUX_RIGHT_PANES", Kind: KindEnum, Options: []string{"1", "2", "3"}, Description: "Stacked shell panes in the right tmux column"},
	{Key: "AUTO_KILL_TMUX", Kind: KindEnum, Options: []string{"OFF", "8H", "1D", "3D", "7D"}, Description: "Kill unattached LTS tmux sessions idle longer than this"},
	{Key: "PACKAGE_MANAGER", Kind: KindEnum, Options: []string{"auto", "pnpm", "npm", "yarn", "bun"}, Description: "Default package manager (auto = detect per repo)"},
	{Key: "AUTO_REFRESH", Kind: KindEnum, Options: []string{"OFF", "15M", "30M", "1H", "6H", "12H", "24H"}, Description: "Reserved: stored but not acted on by this version"},
	{Key: "AUTO_CLEAN_MODULES", Kind: KindEnum, Options: []string{"OFF", "1D", "3D", "7D", "14D", "30D"}, Description: "Remove node_modules from worktrees idle longer than this (TUI startup/hourly)"},
	{Key: "SORT_ORDER", Kind: KindEnum, Options: []string{"activity", "created", "name"}, Description: "Ordering of repos and worktrees"},
	{Key: "SHOW_HIDDEN_REPOS", Kind: KindBool, Description: "Show hidden repos grayed out in the TUI"},
	{Key: "LAYOUT", Kind: KindEnum, Options: []string{"board", "explorer"}, Description: "TUI main view layout"},
	{Key: "CLICK_USAGE", Kind: KindEnum, Options: []string{"ide", "ai", "terminal"}, Description: "TUI click mode restored on launch"},
	{Key: "THEME", Kind: KindText, Description: "TUI color theme key"},
	{Key: "LAUNCH_GREETING", Kind: KindBool, Description: "Greeting line in the TUI header"},
	{Key: "DONE_SOUND", Kind: KindEnum, Options: []string{"off", "glass", "submarine", "ping", "pop", "hero", "bell"}, Description: "Sound played when a worktree finishes creating"},
	{Key: "SIZE_SCANNING", Kind: KindBool, Description: "Background worktree size scan (TUI SIZE column)"},
	{Key: "DAILY_CHECK_FOR_UPDATES", Kind: KindBool, Description: "Check for new releases once a day (TUI)"},
	{Key: "AUTO_UPDATE_NEW_RELEASE", Kind: KindBool, Description: "Install new releases automatically in the background (TUI)"},
	{Key: "GITHUB_INTEGRATION", Kind: KindBool, Description: "gh-powered features: clone browser, PRs, hibernate"},
	{Key: "ENABLE_HIBERNATE", Kind: KindBool, Description: "Show the destructive Hibernate Repo action in the TUI"},
	{Key: "COPY_ENV_FILES", Kind: KindBool, Description: "Copy .env* files into new worktrees"},
	{Key: "COPY_MCP_JSON", Kind: KindBool, Description: "Copy .mcp.json files into new worktrees"},
	{Key: "OPEN_ENV_IDE", Kind: KindBool, Description: "Workspace file auto-opens .env files in the IDE"},
	{Key: "NEW_WT_PACKAGE_INSTALL", Kind: KindBool, Description: "Install dependencies when creating worktrees (default)"},
}

var repoKeys = []KeyDef{
	{Key: "BASIS_BRANCH", Kind: KindText, Description: "Branch new worktrees are created from and rebased onto"},
	{Key: "PACKAGE_MANAGER", Kind: KindEnum, AllowEmpty: true, Options: []string{"", "default", "pnpm", "npm", "yarn", "bun"}, Description: "Per-repo package manager override (default/empty = global)"},
	{Key: "SETUP_SCRIPT", Kind: KindText, AllowEmpty: true, Description: "Command run inside every new worktree after install"},
	{Key: "HIDDEN", Kind: KindBool, Description: "Hide this repo from the main view and all-repo operations"},
	{Key: "LTS_ROOT", Kind: KindText, AllowEmpty: true, Description: "Directory holding this repo's <repo>-lts folder (empty = next to the repos)"},
}

// GlobalKeys returns the documented global keys (sorted by key).
func GlobalKeys() []KeyDef { return sortedKeys(globalKeys) }

// RepoKeys returns the documented per-repo keys (sorted by key).
func RepoKeys() []KeyDef { return sortedKeys(repoKeys) }

func sortedKeys(in []KeyDef) []KeyDef {
	out := append([]KeyDef(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func findKey(defs []KeyDef, key string) (KeyDef, bool) {
	key = strings.ToUpper(strings.TrimSpace(key))
	for _, d := range defs {
		if d.Key == key {
			return d, true
		}
	}
	return KeyDef{}, false
}

// LookupGlobalKey returns the definition for a global key.
func LookupGlobalKey(key string) (KeyDef, bool) { return findKey(globalKeys, key) }

// LookupRepoKey returns the definition for a per-repo key.
func LookupRepoKey(key string) (KeyDef, bool) { return findKey(repoKeys, key) }

// GlobalKeyNames / RepoKeyNames list key names (for suggestions).
func GlobalKeyNames() []string { return keyNames(globalKeys) }
func RepoKeyNames() []string   { return keyNames(repoKeys) }

func keyNames(defs []KeyDef) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Key
	}
	sort.Strings(names)
	return names
}

// Validate normalizes and checks a value against the definition.
func (d KeyDef) Validate(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if d.AllowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("%s cannot be empty", d.Key)
	}
	switch d.Kind {
	case KindBool:
		switch strings.ToLower(value) {
		case "true", "on", "yes", "1":
			return "true", nil
		case "false", "off", "no", "0":
			return "false", nil
		}
		return "", fmt.Errorf("%s must be true or false", d.Key)
	case KindInt:
		n, err := strconv.Atoi(value)
		if err != nil || n < d.Min || n > d.Max {
			return "", fmt.Errorf("%s must be a number between %d and %d", d.Key, d.Min, d.Max)
		}
		return strconv.Itoa(n), nil
	case KindEnum:
		for _, o := range d.Options {
			if strings.EqualFold(o, value) {
				return o, nil
			}
		}
		var shown []string
		for _, o := range d.Options {
			if o != "" {
				shown = append(shown, o)
			}
		}
		return "", fmt.Errorf("%s must be one of: %s", d.Key, strings.Join(shown, ", "))
	}
	return value, nil
}

func boolStr(b bool) string { return strconv.FormatBool(b) }

// GetGlobal returns a global key's current value as its config-file string.
func (g *GlobalConfig) Get(key string) (string, bool) {
	switch strings.ToUpper(key) {
	case "IDE_COMMAND":
		return g.IDECommand, true
	case "AI_CLI_COMMAND":
		return g.AICliCommand, true
	case "TERMINAL":
		return g.Terminal, true
	case "TERMINAL_MULTIPLEXER":
		return g.Multiplexer, true
	case "TMUX_AI_PANE_WIDTH":
		return strconv.Itoa(g.TmuxAIPaneWidth), true
	case "TMUX_RIGHT_PANES":
		return strconv.Itoa(g.TmuxRightPanes), true
	case "AUTO_KILL_TMUX":
		return g.AutoKillTmux, true
	case "PACKAGE_MANAGER":
		return g.PackageManager, true
	case "AUTO_REFRESH":
		return g.AutoRefresh, true
	case "AUTO_CLEAN_MODULES":
		return g.AutoCleanModules, true
	case "SORT_ORDER":
		return g.SortOrder, true
	case "SHOW_HIDDEN_REPOS":
		return boolStr(g.ShowHiddenRepos), true
	case "LAYOUT":
		return g.Layout, true
	case "CLICK_USAGE":
		return g.ClickUsage, true
	case "THEME":
		return g.Theme, true
	case "LAUNCH_GREETING":
		return boolStr(g.LaunchGreeting), true
	case "DONE_SOUND":
		return g.DoneSound, true
	case "SIZE_SCANNING":
		return boolStr(g.SizeScanning), true
	case "DAILY_CHECK_FOR_UPDATES":
		return boolStr(g.CheckForUpdates), true
	case "AUTO_UPDATE_NEW_RELEASE":
		return boolStr(g.AutoUpdate), true
	case "GITHUB_INTEGRATION":
		return boolStr(g.GithubIntegration), true
	case "ENABLE_HIBERNATE":
		return boolStr(g.EnableHibernate), true
	case "COPY_ENV_FILES":
		return boolStr(g.CopyEnvFiles), true
	case "COPY_MCP_JSON":
		return boolStr(g.CopyMCPJson), true
	case "OPEN_ENV_IDE":
		return boolStr(g.OpenEnvInIDE), true
	case "NEW_WT_PACKAGE_INSTALL":
		return boolStr(g.InstallOnCreate), true
	}
	return "", false
}

// Set validates value against the key's definition and assigns it.
// It does not save; callers persist with Config.SaveGlobal.
func (g *GlobalConfig) Set(key, value string) error {
	def, ok := LookupGlobalKey(key)
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	v, err := def.Validate(value)
	if err != nil {
		return err
	}
	b := v == "true"
	switch def.Key {
	case "IDE_COMMAND":
		g.IDECommand = v
	case "AI_CLI_COMMAND":
		g.AICliCommand = v
	case "TERMINAL":
		g.Terminal = v
	case "TERMINAL_MULTIPLEXER":
		g.Multiplexer = v
	case "TMUX_AI_PANE_WIDTH":
		g.TmuxAIPaneWidth, _ = strconv.Atoi(v)
	case "TMUX_RIGHT_PANES":
		g.TmuxRightPanes, _ = strconv.Atoi(v)
	case "AUTO_KILL_TMUX":
		g.AutoKillTmux = v
	case "PACKAGE_MANAGER":
		g.PackageManager = v
	case "AUTO_REFRESH":
		g.AutoRefresh = v
	case "AUTO_CLEAN_MODULES":
		g.AutoCleanModules = v
	case "SORT_ORDER":
		g.SortOrder = v
	case "SHOW_HIDDEN_REPOS":
		g.ShowHiddenRepos = b
	case "LAYOUT":
		g.Layout = v
	case "CLICK_USAGE":
		g.ClickUsage = v
	case "THEME":
		g.Theme = v
	case "LAUNCH_GREETING":
		g.LaunchGreeting = b
	case "DONE_SOUND":
		g.DoneSound = v
	case "SIZE_SCANNING":
		g.SizeScanning = b
	case "DAILY_CHECK_FOR_UPDATES":
		g.CheckForUpdates = b
	case "AUTO_UPDATE_NEW_RELEASE":
		g.AutoUpdate = b
	case "GITHUB_INTEGRATION":
		g.GithubIntegration = b
	case "ENABLE_HIBERNATE":
		g.EnableHibernate = b
	case "COPY_ENV_FILES":
		g.CopyEnvFiles = b
	case "COPY_MCP_JSON":
		g.CopyMCPJson = b
	case "OPEN_ENV_IDE":
		g.OpenEnvInIDE = b
	case "NEW_WT_PACKAGE_INSTALL":
		g.InstallOnCreate = b
	}
	return nil
}

// Get returns a per-repo key's value as stored ("" = unset / default).
func (rc RepoLocalConfig) Get(key string) (string, bool) {
	switch strings.ToUpper(key) {
	case "BASIS_BRANCH":
		return rc.BasisBranch, true
	case "PACKAGE_MANAGER":
		return rc.PackageManager, true
	case "SETUP_SCRIPT":
		return rc.SetupScript, true
	case "HIDDEN":
		return boolStr(rc.Hidden), true
	case "LTS_ROOT":
		return rc.LTSRoot, true
	}
	return "", false
}

// SetRepoKey validates and saves a per-repo key.
func (c *Config) SetRepoKey(repoName, key, value string) error {
	def, ok := LookupRepoKey(key)
	if !ok {
		return fmt.Errorf("unknown repo setting %q", key)
	}
	v, err := def.Validate(value)
	if err != nil {
		return err
	}
	k := strings.ToUpper(repoName)
	rc, ok := c.Local[k]
	if !ok {
		rc = DefaultRepoLocal()
	}
	switch def.Key {
	case "BASIS_BRANCH":
		rc.BasisBranch = v
	case "PACKAGE_MANAGER":
		if v == "default" {
			v = ""
		}
		rc.PackageManager = v
	case "SETUP_SCRIPT":
		rc.SetupScript = v
	case "HIDDEN":
		rc.Hidden = v == "true"
	case "LTS_ROOT":
		if v != "" && !filepath.IsAbs(v) {
			return fmt.Errorf("LTS_ROOT must be an absolute path")
		}
		if v != "" {
			v = filepath.Clean(v)
		}
		if v == filepath.Clean(c.WorkDir) {
			v = "" // the default location — store as unset
		}
		rc.LTSRoot = v
	}
	c.Local[k] = rc
	return c.SaveLocal()
}
