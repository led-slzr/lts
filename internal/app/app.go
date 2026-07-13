package app

import (
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strconv"

	"lts-revamp/internal/config"
	"lts-revamp/internal/gh"
	"lts-revamp/internal/git"
	"lts-revamp/internal/opener"
	"lts-revamp/internal/tools"
	"lts-revamp/internal/ui"
	"lts-revamp/internal/update"
	"lts-revamp/internal/version"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	config      config.Config
	repos       []git.Repo
	width       int
	height      int
	clickUsage  opener.ClickUsage
	focusedCard int
	focusedWT   int // -1=card border, -2=header, 0+=worktree index
	hoveredBtn  ui.HoverButton
	statusMsg   string
	statusGen   int // incremented on each statusMsg change; used to avoid stale clears

	// Pre-computed layout (computed in Update, used in View)
	gridResult    ui.GridResult
	headerView    string
	headerH       int
	footerY       int
	createBtnY    int
	scrollY       int // vertical scroll offset for main content area (lines)
	contentHeight int // total height of scrollable content (grid + legend + create btn)

	// Modal
	modal    ui.ModalModel
	settings ui.SettingsModel

	// Rename input — the target is snapshotted at dialog open so background
	// repo reloads can't shift what the dialog acts on
	renameActive       bool
	renameInput        textinput.Model
	renameRepo         git.Repo
	renameWT           git.Worktree
	renameIsBasis      bool // true = changing the repo's basis branch, not renaming a worktree
	renameRemoteBranch bool // true = also rename remote branch (push new, delete old)

	// Loading animation
	initialLoad   bool // true until first ReposLoadedMsg
	loaderFrame   int
	loaderTicking bool // a spinner tick loop is alive (avoid double loops)

	// Context menu — target snapshotted at open (same reason as rename)
	contextMenu ui.ContextMenuModel
	menuRepo    git.Repo
	menuWT      git.Worktree
	menuHasWT   bool

	// Post-creation prompt
	openPromptActive    bool
	openPromptResults   []*git.CreateResult
	openPromptSelection opener.ClickUsage // option highlighted in the open prompt
	openPromptHovered   opener.ClickUsage // option under the mouse (-1 = none)

	// Delete confirmation — target snapshotted at dialog open
	deleteConfirmActive bool
	deleteRepo          git.Repo
	deleteWT            git.Worktree
	deleteTypedInput    textinput.Model // for "type DELETE" confirmation
	deleteDangerous     bool            // true = requires typing DELETE
	deleteRemoteBranch  bool            // true = also delete remote branch (for merged)
	deleteLocalBranch   bool            // true = also delete local branch
	deleteProtected     bool            // branch is protected: worktree removable, branch always kept

	// Cleanup confirmation
	cleanupConfirmActive bool
	cleanupRemoteBranch  bool // true = also delete remote branches during cleanup

	// Header hover states
	versionHovered     bool
	hoveredUsage       opener.ClickUsage // -1 = none
	updateAvailVersion string            // non-empty when update available but not auto-installed
	updateBadgeHovered bool

	// History suggestion hover (empty state)
	hoveredHistory int // -1 = none

	// Relaunch: set to a directory path to relaunch LTS there after quit
	RelaunchDir string

	// RelaunchSetup: quit into the setup wizard, then relaunch here
	RelaunchSetup bool

	// Busy repos: name → status label of the operation holding the lock.
	// Operations lock their target repos (constituent names for monorepo
	// groups); ops on disjoint repos run concurrently. Only the Update
	// thread touches this map.
	busy map[string]string

	// Live LTS tmux sessions (session name → alive), for the ● indicators
	tmuxLive map[string]bool

	// Last auto-maintenance evaluation (throttles the hourly tick)
	lastMaintenance time.Time

	// Launch greeting shown in the header gap
	greeting string

	// GitHub clone browser + session cache of cloneable repos
	cloneUI     ui.CloneModel
	ghRepos     []gh.Repo
	ghFetchedAt time.Time
	ghState     ui.CloneAvail // cached availability (refreshed with discovery)
	ghUser      string        // authenticated GitHub login (fetched once)
	ghUserHover bool

	// Repo name → origin is a GitHub remote (drives PR surfaces)
	githubRemotes map[string]bool

	// Hibernate — repo snapshotted at open; the repo stays locked from
	// audit start until the dialog closes so nothing mutates it while the
	// user reads the checklist
	hibernateActive       bool
	hibernateRepo         git.Repo
	hibernateAudit        *git.HibernateAudit // nil while the audit runs
	hibernateAuditPending bool                // an audit goroutine is in flight
	hibernateInput        textinput.Model

	// Post-clone .env restore prompt (offered when a hibernate backup exists)
	envRestoreActive bool
	envRestoreRepo   string // repo dir name
	envRestoreDir    string // backup dir holding the files
	envRestoreMain   int    // restorable main-repo files
	envRestoreWT     int    // worktree files (stay in the backup)

	// Disk gauge + background worktree size scanning
	diskUsed, diskTotal uint64
	sizeCache           map[string]sizeEntry
	sizeScanning        bool

	// Theme Studio (full-screen theme browser with live preview)
	themeStudio ui.StudioModel

	// Explorer layout state (selection, pane focus, scrolls)
	explorer    ui.ExplorerState
	hoveredView int // header View toggle hover: -1 none, 0 Board, 1 Explorer

	// Log panel
	logPanel ui.LogPanelModel
	logChan  chan LogEntryMsg // persistent, shared by all operations
}

func NewModel(cfg config.Config) Model {
	ti := textinput.New()
	ti.Placeholder = "new-branch-name"
	ti.CharLimit = 100
	ti.Width = 40

	di := textinput.New()
	di.Placeholder = "DELETE"
	di.CharLimit = 6
	di.Width = 10

	hi := textinput.New()
	hi.Placeholder = "DELETE"
	hi.CharLimit = 6
	hi.Width = 10

	ui.CurrentWorkDir = cfg.WorkDir
	ui.ApplyTheme(ui.ThemeByKey(cfg.Global.Theme))

	return Model{
		config:            cfg,
		clickUsage:        clickUsageFromString(cfg.Global.ClickUsage),
		focusedCard:       -1,
		focusedWT:         -1,
		hoveredBtn:        ui.BtnNone,
		hoveredUsage:      -1,
		hoveredHistory:    -1,
		hoveredView:       -1,
		explorer:          ui.ExplorerState{SelectedWT: -1},
		openPromptHovered: -1,
		renameInput:       ti,
		deleteTypedInput:  di,
		hibernateInput:    hi,
		initialLoad:       true,
		loaderTicking:     true, // Init issues the first tick
		greeting:          launchGreeting(cfg),
		ghState:           ghAvailability(cfg.Global.GithubIntegration),
		busy:              make(map[string]string),
		sizeCache:         make(map[string]sizeEntry),
		logPanel:          ui.NewLogPanel(),
		logChan:           make(chan LogEntryMsg, 64),
	}
}

// greetingTemplates rotate per launch; %s is the git user's first name.
var greetingTemplates = []string{
	"Heyya %s! Let's get worktree-ing",
	"Welcome back, %s — branches await",
	"Ready to branch out, %s?",
	"May your merges be clean, %s",
	"Plant something great today, %s",
	"Back at it, %s?",
	"Ship it, %s",
	"Sick leave k na %s, g",
	"%s's work be like: 'pls fix, push, PR'",
	"Whats uppp, %s",
	"Kape muna bago worktree, %s?",
	"Push mo na yan, %s",
	"Deploy on a Friday? Bold move, %s",
	"Merge conflicts fear %s",
	"works on my machine — %s, probably",
	"Another day, another branch, %s",
	"How about you touch some grass for once, %s",
	"Lets goooo %s",
	"Have you committed today, %s?",
	"%s, the branches won't merge themselves",
	"Rebase responsibly, %s",
	"%s vs the backlog: round infinity",
	"Nice worktrees, %s. Did an AI make those?",
	"One commit at a time, %s, you got this",
	"Small diffs, big dreams, %s",
	"Hydrate then dominate, %s",
}

var namelessGreetings = []string{
	"Let's get worktree-ing",
	"Branches await",
	"Ready to branch out?",
	"May your merges be clean",
	"Pahinga, when kaya",
	"LTS FTW LGTM",
	"Sana all merged na",
	"It's not a bug, it's a feature",
	"This is a reminder na magkape ka na",
	"Hays.",
	"4x+12=3, find x",
	"7 x 8 = ? — no calculator allowed",
	"What has keys but can't open locks?",
	"I speak without a mouth and hear without ears. What am I?",
	"How much wood would a woodchuck chuck if a woodchuck could chuck wood?",
	"She sells seashells by the seashore",
	"Peter Piper picked a peck of pickled peppers",
	"If a branch is deleted and no one fetched it, did it ever exist?",
	"We are all just worktrees of the same repo",
	"To merge or to rebase — that is the question",
	"The unexamined diff is not worth merging",
	"A repo is a memory of every mistake we chose to keep",
}

// weekendGreeting counts down to Saturday with matching energy.
func weekendGreeting(name string) string {
	switch wd := time.Now().Weekday(); wd {
	case time.Saturday, time.Sunday:
		return "It's the weekend, " + name + " — why are you here?"
	case time.Friday:
		return "It's Friday, " + name + ", almost there"
	default:
		days := int(time.Saturday - wd)
		unit := "days"
		if days == 1 {
			unit = "day"
		}
		return fmt.Sprintf("%d %s left till the weekend, %s, you got this", days, unit, name)
	}
}

// launchGreeting honors the Launch Greeting setting (some want the jokes,
// some want a quiet tool).
func launchGreeting(cfg config.Config) string {
	if !cfg.Global.LaunchGreeting {
		return ""
	}
	return pickGreeting()
}

// pickGreeting composes a random launch greeting from the git user name.
func pickGreeting() string {
	var pool []string
	if fields := strings.Fields(git.UserName()); len(fields) > 0 {
		first := fields[0]
		for _, t := range greetingTemplates {
			pool = append(pool, fmt.Sprintf(t, first))
		}
		pool = append(pool, weekendGreeting(first))
	}
	pool = append(pool, namelessGreetings...)
	return pool[rand.IntN(len(pool))]
}

// usageLabels returns the display names of the click-usage targets,
// derived from the configured commands.
func (m *Model) usageLabels() ui.UsageLabels {
	return ui.UsageLabels{
		IDE:      m.config.IDELabel(),
		AICli:    m.config.AICliLabel(),
		Terminal: m.config.TerminalLabel(),
	}
}

// openerOpts assembles the opener configuration from settings.
func (m *Model) openerOpts() opener.Options {
	return opener.Options{
		IDECommand:      m.config.Global.IDECommand,
		AICliCommand:    m.config.Global.AICliCommand,
		Terminal:        m.config.Global.Terminal,
		Multiplexer:     m.config.Global.Multiplexer,
		TmuxAIPaneWidth: m.config.Global.TmuxAIPaneWidth,
		TmuxRightPanes:  m.config.Global.TmuxRightPanes,
	}
}

// tmuxFallbackNote appends a hint when tmux mode is configured but the
// binary is missing (opens fall back to a plain terminal).
func (m *Model) tmuxFallbackNote(mode opener.ClickUsage) string {
	if mode != opener.ClickIDE && m.config.Global.Multiplexer == "tmux" && !opener.TmuxAvailable() {
		return " (tmux not found — opened plain)"
	}
	return ""
}

// explorerZoneAt finds the Explorer hit zone at a screen position,
// preferring the most specific zone (actions before rows before repos).
func explorerZoneAt(zones []ui.HitZone, x, y int) (ui.HitZone, bool) {
	best := ui.HitZone{}
	found := false
	rank := func(t ui.HitZoneType) int {
		switch t {
		case ui.ZoneExplorerAction, ui.ZoneExplorerNew:
			return 2
		case ui.ZoneExplorerRow:
			return 1
		case ui.ZoneExplorerRepo, ui.ZoneClone:
			return 0
		}
		return -1
	}
	for _, z := range zones {
		if rank(z.Type) < 0 {
			continue
		}
		if y >= z.Y && y < z.Y+z.H && x >= z.X && x < z.X+z.W {
			if !found || rank(z.Type) > rank(best.Type) {
				best = z
				found = true
			}
		}
	}
	return best, found
}

// explorerActive reports whether the Explorer layout is showing (it needs
// repos; the empty state stays shared with Board).
func (m *Model) explorerActive() bool {
	return m.config.Global.Layout == "explorer" && len(m.repos) > 0
}

// clampExplorer keeps the Explorer selection valid after repo reloads.
func (m *Model) clampExplorer() {
	if m.explorer.SelectedRepo >= len(m.repos) {
		m.explorer.SelectedRepo = len(m.repos) - 1
	}
	if m.explorer.SelectedRepo < 0 {
		m.explorer.SelectedRepo = 0
	}
	if len(m.repos) > 0 {
		wts := len(m.repos[m.explorer.SelectedRepo].Worktrees)
		if m.explorer.SelectedWT >= wts {
			m.explorer.SelectedWT = wts - 1
		}
	}
}

// ensureExplorerRowVisible scrolls the sheet so the selected row shows.
func (m *Model) ensureExplorerRowVisible() {
	// Sheet rows capacity mirrors LayoutExplorer: height - borders(2) -
	// title(1) - column header(1) - nav hint(1) - action strip(1)
	// [- migration notice(1)]
	exH := m.height - m.headerH - 2
	if m.logPanel.Visible && len(m.logPanel.Entries) > 0 {
		exH -= 9
	}
	capacity := exH - 6
	if m.explorer.SelectedRepo < len(m.repos) && m.repos[m.explorer.SelectedRepo].NeedsMigration {
		capacity--
	}
	if capacity < 1 {
		capacity = 1
	}
	if m.explorer.SelectedWT < m.explorer.SheetScroll {
		m.explorer.SheetScroll = m.explorer.SelectedWT
	}
	if m.explorer.SelectedWT >= m.explorer.SheetScroll+capacity {
		m.explorer.SheetScroll = m.explorer.SelectedWT - capacity + 1
	}
}

