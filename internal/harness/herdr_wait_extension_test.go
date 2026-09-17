package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const herdrTimeoutOut = `{"error":{"code":"timeout","message":"timed out waiting for agent status"},"id":"cli:agent:prompt"}`

func agentGet(status string) []byte {
	return []byte(`{"result":{"agent":{"agent_status":"` + status + `"}}}`)
}

type scriptedHerdr struct {
	calls []string
	get   []string // successive agent_status values; "" means the get fails
	wait  []error  // successive wait results
}

func (s *scriptedHerdr) run(_ context.Context, args ...string) ([]byte, error) {
	s.calls = append(s.calls, strings.Join(args[:2], " "))
	switch args[1] {
	case "get":
		status := s.get[0]
		s.get = s.get[1:]
		if status == "" {
			return nil, errors.New("agent not found")
		}
		return agentGet(status), nil
	case "wait":
		err := s.wait[0]
		s.wait = s.wait[1:]
		if err != nil {
			return []byte(herdrTimeoutOut), err
		}
		return []byte(`{"result":{}}`), nil
	}
	return nil, errors.New("unexpected")
}

func TestExtendHerdrWait(t *testing.T) {
	original := errors.New("exit status 1")
	for _, test := range []struct {
		name      string
		get       []string
		wait      []error
		ceiling   time.Duration
		wantErr   bool
		wantCalls []string
	}{
		{name: "still working then finishes", get: []string{"working"}, wait: []error{nil}, ceiling: time.Hour,
			wantCalls: []string{"agent get", "agent wait"}},
		{name: "second wait times out then agent is done", get: []string{"working", "done"}, wait: []error{original}, ceiling: time.Hour,
			wantCalls: []string{"agent get", "agent wait", "agent get"}},
		{name: "finished right at the timeout", get: []string{"idle"}, ceiling: time.Hour,
			wantCalls: []string{"agent get"}},
		{name: "blocked keeps the original failure", get: []string{"blocked"}, ceiling: time.Hour, wantErr: true,
			wantCalls: []string{"agent get"}},
		{name: "vanished pane keeps the original failure", get: []string{""}, ceiling: time.Hour, wantErr: true,
			wantCalls: []string{"agent get"}},
		{name: "ceiling reached", get: []string{"working"}, ceiling: 15 * time.Minute, wantErr: true,
			wantCalls: []string{"agent get"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &scriptedHerdr{get: test.get, wait: test.wait}
			var notes []string
			_, err := extendHerdrWait(context.Background(), "sj-t-a1-x", []byte(herdrTimeoutOut), original, test.ceiling, fake.run,
				func(text string) { notes = append(notes, text) })
			if (err != nil) != test.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, test.wantErr)
			}
			if strings.Join(fake.calls, ",") != strings.Join(test.wantCalls, ",") {
				t.Fatalf("calls = %v, want %v", fake.calls, test.wantCalls)
			}
			if strings.Contains(strings.Join(test.wantCalls, ","), "agent wait") && len(notes) == 0 {
				t.Fatal("extension was not surfaced as a status event")
			}
		})
	}
	if !isHerdrWaitTimeout(herdrTimeoutOut) || isHerdrWaitTimeout(`{"error":{"code":"agent_not_running"}}`) {
		t.Fatal("timeout detection is wrong")
	}
}
