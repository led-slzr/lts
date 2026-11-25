package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"lts-revamp/internal/git"
	"lts-revamp/internal/lock"

	"github.com/spf13/cobra"
)

func init() { register(addWatch) }

// watchEvent is one line of `lts watch --json` (NDJSON).
type watchEvent struct {
	SchemaVersion int    `json:"schema_version"`
	Type          string `json:"type"`
	Time          string `json:"time"`
	ID            string `json:"id,omitempty"`
	Repo          string `json:"repo,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Path          string `json:"path,omitempty"`
	Owner         string `json:"owner,omitempty"`
	From          string `json:"from,omitempty"`
	To            string `json:"to,omitempty"`
	Worktrees     int    `json:"worktrees,omitempty"`
}

type watchSnap struct {
	wts   map[string]Target // id → target
	busy  map[string]string // repo → owner
	stale map[string]string // path → repo
	state map[string]string // id → state (with --status)
}

func addWatch(a *app, root *cobra.Command) {
	var interval time.Duration
	var withStatus bool
	var maxPolls int
	cmd := &cobra.Command{
		Use:     "watch",
		Short:   "Stream worktree events (added, removed, busy, missing, state) as they happen",
		GroupID: groupInspect,
		Long: `Watch the workspace and print one event per change until interrupted
(Ctrl+C ends it with exit 0). With --json each event is one JSON object
per line (NDJSON) — ideal for an orchestrator that reacts to workers:

  ready            initial snapshot taken (.worktrees = count)
  worktree_added   a worktree appeared        (.id .repo .branch .path)
  worktree_removed a worktree disappeared     (.id)
  busy / idle      a repo's lock was taken / released (.repo .owner)
  missing          a worktree folder was deleted by hand (.repo .path)
  state_changed    git state changed (.id .from .to) — only with --status

Polls every --interval (default 2s; cheap — no git status). --status adds
per-worktree state, which runs git status for every worktree each poll,
so its interval is at least 10s.` + exitCodesHelp(),
		Example: `  lts watch
  lts watch --json | jq -c 'select(.type=="idle")'
  lts watch --status --interval 15s --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if withStatus && interval < 10*time.Second {
				interval = 10 * time.Second
			}
			if interval < 500*time.Millisecond {
				interval = 500 * time.Millisecond
			}
			env, err := a.loadEnv(envOpts{})
			if err != nil {
				return err
			}
			a.streaming = true
			a.out.wrote = true
			emit := func(ev watchEvent) {
				ev.SchemaVersion, ev.Time = SchemaVersion, time.Now().UTC().Format(time.RFC3339)
				if a.out.JSON {
					b, _ := json.Marshal(ev)
					fmt.Fprintln(a.out.Out, string(b))
					return
				}
				line := ev.Type
				for _, f := range []string{ev.ID, ev.Repo, ev.Branch, ev.Owner, ev.Path} {
					if f != "" {
						line += "  " + f
					}
				}
				if ev.From != "" || ev.To != "" {
					line += "  " + ev.From + " → " + ev.To
				}
				fmt.Fprintf(a.out.Out, "%s  %s\n", time.Now().Format("15:04:05"), line)
			}
			snap := func() watchSnap {
				env.Cfg = reloadCfg(env)
				env.discoverWith(git.DiscoverOptions{SkipStatus: true, SkipMigrationCheck: true})
				s := watchSnap{wts: map[string]Target{}, busy: map[string]string{}, stale: map[string]string{}, state: map[string]string{}}
				for _, t := range env.allTargets() {
					s.wts[t.ID()] = t
					if withStatus {
						s.state[t.ID()] = statusOf(env, t).Slug()
					}
				}
				for _, r := range env.realRepos(true) {
					if locked, owner := lock.Holder(env.WorkDir, r.Name); locked {
						s.busy[r.Name] = owner
					}
					for _, st := range r.Stale {
						s.stale[st.Path] = r.Name
					}
				}
				return s
			}
			prev := snap()
			emit(watchEvent{Type: "ready", Worktrees: len(prev.wts)})
			for polls := 1; maxPolls == 0 || polls < maxPolls; polls++ {
				deadline := time.Now().Add(interval)
				for time.Now().Before(deadline) {
					if a.interrupted.Load() {
						return nil
					}
					time.Sleep(100 * time.Millisecond)
				}
				cur := snap()
				for _, id := range sortedKeys(cur.wts) {
					if _, ok := prev.wts[id]; !ok {
						t := cur.wts[id]
						emit(watchEvent{Type: "worktree_added", ID: id, Repo: t.Repo.Name, Branch: t.WT.Branch, Path: t.WT.Path})
					}
				}
				for _, id := range sortedKeys(prev.wts) {
					if _, ok := cur.wts[id]; !ok {
						emit(watchEvent{Type: "worktree_removed", ID: id, Repo: prev.wts[id].Repo.Name, Branch: prev.wts[id].WT.Branch})
					}
				}
				for _, r := range sortedKeys(cur.busy) {
					if _, was := prev.busy[r]; !was {
						emit(watchEvent{Type: "busy", Repo: r, Owner: cur.busy[r]})
					}
				}
				for _, r := range sortedKeys(prev.busy) {
					if _, still := cur.busy[r]; !still {
						emit(watchEvent{Type: "idle", Repo: r})
					}
				}
				for _, p := range sortedKeys(cur.stale) {
					if _, was := prev.stale[p]; !was {
						emit(watchEvent{Type: "missing", Repo: cur.stale[p], Path: p})
					}
				}
				for _, id := range sortedKeys(cur.state) {
					if before, ok := prev.state[id]; ok && before != cur.state[id] {
						emit(watchEvent{Type: "state_changed", ID: id, From: before, To: cur.state[id]})
					}
				}
				prev = cur
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "poll interval")
	cmd.Flags().BoolVar(&withStatus, "status", false, "also report git state changes (runs git status each poll)")
	cmd.Flags().IntVar(&maxPolls, "max-polls", 0, "stop after N polls (0 = until interrupted)")
	cmd.Flags().MarkHidden("max-polls")
	root.AddCommand(cmd)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
