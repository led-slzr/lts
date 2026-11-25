package agentskills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsBundled(t *testing.T) {
	names := Names()
	for _, want := range []string{"context", "start", "finish", "tidy", "sync"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("missing bundled skill %q (have %v)", want, names)
		}
	}
	for _, s := range Skills() {
		if !strings.HasPrefix(s.Content, "---\nname: "+s.Name+"\ndescription: ") {
			t.Errorf("%s: bad frontmatter", s.Name)
		}
		if st := Standalone(s, names); !strings.Contains(st, "name: lts-"+s.Name+"\n") {
			t.Errorf("%s: standalone name not prefixed", s.Name)
		}
	}
}

func TestInstallIdempotentAndPreservesSettings(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	os.WriteFile(settings, []byte(`{"model":"opus","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo mine"}]}]}}`), 0644)

	for i := 0; i < 2; i++ {
		if _, err := Install(Options{ClaudeDir: dir, Hooks: true}); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(settings)
	var m map[string]any
	json.Unmarshal(data, &m)
	if m["model"] != "opus" {
		t.Fatal("unrelated settings must survive")
	}
	ss := m["hooks"].(map[string]any)["SessionStart"].([]any)
	if len(ss) != 2 || !strings.Contains(string(data), "echo mine") {
		t.Fatalf("want user hook + one LTS hook, got %s", data)
	}
	if strings.Count(string(data), "lts hook session-start") != 1 {
		t.Fatal("install must be idempotent")
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "lts-start", "SKILL.md")); err != nil {
		t.Fatal("skill not written")
	}
	if _, err := os.Stat(settings + ".lts-backup"); err != nil {
		t.Fatal("original settings should be backed up")
	}

	if _, err := Uninstall(Options{ClaudeDir: dir}); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(settings)
	if strings.Contains(string(data), "lts hook") || !strings.Contains(string(data), "echo mine") {
		t.Fatalf("uninstall must remove only LTS hooks: %s", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "lts-start")); !os.IsNotExist(err) {
		t.Fatal("skills should be removed")
	}
}
