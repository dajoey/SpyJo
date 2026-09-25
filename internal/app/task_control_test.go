package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agent0ai/spynel/internal/config"
	"github.com/agent0ai/spynel/internal/core"
	"github.com/agent0ai/spynel/internal/harness"
	"github.com/agent0ai/spynel/internal/orchestrator"
	"github.com/agent0ai/spynel/internal/workspace"
)

// taskControlHarness records every control primitive the task-control route
// can reach: queued control sends, live (interrupt) deliveries, queued-control
// withdrawals, and job interrupts.
type taskControlHarness struct {
	*heldServiceHarness
	controlRequests   []harness.ControlRequest
	controlResult     harness.ControlResult
	controlErr        error
	liveRequests      []harness.ControlRequest
	liveErr           error
	cancelledControls []string
}

func (h *taskControlHarness) SendControl(_ context.Context, _ string, request harness.ControlRequest) (harness.ControlResult, error) {
	h.controlRequests = append(h.controlRequests, request)
	return h.controlResult, h.controlErr
}

func (h *taskControlHarness) SendLiveControl(_ context.Context, _ string, request harness.ControlRequest) (harness.ControlResult, error) {
	h.liveRequests = append(h.liveRequests, request)
	return harness.ControlResult{}, h.liveErr
}

func (h *taskControlHarness) CancelQueuedControl(_ string, id string) bool {
	if id == "" {
		return false
	}
	for _, known := range h.cancelledControls {
		if known == id {
			return false
		}
	}
	h.cancelledControls = append(h.cancelledControls, id)
	return true
}

func newTaskControlService(t *testing.T) (*Service, *taskControlHarness) {
	t.Helper()
	root := t.TempDir()
	if err := workspace.Init(root, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.PathForRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	target := &taskControlHarness{heldServiceHarness: newHeldServiceHarness(), controlResult: harness.ControlResult{Queued: true}}
	service := New(cfg, target)
	return service, target
}

// claimTaskForControl stages a claimed task exactly like a live dispatch:
// document in working/, a lease naming the canonical orchestrator session key,
// and a live durable job bound to that session.
func claimTaskForControl(t *testing.T, service *Service, taskID string, attempt int, frontMatter map[string]any) (string, orchestrator.Lease) {
	t.Helper()
	path := filepath.Join(service.Config.StatePath("tasks", "working"), taskID+".md")
	if frontMatter == nil {
		frontMatter = map[string]any{}
	}
	frontMatter["id"] = taskID
	if _, ok := frontMatter["status"]; !ok {
		frontMatter["status"] = "working"
	}
	if _, ok := frontMatter["title"]; !ok {
		frontMatter["title"] = "Task control fixture"
	}
	if _, ok := frontMatter["created_at"]; !ok {
		frontMatter["created_at"] = "2026-09-25T08:00:00Z"
	}
	if _, ok := frontMatter["updated_at"]; !ok {
		frontMatter["updated_at"] = "2026-09-25T08:00:00Z"
	}
	if _, ok := frontMatter["review_required"]; !ok {
		frontMatter["review_required"] = true
	}
	if _, ok := frontMatter["attempt"]; !ok {
		frontMatter["attempt"] = attempt
	}
	document := orchestrator.Document{FrontMatter: frontMatter, Body: "# Task\n\n## Progress\n\n- claimed\n"}
	if err := orchestrator.WriteDocument(path, document); err != nil {
		t.Fatal(err)
	}
	lease := orchestrator.Lease{
		ID: "lease-" + taskID, OwnerID: "owner-" + taskID,
		SessionKey: fmt.Sprintf("orchestrator:tasks:task_implementation:%s:%d", taskID, attempt),
		File:       path, Route: "tasks", State: "processing", Phase: "task_implementation",
		StartedAt: time.Now().UTC(), HeartbeatAt: time.Now().UTC(), ClaimAttempt: attempt,
	}
	writeJobLease(t, service, lease)
	service.Runtime.BeginJobWithDetails(lease.SessionKey, "orchestrator", "markdown", taskID+".md", JobDetails{
		Kind: "task", Route: "tasks", DurableFile: path,
	})
	return path, lease
}

func taskControlCode(err error) string {
	if err == nil {
		return ""
	}
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	return ""
}

func TestTaskControlResolvesLiveJobByTaskIDWithStrongIdentityChecks(t *testing.T) {
	service, _ := newTaskControlService(t)
	taskID := "tasks-20260925-control-demo"
	path, _ := claimTaskForControl(t, service, taskID, 2, nil)
	jobID := service.Runtime.Jobs()[0].ID
	service.Runtime.RecordJobEvent(jobID, core.Event{Kind: core.EventStatus, Text: "working hard", Done: true})

	result, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "output", Tail: 4096})
	if err != nil {
		t.Fatalf("output control failed: %v", err)
	}
	if !result.OK || result.Job != service.Runtime.Jobs()[0].Number || !strings.Contains(result.Output, "working hard") {
		t.Fatalf("output result = %#v", result)
	}

	// Unknown id resolves to a routable not_found.
	if _, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: "tasks-nope", Action: "output"}); taskControlCode(err) != "not_found" {
		t.Fatalf("unknown task error = %v code=%q", err, taskControlCode(err))
	}

	// A job whose durable document names a different id is never resolved:
	// the strong identity check must reject session-key matches alone.
	if err := os.WriteFile(path, []byte("---\nid: tasks-someone-else\nstatus: working\ntitle: swap\n---\n# Swapped\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "output"}); taskControlCode(err) != "not_found" {
		t.Fatalf("swapped identity error = %v code=%q", err, taskControlCode(err))
	}

	// Terminal execution states are not steerable.
	if err := os.WriteFile(path, []byte("---\nid: "+taskID+"\nstatus: working\ntitle: fixture\n---\n# Task\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service.Runtime.UpdateJob(jobID, core.ExecutionStatus{State: string(JobAwaitingTransition)})
	if _, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "output"}); taskControlCode(err) != "not_steerable" {
		t.Fatalf("terminal error = %v code=%q", err, taskControlCode(err))
	}
}

