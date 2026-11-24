// Package agentskills installs the LTS agent skills (embedded from
// plugin/skills) and Claude Code hooks without the plugin marketplace.
package agentskills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"lts-revamp/plugin"
)

// Prefix namespaces standalone skill dirs/names (plugin skills are
// namespaced by the plugin instead: /lts:start vs /lts-start).
const Prefix = "lts-"

// Skill is one bundled skill.
type Skill struct {
	Name    string // plugin name, e.g. "start"
	Content string // SKILL.md as shipped in the plugin
}

// Skills returns every bundled skill, sorted by name.
func Skills() []Skill {
	var out []Skill
	entries, _ := fs.ReadDir(plugin.Skills, "skills")
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(plugin.Skills, "skills/"+e.Name()+"/SKILL.md")
		if err != nil {
			continue
		}
		out = append(out, Skill{Name: e.Name(), Content: string(data)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns the bundled skill names.
func Names() []string {
	var n []string
	for _, s := range Skills() {
		n = append(n, s.Name)
	}
	return n
}

var nameLine = regexp.MustCompile(`(?m)^name:\s*(\S+)\s*$`)

// Standalone rewrites a plugin skill for ~/.claude/skills: the frontmatter
// name and cross-references gain the "lts-" prefix.
func Standalone(s Skill, all []string) string {
	c := nameLine.ReplaceAllString(s.Content, "name: "+Prefix+s.Name)
	for _, n := range all {
		c = strings.ReplaceAll(c, "- `"+n+"` —", "- `"+Prefix+n+"` —")
	}
	return c
}

// ClaudeDir is $CLAUDE_CONFIG_DIR or ~/.claude.
func ClaudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// HookCommand is the shell command a hook entry runs. It is a no-op when
// lts isn't on PATH, so a removed binary never breaks sessions.
func HookCommand(event string) string {
	return "command -v lts >/dev/null 2>&1 && lts hook " + event + " || true"
}

// Options configures Install / Uninstall.
type Options struct {
	ClaudeDir    string // default ClaudeDir()
	Hooks        bool   // SessionStart + SubagentStart context hooks
	WorktreeHook bool   // WorktreeCreate → LTS worktrees (opt-in)
	DryRun       bool
}

// Result reports what Install / Uninstall did (or would do).
type Result struct {
	SkillsDir    string   `json:"skills_dir"`
	Skills       []string `json:"skills"`
	SettingsPath string   `json:"settings_path,omitempty"`
	Hooks        []string `json:"hooks,omitempty"`
	Backup       string   `json:"backup,omitempty"`
	DryRun       bool     `json:"dry_run"`
}

func (o *Options) dir() string {
	if o.ClaudeDir != "" {
		return o.ClaudeDir
	}
	return ClaudeDir()
}

// Install writes the skills to <claude>/skills/lts-*/SKILL.md and, with
// Hooks/WorktreeHook, merges hook entries into <claude>/settings.json
// (idempotent; the original is backed up once to settings.json.lts-backup).
func Install(o Options) (Result, error) {
	root := o.dir()
	res := Result{SkillsDir: filepath.Join(root, "skills"), DryRun: o.DryRun}
	// Validate settings.json before writing anything (no half installs)
	if o.Hooks || o.WorktreeHook {
		if _, err := readSettings(filepath.Join(root, "settings.json")); err != nil {
			return res, err
		}
	}
	all := Names()
	for _, s := range Skills() {
		dir := filepath.Join(res.SkillsDir, Prefix+s.Name)
		res.Skills = append(res.Skills, Prefix+s.Name)
		if o.DryRun {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return res, err
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(Standalone(s, all)), 0644); err != nil {
			return res, err
		}
	}
	var events []string
	if o.Hooks {
		events = append(events, "SessionStart", "SubagentStart")
	}
	if o.WorktreeHook {
		events = append(events, "WorktreeCreate", "WorktreeRemove")
	}
	if len(events) == 0 {
		return res, nil
	}
	res.SettingsPath = filepath.Join(root, "settings.json")
	res.Hooks = events
	if o.DryRun {
		return res, nil
	}
	backup, err := editSettings(res.SettingsPath, func(hooks map[string]any) {
		for _, ev := range events {
			addHook(hooks, ev, HookCommand(eventArg(ev)))
		}
	})
	res.Backup = backup
	return res, err
}

// Uninstall removes the lts-* skills and any LTS hook entries.
func Uninstall(o Options) (Result, error) {
	root := o.dir()
	res := Result{SkillsDir: filepath.Join(root, "skills"), DryRun: o.DryRun}
	for _, n := range Names() {
		dir := filepath.Join(res.SkillsDir, Prefix+n)
		if _, err := os.Stat(dir); err == nil {
			res.Skills = append(res.Skills, Prefix+n)
			if !o.DryRun {
				os.RemoveAll(dir)
			}
		}
	}
	res.SettingsPath = filepath.Join(root, "settings.json")
	if _, err := os.Stat(res.SettingsPath); err != nil {
		res.SettingsPath = ""
		return res, nil
	}
	var removed []string
	edit := func(hooks map[string]any) {
		for _, ev := range []string{"SessionStart", "SubagentStart", "WorktreeCreate", "WorktreeRemove"} {
			if removeHook(hooks, ev, "lts hook ") {
				removed = append(removed, ev)
			}
		}
	}
	if o.DryRun {
		probe, _ := readSettings(res.SettingsPath)
		if h, ok := probe["hooks"].(map[string]any); ok {
			edit(h)
		}
	} else {
		b, err := editSettings(res.SettingsPath, edit)
		if err != nil {
			// skills are already removed; report the hooks we couldn't
			return res, fmt.Errorf("removed the skills, but could not edit hooks: %w", err)
		}
		res.Backup = b
	}
	res.Hooks = removed
	return res, nil
}

func eventArg(ev string) string {
	switch ev {
	case "SessionStart":
		return "session-start"
	case "SubagentStart":
		return "subagent-start"
	case "WorktreeCreate":
		return "worktree-create"
	case "WorktreeRemove":
		return "worktree-remove"
	}
	return strings.ToLower(ev)
}

func readSettings(path string) (map[string]any, error) {
	m := map[string]any{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return m, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep large integers exact on rewrite
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object (%v) — fix it or add the hooks by hand", path, err)
	}
	if h, ok := m["hooks"]; ok {
		if _, isObj := h.(map[string]any); !isObj {
			return nil, fmt.Errorf("%s: \"hooks\" is not an object — fix it or add the hooks by hand", path)
		}
	}
	return m, nil
}

// editSettings applies fn to settings.hooks and writes the file back,
// backing up the original once. Returns the backup path ("" if none).
func editSettings(path string, fn func(hooks map[string]any)) (string, error) {
	m, err := readSettings(path)
	if err != nil {
		return "", err
	}
	hooks, _ := m["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	before, _ := json.Marshal(hooks)
	fn(hooks)
	after, _ := json.Marshal(hooks)
	if string(before) == string(after) {
		return "", nil // already in the desired state
	}
	if len(hooks) == 0 {
		delete(m, "hooks")
	} else {
		m["hooks"] = hooks
	}
	backup := ""
	if orig, err := os.ReadFile(path); err == nil {
		backup = path + ".lts-backup"
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			if err := os.WriteFile(backup, orig, 0644); err != nil {
				return "", err
			}
		}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep shell operators (&&, >) readable
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return backup, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return backup, err
	}
	return backup, os.WriteFile(path, buf.Bytes(), 0644)
}

// addHook appends a command hook for event unless an LTS one exists.
func addHook(hooks map[string]any, event, command string) {
	groups, _ := hooks[event].([]any)
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if c, _ := hm["command"].(string); strings.Contains(c, "lts hook "+eventArg(event)) {
				return
			}
		}
	}
	groups = append(groups, map[string]any{
		"hooks": []any{map[string]any{"type": "command", "command": command}},
	})
	hooks[event] = groups
}

// removeHook drops command hooks containing marker; reports whether any went.
func removeHook(hooks map[string]any, event, marker string) bool {
	groups, _ := hooks[event].([]any)
	removed := false
	var keptGroups []any
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		var kept []any
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if c, _ := hm["command"].(string); strings.Contains(c, marker) {
				removed = true
				continue
			}
			kept = append(kept, h)
		}
		if len(kept) > 0 {
			gm["hooks"] = kept
			keptGroups = append(keptGroups, gm)
		}
	}
	if len(keptGroups) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = keptGroups
	}
	return removed
}
