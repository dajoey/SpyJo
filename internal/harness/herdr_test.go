package harness

import (
	"context"
	"path/filepath"
	"testing"
)

func TestNewHerdrDefaults(t *testing.T) {
	cfg := HarnessConfig{Name: "herdr"}
	h, err := NewHerdr(cfg)
	if err != nil {
		t.Fatalf("NewHerdr failed: %v", err)
	}
	if h.config.Command != "herdr" {
		t.Errorf("expected command 'herdr', got %q", h.config.Command)
	}
	if h.config.Cwd != "." {
		t.Errorf("expected cwd '.', got %q", h.config.Cwd)
	}
}

func TestHerdrModels(t *testing.T) {
	h, err := NewHerdr(HarnessConfig{Name: "herdr"})
	if err != nil {
		t.Fatalf("NewHerdr failed: %v", err)
	}
	models, err := h.Models(context.Background())
	if err != nil {
		t.Fatalf("Models failed: %v", err)
	}
	if len(models) == 0 {
		t.Fatalf("expected at least base models, got 0")
	}

	foundClaude := false
	for _, m := range models {
		if m.ID == "claude" {
			foundClaude = true
			break
		}
	}
	if !foundClaude {
		t.Errorf("expected 'claude' model in Herdr models list")
	}
}

func TestHerdrSessionPersistence(t *testing.T) {
	dir := t.TempDir()
	sessionsFile := filepath.Join(dir, "sessions.json")

	h, err := NewHerdr(HarnessConfig{Name: "herdr", SessionsFile: sessionsFile})
	if err != nil {
		t.Fatalf("NewHerdr failed: %v", err)
	}

	h.rememberSession("test-key", "session-12345")
	if id := h.ThreadID("test-key"); id != "session-12345" {
		t.Fatalf("expected threadID 'session-12345', got %q", id)
	}

	// Reload from another instance
	h2, err := NewHerdr(HarnessConfig{Name: "herdr", SessionsFile: sessionsFile})
	if err != nil {
		t.Fatalf("NewHerdr h2 failed: %v", err)
	}
	if id := h2.ThreadID("test-key"); id != "session-12345" {
		t.Fatalf("expected reloaded threadID 'session-12345', got %q", id)
	}
}

func TestHerdrInBuiltinRegistry(t *testing.T) {
	registry := NewBuiltinRegistry()
	h, err := registry.Create(HarnessConfig{Name: "herdr", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("registry.Create(herdr) failed: %v", err)
	}
	if _, ok := h.(*Herdr); !ok {
		t.Fatalf("expected *Herdr, got %T", h)
	}
}

func TestCleanHerdrTerminalOutput(t *testing.T) {
	raw := `  ┃  <!-- Add concise, lasting workspace-specific behavior for the communication agent below. -->
  ┃  </workspace_owner_persistent_instructions>
  ┃
  ┃  End of persistent instructions for the chat agent. The precedence stated above still applies to every imported rule.
  ┃

     Thought: 7.3s

     Acknowledging no available tools and explaining inability to inspect files while noting no prior durable work.

     Fresh start: nothing has been dispatched in this conversation yet, so there’s nothing to report. What would you like to work on first?

     ▣  Build · Muse Spark 1.3 Contributor · 10.1s

  ┃
  ┃
  ┃
  ┃  Build · Muse Spark 1.3 Contributor OpenCode Go · high                                                                                                                                                         ~/aibs/opencode
  ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
   /home/dajoey/aibs/opencode                                                                                                                                             214.9K (20%) · $0.26  ctrl+p commands    • OpenCode 1.18.30`

	got := cleanHerdrTerminalOutput(raw)
	want := "Fresh start: nothing has been dispatched in this conversation yet, so there’s nothing to report. What would you like to work on first?"
	if got != want {
		t.Fatalf("cleanHerdrTerminalOutput = %q, want %q", got, want)
	}
}
