package localapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agent0ai/spynel/internal/app"
	"github.com/agent0ai/spynel/internal/core"
	"github.com/agent0ai/spynel/internal/history"
)

// B2 (phase 4 "Front desk"): the web channel is admitted by the same local
// operator boundary as cli/tui, and web-channel conversations' committed
// records are readable through the events/snapshot routes with an explicit
// channel parameter while default (cli) behavior stays byte-identical.
func TestValidateLocalMessageWebChannelAdmission(t *testing.T) {
	web := core.Message{Channel: "web", Conversation: "frontdesk.abc123", Text: "hello", SourceMessageID: "web-frontdesk-1"}
	if err := app.ValidateLocalMessage(web); err != nil {
		t.Fatalf("web channel rejected: %v", err)
	}
	for _, channel := range []string{"cli", "tui"} {
		message := web
		message.Channel = channel
		if err := app.ValidateLocalMessage(message); err != nil {
			t.Fatalf("%s channel rejected: %v", channel, err)
		}
	}
	for _, channel := range []string{"telegram", "whatsapp", "webx", ""} {
		message := web
		message.Channel = channel
		if err := app.ValidateLocalMessage(message); err == nil {
			t.Fatalf("remote/unknown channel %q admitted by the local boundary", channel)
		}
	}
}

func postMessage(t *testing.T, server *Server, token string, body string) (int, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/message", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	server.message(recorder, request)
	response, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatal(err)
	}
	return recorder.Code, string(response)
}

func TestWebMessageRoundTripAndDuplicateFence(t *testing.T) {
	_, server, _, cancel, done := startTestServer(t, t.TempDir())
	defer func() { cancel(); <-done; _ = server.Service.Close() }()
	token := server.Token
	code, body := postMessage(t, server, token,
		`{"channel":"web","conversation":"frontdesk.round1","sender":"Joey","source_message_id":"web-frontdesk-r1","text":"hello front desk"}`)
	if code != http.StatusOK {
		t.Fatalf("web message rejected: %d %s", code, body)
	}
	if !strings.Contains(body, "reply for chat:web:frontdesk.round1") {
		t.Fatalf("web message stream missed the reply: %s", body)
	}
	time.Sleep(50 * time.Millisecond)
	// Committed records must live under the web channel's own history.
	events, _, err := server.Service.History.EventSnapshot("web", "frontdesk.round1")
	if err != nil || len(events) == 0 {
		t.Fatalf("web history snapshot: %#v %v", events, err)
	}
	if events[0].Kind != "user" || events[0].Text != "hello front desk" {
		t.Fatalf("web user record wrong: %#v", events[0])
	}
	terminal := events[len(events)-1]
	if terminal.Kind != "assistant" || !terminal.Done {
		t.Fatalf("web terminal record wrong: %#v", terminal)
	}
	// The same source identity retried must be fenced, not double-dispatched.
	code, body = postMessage(t, server, token,
		`{"channel":"web","conversation":"frontdesk.round1","sender":"Joey","source_message_id":"web-frontdesk-r1","text":"hello front desk"}`)
	if code != http.StatusOK || !strings.Contains(body, "duplicate_request") {
		t.Fatalf("web duplicate not fenced: %d %s", code, body)
	}
}

func TestEventsAndConversationAcceptChannelParameter(t *testing.T) {
	_, server, _, cancel, done := startTestServer(t, t.TempDir())
	defer func() { cancel(); <-done; _ = server.Service.Close() }()
	appendHistory := func(channel, conversation, role, content string) {
		if _, err := server.Service.History.Append(channel, conversation, history.Entry{
			At: time.Now().UTC(), Role: role, Content: content, AcceptedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	appendHistory("cli", "frontdesk.shared", "user", "cli side")
	appendHistory("web", "frontdesk.shared", "user", "web side")

	request := httptest.NewRequest(http.MethodGet, "/v1/conversation?conversation=frontdesk.shared&channel=web", nil)
	recorder := httptest.NewRecorder()
	server.conversation(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("web channel snapshot: %d %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "web side") || strings.Contains(recorder.Body.String(), "cli side") {
		t.Fatalf("channel=web snapshot returned the wrong channel: %s", recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/conversation?conversation=frontdesk.shared", nil)
	recorder = httptest.NewRecorder()
	server.conversation(recorder, request)
	if !strings.Contains(recorder.Body.String(), "cli side") || strings.Contains(recorder.Body.String(), "web side") {
		t.Fatalf("default snapshot must stay cli: %s", recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/events?conversation=frontdesk.shared&channel=web", nil)
	recorder = httptest.NewRecorder()
	ctx, cancelEvents := context.WithCancel(context.Background())
	*request = *request.WithContext(ctx)
	go func() {
		time.Sleep(150 * time.Millisecond)
		appendHistory("web", "frontdesk.shared", "user", "web live")
		time.Sleep(200 * time.Millisecond)
		cancelEvents()
	}()
	server.events(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "web live") {
		t.Fatalf("events channel=web: %d %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/events?conversation=frontdesk.shared&channel=telegram", nil)
	recorder = httptest.NewRecorder()
	server.events(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("remote channel on events must be a bad request: %d", recorder.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/conversation?conversation=frontdesk.shared&channel=whatsapp", nil)
	recorder = httptest.NewRecorder()
	server.conversation(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("remote channel on conversation must be a bad request: %d", recorder.Code)
	}
}
