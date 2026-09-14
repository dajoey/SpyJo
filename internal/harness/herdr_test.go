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