func TestTaskControlResolutionIgnoresNonTaskJobsAndNotificationOrigins(t *testing.T) {
	service, target := newTaskControlService(t)
	taskID := "tasks-20260925-control-cross"
	_, lease := claimTaskForControl(t, service, taskID, 1, map[string]any{
		"notify": map[string]any{"enabled": true, "origin": "telegram/TG-private", "on": []any{"done"}},
	})
	// A goal job and a chat job share the task id inside their session keys but
	// must never resolve: control admission is kind-based, with no conversation,
	// channel, or notification-origin filtering of the resolved task job itself.
	service.Runtime.BeginJobWithDetails(fmt.Sprintf("orchestrator:goals:goal_planning:%s:1", taskID), "orchestrator", "markdown", taskID+".md", JobDetails{Kind: "goal", Route: "goals"})
	service.Runtime.BeginJob("chat:tui:"+taskID, "tui", "local", "question")

	result, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "message", Text: "cross-origin control"})
	if err != nil {
		t.Fatalf("control failed: %v", err)
	}
	if !result.Queued || result.ControlID == "" || len(target.controlRequests) != 1 {
		t.Fatalf("cross-origin result = %#v requests=%d", result, len(target.controlRequests))
	}
	if session := service.Runtime.Jobs()[0].SessionKey; session != lease.SessionKey {
		t.Fatalf("resolved session %q, want the task job %q", session, lease.SessionKey)
	}
}

func TestTaskControlMessageQueuesWithAuditableControlIDAndEnvelope(t *testing.T) {
	service, target := newTaskControlService(t)
	taskID := "tasks-20260925-control-msg"
	claimTaskForControl(t, service, taskID, 1, nil)

	result, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "message", Text: "prioritise the failing test first"})
	if err != nil {
		t.Fatalf("message control failed: %v", err)
	}
	if !result.Queued || result.ControlID == "" {
		t.Fatalf("message result = %#v", result)
	}
	if len(target.controlRequests) != 1 {
		t.Fatalf("control requests = %d", len(target.controlRequests))
	}
	request := target.controlRequests[0]
	for _, want := range []string{
		"nonterminal operator coordination",
		`encoding="json"`,
		"prioritise the failing test first",
		"quote it verbatim",
	} {
		if !strings.Contains(request.Prompt, want) {
			t.Fatalf("control prompt missing %q:\n%s", want, request.Prompt)
		}
	}
	if request.ContinuationPrompt == "" || request.Validate == nil || request.PrepareContinuation == nil || request.ReserveProviderTurn == nil {
		t.Fatalf("continuation guards missing: %#v", request)
	}

	// The queued envelope must instruct an auditable verbatim Progress quote.
	if !strings.Contains(request.Prompt, "## Progress") {
		t.Fatalf("prompt does not point at the durable progress log:\n%s", request.Prompt)
	}
}