// ensureExplorerRepoVisible scrolls the sidebar so the selected repo shows.
func (m *Model) ensureExplorerRepoVisible() {
	exH := m.height - m.headerH - 2
	if m.logPanel.Visible && len(m.logPanel.Entries) > 0 {
		exH -= 9
	}
	capacity := exH - 3 // borders(2) + title(1)
	if capacity < 1 {
		capacity = 1
	}
	if m.explorer.SelectedRepo < m.explorer.SidebarScroll {
		m.explorer.SidebarScroll = m.explorer.SelectedRepo
	}
	if m.explorer.SelectedRepo >= m.explorer.SidebarScroll+capacity {
		m.explorer.SidebarScroll = m.explorer.SelectedRepo - capacity + 1
	}
}

// repoActivity returns a repo's newest worktree activity (for sorting).
func repoActivity(r git.Repo) int64 {
	var newest int64
	for _, wt := range r.Worktrees {
		if wt.LastActivity > newest {
			newest = wt.LastActivity
		}
	}
	return newest
}

// repoCreated returns a repo's newest worktree creation time (for sorting).
func repoCreated(r git.Repo) int64 {
	var newest int64
	for _, wt := range r.Worktrees {
		if wt.CreatedAt > newest {
			newest = wt.CreatedAt
		}
	}
	return newest
}

// sortRepos orders repos (Board cards / Explorer sidebar) and each repo's
// worktrees by the configured sort: newest activity first (default), newest
// created first, or alphabetical. Name breaks ties so ordering is stable;
// unknown timestamps sink to the bottom.
func sortRepos(repos []git.Repo, order string) {
	repoKey := repoActivity
	wtKey := func(wt git.Worktree) int64 { return wt.LastActivity }
	switch order {
	case "name":
		repoKey = nil
	case "created":
		repoKey = repoCreated
		wtKey = func(wt git.Worktree) int64 { return wt.CreatedAt }
	}

	sort.SliceStable(repos, func(i, j int) bool {
		if repoKey != nil {
			ki, kj := repoKey(repos[i]), repoKey(repos[j])
			if ki != kj {
				return ki > kj
			}
		}
		return repos[i].Name < repos[j].Name
	})
	for ri := range repos {
		wts := repos[ri].Worktrees
		sort.SliceStable(wts, func(i, j int) bool {
			if repoKey != nil {
				ki, kj := wtKey(wts[i]), wtKey(wts[j])
				if ki != kj {
					return ki > kj
				}
			}
			return wts[i].Branch < wts[j].Branch
		})
	}
}

// ageDuration parses maintenance age options ("8H", "1D"); "OFF"/"" = 0.
func ageDuration(v string) time.Duration {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" || v == "OFF" || len(v) < 2 {
		return 0
	}
	n, err := strconv.Atoi(v[:len(v)-1])
	if err != nil || n <= 0 {
		return 0
	}
	switch v[len(v)-1] {
	case 'H':
		return time.Duration(n) * time.Hour
	case 'D':
		return time.Duration(n) * 24 * time.Hour
	}
	return 0
}

// maintenanceCandidates returns the worktree paths idle for longer than
// maxAge (by LastActivity) plus the repo locks cleaning them requires.
// Busy repos and worktrees without activity data are skipped.
func (m *Model) maintenanceCandidates(maxAge time.Duration) (paths, locks []string) {
	cutoff := time.Now().Add(-maxAge).Unix()
	lockSeen := make(map[string]bool)
	for _, r := range m.repos {
		if m.repoBusy(r) || r.NeedsMigration || r.Hidden {
			continue
		}
		for _, wt := range r.Worktrees {
			if wt.LastActivity > 0 && wt.LastActivity < cutoff {
				paths = append(paths, wt.Path)
				for _, n := range lockSet(r) {
					if !lockSeen[n] {
						lockSeen[n] = true
						locks = append(locks, n)
					}
				}
			}
		}
	}
	return paths, locks
}

// startAutoMaintenance kicks off the startup sweep when configured:
// node_modules of idle worktrees are removed (worktrees with an attached
// tmux session are skipped) and idle unattached sessions are killed.
func (m *Model) startAutoMaintenance() tea.Cmd {
	modAge := ageDuration(m.config.Global.AutoCleanModules)
	tmuxAge := time.Duration(0)
	if m.config.Global.Multiplexer == "tmux" {
		tmuxAge = ageDuration(m.config.Global.AutoKillTmux)
	}
	if modAge == 0 && tmuxAge == 0 {
		return nil
	}
	var paths, locks []string
	if modAge > 0 {
		paths, locks = m.maintenanceCandidates(modAge)
	}
	if len(paths) == 0 && tmuxAge == 0 {
		return nil
	}
	logFn, startCmd := m.beginOp("Auto-maintenance...", locks...)
	return tea.Batch(startCmd, maintenanceCmd(logFn, paths, tmuxAge, locks))
}

// sizeEntry caches one worktree's scanned size, keyed to the activity that
// was current when scanned (activity change → rescan) with a time cap.
type sizeEntry struct {
	size      ui.WTSize
	activity  int64
	scannedAt time.Time
}

// diskUsage returns used and total bytes of the volume containing path.
func diskUsage(path string) (used, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bsize := uint64(st.Bsize)
	total = st.Blocks * bsize
	return total - st.Bavail*bsize, total
}

// wtSizes projects the cache into the Explorer's SIZE column input.
// nil (scanning disabled, or nothing scanned yet) hides the column.
func (m *Model) wtSizes() map[string]ui.WTSize {
	if !m.config.Global.SizeScanning || len(m.sizeCache) == 0 {
		return nil
	}
	out := make(map[string]ui.WTSize, len(m.sizeCache))
	for p, e := range m.sizeCache {
		out[p] = e.size
	}
	return out
}

// staleSizePaths returns worktrees needing a (re)scan: unscanned, activity
// changed since the last scan, or cache older than 10 minutes.
func (m *Model) staleSizePaths() []string {
	var paths []string
	for _, r := range m.repos {
		for _, wt := range r.Worktrees {
			e, ok := m.sizeCache[wt.Path]
			if !ok || e.activity != wt.LastActivity || time.Since(e.scannedAt) > 10*time.Minute {
				paths = append(paths, wt.Path)
			}
		}
	}
	return paths
}

// scanSizesCmd walks the given worktrees sequentially in one goroutine —
// size scanning is heavy I/O and must never block the UI.
func scanSizesCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		sizes := make(map[string]ui.WTSize, len(paths))
		for _, p := range paths {
			total, modules := git.WorktreeSize(p)
			sizes[p] = ui.WTSize{Total: total, Modules: modules, Known: true}
		}
		return SizesScannedMsg{Sizes: sizes}
	}
}

// prAble reports whether a worktree is ready for a PR: single GitHub repo,
// remote branch exists, not already merged.
func (m *Model) prAble(repo git.Repo, wt git.Worktree) bool {
	return m.ghState == ui.CloneReady &&
		!repo.IsMonorepo &&
		m.githubRemotes[repo.Name] &&
		deleteHasRemote(wt.Status) &&
		wt.Status != git.StatusMergedCleanable &&
		wt.Status != git.StatusMergedDirty
}

// prCreateCmd opens the browser's PR page (create, or the existing PR).
func prCreateCmd(wtPath, base, branch string) tea.Cmd {
	return func() tea.Msg {
		err := gh.PRCreateWeb(wtPath, base)
		return PRDoneMsg{Branch: branch, Err: err}
	}
}

// ghUserCmd fetches the authenticated GitHub login (network — async).
func ghUserCmd() tea.Cmd {
	return func() tea.Msg {
		login, err := gh.Login()
		if err != nil {
			return GhUserMsg{}
		}
		return GhUserMsg{Login: login}
	}
}

// ghRepoListCmd fetches the cloneable repos (network — goroutine only).
func ghRepoListCmd() tea.Cmd {
	return func() tea.Msg {
		repos, err := gh.RepoList()
		return GhRepoListMsg{Repos: repos, Err: err}
	}
}

func cloneRepoCmd(logFn git.LogFunc, nameWithOwner, workDir string, locked []string) tea.Cmd {
	return func() tea.Msg {
		logFn("clone", "Cloning "+nameWithOwner+"...", false)
		repoName, err := gh.Clone(nameWithOwner, workDir)
		if err != nil {
			logFn("clone", err.Error(), true)
		} else {
			logFn("clone", "Cloned into "+repoName, false)
		}
		return CloneDoneMsg{RepoName: repoName, Locked: locked, Err: err}
	}
}

// ghAvailability maps gh's install/auth state for the clone surfaces.
// Spawns subprocesses — call from goroutines or one-shot paths only.
// enabled=false (the Settings master switch) hides every GitHub surface
// and skips the subprocess probes entirely.
func ghAvailability(enabled bool) ui.CloneAvail {
	if !enabled {
		return ui.CloneDisabled
	}
	if !gh.Available() {
		return ui.CloneMissing
	}
	if !gh.Authed() {
		return ui.CloneNoAuth
	}
	return ui.CloneReady
}

// openCloneBrowser opens the clone modal with the cached list (refreshing
// in the background when stale) or a fresh fetch.
func (m Model) openCloneBrowser() (Model, tea.Cmd) {
	if !m.config.Global.GithubIntegration {
		m.statusMsg = "GitHub integration is disabled in Settings"
		return m, clearStatusCmd()
	}
	if !gh.Available() {
		m.statusMsg = "GitHub CLI not found — install gh (brew install gh) and run gh auth login"
		return m, clearStatusCmd()
	}
	if !gh.Authed() {
		m.statusMsg = "GitHub CLI not authenticated — run gh auth login"
		return m, clearStatusCmd()
	}
	m.cloneUI = ui.NewCloneModal(m.ghRepos)
	var cmds []tea.Cmd
	cmds = append(cmds, textinput.Blink)
	if len(m.ghRepos) == 0 || time.Since(m.ghFetchedAt) > 5*time.Minute {
		cmds = append(cmds, ghRepoListCmd())
	}
	return m, tea.Batch(cmds...)
}

// maintenanceTickCmd schedules the next hourly auto-maintenance check.
func maintenanceTickCmd() tea.Cmd {
	return tea.Tick(time.Hour, func(time.Time) tea.Msg { return MaintenanceTickMsg{} })
}

func maintenanceCmd(logFn git.LogFunc, paths []string, tmuxIdle time.Duration, locked []string) tea.Cmd {
	return func() tea.Msg {
		attached := opener.AttachedSessions()
		cleaned := 0
		var freed int64
		for _, p := range paths {
			if attached[opener.SessionName(p)] {
				logFn("auto", "Skipping "+filepath.Base(p)+" — tmux session attached", false)
				continue
			}
			r, f, err := git.CleanModules(p, logFn)
			if err == nil {
				cleaned += r
				freed += f
			}
		}
		var killed []string
		if tmuxIdle > 0 {
			killed = opener.KillIdleSessions(tmuxIdle)
			for _, k := range killed {
				logFn("auto", "Killed idle session "+k, false)
			}
		}
		return MaintenanceDoneMsg{Cleaned: cleaned, Freed: freed, Killed: killed, Locked: locked}
	}
}

// openCreateModalFor opens the create modal pre-seeded with a repo's
// selection and jumps straight to the branch step.
func (m Model) openCreateModalFor(repoIdx int) (Model, tea.Cmd) {
	if repoIdx >= 0 && repoIdx < len(m.repos) && m.repos[repoIdx].Hidden {
		m.statusMsg = m.repos[repoIdx].Name + " is hidden — unhide it first"
		return m, clearStatusCmd()
	}
	if repoIdx < 0 || repoIdx >= len(m.repos) {
		return m, nil
	}
	target := m.repos[repoIdx]
	m.modal = ui.NewModal(m.creatableRepos(), m.config.WorkDir, m.resolvePM, m.config.Global.InstallOnCreate)
	// Monorepo cards pre-select their constituent repos
	want := map[string]bool{target.Name: true}
	if target.IsMonorepo {
		want = make(map[string]bool)
		for _, n := range target.RepoNames {
			want[n] = true
		}
	}
	matched := 0
	for i, r := range m.modal.Repos {
		if want[r.Name] {
			m.modal.Selected[i] = true
			matched++
		}
	}
	if matched == 0 {
		// Target vanished (reload race) — stay on the select step rather
		// than letting the modal's cursor fallback pre-seed the wrong repo
		return m, textinput.Blink
	}
	// Advance to the branch step (starts the background branch fetch)
	var cmd tea.Cmd
	m.modal, cmd = m.modal.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return m, cmd
}

// clickUsageFromString restores the persisted click mode ("ide" default).
func clickUsageFromString(s string) opener.ClickUsage {
	switch s {
	case "ai":
		return opener.ClickAICli
	case "terminal":
		return opener.ClickTerminal
	}
	return opener.ClickIDE
}

func clickUsageString(u opener.ClickUsage) string {
	switch u {
	case opener.ClickAICli:
		return "ai"
	case opener.ClickTerminal:
		return "terminal"
	}
	return "ide"
}

// setClickUsage switches the click mode and persists it — like Layout,
// it's "where I left off" state restored on the next launch.
func (m *Model) setClickUsage(u opener.ClickUsage) {
	m.clickUsage = u
	m.config.Global.ClickUsage = clickUsageString(u)
	m.config.SaveGlobal()
}

// toggleLayout switches Board ↔ Explorer and persists the choice.
func (m *Model) toggleLayout() {
	if m.config.Global.Layout == "explorer" {
		m.config.Global.Layout = "board"
	} else {
		m.config.Global.Layout = "explorer"
		if m.explorer.SelectedWT < 0 && len(m.repos) > 0 && len(m.repos[0].Worktrees) > 0 {
			m.explorer.SelectedWT = 0
		}
	}
	m.config.SaveGlobal()
	m.statusMsg = "Layout: " + m.config.Global.Layout
}

// markSessionLive flips the tmux indicator on immediately after LTS opens a
// session, without waiting for the next discovery reload.
func (m *Model) markSessionLive(path string, mode opener.ClickUsage) {
	if mode == opener.ClickIDE || m.config.Global.Multiplexer != "tmux" || !opener.TmuxAvailable() {
		return
	}
	if m.tmuxLive == nil {
		m.tmuxLive = make(map[string]bool)
	}
	m.tmuxLive[opener.SessionName(path)] = true
}

