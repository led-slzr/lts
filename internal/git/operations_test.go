package git

import (
	"os"
	"path/filepath"
	"testing"
)

// Simulates deleting the last monorepo worktree: the branch subdir has been
// removed, leaving the -lts root with metadata + .DS_Store junk.
func TestCleanEmptyLTSDirsIgnoresJunk(t *testing.T) {
	root := t.TempDir()
	lts := filepath.Join(root, "erp-ui-gorocky-erp-lts")
	if err := os.MkdirAll(lts, 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".lts-type", ".lts-repos", ".DS_Store"} {
		if err := os.WriteFile(filepath.Join(lts, f), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cleanEmptyLTSDirs(lts)
	if _, err := os.Stat(lts); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, still exists", lts)
	}
}

// Branch subdir containing only .DS_Store should be removed, then the -lts
// root (metadata-only) should also be removed by the upward walk.
func TestCleanEmptyLTSDirsWalksUpThroughJunkSubdir(t *testing.T) {
	root := t.TempDir()
	lts := filepath.Join(root, "core-goforms-lts")
	sub := filepath.Join(lts, "feat-something")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".lts-type", ".lts-repos"} {
		if err := os.WriteFile(filepath.Join(lts, f), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sub, ".DS_Store"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	cleanEmptyLTSDirs(sub)
	if _, err := os.Stat(lts); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be removed, still exists", lts)
	}
}

// A -lts dir with a real worktree dir must NOT be removed.
func TestCleanEmptyLTSDirsKeepsRealContent(t *testing.T) {
	root := t.TempDir()
	lts := filepath.Join(root, "core-lts")
	if err := os.MkdirAll(filepath.Join(lts, "feat-real-worktree"), 0755); err != nil {
		t.Fatal(err)
	}
	cleanEmptyLTSDirs(lts)
	if _, err := os.Stat(lts); err != nil {
		t.Fatalf("expected %s to survive, got %v", lts, err)
	}
}

func TestParseGitVersion(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"git version 2.39.5 (Apple Git-154)", 2, 39, true},
		{"git version 2.17.0", 2, 17, true},
		{"git version 2.50.1.windows.1", 2, 50, true},
		{"not a version", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, tc := range cases {
		major, minor, ok := parseGitVersion(tc.in)
		if major != tc.major || minor != tc.minor || ok != tc.ok {
			t.Errorf("parseGitVersion(%q) = %d, %d, %v; want %d, %d, %v",
				tc.in, major, minor, ok, tc.major, tc.minor, tc.ok)
		}
	}
}

func TestCheckPrerequisitesOnThisMachine(t *testing.T) {
	if err := CheckPrerequisites(); err != nil {
		t.Errorf("expected prerequisites to pass here, got: %v", err)
	}
}

func TestCopySupportFiles(t *testing.T) {
	src := t.TempDir()
	writeFileT := func(rel, content string) {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeFileT(".env", "A=1")
	writeFileT(".env.local", "B=2")
	writeFileT("apps/web/.env", "C=3")
	writeFileT(".mcp.json", "{}")
	writeFileT("node_modules/pkg/.env", "SKIP=1")
	writeFileT("main.go", "package main")

	exists := func(root, rel string) bool {
		_, err := os.Stat(filepath.Join(root, rel))
		return err == nil
	}

	// env only (default behavior)
	dst1 := t.TempDir()
	copySupportFiles(src, dst1, true, false)
	for _, want := range []string{".env", ".env.local", "apps/web/.env"} {
		if !exists(dst1, want) {
			t.Errorf("env-only: expected %s to be copied", want)
		}
	}
	for _, not := range []string{".mcp.json", "node_modules/pkg/.env", "main.go"} {
		if exists(dst1, not) {
			t.Errorf("env-only: %s should not be copied", not)
		}
	}

	// mcp only
	dst2 := t.TempDir()
	copySupportFiles(src, dst2, false, true)
	if !exists(dst2, ".mcp.json") {
		t.Error("mcp-only: expected .mcp.json to be copied")
	}
	if exists(dst2, ".env") {
		t.Error("mcp-only: .env should not be copied")
	}

	// both off — nothing copied
	dst3 := t.TempDir()
	copySupportFiles(src, dst3, false, false)
	entries, _ := os.ReadDir(dst3)
	if len(entries) != 0 {
		t.Errorf("both-off: expected empty dir, got %d entries", len(entries))
	}
}