func TestTaskControlInterruptDeliversIntoLiveSessionNow(t *testing.T) {
	service, target := newTaskControlService(t)
	taskID := "tasks-20260925-control-int"
	claimTaskForControl(t, service, taskID, 3, nil)

	result, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "interrupt", Text: "change of plan: ship the small fix"})
	if err != nil {
		t.Fatalf("interrupt control failed: %v", err)
	}
	if !result.Delivered || result.Queued {
		t.Fatalf("interrupt result = %#v", result)
	}
	if len(target.liveRequests) != 1 || len(target.controlRequests) != 0 {
		t.Fatalf("live=%d queued=%d", len(target.liveRequests), len(target.controlRequests))
	}
	if !strings.Contains(target.liveRequests[0].Prompt, "change of plan") {
		t.Fatalf("live prompt missing text:\n%s", target.liveRequests[0].Prompt)
	}

	// A harness without any live delivery capability reports a routable error.
	root := t.TempDir()
	if err := workspace.Init(root, false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(config.PathForRoot(root))
	bare := New(cfg, newHeldServiceHarness())
	otherID := "tasks-20260925-control-bare"
	claimTaskForControl(t, bare, otherID, 1, nil)
	_, err = bare.TaskControl(context.Background(), TaskControlRequest{TaskID: otherID, Action: "interrupt", Text: "hello"})
	if taskControlCode(err) != "unsupported" {
		t.Fatalf("bare harness error = %v code=%q", err, taskControlCode(err))
	}
}

func TestTaskControlCancelWithdrawsQueuedMessageByID(t *testing.T) {
	service, _ := newTaskControlService(t)
	taskID := "tasks-20260925-control-cancel"
	claimTaskForControl(t, service, taskID, 1, nil)

	first, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "message", Text: "first thought"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "cancel-queued", ControlID: first.ControlID})
	if err != nil || !result.Cancelled {
		t.Fatalf("cancel result = %#v err = %v", result, err)
	}
	again, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "cancel-queued", ControlID: first.ControlID})
	if err != nil || again.Cancelled {
		t.Fatalf("double cancel result = %#v err = %v", again, err)
	}
}

func TestTaskControlOutputIsBounded(t *testing.T) {
	service, _ := newTaskControlService(t)
	taskID := "tasks-20260925-control-out"
	claimTaskForControl(t, service, taskID, 1, nil)
	jobID := service.Runtime.Jobs()[0].ID
	service.Runtime.RecordJobEvent(jobID, core.Event{Kind: core.EventStatus, Text: strings.Repeat("x", 4096)})

	result, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "output", Tail: 512})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Output) > 4096 || result.State == "" {
		t.Fatalf("bounded output = %d bytes state=%q", len(result.Output), result.State)
	}
	if _, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "output", Tail: 1 << 20}); taskControlCode(err) != "bad_request" {
		t.Fatalf("oversized tail error = %v code=%q", err, taskControlCode(err))
	}
}

func TestTaskControlStopStopsTheRunWithoutTouchingTheDocument(t *testing.T) {
	service, target := newTaskControlService(t)
	taskID := "tasks-20260925-control-stop"
	path, lease := claimTaskForControl(t, service, taskID, 1, nil)
	target.active[lease.SessionKey] = true

	result, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: "stop"})
	if err != nil {
		t.Fatalf("stop control failed: %v", err)
	}
	if !result.Stopped {
		t.Fatalf("stop result = %#v", result)
	}
	if _, still := service.Runtime.Job(service.Runtime.Jobs()[0].ID); false {
		_ = still
	}
	if data, err := os.ReadFile(path); err != nil || !strings.Contains(string(data), "status: working") {
		t.Fatalf("stop mutated the document: err=%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("document left working/: %v", err)
	}
}

