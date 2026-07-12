package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lts-revamp/internal/git"
	"lts-revamp/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func hibernateTestModel() Model {
	m := testModel()
	m.config.Global.EnableHibernate = true
	m.ghState = ui.CloneReady
	m.githubRemotes = map[string]bool{"core": true, "goforms": true}
	return m
}

func key(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestCanHibernateGates(t *testing.T) {
	m := hibernateTestModel()
	if !m.canHibernate(m.repos[0]) {
		t.Error("github-remote repo with gh ready must be hibernatable")
	}
	m.config.Global.EnableHibernate = false
	if m.canHibernate(m.repos[0]) {
		t.Error("hibernate is opt-in — the default-off setting must gate it")
	}
	m.config.Global.EnableHibernate = true
	if m.canHibernate(m.repos[2]) {
		t.Error("monorepo cards must never be hibernatable")
	}
	m.githubRemotes["core"] = false
	if m.canHibernate(m.repos[0]) {
		t.Error("non-github remote must not be hibernatable")
	}
	m.githubRemotes["core"] = true
	m.ghState = ui.CloneNoAuth
	if m.canHibernate(m.repos[0]) {
		t.Error("unauthenticated gh must not be hibernatable")
	}
}

func TestHibernateLockHeldThroughDialogAndCancel(t *testing.T) {
	m := hibernateTestModel()
	m.menuRepo = m.repos[0]
	m.menuHasWT = false

	m2, cmd := executeContextAction(m, ui.BtnHibernate)
	if !m2.hibernateActive || cmd == nil {
		t.Fatal("hibernate should open the dialog and dispatch the audit")
	}
	if _, locked := m2.busy["core"]; !locked {
		t.Fatal("repo must be locked from audit start")
	}

	// Cancel while the audit is still running: dialog closes, but the lock
	// survives until the in-flight audit's result arrives.
	m3, _ := handleHibernateKey(m2, key("esc"))
	if m3.hibernateActive {
		t.Fatal("esc must close the dialog")
	}
	if _, locked := m3.busy["core"]; !locked {
		t.Fatal("lock must survive cancel while the audit is in flight")
	}
	updated, _ := m3.Update(HibernateAuditMsg{RepoName: "core", Audit: git.HibernateAudit{RepoName: "core"}, Locked: []string{"core"}})
	m4 := updated.(Model)
	if _, locked := m4.busy["core"]; locked {
		t.Fatal("stale audit result must release the lock")
	}
	if m4.hibernateActive || m4.hibernateAudit != nil {
		t.Fatal("stale audit result must not resurrect the dialog")
	}
}

func TestHibernateCancelAfterAuditReleasesImmediately(t *testing.T) {
	m := hibernateTestModel()
	m.menuRepo = m.repos[0]
	m2, _ := executeContextAction(m, ui.BtnHibernate)
	updated, _ := m2.Update(HibernateAuditMsg{RepoName: "core", Audit: git.HibernateAudit{RepoName: "core"}, Locked: []string{"core"}})
	m3 := updated.(Model)
	if m3.hibernateAudit == nil || m3.hibernateAuditPending {
		t.Fatal("audit result should land in the open dialog")
	}
	m4, _ := handleHibernateKey(m3, key("esc"))
	if _, locked := m4.busy["core"]; locked {
		t.Fatal("cancel after the audit landed must release the lock immediately")
	}
}

func TestHibernateConfirmRequiresTypedDeleteAndNoBlockers(t *testing.T) {
	blocked := git.HibernateAudit{RepoName: "core",
		Checks: []git.HibernateCheck{{Label: "x", State: git.CheckFail}}}
	clean := git.HibernateAudit{RepoName: "core"}

	// Blockers: even a typed DELETE must not confirm.
	m := hibernateTestModel()
	m.menuRepo = m.repos[0]
	m2, _ := executeContextAction(m, ui.BtnHibernate)
	updated, _ := m2.Update(HibernateAuditMsg{RepoName: "core", Audit: blocked, Locked: []string{"core"}})
	m3 := updated.(Model)
	m3.hibernateInput.SetValue("DELETE")
	m4, _ := handleHibernateKey(m3, key("enter"))
	if !m4.hibernateActive {
		t.Fatal("enter with blockers must not confirm")
	}

	// Clean audit: wrong text stays open, DELETE confirms and relabels the op.
	updated, _ = m4.Update(HibernateAuditMsg{RepoName: "core", Audit: clean, Locked: []string{"core"}})
	m5 := updated.(Model)
	m5.hibernateInput.SetValue("delet")
	m6, _ := handleHibernateKey(m5, key("enter"))
	if !m6.hibernateActive {
		t.Fatal("wrong confirmation text must not confirm")
	}
	m6.hibernateInput.SetValue("delete") // case-insensitive like the delete dialog
	m7, cmd := handleHibernateKey(m6, key("enter"))
	if m7.hibernateActive || cmd == nil {
		t.Fatal("typed DELETE must confirm and dispatch the hibernate")
	}
	if op := m7.busy["core"]; !strings.Contains(op, "Hibernating") {
		t.Fatalf("lock must carry the hibernating label, got %q", op)
	}
}

func TestHibernateReauditKeyWithBlockers(t *testing.T) {
	blocked := git.HibernateAudit{RepoName: "core",
		Checks: []git.HibernateCheck{{Label: "x", State: git.CheckFail}}}
	m := hibernateTestModel()
	m.menuRepo = m.repos[0]
	m2, _ := executeContextAction(m, ui.BtnHibernate)
	updated, _ := m2.Update(HibernateAuditMsg{RepoName: "core", Audit: blocked, Locked: []string{"core"}})
	m3 := updated.(Model)

	m4, cmd := handleHibernateKey(m3, key("r"))
	if m4.hibernateAudit != nil || !m4.hibernateAuditPending || cmd == nil {
		t.Fatal("r must re-dispatch the audit")
	}
	if _, locked := m4.busy["core"]; !locked {
		t.Fatal("re-audit must keep the repo locked")
	}
	// While the re-audit runs, r must not double-dispatch.
	m5, cmd := handleHibernateKey(m4, key("r"))
	if cmd != nil || !m5.hibernateAuditPending {
		t.Fatal("r during a pending audit must be a no-op")
	}
}

func TestHibernateBusyRepoRejected(t *testing.T) {
	m := hibernateTestModel()
	m.beginOp("Creating feat/x...", "core")
	m.menuRepo = m.repos[0]
	m2, cmd := executeContextAction(m, ui.BtnHibernate)
	if m2.hibernateActive || cmd != nil {
		t.Fatal("hibernate must not start on a busy repo")
	}
}

func TestHibernateDoneReleasesAndReports(t *testing.T) {
	m := hibernateTestModel()
	m.beginOp("Hibernating core...", "core")
	updated, _ := m.Update(HibernateDoneMsg{RepoName: "core", Freed: 1 << 30, EnvBacked: 2, Locked: []string{"core"}})
	m2 := updated.(Model)
	if m2.anyBusy() {
		t.Fatal("done must release the lock")
	}
	for _, want := range []string{"Hibernated core", "2 .env", "(c)"} {
		if !strings.Contains(m2.statusMsg, want) {
			t.Errorf("status missing %q: %q", want, m2.statusMsg)
		}
	}
}

func TestEnvRestorePromptFlowAfterClone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()

	// A hibernate left a backup: root .env + nested + one worktree env.
	backup := filepath.Join(home, ".config", "lts", "env-backup", "core", "20260701-120000")
	for rel, content := range map[string]string{
		".env":                               "ROOT=1\n",
		filepath.Join("apps", "web", ".env"): "WEB=1\n",
		filepath.Join("worktrees", "core-feat-x", ".env"): "WT=1\n",
	} {
		p := filepath.Join(backup, rel)
		os.MkdirAll(filepath.Dir(p), 0755)
		os.WriteFile(p, []byte(content), 0600)
	}
	// The fresh clone.
	os.MkdirAll(filepath.Join(work, "core"), 0755)

	m := hibernateTestModel()
	m.config.WorkDir = work
	updated, _ := m.Update(CloneDoneMsg{RepoName: "core", Locked: []string{"core"}})
	m2 := updated.(Model)
	if !m2.envRestoreActive {
		t.Fatal("clone of a hibernated repo must offer the env restore")
	}
	if m2.envRestoreMain != 2 || m2.envRestoreWT != 1 {
		t.Fatalf("counts = (%d, %d), want (2, 1)", m2.envRestoreMain, m2.envRestoreWT)
	}

	m3, _ := handleEnvRestoreKey(m2, key("y"))
	if m3.envRestoreActive {
		t.Fatal("y must dismiss the prompt")
	}
	if data, _ := os.ReadFile(filepath.Join(work, "core", "apps", "web", ".env")); string(data) != "WEB=1\n" {
		t.Fatalf("nested env not restored in place: %q", data)
	}
	if _, err := os.Stat(filepath.Join(work, "core", "worktrees")); !os.IsNotExist(err) {
		t.Fatal("worktree envs must not land in the repo dir")
	}
	if !strings.Contains(m3.statusMsg, "Restored 2") || !strings.Contains(m3.statusMsg, "1 worktree") {
		t.Errorf("status should report both counts: %q", m3.statusMsg)
	}
}