// recomputeLayout recalculates grid, hit zones, and section Y positions.
// Must be called from Update (not View) so the state persists.
func (m *Model) recomputeLayout() {
	if m.width == 0 {
		return
	}

	yPos := 0

	// Header (includes status line) — fixed, not scrollable
	m.headerView = ui.RenderHeader(m.width, m.clickUsage, m.usageLabels(), ui.HeaderOpts{
		Loading:            m.anyBusy(),
		Frame:              m.loaderFrame,
		StatusMsg:          m.statusMsg,
		VersionHovered:     m.versionHovered,
		HoveredUsage:       m.hoveredUsage,
		UpdateAvailable:    m.updateAvailVersion,
		UpdateBadgeHovered: m.updateBadgeHovered,
		Layout:             m.config.Global.Layout,
		HoveredView:        m.hoveredView,
		Greeting:           m.greeting,
		GhUser:             m.headerGhUser(),
		GhUserHovered:      m.ghUserHover,
		DiskUsed:           m.diskUsed,
		DiskTotal:          m.diskTotal,
	})
	m.headerH = lipgloss.Height(m.headerView)
	yPos += m.headerH

	// Explorer layout: a fixed-height master-detail block, no outer scrolling
	if m.explorerActive() {
		logReserve := 0
		if m.logPanel.Visible && len(m.logPanel.Entries) > 0 {
			logReserve = 9
		}
		exH := m.height - m.headerH - 2 - logReserve
		m.clampExplorer()
		canPR := false
		if m.explorer.SelectedRepo < len(m.repos) {
			r := m.repos[m.explorer.SelectedRepo]
			if m.explorer.SelectedWT >= 0 && m.explorer.SelectedWT < len(r.Worktrees) {
				canPR = m.prAble(r, r.Worktrees[m.explorer.SelectedWT])
			}
		}
		m.gridResult = ui.LayoutExplorer(m.repos, m.width, yPos, exH, m.explorer, m.hoveredBtn, m.busyCardNames(), m.tmuxLive, m.ghState, m.wtSizes(), canPR)
		m.createBtnY = 0
		m.contentHeight = lipgloss.Height(m.gridResult.View)
		m.footerY = m.headerH + m.contentHeight
		m.scrollY = 0
		return
	}

	// Grid — pass virtual yPos (header + scroll offset applied later)
	// Hit zones use absolute virtual coordinates; mouse handler adds scrollY
	m.gridResult = ui.LayoutGrid(m.repos, m.width, yPos, m.focusedCard, m.focusedWT, m.hoveredBtn, m.hoveredHistory, m.busyCardNames(), m.tmuxLive, m.ghState)
	gridH := lipgloss.Height(m.gridResult.View)
	yPos += gridH

	// Status legend and create button (only when repos exist)
	if len(m.repos) > 0 {
		legend := ui.RenderStatusLegend(m.width, m.config.Global.Multiplexer == "tmux")
		yPos += lipgloss.Height(legend)

		m.createBtnY = yPos
		createBtn := ui.RenderCreateButton(m.width, false, false)
		yPos += lipgloss.Height(createBtn)
	} else {
		m.createBtnY = 0
	}

	// Total scrollable content height (everything between header and footer)
	m.contentHeight = yPos - m.headerH

	// Footer — fixed at bottom
	// footerY is where the footer renders on screen (after visible content)
	m.footerY = yPos

	// Clamp scroll
	m.clampScroll()
}

// clampScroll ensures scrollY stays within valid bounds.
func (m *Model) clampScroll() {
	// Available viewport height = terminal height - header - footer(~1 line) - 1
	viewportH := m.height - m.headerH - 2
	if viewportH < 1 {
		viewportH = 1
	}
	maxScroll := m.contentHeight - viewportH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.scrollY > maxScroll {
		m.scrollY = maxScroll
	}
	if m.scrollY < 0 {
		m.scrollY = 0
	}
}

