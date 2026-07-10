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
