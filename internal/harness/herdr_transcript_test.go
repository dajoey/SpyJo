package harness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractTranscriptMessageReturnsLastAssistantText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	since := time.Date(2026, 9, 18, 17, 0, 0, 0, time.UTC)
	lines := []string{
		`{"type":"session","id":"x"}`,
		`{"type":"message","timestamp":"2026-09-18T17:00:01Z","message":{"role":"user","content":[{"type":"text","text":"You are the communication agent"}]}}`,
		`{"type":"message","timestamp":"2026-09-18T17:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"first"}]}}`,
		`{"type":"message","timestamp":"2026-09-18T17:00:03Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hm"},{"type":"tool_use","name":"bash"}]}}`,
		`{"type":"message","timestamp":"2026-09-18T17:00:04Z","message":{"role":"toolResult","content":[{"type":"text","text":"6"}]}}`,
		`{"type":"message","timestamp":"2026-09-18T17:00:05Z","message":{"role":"assistant","content":[{"type":"text","text":" Six tasks are waiting. "}]}}`,
		`{"type":"message","timestamp":"2026-09-18T17:00:06Z","message":{"role":"user","content":"plain string content"}}`,
		`not json`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := extractTranscriptMessage(path, since)
	if err != nil || got != "Six tasks are waiting." {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := extractTranscriptMessage(filepath.Join(t.TempDir(), "missing.jsonl"), since); err == nil {
		t.Fatal("missing transcript must fail")
	}
}

// A turn that errored must never be answered with the previous turn's text.
// Live case 2026-09-18: the Venice key behind the chat seat returned 402 and
// SpyJo replied to three unrelated questions with the same stale paragraph.
func TestExtractTranscriptMessageDoesNotReplayPreviousTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"message","timestamp":"2026-09-18T15:35:12.305Z","message":{"role":"assistant","content":[{"type":"text","text":"All seven are queued for promotion"}]}}`,
		`{"type":"message","timestamp":"2026-09-18T17:51:40.054Z","message":{"role":"user","content":[{"type":"text","text":"test"}]}}`,
		`{"type":"message","timestamp":"2026-09-18T17:51:40.562Z","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"402 API key USD spend limit exceeded"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 9, 18, 17, 51, 40, 0, time.UTC)
	got, err := extractTranscriptMessage(path, since)
	if got != "" {
		t.Fatalf("replayed a previous turn: %q", got)
	}
	var turnErr *transcriptTurnError
	if !errors.As(err, &turnErr) {
		t.Fatalf("want transcriptTurnError, got %v", err)
	}
	if !strings.Contains(turnErr.Detail, "402") {
		t.Fatalf("provider detail lost: %q", turnErr.Detail)
	}
}

// An empty completion with no error message is still a failed turn, not a
// licence to serve older text.
func TestExtractTranscriptMessageEmptyTurnIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"message","timestamp":"2026-09-18T15:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"stale"}]}}`,
		`{"type":"message","timestamp":"2026-09-18T17:00:01Z","message":{"role":"assistant","content":[]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 9, 18, 17, 0, 0, 0, time.UTC)
	got, err := extractTranscriptMessage(path, since)
	if got != "" {
		t.Fatalf("served stale text: %q", got)
	}
	var turnErr *transcriptTurnError
	if !errors.As(err, &turnErr) {
		t.Fatalf("want transcriptTurnError, got %v", err)
	}
}

// A transcript whose newest reply predates the prompt yields no text at all, so
// the caller falls through to reading the live terminal instead.
func TestExtractTranscriptMessageIgnoresPreTurnText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	line := `{"type":"message","timestamp":"2026-09-18T15:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"stale"}]}}`
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := extractTranscriptMessage(path, time.Date(2026, 9, 18, 17, 0, 0, 0, time.UTC))
	if got != "" || err == nil {
		t.Fatalf("got %q, %v", got, err)
	}
	var turnErr *transcriptTurnError
	if errors.As(err, &turnErr) {
		t.Fatal("a transcript with no turn of its own must not report a failed turn")
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