// syncSettingsConfig copies config changes from the settings model's config pointer
// back into the model's config. Required because Model uses value receivers, so
// the settings pointer becomes detached from m.config after each Update copy.
func (m *Model) syncSettingsConfig() {
	if m.settings.Config != nil {
		m.config.Global = m.settings.Config.Global
		m.config.Local = m.settings.Config.Local
	}
	// Toggling the greeting takes effect immediately, not next launch
	if !m.config.Global.LaunchGreeting {
		m.greeting = ""
	} else if m.greeting == "" {
		m.greeting = pickGreeting()
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		checkMigrationCmd(&m.config),
		tea.SetWindowTitle("LTS - Led's Tree Script"),
		loaderTickCmd(),
		listenForLogs(m.logChan), // persistent listener for all operations
		maintenanceTickCmd(),     // hourly auto-maintenance re-check
	}
	if m.config.Global.GithubIntegration && gh.Available() {
		cmds = append(cmds, ghUserCmd())
	}
	if m.config.Global.CheckForUpdates && update.ShouldCheck(m.config.Global.LastUpdateCheck) {
		// Dev builds never auto-replace themselves with the official binary —
		// they check only and surface the update badge for an explicit click.
		cmds = append(cmds, updateCheckCmd(m.config.Global.AutoUpdate && !version.IsDev()))
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.settings.ViewHeight = msg.Height
		m.settings.ViewWidth = msg.Width
		m.recomputeLayout()
		return m, nil

	case tea.KeyMsg:
		// Forward to settings if active
		if m.settings.Active {
			var cmd tea.Cmd
			m.settings, cmd = m.settings.Update(msg)
			// Sync config changes from settings back to model
			m.syncSettingsConfig()
			if !m.settings.Active {
				// Settings closed — reload repos in case basis branch changed
				m.recomputeLayout()
				return m, loadReposCmd(&m.config)
			}
			return m, cmd
		}
		updated, cmd := handleKeyPress(m, msg)
		updated.recomputeLayout()
		return updated, cmd

	case LoaderTickMsg:
		if m.initialLoad || m.anyBusy() {
			m.loaderFrame++
			m.recomputeLayout()
			return m, loaderTickCmd()
		}
		m.loaderTicking = false
		return m, nil

	case tea.MouseMsg:
		result, cmd := m.handleMouse(msg)
		resultModel := result.(Model)
		resultModel.recomputeLayout()
		return resultModel, cmd

	case ui.SettingsActionMsg:
		switch msg.Action {
		case "THEME_STUDIO_ACTION":
			return openThemeStudio(m)
		case "CHECK_FOR_UPDATE_ACTION":
			m.statusMsg = "Checking for updates..."
			m.statusGen++
			m.recomputeLayout()
			return m, updateCheckCmd(false)
		case "KILL_TMUX_SESSIONS_ACTION":
			killed := opener.KillAllSessions()
			m.tmuxLive = nil
			m.settings.SaveStatus = fmt.Sprintf("Killed %d tmux session(s)", killed)
			m.recomputeLayout()
			return m, nil
		case "RESET_SETUP_ACTION":
			// Resetting exec-relaunches the process — it would kill
			// running operations mid-flight
			if m.anyBusy() {
				m.settings.SaveError = "Wait for running operations to finish"
				return m, nil
			}
			m.RelaunchSetup = true
			return m, tea.Quit
		}
		return m, nil

	case ui.SettingsSavedMsg:
		// Setting changed — reload repos to reflect new config immediately
		m.recomputeLayout()
		return m, loadReposCmd(&m.config)

	case ui.SettingsSaveClearMsg:
		// Forward clear to settings model
		if m.settings.Active {
			var cmd tea.Cmd
			m.settings, cmd = m.settings.Update(msg)
			return m, cmd
		}
		return m, nil

	case MigrationCheckMsg:
		if msg.Needed {
			m.statusMsg = "Improving directory structure, please wait..."
			m.recomputeLayout()
			return m, doMigrationCmd(&m.config)
		}
		return m, loadReposCmd(&m.config)

	case MigrationDoneMsg:
		m.statusMsg = ""
		return m, loadReposCmd(&m.config)

	case ReposLoadedMsg:
		wasInitialLoad := m.initialLoad
		msg.Repos = m.applyHiddenRepos(msg.Repos)
		sortRepos(msg.Repos, m.config.Global.SortOrder)
		m.repos = msg.Repos
		m.tmuxLive = msg.TmuxLive
		m.diskUsed, m.diskTotal = msg.DiskUsed, msg.DiskTotal
		m.githubRemotes = msg.GithubRemotes
		wasGhReady := m.ghState == ui.CloneReady
		m.ghState = msg.GhState
		m.initialLoad = false
		if msg.Err != nil {
			m.statusMsg = "Error loading repos: " + msg.Err.Error()
		}
		// Initialize local config for all discovered repos
		var repoNames []string
		for _, r := range m.repos {
			if !r.IsMonorepo {
				repoNames = append(repoNames, r.Name)
			}
		}
		m.config.InitLocalForRepos(repoNames)
		// Save to history if repos were found
		if len(m.repos) > 0 {
			go config.SaveHistory(m.config.WorkDir, len(m.repos))
		}
		m.recomputeLayout()
		// Background worktree size scan for anything stale (one walker)
		var followUps []tea.Cmd
		if m.config.Global.SizeScanning && !m.sizeScanning {
			if stale := m.staleSizePaths(); len(stale) > 0 {
				m.sizeScanning = true
				followUps = append(followUps, scanSizesCmd(stale))
			}
		}
		// gh authenticated mid-session (or just now): fetch the login for
		// the header line, which Init only fetches when gh existed then
		if m.ghState == ui.CloneReady && !wasGhReady && m.ghUser == "" {
			followUps = append(followUps, ghUserCmd())
		}
		if len(followUps) > 0 && !wasInitialLoad {
			return m, tea.Batch(followUps...)
		}
		// Startup auto-maintenance runs after the first discovery; the
		// hourly MaintenanceTickMsg covers long-running instances
		if wasInitialLoad {
			m.lastMaintenance = time.Now()
			if cmd := m.startAutoMaintenance(); cmd != nil {
				followUps = append(followUps, cmd)
			}
		}
		if len(followUps) > 0 {
			return m, tea.Batch(followUps...)
		}
		return m, nil

	case RefreshDoneMsg:
		m.clearBusy(msg.Locked...)
		if msg.Err != nil {
			m.statusMsg = "Refresh error: " + msg.Err.Error()
		} else {
			if len(msg.Failed) > 0 {
				m.statusMsg = fmt.Sprintf("Refreshed %d repos (failed: %s)", msg.Count, strings.Join(msg.Failed, ", "))
			} else {
				m.statusMsg = fmt.Sprintf("Refreshed %d repos", msg.Count)
			}
			// Update last refresh for successful repos only
			for _, r := range m.repos {
				if r.IsMonorepo {
					continue
				}
				isFailed := false
				for _, f := range msg.Failed {
					if f == r.Name {
						isFailed = true
						break
					}
				}
				if !isFailed {
					m.config.SetRepoLastRefresh(r.Name, time.Now().Unix())
				}
			}
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case SingleRefreshDoneMsg:
		m.clearBusy(msg.Locked...)
		if msg.Err != nil {
			m.statusMsg = "Refresh error: " + msg.Err.Error()
		} else {
			m.statusMsg = msg.RepoName + " refreshed"
			m.config.SetRepoLastRefresh(msg.RepoName, time.Now().Unix())
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case RebaseDoneMsg:
		m.clearBusy(msg.Locked...)
		if msg.Err != nil {
			m.statusMsg = "Rebase error: " + msg.Err.Error()
		} else {
			m.statusMsg = "Rebased " + msg.Branch
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case DeleteDoneMsg:
		m.clearBusy(msg.Locked...)
		m.focusedWT = -1
		if msg.Err != nil {
			m.statusMsg = "Delete error: " + msg.Err.Error()
		} else {
			m.statusMsg = "Deleted " + msg.Branch
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case CreateDoneMsg:
		m.clearBusy(msg.Locked...)
		if msg.Err != nil {
			m.statusMsg = "Create error: " + msg.Err.Error()
			m.recomputeLayout()
			return m, tea.Batch(
				loadReposCmd(&m.config),
				clearStatusCmd(),
			)
		}
		names := make([]string, len(msg.Results))
		for i, r := range msg.Results {
			names[i] = r.RepoName
		}
		m.statusMsg = fmt.Sprintf("Created %s on %s", strings.Join(names, ", "), msg.Branch)
		opener.PlayDoneSound(m.config.Global.DoneSound)
		m.openPromptActive = true
		m.openPromptResults = msg.Results
		m.openPromptSelection = opener.ClickIDE
		m.openPromptHovered = -1
		m.recomputeLayout()
		return m, loadReposCmd(&m.config)

	case MaintenanceTickMsg:
		// Re-evaluate hourly so instances left running still sweep items
		// that cross their age threshold after launch
		cmds := []tea.Cmd{maintenanceTickCmd()}
		if time.Since(m.lastMaintenance) > 55*time.Minute {
			m.lastMaintenance = time.Now()
			if cmd := m.startAutoMaintenance(); cmd != nil {
				cmds = append(cmds, cmd)
				m.recomputeLayout()
			}
		}
		return m, tea.Batch(cmds...)

	case MaintenanceDoneMsg:
		m.clearBusy(msg.Locked...)
		for _, k := range msg.Killed {
			delete(m.tmuxLive, k)
		}
		if msg.Cleaned > 0 || len(msg.Killed) > 0 {
			m.statusMsg = fmt.Sprintf("Auto-maintenance: cleaned %d node_modules (%s), killed %d idle session(s)",
				msg.Cleaned, git.HumanBytes(msg.Freed), len(msg.Killed))
		} else {
			m.statusMsg = ""
		}
		m.recomputeLayout()
		return m, clearStatusCmd()

	case SizesScannedMsg:
		m.sizeScanning = false
		now := time.Now()
		activityByPath := make(map[string]int64)
		for _, r := range m.repos {
			for _, wt := range r.Worktrees {
				activityByPath[wt.Path] = wt.LastActivity
			}
		}
		for p, sz := range msg.Sizes {
			m.sizeCache[p] = sizeEntry{size: sz, activity: activityByPath[p], scannedAt: now}
		}
		m.recomputeLayout()
		return m, nil

	case CleanModulesDoneMsg:
		m.clearBusy(msg.Locked...)
		delete(m.sizeCache, msg.WtPath) // force a fresh size next scan
		if msg.Err != nil {
			m.statusMsg = "Clean modules error: " + msg.Err.Error()
		} else if msg.Removed == 0 {
			m.statusMsg = "No node_modules in " + msg.Branch
		} else {
			m.statusMsg = fmt.Sprintf("Cleaned %d node_modules in %s (freed %s)", msg.Removed, msg.Branch, git.HumanBytes(msg.Freed))
		}
		m.recomputeLayout()
		return m, clearStatusCmd()

	case CleanupMergedDoneMsg:
		m.clearBusy(msg.Locked...)
		if msg.Err != nil {
			m.statusMsg = "Cleanup error: " + msg.Err.Error()
		} else if msg.Cleaned == 0 {
			m.statusMsg = "No merged cleanables found"
		} else {
			m.statusMsg = fmt.Sprintf("Cleaned %d merged worktrees", msg.Cleaned)
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case RenameDoneMsg:
		m.clearBusy(msg.Locked...)
		if msg.Err != nil {
			m.statusMsg = "Rename error: " + msg.Err.Error()
		} else {
			m.statusMsg = "Renamed to " + msg.NewBranch
		}
		m.recomputeLayout()
		return m, tea.Batch(
			loadReposCmd(&m.config),
			clearStatusCmd(),
		)

	case MigrateDoneMsg:
		m.clearBusy(msg.Locked...)
		if msg.Err != nil {
			m.statusMsg = "Migration error: " + msg.Err.Error()
			m.recomputeLayout()
			return m, tea.Batch(
				loadReposCmd(&m.config),
				clearStatusCmd(),
			)
		}
		m.statusMsg = fmt.Sprintf("Migrated %s to LTS worktree", msg.Result.Branch)
		opener.PlayDoneSound(m.config.Global.DoneSound)
		m.openPromptActive = true
		m.openPromptResults = []*git.CreateResult{msg.Result}
		m.openPromptSelection = opener.ClickIDE
		m.openPromptHovered = -1
		m.recomputeLayout()
		return m, loadReposCmd(&m.config)

	case LogEntryMsg:
		m.logPanel.Add(msg.Context, msg.Message, msg.IsError)
		m.recomputeLayout()
		// Re-subscribe to get the next log entry
		if m.logChan != nil {
			return m, listenForLogs(m.logChan)
		}
		return m, nil

	case StatusClearMsg:
		// Gen 0 = legacy (always clear). Gen > 0 = only clear if matching current gen.
		if !m.anyBusy() && (msg.Gen == 0 || msg.Gen == m.statusGen) {
			m.statusMsg = ""
			m.recomputeLayout()
		}
		return m, nil

	case UpdateCheckMsg:
		m.config.SetLastUpdateCheck(time.Now().Unix())
		r := msg.Result
		if r.Err != nil {
			if r.LatestVersion == "" {
				// Silently ignore startup check errors (no version info)
				return m, nil
			}
			// Update failed — restore badge for retry
			m.updateAvailVersion = r.LatestVersion
			m.statusMsg = "Update failed: " + r.Err.Error()
			m.statusGen++
			m.recomputeLayout()
			return m, clearStatusAfter(m.statusGen, 10*time.Second)
		}
		if r.Updated {
			m.updateAvailVersion = "" // clear badge
			m.updateBadgeHovered = false
			m.statusMsg = fmt.Sprintf("Updated to v%s — restart to apply", r.LatestVersion)
			m.statusGen++
			m.recomputeLayout()
			return m, clearStatusAfter(m.statusGen, 10*time.Second)
		}
		if r.UpdateAvail {
			// Show the persistent badge when auto-update won't handle it:
			// auto-update disabled, or a dev build (which never auto-replaces)
			if !m.config.Global.AutoUpdate || version.IsDev() {
				m.updateAvailVersion = r.LatestVersion
			}
			m.statusMsg = fmt.Sprintf("New version available: v%s", r.LatestVersion)
			m.statusGen++
			m.recomputeLayout()
			return m, clearStatusAfter(m.statusGen, 10*time.Second)
		}
		// No update available
		m.statusMsg = "Already on latest version (v" + r.CurrentVersion + ")"
		m.statusGen++
		m.recomputeLayout()
		return m, clearStatusAfter(m.statusGen, 5*time.Second)

	case GhUserMsg:
		m.ghUser = msg.Login
		m.recomputeLayout()
		return m, nil

	case PRDoneMsg:
		if msg.Err != nil {
			m.statusMsg = "PR error: " + msg.Err.Error()
		} else {
			m.statusMsg = "Opened PR page for " + msg.Branch
		}
		m.recomputeLayout()
		return m, clearStatusCmd()

	case GhRepoListMsg:
		if msg.Err == nil {
			m.ghRepos = msg.Repos
			m.ghFetchedAt = time.Now()
		}
		if m.cloneUI.Active {
			m.cloneUI.SetRepos(msg.Repos, msg.Err)
		}
		return m, nil

	case ui.CloneSelectedMsg:
		name := msg.NameWithOwner
		dirName := name
		if idx := strings.IndexByte(name, '/'); idx >= 0 {
			dirName = name[idx+1:]
		}
		if _, err := os.Stat(filepath.Join(m.config.WorkDir, dirName)); err == nil {
			m.statusMsg = dirName + " already exists here"
			m.recomputeLayout()
			return m, clearStatusCmd()
		}
		if _, busy := m.busy[dirName]; busy {
			m.statusMsg = dirName + " is busy"
			return m, clearStatusCmd()
		}
		lock := []string{dirName}
		logFn, startCmd := m.beginOp("Cloning "+name+"...", lock...)
		return m, tea.Batch(startCmd, cloneRepoCmd(logFn, name, m.config.WorkDir, lock))

	case ui.CloneCancelMsg:
		return m, nil

	case CloneDoneMsg:
		m.clearBusy(msg.Locked...)
		if msg.Err != nil {
			m.statusMsg = "Clone error: " + msg.Err.Error()
		} else {
			m.statusMsg = "Cloned " + msg.RepoName
			// A hibernate env backup for this repo? Offer it back. Never
			// clobber a prompt another clone already raised — point at the
			// backup instead so the offer isn't silently lost.
			if dir, mainN, wtN, ok := git.LatestEnvBackup(envBackupRoot(), msg.RepoName); ok {
				if mainN > 0 && m.envRestoreActive {
					m.statusMsg += " · .env backup available at " + dir
				} else if mainN > 0 {
					m.envRestoreActive = true
					m.envRestoreRepo = msg.RepoName
					m.envRestoreDir = dir
					m.envRestoreMain = mainN
					m.envRestoreWT = wtN
				} else if wtN > 0 {
					m.statusMsg += " · worktree .env backup at " + dir
				}
			}
		}
		m.recomputeLayout()
		return m, tea.Batch(loadReposCmd(&m.config), clearStatusCmd())

	case HibernateAuditMsg:
		if !m.hibernateActive || m.hibernateRepo.Name != msg.RepoName {
			// Dialog closed while the audit ran — release the locks it held
			m.hibernateAuditPending = false
			m.clearBusy(msg.Locked...)
			return m, nil
		}
		m.hibernateAuditPending = false
		audit := msg.Audit
		m.hibernateAudit = &audit
		if audit.Blockers() == 0 {
			m.hibernateInput.SetValue("")
			m.hibernateInput.Focus()
			return m, textinput.Blink
		}
		return m, nil

	case HibernateDoneMsg:
		m.clearBusy(msg.Locked...)
		if msg.Err != nil {
			m.statusMsg = "Hibernate failed: " + msg.Err.Error()
		} else {
			m.statusMsg = "Hibernated " + msg.RepoName + " — freed " + git.HumanBytes(msg.Freed)
			if msg.EnvBacked > 0 {
				m.statusMsg += fmt.Sprintf(" · %d .env backed up", msg.EnvBacked)
			}
			m.statusMsg += " · re-clone with (c)"
		}
		m.recomputeLayout()
		return m, tea.Batch(loadReposCmd(&m.config), clearStatusCmd())

	case ui.ModalCreateMsg:
		if len(msg.RepoNames) > 0 {
			// The modal can be opened while other operations run — reject
			// creation if any selected repo is still busy
			for _, name := range msg.RepoNames {
				if op, ok := m.busy[name]; ok {
					m.statusMsg = name + " is busy (" + op + ") — try again when it finishes"
					m.recomputeLayout()
					return m, clearStatusCmd()
				}
			}
			logFn, startCmd := m.beginOp("Creating "+msg.Branch+"...", msg.RepoNames...)
			return m, tea.Batch(startCmd, createWorktreeCmd(logFn, msg.RepoNames, msg.Branch, msg.InstallDeps, &m.config))
		}
		return m, nil

	case ui.ModalCancelMsg:
		return m, nil
	}

	// Forward to modal if active
	if m.modal.Active {
		var cmd tea.Cmd
		m.modal, cmd = m.modal.Update(msg)
		return m, cmd
	}
	if m.cloneUI.Active {
		var cmd tea.Cmd
		m.cloneUI, cmd = m.cloneUI.Update(msg)
		return m, cmd
	}

	return m, nil
}

// modalMetrics computes the screen position of modal content.
// modalRendered is the styled modal box (before lipgloss.Place), screenH is terminal height.
// Returns: contentStartY (first content line), modalTop, modalH.
func modalMetrics(modalRendered string, screenH int) (contentStartY, modalTop, modalH int) {
	modalH = lipgloss.Height(modalRendered)
	modalTop = (screenH - modalH) / 2
	contentStartY = modalTop + 2 // border(1) + padding(1)
	return
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.themeStudio.Active {
		return handleThemeStudioMouse(m, msg)
	}

	if m.settings.Active {
		var cmd tea.Cmd
		m.settings, cmd = m.settings.Update(msg)
		m.syncSettingsConfig()
		return m, cmd
	}

	// Clone browser: hover/click repo rows, wheel scrolls
	if m.cloneUI.Active {
		modal := m.cloneUI.View()
		contentStartY, _, _ := modalMetrics(modal, m.height)
		listStartY := contentStartY + m.cloneUI.CloneListContentOffset()
		row := msg.Y - listStartY + m.cloneUI.Scroll
		inList := msg.Y >= listStartY && row >= 0 && row < len(m.cloneUI.Filtered) &&
			row < m.cloneUI.Scroll+ui.CloneListMaxVisible

		switch {
		case msg.Button == tea.MouseButtonWheelUp:
			if m.cloneUI.Scroll > 0 {
				m.cloneUI.Scroll--
			}
		case msg.Button == tea.MouseButtonWheelDown:
			if m.cloneUI.Scroll < len(m.cloneUI.Filtered)-1 {
				m.cloneUI.Scroll++
			}
		case msg.Action == tea.MouseActionMotion:
			m.cloneUI.Hovered = -1
			if inList {
				m.cloneUI.Hovered = row
				m.cloneUI.Cursor = row
			}
		case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && inList:
			m.cloneUI.Cursor = row
			var cmd tea.Cmd
			m.cloneUI, cmd = m.cloneUI.Update(tea.KeyMsg{Type: tea.KeyEnter})
			return m, cmd
		}
		return m, nil
	}

	// Context menu: hover to highlight, click to execute, click elsewhere to close
	if m.contextMenu.Active {
		menuRendered := ui.RenderContextMenu(m.contextMenu, m.width, m.height)
		contentStartY, _, _ := modalMetrics(menuRendered, m.height)
		itemStartY := contentStartY + 2 // skip title + empty line

		hoveredItem := msg.Y - itemStartY
		if hoveredItem >= 0 && hoveredItem < len(m.contextMenu.Items) {
			m.contextMenu.CursorIdx = hoveredItem
		}

		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if hoveredItem >= 0 && hoveredItem < len(m.contextMenu.Items) {
				item := m.contextMenu.Items[hoveredItem]
				m.contextMenu.Active = false
				return executeContextAction(m, item.Action)
			}
			m.contextMenu.Active = false
		}
		return m, nil
	}

	if m.cleanupConfirmActive {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			modal := m.renderCleanupConfirmDialog()
			_, modalTop, modalH := modalMetrics(modal, m.height)
			ynY := modalTop + modalH - 3

			if msg.Y == ynY {
				modalLeft := (m.width - lipgloss.Width(modal)) / 2
				relX := msg.X - modalLeft
				if relX >= 0 && relX < 20 {
					return handleCleanupConfirmKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
				} else {
					return handleCleanupConfirmKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
				}
			}
			// [d] toggle is 2 lines above Y/N (toggle + empty + Y/N)
			if msg.Y == ynY-2 {
				return handleCleanupConfirmKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
			}
		}
		return m, nil
	}

	if m.deleteConfirmActive {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			modal := m.renderDeleteConfirmDialog()
			_, modalTop, modalH := modalMetrics(modal, m.height)
			ynY := modalTop + modalH - 3

			if !m.deleteDangerous && msg.Y == ynY {
				modalLeft := (m.width - lipgloss.Width(modal)) / 2
				relX := msg.X - modalLeft
				if relX >= 0 && relX < 20 {
					return confirmDelete(m)
				} else {
					return cancelDelete(m)
				}
			}
			// Toggle lines are positioned above Y/N (or above "Type DELETE" in dangerous mode).
			// Layout from bottom: blank + toggles. Local branch toggle is always present.
			// Remote toggle appears only when remote exists and local branch is being deleted.
			hasRemote := deleteHasRemote(m.deleteWT.Status) && m.deleteLocalBranch
			// In non-dangerous mode: ynY-1 = blank, ynY-2 = last toggle
			// In dangerous mode: ynY = hint line, ynY-2 = input, ynY-4 = "Type DELETE"
			//   so toggles are further up
			// Protected branches show a note instead of toggles — nothing to click.
			if !m.deleteDangerous && !m.deleteProtected {
				if hasRemote {
					// ynY-2 = remote toggle, ynY-3 = local toggle
					if msg.Y == ynY-2 {
						return handleDeleteConfirmKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
					}
					if msg.Y == ynY-3 {
						return handleDeleteConfirmKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
					}
				} else {
					// ynY-2 = local toggle only
					if msg.Y == ynY-2 {
						return handleDeleteConfirmKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
					}
				}
			}
		}
		return m, nil
	}

	if m.hibernateActive {
		return m, nil
	}

	if m.modal.Active {
		modal := m.modal.View(m.width, m.height)
		contentStartY, _, _ := modalMetrics(modal, m.height)

		switch m.modal.Step {
		case ui.ModalSelectRepos:
			repoStartY := contentStartY + 4 // title(1) + empty(1) + description(1) + empty(1)
			hoveredRepo := msg.Y - repoStartY
			if hoveredRepo >= 0 && hoveredRepo < len(m.modal.Repos) {
				m.modal.CursorIdx = hoveredRepo
			}

			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				clickedRepo := msg.Y - repoStartY
				if clickedRepo >= 0 && clickedRepo < len(m.modal.Repos) {
					m.modal.CursorIdx = clickedRepo
					if m.modal.Selected[clickedRepo] {
						delete(m.modal.Selected, clickedRepo)
					} else {
						m.modal.Selected[clickedRepo] = true
					}
				}
			}

		case ui.ModalEnterBranch:
			// Branch list mouse handling
			branchStartY := contentStartY + m.modal.BranchListContentOffset()
			visible := ui.BranchListMaxVisible
			total := len(m.modal.FilteredBranches)
			if total < visible {
				visible = total
			}
			maxScroll := total - visible
			if maxScroll < 0 {
				maxScroll = 0
			}

			modalW := lipgloss.Width(modal)
			modalLeft := (m.width - modalW) / 2
			contentLeft := modalLeft + 3 // border(1) + padding(2)

			hasScrollbar := total > visible
			// List width matches render: fullW(54) - 3(gap+scroll+pad) = 51, or 54 if no scrollbar
			listW := 54
			if hasScrollbar {
				listW = 54 - 3
			}
			listRight := contentLeft + listW - 1
			scrollbarX := contentLeft + listW + 1 // after the 1-char space gap

			rowInList := msg.Y - branchStartY
			onScrollbar := hasScrollbar && msg.X >= scrollbarX && msg.X <= scrollbarX+1
			inList := msg.X >= contentLeft && msg.X <= listRight && rowInList >= 0 && rowInList < visible

			// Scrollbar interaction
			m.modal.ScrollbarHovered = false
			if onScrollbar && rowInList >= 0 && rowInList < visible {
				isDrag := msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
				isHeldMotion := msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft
				isDragging := (isDrag || isHeldMotion) && maxScroll > 0

				// Show thick thumb when hovering the thumb OR actively dragging
				if isDragging {
					m.modal.ScrollbarHovered = true
				} else {
					thumbLen := visible * visible / total
					if thumbLen < 1 {
						thumbLen = 1
					}
					if thumbLen > visible {
						thumbLen = visible
					}
					thumbStart := 0
					trackSpace := visible - thumbLen
					if maxScroll > 0 && trackSpace > 0 {
						thumbStart = m.modal.BranchScroll * trackSpace / maxScroll
					}
					if rowInList >= thumbStart && rowInList < thumbStart+thumbLen {
						m.modal.ScrollbarHovered = true
					}
				}

				if isDragging {
					if visible > 1 {
						m.modal.BranchScroll = rowInList * maxScroll / (visible - 1)
					}
					if m.modal.BranchScroll > maxScroll {
						m.modal.BranchScroll = maxScroll
					}
				}
			} else if inList {
				// Hover: highlight branch items
				m.modal.BranchHovered = m.modal.BranchScroll + rowInList

				// Click: fill input with branch name
				if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
					idx := m.modal.BranchScroll + rowInList
					if idx < total {
						m.modal.Input.SetValue(m.modal.FilteredBranches[idx].Name)
						m.modal.Input.CursorEnd()
						m.modal.FilterBranches()
					}
				}
			} else {
				m.modal.BranchHovered = -1
			}

			// Mouse wheel: scroll branch list
			if msg.Action == tea.MouseActionPress {
				if msg.Button == tea.MouseButtonWheelUp && m.modal.BranchScroll > 0 {
					m.modal.BranchScroll--
				}
				if msg.Button == tea.MouseButtonWheelDown && m.modal.BranchScroll < maxScroll {
					m.modal.BranchScroll++
				}
			}

		case ui.ModalConfirm:
			// Hover/click on plan rows toggles the per-repo install checkbox
			if m.modal.HasInstallToggles() {
				row := msg.Y - (contentStartY + m.modal.ConfirmRowsOffset())
				if row >= 0 && row < len(m.modal.PlanWTNames) {
					m.modal.ConfirmCursor = row
					if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
						m.modal.ToggleInstall(row)
					}
					return m, nil
				}
			}
			// Click anywhere else confirms creation
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				var cmd tea.Cmd
				m.modal, cmd = m.modal.Update(tea.KeyMsg{Type: tea.KeyEnter})
				return m, cmd
			}
		}
		return m, nil
	}

	if m.renameActive {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if renameHasRemote(m) {
				modal := m.renderRenameDialog()
				_, modalTop, modalH := modalMetrics(modal, m.height)
				toggleY := modalTop + modalH - 3 - 2 // 2 lines above bottom hint
				if msg.Y == toggleY {
					m.renameRemoteBranch = !m.renameRemoteBranch
					return m, nil
				}
			}
		}
		return m, nil
	}

	// Background-raised prompts: below every user-opened dialog in all
	// three routing layers — they take the screen only once it's free.
	if m.openPromptActive {
		modal := m.renderOpenPromptDialog()
		_, modalTop, modalH := modalMetrics(modal, m.height)
		modalLeft := (m.width - lipgloss.Width(modal)) / 2
		optionsY := modalTop + modalH - 5 // options line: above blank + hint line
		m.openPromptHovered = -1
		if msg.Y == optionsY {
			relX := msg.X - modalLeft
			for _, opt := range m.openPromptOptions() {
				if relX >= opt.x && relX < opt.x+opt.w {
					m.openPromptHovered = opt.usage
					if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
						m.openPromptSelection = opt.usage
						return openCreatedWorkspaces(m, opt.usage)
					}
					break
				}
			}
		}
		return m, nil
	}

	if m.envRestoreActive {
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			modal := m.renderEnvRestoreDialog()
			_, modalTop, modalH := modalMetrics(modal, m.height)
			ynY := modalTop + modalH - 3
			if msg.Y == ynY {
				modalLeft := (m.width - lipgloss.Width(modal)) / 2
				relX := msg.X - modalLeft
				if relX >= 0 && relX < 20 {
					return handleEnvRestoreKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
				}
				return handleEnvRestoreKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
			}
		}
		return m, nil
	}

	// Mouse wheel: scroll grid area or log panel
	if msg.Action == tea.MouseActionPress {
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			// Determine which area the cursor is in
			screenFooterY := m.headerH + m.contentHeight - m.scrollY
			inLogArea := m.logPanel.Visible && msg.Y > screenFooterY+1

			if inLogArea {
				if msg.Button == tea.MouseButtonWheelUp {
					m.logPanel.ScrollUp(1)
				} else {
					m.logPanel.ScrollDown(1)
				}
			} else if m.explorerActive() {
				// Scroll the pane under the cursor. Clamp against content —
				// the render clamps a copy, so unbounded state would make
				// scrolling back take as long as the over-scroll.
				delta := 2
				if msg.Button == tea.MouseButtonWheelUp {
					delta = -2
				}
				if msg.X < ui.MarginH+ui.ExplorerSidebarWidth(m.width) {
					m.explorer.SidebarScroll += delta
					if maxS := len(m.repos) - 1; m.explorer.SidebarScroll > maxS {
						m.explorer.SidebarScroll = maxS
					}
					if m.explorer.SidebarScroll < 0 {
						m.explorer.SidebarScroll = 0
					}
				} else {
					m.explorer.SheetScroll += delta
					wts := 0
					if m.explorer.SelectedRepo < len(m.repos) {
						wts = len(m.repos[m.explorer.SelectedRepo].Worktrees)
					}
					if maxS := wts - 1; m.explorer.SheetScroll > maxS {
						m.explorer.SheetScroll = maxS
					}
					if m.explorer.SheetScroll < 0 {
						m.explorer.SheetScroll = 0
					}
				}
				m.recomputeLayout()
			} else {
				// Scroll main content
				if msg.Button == tea.MouseButtonWheelUp {
					m.scrollY -= 2
				} else {
					m.scrollY += 2
				}
				m.clampScroll()
				m.recomputeLayout()
			}
			return m, nil
		}
	}

	x, y := msg.X, msg.Y
	// Translate screen Y to virtual Y for hit testing (account for scroll)
	virtualY := y + m.scrollY

	switch msg.Action {
	case tea.MouseActionMotion:
		m.hoveredBtn = ui.BtnNone
		wasVersionHovered := m.versionHovered
		prevHoveredUsage := m.hoveredUsage
		m.versionHovered = false
		m.hoveredUsage = -1
		wasUpdateBadgeHovered := m.updateBadgeHovered
		m.updateBadgeHovered = false

		// Check version label hover (fixed screen position in header)
		vx, vy, vw := ui.VersionHitZone()
		if y == vy && x >= vx && x < vx+vw {
			m.versionHovered = true
			m.recomputeLayout()
			return m, nil
		}

		// Check update badge hover
		if m.updateAvailVersion != "" {
			bx, by, bw := ui.UpdateBadgeHitZone()
			if y == by && x >= bx && x < bx+bw {
				m.updateBadgeHovered = true
			}
		}
		if wasUpdateBadgeHovered != m.updateBadgeHovered {
			m.recomputeLayout()
		}

		// Check click usage toggle hover (fixed screen position in header)
		usageY, usageZones := ui.ClickUsageHitZones(m.width, m.usageLabels(), m.updateAvailVersion)
		if y == usageY {
			for _, z := range usageZones {
				if x >= z.X && x < z.X+z.W {
					m.hoveredUsage = z.Usage
					break
				}
			}
		}

		// GitHub login hover (line below Status)
		prevGhHover := m.ghUserHover
		m.ghUserHover = false
		if m.ghUser != "" {
			gx, gy, gw := ui.GhUserHitZone(m.width, m.usageLabels(), m.ghUser, m.updateAvailVersion)
			if y == gy && x >= gx && x < gx+gw {
				m.ghUserHover = true
			}
		}
		if prevGhHover != m.ghUserHover {
			m.recomputeLayout()
		}

		// Check view toggle hover (line below click usage)
		prevHoveredView := m.hoveredView
		m.hoveredView = -1
		viewY, viewZones := ui.ViewToggleHitZones(m.width, m.usageLabels(), m.updateAvailVersion)
		if y == viewY {
			for _, z := range viewZones {
				if x >= z.X && x < z.X+z.W {
					m.hoveredView = int(z.Usage)
					break
				}
			}
		}

		// Recompute header if any header hover state changed
		if wasVersionHovered != m.versionHovered || prevHoveredUsage != m.hoveredUsage || prevHoveredView != m.hoveredView {
			m.recomputeLayout()
		}

		// Check footer buttons (footer is at fixed screen position, 1 line tall)
		screenFooterY := m.headerH + m.contentHeight - m.scrollY
		if y == screenFooterY && screenFooterY > 0 {
			m.focusedCard = -1
			m.focusedWT = -1
			var btn ui.HoverButton
			if len(m.repos) > 0 {
				btn = ui.GetFooterButtonAtX(x, m.width)
			} else {
				btn = ui.GetFooterMinimalButtonAtX(x, m.width)
			}
			// Suppress hover on operation buttons during loading
			if m.anyBusy() && ui.IsOperationBtn(btn) {
				m.hoveredBtn = ui.BtnNone
			} else {
				m.hoveredBtn = btn
			}
			return m, nil
		}

		// Explorer layout: hover previews selection and lights up actions
		if m.explorerActive() {
			if z, ok := explorerZoneAt(m.gridResult.HitZones, x, y); ok {
				switch z.Type {
				case ui.ZoneExplorerRepo:
					if m.explorer.SelectedRepo != z.RepoIdx {
						m.explorer.SelectedRepo = z.RepoIdx
						m.explorer.SelectedWT = 0
						m.explorer.SheetScroll = 0
					}
					m.explorer.FocusSheet = false
				case ui.ZoneExplorerRow:
					m.explorer.FocusSheet = true
					m.explorer.SelectedWT = z.WTIdx
				case ui.ZoneExplorerAction, ui.ZoneExplorerNew, ui.ZoneClone:
					m.hoveredBtn = z.Button
				}
			}
			return m, nil
		}

		// Board clone tile hover
		for _, z := range m.gridResult.HitZones {
			if z.Type == ui.ZoneClone && x >= z.X && x < z.X+z.W && virtualY >= z.Y && virtualY < z.Y+z.H {
				m.hoveredBtn = ui.BtnClone
			}
		}

		// Hit test grid (using virtual Y)
		repoIdx, wtIdx, btn := ui.HitTest(m.gridResult.HitZones, x, virtualY)
		m.focusedCard = repoIdx
		m.focusedWT = wtIdx
		if btn != ui.BtnNone && !(m.anyBusy() && ui.IsOperationBtn(btn)) {
			m.hoveredBtn = btn
		}

		// History suggestion hover (empty state) — no hover while loading;
		// the click is blocked then too (relaunch would kill the operation)
		prevHistory := m.hoveredHistory
		m.hoveredHistory = -1
		if !m.anyBusy() {
			m.hoveredHistory = ui.HistoryHitTest(m.gridResult.HitZones, x, virtualY)
		}
		if prevHistory != m.hoveredHistory {
			m.recomputeLayout()
		}

		// Detect inline buttons when hovering repo header or worktree
		// (suppressed for busy repos — every inline action is a mutation).
		// Skip for migration cards — they don't have inline context buttons
		isMigrationCard := repoIdx >= 0 && repoIdx < len(m.repos) && m.repos[repoIdx].NeedsMigration
		if !isMigrationCard && repoIdx >= 0 && repoIdx < len(m.repos) && !m.repoBusy(m.repos[repoIdx]) && (wtIdx == -2 || wtIdx >= 0) && m.gridResult.CardWidth > 0 {
			cardX := m.getCardScreenX(repoIdx)
			inlineBtn := ui.DetectInlineButton(x, cardX, m.gridResult.CardWidth, wtIdx)
			if inlineBtn != ui.BtnNone {
				m.hoveredBtn = inlineBtn
			}
		}

		// Check create button hover (virtual Y + X bounds, only when repos exist)
		if len(m.repos) > 0 && virtualY >= m.createBtnY && virtualY < m.createBtnY+3 && m.createBtnY > 0 {
			cbX, cbW := ui.CreateBtnHitZone(m.width)
			if x >= cbX && x < cbX+cbW {
				m.hoveredBtn = ui.BtnCreateWT
			}
		}

		return m, nil

	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}

		// Hit-test at click position (using virtual Y)
		repoIdx, wtIdx, btn := ui.HitTest(m.gridResult.HitZones, x, virtualY)
		m.focusedCard = repoIdx
		m.focusedWT = wtIdx
		if btn != ui.BtnNone && !(m.anyBusy() && ui.IsOperationBtn(btn)) {
			m.hoveredBtn = btn
		}

		// Detect inline buttons on click (suppress during loading)
		// Skip for migration cards — they don't have inline context buttons
		isMigrationCard := repoIdx >= 0 && repoIdx < len(m.repos) && m.repos[repoIdx].NeedsMigration
		if !isMigrationCard && repoIdx >= 0 && repoIdx < len(m.repos) && !m.repoBusy(m.repos[repoIdx]) && (wtIdx == -2 || wtIdx >= 0) && m.gridResult.CardWidth > 0 {
			cardX := m.getCardScreenX(repoIdx)
			inlineBtn := ui.DetectInlineButton(x, cardX, m.gridResult.CardWidth, wtIdx)
			if inlineBtn != ui.BtnNone {
				m.hoveredBtn = inlineBtn
			}
		}

		// Check footer at click position (fixed screen position, 1 line tall)
		screenFooterY := m.headerH + m.contentHeight - m.scrollY
		if y == screenFooterY && screenFooterY > 0 {
			var btn ui.HoverButton
			if len(m.repos) > 0 {
				btn = ui.GetFooterButtonAtX(x, m.width)
			} else {
				btn = ui.GetFooterMinimalButtonAtX(x, m.width)
			}
			if !(m.anyBusy() && ui.IsOperationBtn(btn)) {
				m.hoveredBtn = btn
			}
		}

		// Check create button (virtual Y + X bounds; suppress during loading)
		if virtualY >= m.createBtnY && virtualY < m.createBtnY+3 && m.createBtnY > 0 {
			cbX, cbW := ui.CreateBtnHitZone(m.width)
			if x >= cbX && x < cbX+cbW {
				m.hoveredBtn = ui.BtnCreateWT
			}
		}

		// Check version label click (fixed screen position in header)
		vx, vy, vw := ui.VersionHitZone()
		if y == vy && x >= vx && x < vx+vw {
			_ = opener.OpenURL(ui.ReleaseURL())
			m.statusMsg = "Opened release page"
			return m, clearStatusCmd()
		}

		// Check "(Update Available)" badge click — triggers update
		if m.updateAvailVersion != "" {
			bx, by, bw := ui.UpdateBadgeHitZone()
			if y == by && x >= bx && x < bx+bw {
				m.statusMsg = "Updating..."
				m.updateAvailVersion = ""
				m.updateBadgeHovered = false
				m.recomputeLayout()
				return m, doUpdateCmd()
			}
		}

		// Check click usage toggle click (fixed screen position in header)
		usageY, usageZones := ui.ClickUsageHitZones(m.width, m.usageLabels(), m.updateAvailVersion)
		if y == usageY {
			for _, z := range usageZones {
				if x >= z.X && x < z.X+z.W && z.Usage != m.clickUsage {
					m.setClickUsage(z.Usage)
					m.statusMsg = fmt.Sprintf("Click usage: %s", m.clickUsage)
					m.recomputeLayout()
					return m, clearStatusCmd()
				}
			}
		}

		// GitHub login click → open the profile
		if m.ghUser != "" {
			gx, gy, gw := ui.GhUserHitZone(m.width, m.usageLabels(), m.ghUser, m.updateAvailVersion)
			if y == gy && x >= gx && x < gx+gw {
				_ = opener.OpenURL("https://github.com/" + m.ghUser)
				m.statusMsg = "Opened GitHub profile"
				return m, clearStatusCmd()
			}
		}

		// View toggle click (line below click usage)
		viewY, viewZones := ui.ViewToggleHitZones(m.width, m.usageLabels(), m.updateAvailVersion)
		if y == viewY {
			for _, z := range viewZones {
				if x >= z.X && x < z.X+z.W {
					want := "board"
					if int(z.Usage) == 1 {
						want = "explorer"
					}
					if want != m.config.Global.Layout {
						m.toggleLayout()
						m.recomputeLayout()
						return m, clearStatusCmd()
					}
				}
			}
		}

		// History suggestion click (empty state) — blocked during operations:
		// relaunching replaces the process and would kill running ops
		if !m.anyBusy() {
			histIdx := ui.HistoryHitTest(m.gridResult.HitZones, x, virtualY)
			if histIdx >= 0 {
				suggestions := config.GetHistorySuggestions(m.config.WorkDir)
				if histIdx < len(suggestions) {
					m.RelaunchDir = suggestions[histIdx].Path
					return m, tea.Quit
				}
			}
		}

		// Settings and Exit stay available during operations
		if m.hoveredBtn == ui.BtnSettings {
			var repoNames []string
			for _, r := range m.repos {
				if !r.IsMonorepo {
					repoNames = append(repoNames, r.Name)
				}
			}
			m.settings = ui.NewSettings(&m.config, repoNames)
			m.settings.ViewHeight = m.height
			m.settings.ViewWidth = m.width
			return m, nil
		}
		if m.hoveredBtn == ui.BtnExit {
			return m, tea.Quit
		}

		// Refresh-all and cleanup touch every repo — they need all locks free
		if !m.anyBusy() {
			if m.hoveredBtn == ui.BtnRefreshAll && len(m.repos) > 0 {
				lock := m.allRepoNames()
				logFn, startCmd := m.beginOp("Refreshing all repos...", lock...)
				return m, tea.Batch(startCmd, refreshAllCmd(logFn, &m.config, lock))
			}
			if m.hoveredBtn == ui.BtnCleanupMerged && len(m.repos) > 0 {
				m.cleanupConfirmActive = true
				m.cleanupRemoteBranch = false
				m.statusMsg = "Cleanup merged worktrees? [Y]es / [N]o"
				return m, nil
			}
		}

		// Explorer layout: clicks act on sidebar/sheet zones; board-specific
		// targets below (create button, migrate, cards) don't exist here
		if m.explorerActive() {
			if z, ok := explorerZoneAt(m.gridResult.HitZones, x, y); ok {
				switch z.Type {
				case ui.ZoneExplorerRepo:
					m.explorer.SelectedRepo = z.RepoIdx
					m.explorer.SelectedWT = 0
					m.explorer.SheetScroll = 0
					m.explorer.FocusSheet = false
					return m, nil
				case ui.ZoneExplorerRow:
					m.explorer.FocusSheet = true
					m.explorer.SelectedWT = z.WTIdx
					return explorerAction(m, ui.BtnOpen)
				case ui.ZoneExplorerAction:
					m.explorer.FocusSheet = true
					m.explorer.SelectedWT = z.WTIdx
					return explorerAction(m, z.Button)
				case ui.ZoneExplorerNew:
					return m.openCreateModalFor(z.RepoIdx)
				case ui.ZoneClone:
					return m.openCloneBrowser()
				}
			}
			return m, nil
		}

		// Create button (only when repos exist) — the modal opens anytime;
		// busy conflicts are checked when creation is confirmed
		if m.hoveredBtn == ui.BtnCreateWT && len(m.repos) > 0 {
			m.modal = ui.NewModal(m.creatableRepos(), m.config.WorkDir, m.resolvePM, m.config.Global.InstallOnCreate)
			return m, textinput.Blink
		}

		// Migrate button — needs its own repo free
		if m.hoveredBtn == ui.BtnMigrate && m.focusedCard >= 0 && m.focusedCard < len(m.repos) {
			repo := m.repos[m.focusedCard]
			if repo.NeedsMigration && repo.Path != "" && !m.repoBusy(repo) {
				lock := lockSet(repo)
				logFn, startCmd := m.beginOp("Migrating "+repo.Name+"...", lock...)
				return m, tea.Batch(startCmd, migrateCmd(logFn, repo.Path, &m.config, lock))
			}
		}

		// Clone tile / empty-state entry click (Board): scan the zone
		// directly — don't depend on a preceding motion event
		for _, z := range m.gridResult.HitZones {
			if z.Type == ui.ZoneClone && x >= z.X && x < z.X+z.W && virtualY >= z.Y && virtualY < z.Y+z.H {
				return m.openCloneBrowser()
			}
		}

		// Context menu trigger [▸] — open context menu (not for migration cards).
		// The target repo/worktree is snapshotted here; actions never resolve
		// indices later (the repo list may reload while the menu is open).
		// Blocked for busy repos — every menu action is a mutation.
		if m.hoveredBtn == ui.BtnContextMenu && m.focusedCard >= 0 && m.focusedCard < len(m.repos) &&
			!isMigrationCard && !m.repoBusy(m.repos[m.focusedCard]) {
			repo := m.repos[m.focusedCard]
			if m.focusedWT == -2 {
				// Repo header context menu
				m.contextMenu = ui.ContextMenuModel{
					Active: true,
					Items:  ui.RepoContextItems(repo.IsMonorepo, m.canHibernate(repo), m.canHideRepo(repo), repo.Hidden),
					X:      x, Y: y,
				}
				m.menuRepo = repo
				m.menuWT = git.Worktree{}
				m.menuHasWT = false
			} else if m.focusedWT >= 0 && m.focusedWT < len(repo.Worktrees) {
				// Worktree context menu
				wt := repo.Worktrees[m.focusedWT]
				m.contextMenu = ui.ContextMenuModel{
					Active: true,
					Items:  ui.WorktreeContextItems(repo.IsMonorepo, m.tmuxLive[opener.SessionName(wt.Path)], m.prAble(repo, wt)),
					X:      x, Y: y,
				}
				m.menuRepo = repo
				m.menuWT = repo.Worktrees[m.focusedWT]
				m.menuHasWT = true
			}
			return m, nil
		}

		// Click on repo header (not on button) — open main repo
		if m.focusedCard >= 0 && m.focusedWT == -2 && m.hoveredBtn == ui.BtnNone {
			repo := m.repos[m.focusedCard]
			if repo.Path != "" {
				err := opener.OpenRepo(repo.Path, m.clickUsage, m.openerOpts())
				if err != nil {
					m.statusMsg = fmt.Sprintf("Failed to open: %s", err.Error())
				} else {
					m.statusMsg = fmt.Sprintf("Opened %s in %s%s", repo.Name, m.clickUsage, m.tmuxFallbackNote(m.clickUsage))
					m.markSessionLive(repo.Path, m.clickUsage)
				}
				return m, clearStatusCmd()
			}
		}

		// Click on worktree (not on button) — open it
		if m.focusedCard >= 0 && m.focusedWT >= 0 && m.hoveredBtn == ui.BtnNone {
			repo := m.repos[m.focusedCard]
			if m.focusedWT < len(repo.Worktrees) {
				wt := repo.Worktrees[m.focusedWT]
				err := opener.OpenWorktree(wt.Path, m.clickUsage, m.openerOpts())
				if err != nil {
					m.statusMsg = fmt.Sprintf("Failed to open: %s", err.Error())
				} else {
					m.statusMsg = fmt.Sprintf("Opened %s in %s%s", wt.Branch, m.clickUsage, m.tmuxFallbackNote(m.clickUsage))
					m.markSessionLive(wt.Path, m.clickUsage)
				}
				return m, clearStatusCmd()
			}
		}
	}

	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Minimum terminal size check
	minWidth, minHeight := 60, 20
	if m.width < minWidth || m.height < minHeight {
		msg := ui.RenderResizePrompt(m.width, m.height, minWidth, minHeight)
		return paintBlack(msg, m.width, m.height)
	}

	// Show tree growth animation during initial load
	if m.initialLoad {
		header := ui.RenderHeader(m.width, m.clickUsage, m.usageLabels())
		loader := ui.RenderLoader(m.width, m.loaderFrame, "Discovering repositories")
		content := header + "\n" + loader
		return paintBlack(content, m.width, m.height)
	}

	var sections []string

	// Header (pre-computed, includes status line) — fixed at top
	sections = append(sections, m.headerView)

	// --- Scrollable content area ---
	// Explorer shows status text inline and creates via [+ new], so the
	// legend and the big create button are Board-only
	hasRepos := len(m.repos) > 0
	var scrollable []string
	scrollable = append(scrollable, m.gridResult.View)
	if hasRepos && !m.explorerActive() {
		scrollable = append(scrollable, ui.RenderStatusLegend(m.width, m.config.Global.Multiplexer == "tmux"))
		scrollable = append(scrollable, ui.RenderCreateButton(m.width, m.hoveredBtn == ui.BtnCreateWT, false))
	}
	scrollContent := strings.Join(scrollable, "\n")

	// Apply vertical scroll: slice visible lines from the scrollable content
	scrollLines := strings.Split(scrollContent, "\n")
	viewportH := m.height - m.headerH - 2 // reserve: footer(1) + buffer(1)
	if viewportH < 1 {
		viewportH = 1
	}

	// Check if scroll indicator will be needed (reserve 1 line for it)
	needsIndicator := len(scrollLines) > viewportH
	if needsIndicator && viewportH > 2 {
		viewportH-- // reserve line for scroll indicator
	}

	startLine := m.scrollY
	if startLine > len(scrollLines) {
		startLine = len(scrollLines)
	}
	endLine := startLine + viewportH
	if endLine > len(scrollLines) {
		endLine = len(scrollLines)
	}
	visibleLines := scrollLines[startLine:endLine]

	// Scroll indicator
	canScrollUp := m.scrollY > 0
	canScrollDown := endLine < len(scrollLines)
	if canScrollUp || canScrollDown {
		indicator := ui.RenderScrollIndicator(m.width, canScrollUp, canScrollDown)
		visibleLines = append(visibleLines, indicator)
	}

	sections = append(sections, strings.Join(visibleLines, "\n"))

	// Footer — fixed at bottom (minimal when no repos)
	if hasRepos {
		sections = append(sections, ui.RenderFooter(m.width, m.hoveredBtn, m.anyBusy()))
	} else {
		sections = append(sections, ui.RenderFooterMinimal(m.width, m.hoveredBtn))
	}

	// Log panel (fills remaining space below footer)
	if m.logPanel.Visible && len(m.logPanel.Entries) > 0 {
		footerScreenY := m.headerH + len(visibleLines) + 1
		availHeight := m.height - footerScreenY - 2
		if availHeight > 3 {
			logView := ui.RenderLogPanel(m.logPanel, m.width, availHeight)
			if logView != "" {
				sections = append(sections, logView)
			}
		}
	}

	content := strings.Join(sections, "\n")

	// --- Overlay dialogs (rendered on top of content) ---
	placeDialog := func(modal string) string {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
	}

	// Context menu
	if m.contextMenu.Active {
		dialog := ui.RenderContextMenuPlaced(m.contextMenu, m.width, m.height)
		return paintBlack(dialog, m.width, m.height)
	}

	// Rename dialog
	if m.renameActive {
		return paintBlack(placeDialog(m.renderRenameDialog()), m.width, m.height)
	}

	// Cleanup confirmation
	if m.cleanupConfirmActive {
		return paintBlack(placeDialog(m.renderCleanupConfirmDialog()), m.width, m.height)
	}

	// Delete confirmation
	if m.deleteConfirmActive {
		return paintBlack(placeDialog(m.renderDeleteConfirmDialog()), m.width, m.height)
	}

	// Hibernate audit/confirm
	if m.hibernateActive {
		return paintBlack(placeDialog(m.renderHibernateDialog()), m.width, m.height)
	}

	// Theme Studio — full screen, renders in whichever theme the cursor
	// is trying on (the screen is its own preview)
	if m.themeStudio.Active {
		return paintBlack(ui.RenderThemeStudio(m.themeStudio, m.width, m.height), m.width, m.height)
	}

	// Settings
	if m.settings.Active {
		dialog := m.settings.View(m.width, m.height)
		return paintBlack(dialog, m.width, m.height)
	}

	// Clone browser
	if m.cloneUI.Active {
		return paintBlack(placeDialog(m.cloneUI.View()), m.width, m.height)
	}

	// Create worktree modal
	if m.modal.Active {
		dialog := m.modal.ViewPlaced(m.width, m.height)
		return paintBlack(dialog, m.width, m.height)
	}

	// Background-raised prompts render below every user-opened dialog —
	// same rank they hold in the key and mouse layers.
	if m.openPromptActive {
		return paintBlack(placeDialog(m.renderOpenPromptDialog()), m.width, m.height)
	}
	if m.envRestoreActive {
		return paintBlack(placeDialog(m.renderEnvRestoreDialog()), m.width, m.height)
	}

	return paintBlack(content, m.width, m.height)
}

