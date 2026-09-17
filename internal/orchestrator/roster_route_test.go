package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agent0ai/spynel/internal/config"
	"github.com/agent0ai/spynel/internal/extensions"
	"github.com/agent0ai/spynel/internal/roster"
)

func TestResolveTargetModelThroughRoster(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, config.StateDirectoryName)
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Root: root, Harness: config.Harness{Name: "herdr", Model: "opencode", DeveloperModel: "opencode", ReviewerModel: "pi"}}
	manager := New(cfg, newFakeRecipient(), extensions.Runner{})
	task := func(name, frontMatter string) Lease {
		path := filepath.Join(root, name+".md")
		if err := os.WriteFile(path, []byte("---\nid: "+name+"\n"+frontMatter+"---\n# T\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return Lease{File: path, SessionKey: "orchestrator:tasks:x:" + name + ":1"}
	}
	resolve := func(routeName, phase string, lease Lease) string {
		lease.Phase = phase
		return manager.resolveTargetModel(workflowRoute{Name: routeName}, lease)
	}

	// No roster file: legacy routing is untouched.
	if got := resolve("tasks", phaseTaskReview, task("legacy", "")); got != "pi" {
		t.Fatalf("legacy review = %q", got)
	}

	body := `
staff:
  implementer: {runner: opencode}
  architect: {runner: agy}
  thinker: {runner: pi}
  principal: {runner: claude, args: ["--model", "opus"], daily_cap: 1, fallback: thinker}
roles:
  task_implementation: implementer
  task_review: thinker
  task_review_high_risk: principal
  goal_planning: principal
escalation: {after_attempt: 3, staff: principal}
`
	if err := os.WriteFile(roster.Path(state), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, route, phase, frontMatter, want string }{
		{"role default", "tasks", phaseTaskImplementation, "", "opencode"},
		{"task staff", "tasks", phaseTaskImplementation, "staff: architect\n", "agy"},
		{"staff beats agent", "tasks", phaseTaskImplementation, "staff: architect\nagent: kimi\n", "agy"},
		{"legacy agent pin", "tasks", phaseTaskImplementation, "agent: hermes\n", "hermes"},
		{"unknown staff falls to role", "tasks", phaseTaskImplementation, "staff: nobody\n", "opencode"},
		{"ordinary review", "tasks", phaseTaskReview, "", "pi"},
		{"high-risk review", "tasks", phaseTaskReview, "risk: high\n", "@principal"},
		{"cap reached falls back", "goals", phaseGoalPlanning, "", "pi"},
		{"unmapped role is legacy", "goals", phaseGoalReview, "", "pi"},
		{"escalation over cap falls back", "tasks", phaseTaskImplementation, "attempt: 4\n", "pi"},
	} {
		if got := resolve(test.route, test.phase, task(test.name[:4]+test.phase+test.want, test.frontMatter)); got != test.want {
			t.Errorf("%s: got %q, want %q", test.name, got, test.want)
		}
	}

	// A broken roster is ignored, never fatal.
	if err := os.WriteFile(roster.Path(state), []byte("staff: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolve("tasks", phaseTaskReview, task("broken", "")); got != "pi" {
		t.Fatalf("broken roster review = %q", got)
	}
}
