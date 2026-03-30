package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GlobalConfig holds settings that apply regardless of working directory.
// Stored at ~/.config/lts/config
type GlobalConfig struct {
	IDECommand     string // windsurf, code, cursor, zed
	AICliCommand   string // claude, opencode, "claude --dangerously-skip-permissions"
	PackageManager string // pnpm, npm, yarn, bun
	AutoRefresh    string // 30M, 1H, 24H, etc.
	Terminal       string // ghostty, iterm, terminal, wezterm, alacritty
}

// RepoLocalConfig holds per-repo settings.
type RepoLocalConfig struct {
	BasisBranch string // main, dev, master, etc.
	LastRefresh int64  // unix timestamp
}

// Config is the merged configuration used by the app.
type Config struct {
	Global  GlobalConfig
	Local   map[string]RepoLocalConfig // key = uppercased repo name
	WorkDir string
}

func DefaultGlobal() GlobalConfig {
	return GlobalConfig{
		IDECommand:     "windsurf",
		AICliCommand:   "claude",
		PackageManager: "pnpm",
		AutoRefresh:    "24H",
		Terminal:       "ghostty",
	}
}

func DefaultRepoLocal() RepoLocalConfig {
	return RepoLocalConfig{
		BasisBranch: "main",
		LastRefresh: 0,
	}
}

// GlobalConfigDir returns ~/.config/lts
func GlobalConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "lts")
}

// GlobalConfigPath returns ~/.config/lts/config
func GlobalConfigPath() string {
	return filepath.Join(GlobalConfigDir(), "config")
}

// LocalConfigPath returns {workDir}/.lts.conf
func LocalConfigPath(workDir string) string {
	return filepath.Join(workDir, ".lts.conf")
}

// Load reads global and local configs, merging them into a Config.
// Also migrates old lts.conf if found.
func Load(workDir string) Config {
	cfg := Config{
		Global:  DefaultGlobal(),
		Local:   make(map[string]RepoLocalConfig),
		WorkDir: workDir,
	}

	// Migrate old lts.conf if new .lts.conf doesn't exist yet
	oldConf := filepath.Join(workDir, "lts.conf")
	newConf := LocalConfigPath(workDir)
	if _, err := os.Stat(oldConf); err == nil {
		if _, err := os.Stat(newConf); os.IsNotExist(err) {
			migrateOldConfig(oldConf, &cfg)
		}
	}

	// Load global config (create if missing)
	globalPath := GlobalConfigPath()
	if _, err := os.Stat(globalPath); os.IsNotExist(err) {
		os.MkdirAll(GlobalConfigDir(), 0755)
		cfg.SaveGlobal()
	}
	loadGlobal(&cfg.Global)

	// Load local config
	loadLocal(workDir, cfg.Local)

	return cfg
}

// InitLocalForRepos ensures all discovered repos have local config entries and saves.
func (c *Config) InitLocalForRepos(repoNames []string) {
	changed := false
	for _, name := range repoNames {
		key := strings.ToUpper(name)
		if _, ok := c.Local[key]; !ok {
			c.Local[key] = DefaultRepoLocal()
			changed = true
		}
	}
	if changed {
		c.SaveLocal()
	}
}

// GetRepoBasisBranch returns the basis branch for a specific repo.
func (c *Config) GetRepoBasisBranch(repoName string) string {
	key := strings.ToUpper(repoName)
	if rc, ok := c.Local[key]; ok && rc.BasisBranch != "" {
		return rc.BasisBranch
	}
	return "main"
}

// GetRepoLastRefresh returns the last refresh timestamp for a specific repo.
func (c *Config) GetRepoLastRefresh(repoName string) int64 {
	key := strings.ToUpper(repoName)
	if rc, ok := c.Local[key]; ok {
		return rc.LastRefresh
	}
	return 0
}

// SetRepoLastRefresh updates the last refresh timestamp for a repo and saves.
func (c *Config) SetRepoLastRefresh(repoName string, ts int64) {
	key := strings.ToUpper(repoName)
	rc := c.Local[key]
	rc.LastRefresh = ts
	if rc.BasisBranch == "" {
		rc.BasisBranch = "main"
	}
	c.Local[key] = rc
	c.SaveLocal()
}

// SetRepoBasisBranch updates the basis branch for a repo and saves.
func (c *Config) SetRepoBasisBranch(repoName, branch string) {
	key := strings.ToUpper(repoName)
	rc := c.Local[key]
	rc.BasisBranch = branch
	c.Local[key] = rc
	c.SaveLocal()
}

