package config

import "testing"

func TestGlobalSetGetRoundTrip(t *testing.T) {
	g := DefaultGlobal()
	for _, d := range GlobalKeys() {
		if _, ok := g.Get(d.Key); !ok {
			t.Errorf("Get(%s) not implemented", d.Key)
		}
	}
	cases := []struct{ key, in, want string }{
		{"PACKAGE_MANAGER", "PNPM", "pnpm"},
		{"COPY_MCP_JSON", "yes", "true"},
		{"TMUX_AI_PANE_WIDTH", "60", "60"},
		{"ide_command", "nvim", "nvim"},
		{"AI_CLI_COMMAND", "", ""},
	}
	for _, c := range cases {
		if err := g.Set(c.key, c.in); err != nil {
			t.Fatalf("Set(%s,%q): %v", c.key, c.in, err)
		}
		if got, _ := g.Get(c.key); got != c.want {
			t.Errorf("Get(%s) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestGlobalSetRejectsInvalid(t *testing.T) {
	g := DefaultGlobal()
	bad := []struct{ key, val string }{
		{"PACKAGE_MANAGER", "pip"},
		{"TMUX_AI_PANE_WIDTH", "95"},
		{"COPY_ENV_FILES", "maybe"},
		{"IDE_COMMAND", ""},
		{"NOPE", "x"},
	}
	for _, b := range bad {
		if err := g.Set(b.key, b.val); err == nil {
			t.Errorf("Set(%s,%q) accepted, want error", b.key, b.val)
		}
	}
}
