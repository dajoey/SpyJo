package harness

import (
	"testing"
	"time"
)

func TestHerdrStatusIntervalKeepsChatLiveAndQuietsOrchestratorWorkers(t *testing.T) {
	if got := herdrStatusInterval("orchestrator:tasks:task_implementation:t-1:1"); got != 30*time.Second {
		t.Fatalf("orchestrator worker interval = %s, want 30s", got)
	}
	if got := herdrStatusInterval("chat:telegram:TG-1"); got != 2*time.Second {
		t.Fatalf("chat interval = %s, want 2s", got)
	}
}
