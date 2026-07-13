package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSetupScriptExecutesInWorktree(t *testing.T) {
	dir := t.TempDir()
	log := &CreateLog{}
	runSetupScript(dir, "echo ready > setup-ran.txt && echo chained >> setup-ran.txt", log)

	data, err := os.ReadFile(filepath.Join(dir, "setup-ran.txt"))
	if err != nil {
		t.Fatalf("script should run in the worktree dir: %v", err)
	}
	if !strings.Contains(string(data), "chained") {
		t.Fatal("&& chaining must work")
	}
	joined := strings.Join(log.Steps, " | ")
	if !strings.Contains(joined, "Setup script ✓") {
		t.Fatalf("success should be logged, got %q", joined)
	}
}

func TestRunSetupScriptFailureIsLoggedNotFatal(t *testing.T) {
	log := &CreateLog{}
	runSetupScript(t.TempDir(), "exit 7", log)
	joined := strings.Join(log.Steps, " | ")
	if !strings.Contains(joined, "Setup script failed") {
		t.Fatalf("failure should be logged, got %q", joined)
	}
}

func TestRunSetupScriptEmptyIsNoop(t *testing.T) {
	log := &CreateLog{}
	runSetupScript(t.TempDir(), "", log)
	if len(log.Steps) != 0 {
		t.Fatalf("empty script should log nothing, got %v", log.Steps)
	}
}