func (m Model) renderRenameDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)

	title := "Rename Branch"
	context := ""
	isBasisChange := m.renameIsBasis
	if isBasisChange {
		title = "Change Basis Branch"
		context = "Repository: " + m.renameRepo.Name
	} else if m.renameWT.Path != "" {
		context = "Current: " + m.renameWT.Branch
		if m.renameRepo.IsMonorepo {
			title = "Rename Branch (all " + fmt.Sprintf("%d", len(m.renameRepo.RepoNames)) + " repos)"
		}
	}

	content := titleStyle.Render(title) + "\n\n"
	if context != "" {
		content += dimStyle.Render(context) + "\n\n"
	}
	content += m.renameInput.View() + "\n"

	// Show remote branch toggle for actual renames (not basis branch changes)
	if !isBasisChange && renameHasRemote(m) {
		content += "\n"
		if m.renameRemoteBranch {
			content += whiteStyle.Render("  [ctrl+d] ✓ Rename remote branch") + "\n"
		} else {
			content += dimStyle.Render("  [ctrl+d] Rename remote branch") + "\n"
		}
	}

	content += "\n"
	content += dimStyle.Render("enter confirm • esc cancel")

	return ui.ModalStyle.Width(50).Render(content)
}

// openPromptOption is one open-mode choice with its rendered X range relative
// to the modal's left edge — shared by the dialog renderer and mouse hit-testing.
type openPromptOption struct {
	usage opener.ClickUsage
	label string
	x     int
	w     int
}

