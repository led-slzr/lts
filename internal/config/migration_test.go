package config

import (
	"os"
	"testing"
)

// A config file exactly as 2.7.0 wrote it — every key SaveGlobal emitted
// then, none of the 3.0.0 additions.
const v270Config = `IDE_COMMAND="cursor"
AI_CLI_COMMAND="claude --dangerously-skip-permissions"
PACKAGE_MANAGER="npm"
AUTO_REFRESH="1H"
TERMINAL="ghostty"
TERMINAL_MULTIPLEXER="tmux"
TMUX_AI_PANE_WIDTH="65"
TMUX_RIGHT_PANES="2"
LAYOUT="explorer"
SORT_ORDER="created"
DONE_SOUND="glass"
AUTO_CLEAN_MODULES="7D"
AUTO_KILL_TMUX="1D"
DAILY_CHECK_FOR_UPDATES="true"
AUTO_UPDATE_NEW_RELEASE="true"
OPEN_ENV_IDE="false"
NEW_WT_PACKAGE_INSTALL="true"
COPY_ENV_FILES="true"
COPY_MCP_JSON="true"
LAST_UPDATE_CHECK="1750000000"
`

func writeV270Config(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(GlobalConfigDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GlobalConfigPath(), []byte(v270Config), 0644); err != nil {
		t.Fatal(err)
	}
}

// Upgrading 2.7.0 → 3.0.0 must be frictionless: every saved preference
// survives verbatim, and every new setting lands on a default that
// preserves 2.7.0 behavior (except the deliberate opt-ins).
func TestUpgradeFrom270IsFrictionless(t *testing.T) {
	writeV270Config(t)
	cfg := Load(t.TempDir())
	g := cfg.Global

	// Every 2.7.0 preference survives verbatim.
	if g.IDECommand != "cursor" || g.AICliCommand != "claude --dangerously-skip-permissions" ||
		g.PackageManager != "npm" || g.AutoRefresh != "1H" || g.Terminal != "ghostty" ||
		g.Multiplexer != "tmux" || g.TmuxAIPaneWidth != 65 || g.TmuxRightPanes != 2 ||
		g.Layout != "explorer" || g.SortOrder != "created" || g.DoneSound != "glass" ||
		g.AutoCleanModules != "7D" || g.AutoKillTmux != "1D" ||
		!g.CheckForUpdates || g.OpenEnvInIDE || !g.InstallOnCreate ||
		!g.CopyEnvFiles || !g.CopyMCPJson || g.LastUpdateCheck != 1750000000 {
		t.Fatalf("2.7.0 preferences must survive verbatim, got %+v", g)
	}

	// Users who had auto-update on keep it on — the default flip only
	// affects configs without the key.
	if !g.AutoUpdate {
		t.Fatal("saved AUTO_UPDATE_NEW_RELEASE=true must be honored")
	}

	// Every 3.0.0 addition lands on a behavior-preserving default.
	if g.Theme != "classic-lts" {
		t.Errorf("theme should default to the 2.7.0 look, got %q", g.Theme)
	}
	if g.ClickUsage != "ide" {
		t.Errorf("click usage should default to IDE (the 2.7.0 launch state), got %q", g.ClickUsage)
	}
	if !g.GithubIntegration || !g.LaunchGreeting || !g.SizeScanning {
		t.Error("GitHub integration, greeting, and size scanning were 2.7.0 behavior — must default on")
	}
	if g.EnableHibernate {
		t.Error("hibernate is new and destructive — must default off")
	}

	// Round-trip: saving with 3.0.0 and re-loading changes nothing.
	if err := cfg.SaveGlobal(); err != nil {
		t.Fatal(err)
	}
	if reloaded := Load(cfg.WorkDir); reloaded.Global != g {
		t.Fatalf("3.0.0 save/load round-trip drifted:\n before %+v\n after  %+v", g, reloaded.Global)
	}
}

// A hand-rolled config missing the auto-update key gets the new safe
// default (off) — the one deliberate behavior change.
func TestAutoUpdateDefaultsOffWhenKeyAbsent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(GlobalConfigDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GlobalConfigPath(), []byte("IDE_COMMAND=\"code\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := Load(t.TempDir())
	if cfg.Global.AutoUpdate {
		t.Fatal("absent AUTO_UPDATE_NEW_RELEASE must default to off")
	}
	if !cfg.Global.CheckForUpdates {
		t.Fatal("update checks (badge only) stay on by default")
	}
}

func TestSetupScriptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := Config{Global: DefaultGlobal(), Local: map[string]RepoLocalConfig{}, WorkDir: dir}
	script := `pnpm --filter @gorocky/shared-types build && echo done`
	if err := c.SetRepoSetupScript("core", script); err != nil {
		t.Fatal(err)
	}
	reloaded := map[string]RepoLocalConfig{}
	loadLocal(dir, reloaded)
	if reloaded["CORE"].SetupScript != script {
		t.Fatalf("setup script did not survive reload: %q", reloaded["CORE"].SetupScript)
	}
	// Clearing removes the key entirely.
	if err := c.SetRepoSetupScript("core", ""); err != nil {
		t.Fatal(err)
	}
	reloaded = map[string]RepoLocalConfig{}
	loadLocal(dir, reloaded)
	if reloaded["CORE"].SetupScript != "" {
		t.Fatal("cleared script should not persist")
	}
}

// Setup scripts with embedded quotes and shell operators survive the
// KEY="value" config format (only leading/trailing quotes are trimmed).
func TestSetupScriptEmbeddedQuotesSurvive(t *testing.T) {
	dir := t.TempDir()
	c := Config{Global: DefaultGlobal(), Local: map[string]RepoLocalConfig{}, WorkDir: dir}
	script := `pnpm build && echo "shared types ready" | tee -a log.txt`
	if err := c.SetRepoSetupScript("core", script); err != nil {
		t.Fatal(err)
	}
	reloaded := map[string]RepoLocalConfig{}
	loadLocal(dir, reloaded)
	if reloaded["CORE"].SetupScript != script {
		t.Fatalf("script drifted through save/load:\n saved  %q\n loaded %q", script, reloaded["CORE"].SetupScript)
	}
}
