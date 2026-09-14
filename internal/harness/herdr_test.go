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

func TestWorkerNameAndLabel(t *testing.T) {
	tests := []struct {
		key          string
		model        string
		wantName     string
		wantTabLabel string
	}{
		{
			key:          "orchestrator:tasks:task_implementation:tasks-20260914-helm-fleet-update-03:1",
			model:        "opencode",
			wantName:     "sj-t-a1-helm-fleet-update-03",
			wantTabLabel: "Task: helm-fleet-update-03 (opencode)",
		},
		{
			key:          "orchestrator:tasks:task_review:tasks-20260914-helm-lmc-verify-02:1",
			model:        "kimi",
			wantName:     "sj-r-a1-helm-lmc-verify-02",
			wantTabLabel: "Review: helm-lmc-verify-02 (kimi)",
		},
		{
			key:          "orchestrator:goals:planning:goals-20260914-202731-helm01:1",
			model:        "opencode",
			wantName:     "sj-p-a1-helm01",
			wantTabLabel: "Plan: helm01 (opencode)",
		},
		{
			key:          "orchestrator:semantic-heartbeat",
			model:        "opencode",
			wantName:     "sj-hb-opencode",
			wantTabLabel: "Heartbeat (%s)",
		},
		{
			key:          "chat:tui:local-2a0229b8d17f26ca2335fa4eaaf39397",
			model:        "opencode",
			wantName:     "sj-c-tui-2a0229b8d17f",
			wantTabLabel: "Chat: 2a0229b8d17f (opencode)",
		},
		{
			key:          "chat:tui:local-71f92e03b302",
			model:        "opencode",
			wantName:     "sj-c-tui-71f92e03b302",
			wantTabLabel: "Chat: 71f92e03b302 (opencode)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			gotName, gotLabel := workerNameAndLabel(tc.key, tc.model)
			if len(gotName) > 32 {
				t.Errorf("workerNameAndLabel name %q exceeds 32 chars (len=%d)", gotName, len(gotName))
			}
			if gotName != tc.wantName {
				t.Errorf("workerNameAndLabel(%q, %q) name = %q, want %q", tc.key, tc.model, gotName, tc.wantName)
			}
			if tc.wantTabLabel == "Heartbeat (%s)" {
				tc.wantTabLabel = "Heartbeat (opencode)"
			}
			if gotLabel != tc.wantTabLabel {
				t.Errorf("workerNameAndLabel(%q, %q) label = %q, want %q", tc.key, tc.model, gotLabel, tc.wantTabLabel)
			}
		})
	}
}
