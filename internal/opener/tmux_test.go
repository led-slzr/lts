package opener

import "testing"

func TestSessionName(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		// single-repo worktree: group name folded in
		{"/w/core-lts/feat-login", "lts-core-feat-login"},
		// monorepo branch subdir
		{"/w/core-goforms-lts/feat-x", "lts-core-goforms-feat-x"},
		// main repo dir (parent isn't a -lts dir)
		{"/w/core", "lts-core"},
		// characters tmux can't take in targets get sanitized
		{"/w/core-lts/fix-v2.1", "lts-core-fix-v2-1"},
		{"/w/core-lts/feat login", "lts-core-feat-login"},
	}
	for _, tc := range cases {
		if got := SessionName(tc.path); got != tc.want {
			t.Errorf("SessionName(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestOptionsTmuxEnabled(t *testing.T) {
	if (Options{Multiplexer: "none"}).tmuxEnabled() {
		t.Error("none must not enable tmux")
	}
	if (Options{}).tmuxEnabled() {
		t.Error("empty must not enable tmux")
	}
	// "tmux" enables only when the binary exists — don't assert either way
	// on the machine's tmux, just that it doesn't panic
	_ = (Options{Multiplexer: "tmux"}).tmuxEnabled()
}