// AICliLabel returns a display label derived from the AI CLI command.
// e.g. "claude" → "Claude", "opencode" → "Opencode", "claude --dangerously-skip-permissions" → "Claude"
func (c *Config) AICliLabel() string {
	cmd := c.Global.AICliCommand
	if cmd == "" {
		return "AI CLI"
	}
	// Take first word (before any flags)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "AI CLI"
	}
	name := parts[0]
	// Title case
	if len(name) > 0 {
		return strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

// SaveGlobal writes the global config to ~/.config/lts/config
func (c *Config) SaveGlobal() error {
	dir := GlobalConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	lines := []string{
		fmt.Sprintf("IDE_COMMAND=\"%s\"", c.Global.IDECommand),
		fmt.Sprintf("AI_CLI_COMMAND=\"%s\"", c.Global.AICliCommand),
		fmt.Sprintf("PACKAGE_MANAGER=\"%s\"", c.Global.PackageManager),
		fmt.Sprintf("AUTO_REFRESH=\"%s\"", c.Global.AutoRefresh),
		fmt.Sprintf("TERMINAL=\"%s\"", c.Global.Terminal),
	}
	return os.WriteFile(GlobalConfigPath(), []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// SaveLocal writes the local config to {workDir}/.lts.conf
func (c *Config) SaveLocal() error {
	var lines []string
	for key, rc := range c.Local {
		lines = append(lines, fmt.Sprintf("%s_BASIS_BRANCH=\"%s\"", key, rc.BasisBranch))
		lines = append(lines, fmt.Sprintf("%s_LAST_REFRESH=\"%d\"", key, rc.LastRefresh))
	}
	return os.WriteFile(LocalConfigPath(c.WorkDir), []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// EnsureRepoExists makes sure a repo has a local config entry.
func (c *Config) EnsureRepoExists(repoName string) {
	key := strings.ToUpper(repoName)
	if _, ok := c.Local[key]; !ok {
		c.Local[key] = DefaultRepoLocal()
	}
}

// --- Internal loading ---

func loadGlobal(g *GlobalConfig) {
	f, err := os.Open(GlobalConfigPath())
	if err != nil {
		return
	}
	defer f.Close()

	kv := parseKeyValue(f)
	if v, ok := kv["IDE_COMMAND"]; ok {
		g.IDECommand = v
	}
	if v, ok := kv["AI_CLI_COMMAND"]; ok {
		g.AICliCommand = v
	}
	if v, ok := kv["PACKAGE_MANAGER"]; ok {
		g.PackageManager = v
	}
	if v, ok := kv["AUTO_REFRESH"]; ok {
		g.AutoRefresh = v
	}
	if v, ok := kv["TERMINAL"]; ok {
		g.Terminal = v
	}
}

func loadLocal(workDir string, local map[string]RepoLocalConfig) {
	f, err := os.Open(LocalConfigPath(workDir))
	if err != nil {
		return
	}
	defer f.Close()

	kv := parseKeyValue(f)
	for k, v := range kv {
		if strings.HasSuffix(k, "_BASIS_BRANCH") {
			repo := strings.TrimSuffix(k, "_BASIS_BRANCH")
			rc := local[repo]
			rc.BasisBranch = v
			local[repo] = rc
		} else if strings.HasSuffix(k, "_LAST_REFRESH") {
			repo := strings.TrimSuffix(k, "_LAST_REFRESH")
			rc := local[repo]
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				rc.LastRefresh = n
			}
			local[repo] = rc
		}
	}
}

func parseKeyValue(f *os.File) map[string]string {
	kv := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), "\"")
		kv[key] = val
	}
	return kv
}

// migrateOldConfig reads the old lts.conf and migrates to new format.
func migrateOldConfig(oldPath string, cfg *Config) {
	f, err := os.Open(oldPath)
	if err != nil {
		return
	}
	defer f.Close()

	kv := parseKeyValue(f)

	// Migrate global settings
	if v, ok := kv["IDE_COMMAND"]; ok {
		cfg.Global.IDECommand = v
	}
	if v, ok := kv["PACKAGE_MANAGER"]; ok {
		cfg.Global.PackageManager = v
	}

	// Migrate BASIS_BRANCH and LAST_REFRESH as defaults for all repos
	basisBranch := "main"
	if v, ok := kv["BASIS_BRANCH"]; ok {
		basisBranch = v
	}
	var lastRefresh int64
	if v, ok := kv["LAST_REFRESH"]; ok {
		lastRefresh, _ = strconv.ParseInt(v, 10, 64)
	}

	// Scan for repos in workDir and create local entries
	entries, err := os.ReadDir(cfg.WorkDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() || strings.HasSuffix(e.Name(), "-lts") {
				continue
			}
			gitDir := filepath.Join(cfg.WorkDir, e.Name(), ".git")
			if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
				key := strings.ToUpper(e.Name())
				cfg.Local[key] = RepoLocalConfig{
					BasisBranch: basisBranch,
					LastRefresh: lastRefresh,
				}
			}
		}
	}

	// Save new configs
	cfg.SaveGlobal()
	cfg.SaveLocal()

	// Rename old file
	os.Rename(oldPath, oldPath+".migrated")
}