func (m Model) openPromptOptions() []openPromptOption {
	labels := []struct {
		usage opener.ClickUsage
		label string
	}{
		{opener.ClickIDE, m.config.IDELabel()},
		{opener.ClickAICli, m.config.AICliLabel()},
		{opener.ClickTerminal, m.config.TerminalLabel()},
	}
	opts := make([]openPromptOption, 0, len(labels))
	x := 3 + 2 // border(1) + padding(2), then line indent
	for _, l := range labels {
		w := lipgloss.Width(l.label) + 2 // Padding(0,1) on the option styles
		opts = append(opts, openPromptOption{l.usage, l.label, x, w})
		x += w + 1 // "│" separator
	}
	return opts
}

func (m Model) renderOpenPromptDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)
	hoveredStyle := lipgloss.NewStyle().
		Foreground(ui.ColorWhite).
		Background(ui.ColorBlack).
		Bold(true).
		Underline(true).
		Padding(0, 1)

	content := titleStyle.Render("Worktree Created") + "\n\n"
	for _, r := range m.openPromptResults {
		content += whiteStyle.Render("  "+r.RepoName) + dimStyle.Render(" → "+r.Branch) + "\n"
	}
	content += "\n" + dimStyle.Render("How would you like to open this workspace?") + "\n\n"

	optionsLine := "  "
	opts := m.openPromptOptions()
	for i, opt := range opts {
		switch {
		case opt.usage == m.openPromptSelection:
			optionsLine += ui.ClickUsageActiveStyle.Render(opt.label)
		case opt.usage == m.openPromptHovered:
			optionsLine += hoveredStyle.Render(opt.label)
		default:
			optionsLine += ui.ClickUsageInactiveStyle.Render(opt.label)
		}
		if i < len(opts)-1 {
			optionsLine += dimStyle.Render("│")
		}
	}
	content += optionsLine + "\n\n"
	content += dimStyle.Render("←/→ select · enter open · esc skip")

	return ui.ModalStyle.Width(50).Render(content)
}

