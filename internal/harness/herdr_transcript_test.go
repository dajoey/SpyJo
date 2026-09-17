package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTranscriptMessageReturnsLastAssistantText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"session","id":"x"}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"You are the communication agent"}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"first"}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hm"},{"type":"tool_use","name":"bash"}]}}`,
		`{"type":"message","message":{"role":"toolResult","content":[{"type":"text","text":"6"}]}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":" Six tasks are waiting. "}]}}`,
		`{"type":"message","message":{"role":"user","content":"plain string content"}}`,
		`not json`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := extractTranscriptMessage(path)
	if err != nil || got != "Six tasks are waiting." {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := extractTranscriptMessage(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("missing transcript must fail")
	}
}

func TestTranscriptPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pi := filepath.Join(home, ".pi", "agent", "sessions", "--w--", "a.jsonl")
	if got := transcriptPath("pi", pi, "/w"); got != pi {
		t.Fatalf("pi = %q", got)
	}
	want := filepath.Join(home, ".claude", "projects", "-home-u-aibs-spy-jo", "abc-123.jsonl")
	if got := transcriptPath("claude", "abc-123", "/home/u/aibs/spy.jo"); got != want {
		t.Fatalf("claude = %q, want %q", got, want)
	}
	for _, c := range []struct{ kind, id string }{{"claude", "../../etc/passwd"}, {"kimi", "session_1"}, {"pi", "/tmp/evil.jsonl"}, {"claude", ""}} {
		if got := transcriptPath(c.kind, c.id, "/w"); got != "" {
			t.Errorf("%v resolved to %q", c, got)
		}
	}
}