func settleFixture(t *testing.T, action string) (string, orchestrator.Document) {
	t.Helper()
	service, target := newTaskControlService(t)
	taskID := "tasks-20260925-control-" + action
	path, lease := claimTaskForControl(t, service, taskID, 2, nil)
	target.active[lease.SessionKey] = true

	result, err := service.TaskControl(context.Background(), TaskControlRequest{TaskID: taskID, Action: action})
	if err != nil {
		t.Fatalf("%s control failed: %v", action, err)
	}
	if !result.Stopped {
		t.Fatalf("%s result = %#v", action, result)
	}
	settled := filepath.Join(filepath.Dir(filepath.Dir(path)), result.State, filepath.Base(path))
	document, err := orchestrator.ReadDocument(settled)
	if err != nil {
		t.Fatalf("%s settled document unreadable at %s: %v", action, settled, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s left the claimed document in working/: %v", action, err)
	}
	return settled, document
}

func TestTaskControlStopDoneSettlesCheckerAcceptedCompletion(t *testing.T) {
	settled, document := settleFixture(t, "stop-done")

	if status, _ := document.FrontMatter["status"].(string); status != "done" {
		t.Fatalf("settled status = %q", status)
	}
	summary, ok := document.FrontMatter["completion_summary"].(map[string]any)
	if !ok {
		t.Fatalf("no completion summary: %#v", document.FrontMatter["completion_summary"])
	}
	if verdict, _ := summary["verdict"].(string); verdict != "completed" {
		t.Fatalf("verdict = %#v", summary["verdict"])
	}
	updated, _ := document.FrontMatter["updated_at"].(string)
	completed, _ := summary["completed_at"].(string)
	if updated == "" || completed != updated {
		t.Fatalf("completed_at %q must equal updated_at %q", completed, updated)
	}
	for _, key := range []string{"outcome", "evidence", "uncertainty"} {
		if value, _ := summary[key].(string); strings.TrimSpace(value) == "" {
			t.Fatalf("summary.%s is empty: %#v", key, summary[key])
		}
	}
	progress := document.Body
	if !strings.Contains(progress, "Operator control (Helm web conversation)") || !strings.Contains(progress, "Stop and done") {
		t.Fatalf("progress lacks the operator authority line:\n%s", progress)
	}
	if review, ok := document.FrontMatter["review_required"].(bool); ok && review {
		t.Fatalf("operator done settle left review_required true: %#v", document.FrontMatter["review_required"])
	}
	if !strings.Contains(progress, "independent-review requirement") {
		t.Fatalf("progress lacks the recorded authority for clearing review:\n%s", progress)
	}
	_ = settled
}

func TestTaskControlStopCancelDocumentsOperatorAuthority(t *testing.T) {
	settled, document := settleFixture(t, "stop-cancel")

	if status, _ := document.FrontMatter["status"].(string); status != "cancelled" {
		t.Fatalf("settled status = %q", status)
	}
	summary, ok := document.FrontMatter["completion_summary"].(map[string]any)
	if !ok {
		t.Fatalf("no completion summary: %#v", document.FrontMatter["completion_summary"])
	}
	if verdict, _ := summary["verdict"].(string); verdict != "cancelled" {
		t.Fatalf("verdict = %#v", summary["verdict"])
	}
	if !strings.Contains(document.Body, "Stop and cancel") || !strings.Contains(document.Body, "Authority") {
		t.Fatalf("progress lacks the cancellation authority:\n%s", document.Body)
	}
	_ = settled
}

func TestTaskControlValidatesPayloadBounds(t *testing.T) {
	service, _ := newTaskControlService(t)
	taskID := "tasks-20260925-control-bounds"
	claimTaskForControl(t, service, taskID, 1, nil)

	for _, request := range []TaskControlRequest{
		{Action: "output"},
		{TaskID: taskID},
		{TaskID: taskID, Action: "explode"},
		{TaskID: strings.Repeat("t", 200), Action: "output"},
		{TaskID: taskID, Action: "message", Text: strings.Repeat("x", 9000)},
		{TaskID: taskID, Action: "interrupt", Text: "   "},
	} {
		if _, err := service.TaskControl(context.Background(), request); taskControlCode(err) != "bad_request" {
			t.Fatalf("request %#v error = %v code=%q", request, err, taskControlCode(err))
		}
	}
}