func TestEnvRestoreDeclineKeepsBackup(t *testing.T) {
	m := hibernateTestModel()
	m.envRestoreActive = true
	m.envRestoreRepo = "core"
	m.envRestoreDir = "/backup/core/20260701-120000"
	m2, _ := handleEnvRestoreKey(m, key("n"))
	if m2.envRestoreActive {
		t.Fatal("n must dismiss the prompt")
	}
	if !strings.Contains(m2.statusMsg, "kept") {
		t.Errorf("declining should say the backup is kept: %q", m2.statusMsg)
	}
}

// A second clone finishing while a restore prompt is open must not clobber
// it — the first repo can't be re-cloned (its dir exists now), so an
// overwritten offer would be unrecoverable in the UI.
func TestSecondCloneDoesNotClobberPendingRestorePrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, repo := range []string{"core", "erp-ui"} {
		p := filepath.Join(home, ".config", "lts", "env-backup", repo, "20260701-120000", ".env")
		os.MkdirAll(filepath.Dir(p), 0755)
		os.WriteFile(p, []byte("X=1\n"), 0600)
	}

	m := hibernateTestModel()
	updated, _ := m.Update(CloneDoneMsg{RepoName: "core", Locked: []string{"core"}})
	m2 := updated.(Model)
	if !m2.envRestoreActive || m2.envRestoreRepo != "core" {
		t.Fatal("first clone should raise the prompt for core")
	}
	updated, _ = m2.Update(CloneDoneMsg{RepoName: "erp-ui", Locked: []string{"erp-ui"}})
	m3 := updated.(Model)
	if m3.envRestoreRepo != "core" {
		t.Fatalf("second clone clobbered the pending prompt: now offering %q", m3.envRestoreRepo)
	}
	if !strings.Contains(m3.statusMsg, "erp-ui") || !strings.Contains(m3.statusMsg, "env-backup") {
		t.Errorf("second repo's backup should be pointed at in the status: %q", m3.statusMsg)
	}
}