// deleteWarning returns a warning message and whether the status is dangerous (requires typing DELETE).
func deleteWarning(status git.WTStatus) (warning string, dangerous bool) {
	switch status {
	case git.StatusChanged:
		return "Uncommitted changes will be LOST", true
	case git.StatusDiverged:
		return "CRITICAL: Unpushed commits will be permanently lost", true
	case git.StatusToPush:
		return "Unpushed commits will be LOST", true
	case git.StatusNoRemote:
		return "Branch was never pushed — ALL work will be LOST", true
	case git.StatusMergedDirty:
		return "Uncommitted changes will be LOST (branch is merged)", true
	case git.StatusNewDirty:
		return "Uncommitted changes will be LOST (new branch)", true
	case git.StatusMissing:
		return "Worktree directory is missing — will clean up git references", false
	default:
		return "", false
	}
}

// deleteHasRemote returns true if the worktree status implies a remote branch exists.
func deleteHasRemote(status git.WTStatus) bool {
	switch status {
	case git.StatusNew, git.StatusNewDirty, git.StatusNoRemote:
		return false
	default:
		return true
	}
}

func (m Model) renderDeleteConfirmDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorRed).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)
	warnStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorYellow).Background(ui.ColorBlack)
	critStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorRed).Background(ui.ColorBlack)

	branchName := "unknown"
	wt := m.deleteWT
	hasWT := wt.Path != ""
	if hasWT {
		branchName = wt.Branch
	}

	content := titleStyle.Render("Delete Worktree") + "\n\n"
	if m.deleteLocalBranch {
		content += dimStyle.Render("This will remove the worktree and delete the local branch:") + "\n\n"
	} else {
		content += dimStyle.Render("This will remove the worktree directory only:") + "\n\n"
	}
	content += whiteStyle.Render("  "+branchName) + "\n"

	if hasWT {
		warning, dangerous := deleteWarning(wt.Status)
		if warning != "" {
			content += "\n"
			if dangerous {
				content += critStyle.Render("  ⚠ "+warning) + "\n"
			} else {
				content += warnStyle.Render("  ⚠ "+warning) + "\n"
			}
		}

		content += "\n"
		if m.deleteProtected {
			content += warnStyle.Render("  Protected branch — the branch itself will be kept") + "\n"
		} else {
			// Local branch deletion toggle
			branchToggleKey := "b"
			if m.deleteDangerous {
				branchToggleKey = "ctrl+b"
			}
			if m.deleteLocalBranch {
				content += whiteStyle.Render("  ["+branchToggleKey+"] ✓ Also delete local branch") + "\n"
			} else {
				content += dimStyle.Render("  ["+branchToggleKey+"] Also delete local branch") + "\n"
			}

			// Offer remote branch deletion toggle when remote exists and local branch is being deleted
			if deleteHasRemote(wt.Status) && m.deleteLocalBranch {
				toggleKey := "d"
				if m.deleteDangerous {
					toggleKey = "ctrl+d"
				}
				if m.deleteRemoteBranch {
					content += whiteStyle.Render("  ["+toggleKey+"] ✓ Also delete remote branch") + "\n"
				} else {
					content += dimStyle.Render("  ["+toggleKey+"] Also delete remote branch") + "\n"
				}
			}
		}
	}

	content += "\n"
	if m.deleteDangerous {
		content += warnStyle.Render("  Type DELETE to confirm:") + "\n\n"
		content += "  " + m.deleteTypedInput.View() + "\n\n"
		content += dimStyle.Render("  enter confirm • esc cancel")
	} else {
		content += whiteStyle.Render("[Y]") + dimStyle.Render("es  ") + whiteStyle.Render("[N]") + dimStyle.Render("o")
	}

	return ui.ModalStyle.Width(56).Render(content)
}

func (m Model) renderCleanupConfirmDialog() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorGreen).Background(ui.ColorBlack)
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorDim).Background(ui.ColorBlack)
	whiteStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.ColorWhite).Background(ui.ColorBlack)

	content := titleStyle.Render("Cleanup Merged Worktrees") + "\n\n"
	content += dimStyle.Render("This will remove all merged worktrees and their local branches.") + "\n"

	content += "\n"
	if m.cleanupRemoteBranch {
		content += whiteStyle.Render("  [d] ✓ Also delete remote branches") + "\n"
	} else {
		content += dimStyle.Render("  [d] Also delete remote branches") + "\n"
	}

	content += "\n"
	content += whiteStyle.Render("[Y]") + dimStyle.Render("es  ") + whiteStyle.Render("[N]") + dimStyle.Render("o")

	return ui.ModalStyle.Width(56).Render(content)
}

// getCardScreenX computes the screen X position of a card by its repo index.
func (m Model) getCardScreenX(repoIdx int) int {
	cols := m.gridResult.Cols
	if cols <= 0 {
		cols = 1
	}
	col := repoIdx % cols
	return ui.MarginH + col*(m.gridResult.CardWidth+ui.CardGap)
}

// paintBlack ensures the entire screen has a black background by:
// 1. Replacing every ANSI reset with reset+black-bg so gaps between styled text stay black
// 2. Starting each line with black background
// 3. Padding each line to full terminal width
// 4. Filling remaining vertical space with black lines
func paintBlack(content string, width, height int) string {
	blackBg := ui.ThemeBgSeq() + ui.ThemeFgSeq()

	// After every ANSI reset, re-apply the theme canvas background and its
	// default text color — unstyled runes must never inherit the terminal
	// profile's foreground (invisible on light themes in dark terminals)
	content = strings.ReplaceAll(content, "\033[0m", "\033[0m"+blackBg)

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		visWidth := lipgloss.Width(line)
		pad := width - visWidth
		if pad < 0 {
			pad = 0
		}
		// Prepend black bg, append padding spaces to fill width
		lines[i] = blackBg + line + strings.Repeat(" ", pad)
	}
	// Fill remaining vertical space
	emptyLine := blackBg + strings.Repeat(" ", width)
	for len(lines) < height {
		lines = append(lines, emptyLine)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// Commands

// basisResolver creates a BasisBranchResolver over a snapshot of the per-repo
// config, taken on the caller's (Update) thread. Command goroutines must never
// read cfg.Local directly: settings can write that map while an operation
// runs, and concurrent map access panics. Call this (and pkgResolver /
// workspaceOpts) when constructing a command, not inside its goroutine.
func basisResolver(cfg *config.Config) git.BasisBranchResolver {
	snap := make(map[string]string, len(cfg.Local))
	for key, rc := range cfg.Local {
		snap[key] = rc.BasisBranch
	}
	return func(repoName string) string {
		if b, ok := snap[strings.ToUpper(repoName)]; ok && b != "" {
			return b
		}
		return "main"
	}
}

// pkgResolver snapshots the per-repo package-manager config (see
// basisResolver). "auto" resolves per repo via lockfile/corepack detection
// — inside the returned closure, so goroutines detect against the repo's
// files without touching live config.
func pkgResolver(cfg *config.Config) func(string) string {
	def := cfg.Global.PackageManager
	workDir := cfg.WorkDir
	snap := make(map[string]string, len(cfg.Local))
	for key, rc := range cfg.Local {
		snap[key] = rc.PackageManager
	}
	return func(repoName string) string {
		pm, ok := snap[strings.ToUpper(repoName)]
		if !ok || pm == "" {
			pm = def
		}
		if pm == "auto" {
			return tools.DetectPM(filepath.Join(workDir, repoName))
		}
		return pm
	}
}

// resolvePM resolves a repo's package manager to a concrete tool on the
// Update thread (rebase, rename, modal display) — never returns "auto".
func resolvePMWith(cfg *config.Config, repoName string) string {
	pm := cfg.GetRepoPackageManager(repoName)
	if pm == "auto" {
		return tools.DetectPM(filepath.Join(cfg.WorkDir, repoName))
	}
	return pm
}

func (m *Model) resolvePM(repoName string) string {
	return resolvePMWith(&m.config, repoName)
}

func checkMigrationCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		needed := git.NeedsMigration(cfg.WorkDir) || git.NeedsWorkspaceRepair(cfg.WorkDir)
		return MigrationCheckMsg{Needed: needed}
	}
}

func doMigrationCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		count := git.MigrateDirectoryStructure(cfg.WorkDir)
		// Also repair workspace file contents that may have been left stale
		count += git.RepairWorkspaceContents(cfg.WorkDir)
		return MigrationDoneMsg{Count: count}
	}
}

func loadReposCmd(cfg *config.Config) tea.Cmd {
	workDir := cfg.WorkDir
	resolve := basisResolver(cfg)
	ghEnabled := cfg.Global.GithubIntegration
	return func() tea.Msg {
		repos := git.DiscoverRepos(workDir, resolve)
		used, total := diskUsage(workDir)
		state := ghAvailability(ghEnabled)
		var remotes map[string]bool
		if state == ui.CloneReady {
			remotes = make(map[string]bool)
			for _, r := range repos {
				if !r.IsMonorepo && r.Path != "" {
					remotes[r.Name] = gh.RemoteIsGitHub(r.Path)
				}
			}
		}
		return ReposLoadedMsg{Repos: repos, TmuxLive: opener.LiveSessions(), GhState: state, DiskUsed: used, DiskTotal: total, GithubRemotes: remotes}
	}
}

// headerGhUser is the login shown in the header — hidden whenever the
// GitHub integration isn't live (disabled in Settings, or gh gone).
func (m *Model) headerGhUser() string {
	if m.ghState != ui.CloneReady {
		return ""
	}
	return m.ghUser
}

// anyBusy reports whether any operation is running.
func (m *Model) anyBusy() bool {
	return len(m.busy) > 0
}

// applyHiddenRepos annotates repos marked hidden in local config, dropping
// them entirely unless Show Hidden Repos is on. Hiding is what makes the
// empty-state folder suggestions reachable in directories full of unwanted
// repo-shaped folders (.nvm, .oh-my-zsh, ...).
func (m *Model) applyHiddenRepos(repos []git.Repo) []git.Repo {
	out := repos[:0]
	for _, r := range repos {
		if !r.IsMonorepo && m.config.IsRepoHidden(r.Name) {
			if !m.config.Global.ShowHiddenRepos {
				continue
			}
			r.Hidden = true
		}
		out = append(out, r)
	}
	return out
}

// creatableRepos: hidden repos never appear in the create modal, even when
// Show Hidden Repos renders them grayed out.
func (m *Model) creatableRepos() []git.Repo {
	out := make([]git.Repo, 0, len(m.repos))
	for _, r := range m.repos {
		if !r.Hidden {
			out = append(out, r)
		}
	}
	return out
}

// canHideRepo: only worktree-less plain repos can hide — a repo with live
// LTS worktrees is clearly wanted.
func (m *Model) canHideRepo(repo git.Repo) bool {
	return !repo.IsMonorepo && repo.Path != "" && len(repo.Worktrees) == 0 && !repo.Hidden
}

// lockSet returns the repo names an operation on repo must lock —
// the constituent repos for a monorepo group, the repo itself otherwise.
func lockSet(repo git.Repo) []string {
	if repo.IsMonorepo {
		return repo.RepoNames
	}
	return []string{repo.Name}
}

// repoBusy reports whether repo (or any of its constituents) has an
// operation running.
func (m *Model) repoBusy(repo git.Repo) bool {
	for _, n := range lockSet(repo) {
		if _, ok := m.busy[n]; ok {
			return true
		}
	}
	return false
}

// clearBusy releases the locks an operation held.
func (m *Model) clearBusy(names ...string) {
	for _, n := range names {
		delete(m.busy, n)
	}
}

// allRepoNames returns every real repo name — the lock set for operations
// that touch everything (refresh all, cleanup merged).
func (m *Model) allRepoNames() []string {
	var names []string
	for _, r := range m.repos {
		if !r.IsMonorepo {
			names = append(names, r.Name)
		}
	}
	return names
}

// busyCardNames returns the display names of cards with a running operation
// (a monorepo card is busy when any constituent is).
func (m *Model) busyCardNames() map[string]bool {
	if len(m.busy) == 0 {
		return nil
	}
	out := make(map[string]bool)
	for _, r := range m.repos {
		if m.repoBusy(r) {
			out[r.Name] = true
		}
	}
	return out
}

// listenForLogs returns a tea.Cmd that reads one LogEntryMsg from the channel.
// The Update handler re-subscribes after each message.
func listenForLogs(ch <-chan LogEntryMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// beginOp locks the given repos for an operation, sets the status line, and
// returns the log function (writing to the shared, persistent log channel)
// plus the spinner command when this is the first running operation.
// Callers gate on repoBusy/anyBusy before calling — locks must be disjoint.
func (m *Model) beginOp(statusMsg string, lock ...string) (git.LogFunc, tea.Cmd) {
	for _, n := range lock {
		m.busy[n] = statusMsg
	}
	m.statusMsg = statusMsg
	ch := m.logChan
	logFn := func(ctx, msg string, isError bool) {
		ch <- LogEntryMsg{Context: ctx, Message: msg, IsError: isError}
	}
	if !m.loaderTicking {
		m.loaderTicking = true
		return logFn, loaderTickCmd()
	}
	return logFn, nil
}

func refreshAllCmd(logFn git.LogFunc, cfg *config.Config, locked []string) tea.Cmd {
	workDir := cfg.WorkDir
	resolve := basisResolver(cfg)
	return func() tea.Msg {
		count, failed, err := git.RefreshAllRepos(workDir, resolve, logFn)
		return RefreshDoneMsg{Count: count, Failed: failed, Locked: locked, Err: err}
	}
}

func singleRefreshCmd(logFn git.LogFunc, repoPath, basisBranch, repoName string) tea.Cmd {
	return func() tea.Msg {
		err := git.RefreshRepo(repoPath, basisBranch, logFn)
		return SingleRefreshDoneMsg{RepoName: repoName, Locked: []string{repoName}, Err: err}
	}
}

func rebaseCmd(logFn git.LogFunc, wtPath, mainBranch, pkgManager, branch string, locked []string) tea.Cmd {
	return func() tea.Msg {
		err := git.RebaseWorktree(wtPath, mainBranch, pkgManager, logFn)
		return RebaseDoneMsg{Branch: branch, Locked: locked, Err: err}
	}
}

func deleteCmd(logFn git.LogFunc, repoPath, wtPath, branch string, deleteLocal, deleteRemote bool, locked []string) tea.Cmd {
	return func() tea.Msg {
		err := git.DeleteWorktree(repoPath, wtPath, branch, deleteLocal, deleteRemote, logFn)
		if err == nil {
			opener.KillSession(wtPath) // best-effort tmux cleanup
		}
		return DeleteDoneMsg{Branch: branch, Locked: locked, Err: err}
	}
}

func deleteMonorepoCmd(logFn git.LogFunc, scriptDir, branchSubdir, branch string, repoNames []string, deleteLocal, deleteRemote bool, locked []string) tea.Cmd {
	return func() tea.Msg {
		err := git.DeleteMonorepoWorktree(scriptDir, branchSubdir, branch, repoNames, deleteLocal, deleteRemote, logFn)
		if err == nil {
			opener.KillSession(branchSubdir) // best-effort tmux cleanup
		}
		return DeleteDoneMsg{Branch: branch, Locked: locked, Err: err}
	}
}

// workspaceOpts assembles the create-time options from config, snapshotting
// everything on the caller's (Update) thread — see basisResolver.
// setupResolver snapshots per-repo setup scripts (see basisResolver).
func setupResolver(cfg *config.Config) func(string) string {
	snap := make(map[string]string, len(cfg.Local))
	for key, rc := range cfg.Local {
		snap[key] = rc.SetupScript
	}
	return func(repoName string) string {
		return snap[strings.ToUpper(repoName)]
	}
}

func workspaceOpts(cfg *config.Config) git.WorkspaceOptions {
	return git.WorkspaceOptions{
		PkgManager:   pkgResolver(cfg),
		SetupScript:  setupResolver(cfg),
		AICliCommand: cfg.Global.AICliCommand,
		IDECommand:   cfg.Global.IDECommand,
		OpenEnvInIDE: cfg.Global.OpenEnvInIDE,
		CopyEnv:      cfg.Global.CopyEnvFiles,
		CopyMCP:      cfg.Global.CopyMCPJson,
	}
}

func createWorktreeCmd(logFn git.LogFunc, repoNames []string, branch string, installDeps map[string]bool, cfg *config.Config) tea.Cmd {
	workDir := cfg.WorkDir
	resolve := basisResolver(cfg)
	opts := workspaceOpts(cfg)
	return func() tea.Msg {
		log := &git.CreateLog{Stream: logFn}
		results, err := git.CreateMonorepoWorktrees(repoNames, workDir, branch, resolve, installDeps, opts, log)
		if err != nil {
			logFn("create", "Failed: "+err.Error(), true)
		} else {
			logFn("create", "Worktree created successfully", false)
		}
		return CreateDoneMsg{Results: results, Branch: branch, Log: log, Locked: repoNames, Err: err}
	}
}

func cleanupCmd(logFn git.LogFunc, cfg *config.Config, deleteRemote bool, locked []string) tea.Cmd {
	workDir := cfg.WorkDir
	resolve := basisResolver(cfg)
	return func() tea.Msg {
		cleaned, deletedPaths, err := git.CleanupMergedCleanables(workDir, resolve, deleteRemote, logFn)
		// Cleaned worktrees are gone — their tmux sessions must not linger
		for _, p := range deletedPaths {
			opener.KillSession(p)
		}
		return CleanupMergedDoneMsg{Cleaned: cleaned, Locked: locked, Err: err}
	}
}

func renameCmd(logFn git.LogFunc, repoPath, wtPath, oldBranch, newBranch string, renameRemote bool, cfg *config.Config, locked []string) tea.Cmd {
	pm := resolvePMWith(cfg, filepath.Base(repoPath))
	aiCli, ide, openEnv := cfg.Global.AICliCommand, cfg.Global.IDECommand, cfg.Global.OpenEnvInIDE
	return func() tea.Msg {
		res, err := git.RenameWorktree(repoPath, wtPath, oldBranch, newBranch, renameRemote,
			pm, aiCli, ide, openEnv, logFn)
		if err != nil {
			logFn(newBranch, "Rename failed: "+err.Error(), true)
		} else if res != nil {
			opener.RenameSession(wtPath, res.NewPath) // keep tmux session matching
		}
		return RenameDoneMsg{NewBranch: newBranch, Locked: locked, Err: err}
	}
}

func renameMonorepoCmd(logFn git.LogFunc, branchSubdirPath string, repoNames []string, oldBranch, newBranch string, renameRemote bool, cfg *config.Config, locked []string) tea.Cmd {
	workDir := cfg.WorkDir
	pm, aiCli, ide, openEnv := cfg.Global.PackageManager, cfg.Global.AICliCommand, cfg.Global.IDECommand, cfg.Global.OpenEnvInIDE
	return func() tea.Msg {
		res, err := git.RenameMonorepoWorktrees(workDir, branchSubdirPath, repoNames, oldBranch, newBranch, renameRemote,
			pm, aiCli, ide, openEnv, logFn)
		if err != nil {
			logFn(newBranch, "Rename failed: "+err.Error(), true)
		} else if res != nil {
			opener.RenameSession(branchSubdirPath, res.NewPath) // keep tmux session matching
		}
		return RenameDoneMsg{NewBranch: newBranch, Locked: locked, Err: err}
	}
}

func cleanModulesCmd(logFn git.LogFunc, wtPath, branch string, locked []string) tea.Cmd {
	return func() tea.Msg {
		removed, freed, err := git.CleanModules(wtPath, logFn)
		return CleanModulesDoneMsg{Branch: branch, WtPath: wtPath, Removed: removed, Freed: freed, Locked: locked, Err: err}
	}
}

func migrateCmd(logFn git.LogFunc, repoPath string, cfg *config.Config, locked []string) tea.Cmd {
	workDir := cfg.WorkDir
	basis := cfg.GetRepoBasisBranch(filepath.Base(repoPath))
	opts := workspaceOpts(cfg)
	return func() tea.Msg {
		result, err := git.MigrateToWorktree(repoPath, workDir, basis, opts, logFn)
		if err != nil {
			logFn("migrate", "Failed: "+err.Error(), true)
		} else {
			logFn("migrate", "Migration complete", false)
		}
		return MigrateDoneMsg{Result: result, Locked: locked, Err: err}
	}
}

func updateCheckCmd(autoUpdate bool) tea.Cmd {
	return func() tea.Msg {
		var r update.Result
		if autoUpdate {
			r = update.Update()
		} else {
			r = update.Check()
		}
		return UpdateCheckMsg{Result: r}
	}
}

func doUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		r := update.Update()
		return UpdateCheckMsg{Result: r}
	}
}

func clearStatusCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return StatusClearMsg{} // Gen 0 = always clears
	})
}

func clearStatusAfter(gen int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return StatusClearMsg{Gen: gen}
	})
}

func loaderTickCmd() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return LoaderTickMsg{}
	})
}
