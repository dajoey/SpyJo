package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agent0ai/spynel/internal/roster"
)

func TestRosterModelForKey(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, roster.StateDirName)
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	h := &Herdr{config: HarnessConfig{Model: "opencode", Cwd: root}}
	cfg := HarnessConfig{Model: "opencode", Cwd: root}

	if got := h.rosterModelForKey("chat:telegram:TG-1", cfg); got != "opencode" {
		t.Fatalf("no roster = %q", got)
	}
	body := "staff:\n  thinker: {runner: pi}\n  boss: {runner: claude, args: [\"--model\", \"opus\"]}\nroles:\n  chat: thinker\n  heartbeat: boss\n"
	if err := os.WriteFile(roster.Path(state), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ key, model, want string }{
		{"chat:telegram:TG-1", "opencode", "pi"},
		{"chat:tui:local-1", "", "pi"},
		{"chat:telegram:TG-1", "kimi", "kimi"}, // explicit /model choice wins
		{"orchestrator:semantic-heartbeat", "opencode", "@boss"},
		{"orchestrator:notification:t1:done:1", "opencode", "opencode"}, // role not mapped
		{"orchestrator:tasks:task_review:t1:1", "opencode", "opencode"}, // document routing owns these
	} {
		cfg.Model = c.model
		if got := h.rosterModelForKey(c.key, cfg); got != c.want {
			t.Errorf("%s (%q): got %q, want %q", c.key, c.model, got, c.want)
		}
	}
}
