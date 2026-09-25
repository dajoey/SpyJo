package app

import (
	"strings"
	"testing"
)

func TestSurgicallyUpdateFrontMatterField_ExistingKey(t *testing.T) {
	orig := `---
# Header comment
attempt: 1
created_at: "2026-09-25T10:04:25Z"
staff: cursor # legacy fallback
title: "Some task title"
completion_summary:
    verdict: completed
    outcome: "Multi-line summary block
with indentation"
updated_at: "2026-09-25T10:04:25Z"
---

# Body
Some body text that must be untouched.
`
	updated, err := SurgicallyUpdateFrontMatterField(orig, "staff", "agy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify staff line changed
	if !strings.Contains(updated, "staff: agy") {
		t.Errorf("expected 'staff: agy', got: %s", updated)
	}
	if strings.Contains(updated, "staff: cursor") {
		t.Errorf("expected old staff to be replaced, got: %s", updated)
	}

	// Verify comments, multi-line values, and body are byte-identical
	beforeStaff := strings.Split(orig, "staff: cursor")[0]
	afterStaff := strings.Split(updated, "staff: agy")[0]
	if beforeStaff != afterStaff {
		t.Errorf("content before staff differed:\nwant: %q\ngot:  %q", beforeStaff, afterStaff)
	}

	afterOrigStaff := strings.Split(orig, "staff: cursor # legacy fallback")[1]
	afterNewStaff := strings.Split(updated, "staff: agy")[1]
	if afterOrigStaff != afterNewStaff {
		t.Errorf("content after staff differed:\nwant: %q\ngot:  %q", afterOrigStaff, afterNewStaff)
	}
}

func TestSurgicallyUpdateFrontMatterField_NewKey(t *testing.T) {
	orig := `---
attempt: 1
status: working
---

# Body
`
	updated, err := SurgicallyUpdateFrontMatterField(orig, "staff", "implementer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(updated, "staff: implementer\n") {
		t.Errorf("expected 'staff: implementer', got: %s", updated)
	}
	if !strings.HasPrefix(updated, "---\nattempt: 1\nstatus: working\n") {
		t.Errorf("original front matter prefix was modified: %s", updated)
	}
	if !strings.HasSuffix(updated, "---\n\n# Body\n") {
		t.Errorf("original body suffix was modified: %s", updated)
	}
}

func TestSurgicallyDeleteFrontMatterField(t *testing.T) {
	orig := `---
attempt: 1
status: waiting
wake_at: "2026-09-25T14:00:00Z"
waiting_for: "scheduled wakeup"
title: "Waiting task"
---

# Body
`
	updated, err := SurgicallyDeleteFrontMatterField(orig, "wake_at")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(updated, "wake_at:") {
		t.Errorf("wake_at was not deleted: %s", updated)
	}
	expected := `---
attempt: 1
status: waiting
waiting_for: "scheduled wakeup"
title: "Waiting task"
---

# Body
`
	if updated != expected {
		t.Errorf("content differed:\nwant: %q\ngot:  %q", expected, updated)
	}
}

func TestSurgicallyUpdateYAMLKey_CommentPreservingRoster(t *testing.T) {
	rosterSnippet := `# SpyJo staffing — the ONE place routing lives.
# Read fresh at every dispatch: edit, save, done (no restart).

staff:
  implementer:
    fallback: cursor
    runner: agy
    args: ["--model", "gemini-3.8-flash-high"]
    note: >-
      Routine code, tests, bug fixes, local edits;
      the default for unstamped work.
  cursor:
    runner: cursor
    args: ["--model", "auto"] # Joey 2026-09-17: auto
`
	// Test updating runner under cursor
	updated, err := SurgicallyUpdateYAMLField(rosterSnippet, []string{"staff", "cursor", "runner"}, "opencode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(updated, "runner: opencode") {
		t.Errorf("expected runner: opencode, got: %s", updated)
	}
	// Verify comments and note block are byte-identical
	if !strings.Contains(updated, "# SpyJo staffing — the ONE place routing lives.") {
		t.Errorf("header comment lost")
	}
	if !strings.Contains(updated, "note: >-\n      Routine code, tests, bug fixes, local edits;\n      the default for unstamped work.") {
		t.Errorf("note block changed")
	}
	if !strings.Contains(updated, "# Joey 2026-09-17: auto") {
		t.Errorf("inline comment lost")
	}
}