// The restore prompt is the only dialog a background completion can raise,
// so it can fire while settings is open. Settings owns the keys, so the
// prompt must render below settings and wait its turn.
func TestRestorePromptDefersToOpenSettings(t *testing.T) {
	m := hibernateTestModel()
	m.settings = ui.NewSettings(&m.config, []string{"core"})
	m.settings.ViewHeight = m.height
	m.settings.ViewWidth = m.width
	m.envRestoreActive = true
	m.envRestoreRepo = "core"
	m.envRestoreDir = "/backup/core/20260701-120000"
	m.envRestoreMain = 1
	m.recomputeLayout()

	view := m.View()
	if strings.Contains(view, "Restore .env backup?") {
		t.Fatal("prompt must not render over settings — settings owns the keys")
	}
	// Keys route to settings: esc closes settings, the prompt survives and
	// then takes over.
	updated, _ := m.Update(key("esc"))
	m2 := updated.(Model)
	if m2.settings.Active {
		t.Fatal("esc should close settings")
	}
	if !m2.envRestoreActive {
		t.Fatal("the pending prompt must survive the settings session")
	}
	if !strings.Contains(m2.View(), "Restore .env backup?") {
		t.Fatal("prompt should render once settings closes")
	}
}

func TestCloneOfNeverHibernatedRepoSkipsPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := hibernateTestModel()
	updated, _ := m.Update(CloneDoneMsg{RepoName: "fresh-repo", Locked: []string{"fresh-repo"}})
	m2 := updated.(Model)
	if m2.envRestoreActive {
		t.Fatal("no backup → no prompt")
	}
}
