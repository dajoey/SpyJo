package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent0ai/spynel/internal/config"
	"github.com/agent0ai/spynel/internal/extensions"
	"github.com/agent0ai/spynel/internal/workspace"
)

// Rework finding 2 (2026-09-25 review): an operator settle of an UNCLAIMED
// task (web review composer) must reach the same terminal machinery a claimed
// settle gets through reconcileTransitions: task.completed extension hooks
// and the notification decision turn.
func TestFinalizeOperatorTaskTransitionDoneRunsHooksAndNotificationDecision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell hook fixture")
	}
	root := t.TempDir()
	if err := workspace.Init(root, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.PathForRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Orchestrator.TaskNotifications = config.TaskNotificationsDecide
	extension := filepath.Join(cfg.Resolve(cfg.Extensions.Directory), "counter")
	if err := os.MkdirAll(extension, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "name: counter\nhooks:\n  task.completed: [\"./hook.sh\"]\n"
	script := "#!/bin/sh\nprintf x >> completed.count\nprintf '%s\\n' '{}'\n"
	if err := os.WriteFile(filepath.Join(extension, extensions.ManifestName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extension, "hook.sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	target := &notificationActionHarness{}
	manager := New(cfg, target, extensions.Runner{Directory: cfg.Resolve(cfg.Extensions.Directory), Timeout: time.Second})
	path := filepath.Join(cfg.StatePath("tasks", "done"), "task-operator-done.md")
	doc := Document{FrontMatter: map[string]any{
		"id": "task-operator-done", "title": "Web approved", "status": "done", "attempt": 1, "review_attempt": 1,
		"notify": map[string]any{"enabled": true, "origin": "tui/local", "on": []any{"done"}},
	}, Body: "# Web approved\n\n## Progress\n\n- staged\n"}
	if err := WriteDocument(path, doc); err != nil {
		t.Fatal(err)
	}

	manager.FinalizeOperatorTaskTransition(context.Background(), path, "task-operator-done", "t-web-1", "done")
	manager.Wait()

	if count, err := os.ReadFile(filepath.Join(extension, "completed.count")); err != nil || string(count) != "x" {
		t.Fatalf("task.completed hook did not run exactly once: %q, %v", count, err)
	}
	if target.calls.Load() != 1 {
		t.Fatalf("notification decision turns = %d, want 1", target.calls.Load())
	}
}

// Recovery-live defect (2026-09-25, web Approve on a scratch task): the web
// control route runs FinalizeOperatorTaskTransition on the HTTP request
// context, which is canceled the moment the response returns. The async
// notification turn inherited that cancellation and died at herdr spawn
// ("context canceled", 92ms) — the claimed-settle path passes the
// orchestrator's long-lived loop context and never hits this. The turn must
// not observe the request's cancellation.
func TestFinalizeOperatorTaskTransitionNotificationSurvivesCanceledRequestContext(t *testing.T) {
	target := &notificationActionHarness{}
	var observed atomic.Value // error observed by the harness turn
	target.action = func(ctx context.Context, _ string) error {
		if err := ctx.Err(); err != nil {
			observed.Store(err.Error())
		}
		return ctx.Err()
	}
	manager, path := notificationTestManager(t, config.TaskNotificationsDecide, target)

	reqCtx, cancel := context.WithCancel(context.Background())
	manager.FinalizeOperatorTaskTransition(reqCtx, path, "task-1", "t-web-1", "done")
	cancel() // the HTTP request is gone the moment the control response returns
	manager.Wait()

	if target.calls.Load() != 1 {
		t.Fatalf("notification decision turns = %d, want 1", target.calls.Load())
	}
	if s, _ := observed.Load().(string); s != "" {
		t.Fatalf("notification turn observed cancellation from the dead request context: %s", s)
	}
}

func TestFinalizeOperatorTaskTransitionTodoStampsCodeManagedReworkOnly(t *testing.T) {
	target := &notificationActionHarness{}
	manager, path := notificationTestManager(t, config.TaskNotificationsDecide, target)
	// notificationTestManager stages done/; restage as todo/ with a rejected summary.
	doc, err := ReadDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	doc.FrontMatter["status"] = "todo"
	doc.FrontMatter["review_attempt"] = 2
	doc.FrontMatter["completion_summary"] = map[string]any{
		"verdict": "rejected", "outcome": "operator requested changes", "reviewed_at": "2026-09-25T11:30:00Z",
	}
	todoPath := filepath.Join(filepath.Dir(filepath.Dir(path)), "todo", filepath.Base(path))
	if err := WriteDocument(todoPath, doc); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)

	manager.FinalizeOperatorTaskTransition(context.Background(), todoPath, "task-1", "", "todo")
	manager.Wait()

	after, err := ReadDocument(todoPath)
	if err != nil {
		t.Fatal(err)
	}
	summary, _ := after.FrontMatter["completion_summary"].(map[string]any)
	if rework, ok := summary["rework_count"].(int); !ok || rework != 2 {
		t.Errorf("rework_count = %#v, want 2 (derived from review_attempt)", summary["rework_count"])
	}
	if target.calls.Load() != 0 {
		t.Errorf("todo transition dispatched %d notification turns, want 0", target.calls.Load())
	}
}
