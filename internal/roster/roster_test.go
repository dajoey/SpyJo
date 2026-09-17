package roster

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sample = `
staff:
  implementer: {runner: opencode}
  thinker: {runner: pi}
  principal:
    runner: claude
    args: ["--model", "opus", "--effort", "xhigh"]
    daily_cap: 2
    fallback: thinker
roles:
  task_implementation: implementer
  task_review: thinker
  goal_planning: principal
escalation: {after_attempt: 3, staff: principal}
`

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadMissingIsLegacy(t *testing.T) {
	r, err := Load(t.TempDir())
	if r != nil || err != nil {
		t.Fatalf("got %v, %v", r, err)
	}
}

func TestLoadRejectsBadRosters(t *testing.T) {
	for name, body := range map[string]string{
		"no staff":         "roles: {}",
		"unknown role":     "staff: {a: {runner: pi}}\nroles: {task_review: b}",
		"unknown fallback": "staff: {a: {runner: pi, fallback: b}}",
		"cap no fallback":  "staff: {a: {runner: pi, daily_cap: 1}}",
		"bad runner":       "staff: {a: {runner: \"pi; rm\"}}",
		"bad escalation":   "staff: {a: {runner: pi}}\nescalation: {after_attempt: 0, staff: a}",
	} {
		if _, err := Load(write(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestModelAndExpand(t *testing.T) {
	dir := write(t, sample)
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Model("implementer"); got != "opencode" {
		t.Fatalf("plain staff model = %q", got)
	}
	if got := r.Model("principal"); got != "@principal" {
		t.Fatalf("staff with args model = %q", got)
	}
	kind, args, err := Expand(dir, "@principal")
	if err != nil || kind != "claude" || len(args) != 4 || args[1] != "opus" {
		t.Fatalf("expand = %q %v %v", kind, args, err)
	}
	if kind, args, err := Expand(dir, "kimi"); kind != "kimi" || args != nil || err != nil {
		t.Fatalf("passthrough = %q %v %v", kind, args, err)
	}
	if _, _, err := Expand(dir, "@nobody"); err == nil {
		t.Fatal("unknown staff must fail")
	}
}

func TestAssignCapsCountOncePerSessionAndResetDaily(t *testing.T) {
	dir := write(t, sample)
	r, _ := Load(dir)
	day := time.Date(2026, 9, 17, 10, 0, 0, 0, time.Local)
	for _, step := range []struct{ key, want string }{
		{"s1", "principal"},
		{"s1", "principal"}, // retry of the same session is not a second use
		{"s2", "principal"},
		{"s3", "thinker"}, // cap of 2 reached
		{"s1", "principal"},
	} {
		got, err := r.Assign(dir, "principal", step.key, day)
		if err != nil || got != step.want {
			t.Fatalf("%s: got %q, %v; want %q", step.key, got, err, step.want)
		}
	}
	if got, _ := r.Assign(dir, "principal", "s4", day.Add(24*time.Hour)); got != "principal" {
		t.Fatalf("next day = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime", usageFileName)); err != nil {
		t.Fatal(err)
	}
}
