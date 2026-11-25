package cli

import "testing"

func TestSuggest(t *testing.T) {
	cands := []string{"create", "cleanup-merged", "clean-modules", "config", "status", "feat/login", "fix/login-bug"}
	cases := map[string]string{
		"craete":  "create",
		"stauts":  "status",
		"login":   "feat/login",
		"cleanup": "cleanup-merged",
	}
	for in, want := range cases {
		got := suggest(in, cands, 3)
		if len(got) == 0 || !contains(got, want) {
			t.Errorf("suggest(%q) = %v, want it to include %q", in, got, want)
		}
	}
	if got := suggest("zzzzzz", cands, 3); len(got) != 0 {
		t.Errorf("suggest(zzzzzz) = %v, want none", got)
	}
}
