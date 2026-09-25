package harness

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/agent0ai/spynel/internal/core"
)

// livePromptHarness is a queue-mode harness whose panes accept a live prompt:
// the herdr shape.
type livePromptHarness struct {
	*supervisorHarness
	livePromptsMu chan struct{}
	livePrompts   []string
}

func (h *livePromptHarness) PromptLive(_ context.Context, _ string, prompt string) error {
	h.mu.Lock()
	h.livePrompts = append(h.livePrompts, prompt)
	h.mu.Unlock()
	return nil
}

func TestSupervisorCancelQueuedControlWithdrawsBeforeTurnEnd(t *testing.T) {
	target := &supervisorHarness{name: "claude-code", followUp: FollowUpQueue, active: map[string]bool{}, emits: map[string]core.Emit{}}
	registry := NewRegistry()
	registry.Register("claude-code", func(HarnessConfig) (Harness, error) { return target, nil })
	supervisor := NewSupervisor(registry, HarnessConfig{Name: "claude-code"})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := supervisor.Send(context.Background(), "job", "original", nil); err != nil {
		t.Fatal(err)
	}
	if result, err := supervisor.SendControl(context.Background(), "job", ControlRequest{ID: "withdraw-me", Prompt: "second thought"}); err != nil || !result.Queued {
		t.Fatalf("queue result = %#v, %v", result, err)
	}
	if !supervisor.CancelQueuedControl("job", "withdraw-me") {
		t.Fatal("cancelling a queued control reported failure")
	}
	if supervisor.CancelQueuedControl("job", "withdraw-me") {
		t.Fatal("cancelling twice succeeded")
	}
	if supervisor.CancelQueuedControl("job", "") {
		t.Fatal("cancelling without an id succeeded")
	}
	target.finish("job")
	waitSupervisorState(t, func() bool { return !supervisor.IsActive("job") })
	target.mu.Lock()
	prompts := append([]string(nil), target.prompts["job"]...)
	target.mu.Unlock()
	if len(prompts) != 1 || prompts[0] != "original" {
		t.Fatalf("withdrawn control still delivered: %#v", prompts)
	}
}

func TestSupervisorSendLiveControlPromptsPaneForQueueModeHarnesses(t *testing.T) {
	target := &livePromptHarness{supervisorHarness: &supervisorHarness{name: "herdr", followUp: FollowUpQueue, active: map[string]bool{}, emits: map[string]core.Emit{}}}
	registry := NewRegistry()
	registry.Register("herdr", func(HarnessConfig) (Harness, error) { return target, nil })
	supervisor := NewSupervisor(registry, HarnessConfig{Name: "herdr"})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := supervisor.Send(context.Background(), "job", "original", nil); err != nil {
		t.Fatal(err)
	}
	result, err := supervisor.SendLiveControl(context.Background(), "job", ControlRequest{ID: "live-1", Prompt: "deliver me now"})
	if err != nil || result.Queued {
		t.Fatalf("live result = %#v, %v", result, err)
	}
	target.mu.Lock()
	live := append([]string(nil), target.livePrompts...)
	target.mu.Unlock()
	if len(live) != 1 || !strings.Contains(live[0], "deliver me now") {
		t.Fatalf("live prompts = %#v", live)
	}
	target.mu.Lock()
	queued := append([]string(nil), target.prompts["job"]...)
	target.mu.Unlock()
	if len(queued) != 1 {
		t.Fatalf("live control also queued a provider turn: %#v", queued)
	}
}

func TestSupervisorSendLiveControlRejectsValidationFailures(t *testing.T) {
	target := &livePromptHarness{supervisorHarness: &supervisorHarness{name: "herdr", followUp: FollowUpQueue, active: map[string]bool{}, emits: map[string]core.Emit{}}}
	registry := NewRegistry()
	registry.Register("herdr", func(HarnessConfig) (Harness, error) { return target, nil })
	supervisor := NewSupervisor(registry, HarnessConfig{Name: "herdr"})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := supervisor.Send(context.Background(), "job", "original", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := supervisor.SendLiveControl(context.Background(), "job", ControlRequest{ID: "invalid", Prompt: "nope", Validate: func() bool { return false }}); err == nil || !strings.Contains(err.Error(), "durable state changed") {
		t.Fatalf("validation failure error = %v", err)
	}
	if _, err := supervisor.SendLiveControl(context.Background(), "other", ControlRequest{ID: "inactive", Prompt: "nope"}); err == nil || !strings.Contains(err.Error(), "no longer active") {
		t.Fatalf("inactive error = %v", err)
	}
	if _, err := supervisor.SendLiveControl(context.Background(), "job", ControlRequest{ID: "empty", Prompt: "  "}); err == nil {
		t.Fatal("empty prompt accepted")
	}
}

func TestSupervisorSendLiveControlSteersNativeSteerers(t *testing.T) {
	target := &supervisorHarness{name: "codex", followUp: FollowUpSteer, active: map[string]bool{}, emits: map[string]core.Emit{}}
	registry := NewRegistry()
	registry.Register("codex", func(HarnessConfig) (Harness, error) { return target, nil })
	supervisor := NewSupervisor(registry, HarnessConfig{Name: "codex"})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := supervisor.Send(context.Background(), "job", "original", nil); err != nil {
		t.Fatal(err)
	}
	result, err := supervisor.SendLiveControl(context.Background(), "job", ControlRequest{ID: fmt.Sprintf("steer-%d", 1), Prompt: "steer this turn"})
	if err != nil || result.Queued {
		t.Fatalf("native steer result = %#v, %v", result, err)
	}
	target.mu.Lock()
	prompts := append([]string(nil), target.prompts["job"]...)
	target.mu.Unlock()
	if len(prompts) != 2 || !strings.Contains(prompts[1], "steer this turn") {
		t.Fatalf("steer prompts = %#v", prompts)
	}
}
