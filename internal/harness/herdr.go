package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/agent0ai/spynel/internal/core"
	"github.com/agent0ai/spynel/internal/fsx"
)

// Herdr adapts the Herdr terminal workspace manager as a coding harness.
// It can route tasks to warm existing agent panes (Claude, Kimi, Hermes, OpenCode, Codex)
// or dynamically spawn and supervise new agent panes inside Herdr.
type Herdr struct {
	config HarnessConfig

	keyMu    sync.Mutex
	keyLocks map[string]*sync.Mutex
	mu       sync.Mutex
	sessions map[string]string
	active   map[string]*herdrTurn
	closed   bool
}

type herdrTurn struct {
	target    string
	isDynamic bool
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	done      chan struct{}
}

type herdrAgentItem struct {
	Name          string `json:"name"`
	Agent         string `json:"agent"`
	AgentStatus   string `json:"agent_status"`
	PaneID        string `json:"pane_id"`
	WorkspaceID   string `json:"workspace_id"`
	Cwd           string `json:"cwd"`
	TerminalTitle string `json:"terminal_title_stripped"`
	AgentSession  struct {
		Value string `json:"value"`
	} `json:"agent_session"`
}

type herdrAgentListResponse struct {
	Result struct {
		Agents []herdrAgentItem `json:"agents"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type herdrAgentGetResponse struct {
	Result struct {
		Agent herdrAgentItem `json:"agent"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type herdrPaneSplitResponse struct {
	Result struct {
		Pane struct {
			PaneID string `json:"pane_id"`
		} `json:"pane"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewHerdr(cfg HarnessConfig) (*Herdr, error) {
	if cfg.Command == "" {
		cfg.Command = "herdr"
	}
	if cfg.Cwd == "" {
		cfg.Cwd = "."
	}
	h := &Herdr{
		config:   cfg,
		keyLocks: map[string]*sync.Mutex{},
		sessions: map[string]string{},
		active:   map[string]*herdrTurn{},
	}
	_ = h.loadSessions()
	return h, nil
}

func (h *Herdr) Start(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return errors.New("Herdr harness is closed")
	}
	if _, err := exec.LookPath(h.config.Command); err != nil {
		return fmt.Errorf("herdr command %q not found in PATH: %w", h.config.Command, err)
	}
	// Check Herdr server status
	cmd := exec.Command(h.config.Command, "status")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "status: running") {
		return fmt.Errorf("herdr server is not running (output: %s)", strings.TrimSpace(string(out)))
	}
	return nil
}

func (h *Herdr) Available() (bool, string) {
	if _, err := exec.LookPath(h.config.Command); err != nil {
		return false, "herdr binary not found in PATH"
	}
	cmd := exec.Command(h.config.Command, "status")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "status: running") {
		return false, "herdr daemon is not running"
	}
	return true, "Herdr daemon running"
}

func (h *Herdr) ReadyEvents() <-chan struct{} {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	return ch
}

func (h *Herdr) Models(_ context.Context) ([]Model, error) {
	allEfforts := []string{"low", "medium", "high"}
	return []Model{
		{ID: "opencode", DisplayName: "OpenCode", Description: "Dedicated OpenCode worker in Herdr pane (default)", Default: true, Efforts: allEfforts},
		{ID: "claude", DisplayName: "Claude Code", Description: "Dedicated Claude Code worker in Herdr pane", Efforts: allEfforts},
		{ID: "codex", DisplayName: "Codex", Description: "Dedicated Codex worker in Herdr pane", Efforts: allEfforts},
		{ID: "kimi", DisplayName: "Kimi", Description: "Dedicated Kimi CLI worker in Herdr pane", Efforts: allEfforts},
		{ID: "hermes", DisplayName: "Hermes", Description: "Dedicated Hermes worker in Herdr pane", Efforts: allEfforts},
	}, nil
}

func (h *Herdr) SetModel(model string) {
	h.mu.Lock()
	h.config.Model = model
	h.mu.Unlock()
}

func (h *Herdr) SendWithModel(ctx context.Context, key, prompt, model string, emit core.Emit) (string, bool, error) {
	h.mu.Lock()
	cfg := h.config
	cfg.Model = model
	h.mu.Unlock()
	return h.sendInternal(ctx, key, prompt, cfg, emit)
}

func (h *Herdr) Send(ctx context.Context, key, prompt string, emit core.Emit) (string, bool, error) {
	h.mu.Lock()
	cfg := h.config
	h.mu.Unlock()
	return h.sendInternal(ctx, key, prompt, cfg, emit)
}

func (h *Herdr) lockForKey(key string) *sync.Mutex {
	h.keyMu.Lock()
	defer h.keyMu.Unlock()
	lock, ok := h.keyLocks[key]
	if !ok {
		lock = &sync.Mutex{}
		h.keyLocks[key] = lock
	}
	return lock
}

func (h *Herdr) resolveCallerContext(ctx context.Context, cwd string) (string, string) {
	callerPane := os.Getenv("HERDR_PANE_ID")
	workspaceID := os.Getenv("HERDR_WORKSPACE_ID")

	if callerPane != "" && workspaceID != "" {
		return callerPane, workspaceID
	}

	out, err := exec.CommandContext(ctx, h.config.Command, "pane", "current").Output()
	if err == nil {
		var resp struct {
			Result struct {
				Pane struct {
					PaneID      string `json:"pane_id"`
					WorkspaceID string `json:"workspace_id"`
				} `json:"pane"`
			} `json:"result"`
		}
		if json.Unmarshal(out, &resp) == nil && resp.Result.Pane.PaneID != "" {
			if callerPane == "" {
				callerPane = resp.Result.Pane.PaneID
			}
			if workspaceID == "" {
				workspaceID = resp.Result.Pane.WorkspaceID
			}
		}
	}

	if workspaceID == "" {
		workspaceID = "w8"
	}
	return callerPane, workspaceID
}

func (h *Herdr) resolveTarget(ctx context.Context, model string, cwd string) (target string, paneID string, isDynamic bool, threadID string, err error) {
	model = strings.TrimSpace(model)
	if model == "" || model == "default" {
		model = "opencode"
	}

	// 1. Direct explicit pane target (e.g. "w8:p2")
	if strings.Contains(model, ":") {
		return model, model, false, "", nil
	}

	callerPane, workspaceID := h.resolveCallerContext(ctx, cwd)
	expectedWorkerName := fmt.Sprintf("spyjo-%s-worker", model)

	absCwd := cwd
	if abs, err := filepath.Abs(cwd); err == nil {
		absCwd = abs
	}

	// 2. Search for existing dedicated worker agent in this workspace
	cmd := exec.CommandContext(ctx, h.config.Command, "agent", "list")
	out, err := cmd.Output()
	if err == nil {
		var listResp herdrAgentListResponse
		if json.Unmarshal(out, &listResp) == nil {
			for _, a := range listResp.Result.Agents {
				inWorkspace := workspaceID == "" || a.WorkspaceID == workspaceID || strings.HasPrefix(a.PaneID, workspaceID+":")
				isSpyJoWorker := a.Name == expectedWorkerName || (strings.HasPrefix(a.Name, "spyjo-") && strings.HasSuffix(a.Name, "-worker"))

				// STRICT IMMUNITY: Never touch external fleet panes (w1, w2, w4, w5, etc.)!
				if !inWorkspace && !isSpyJoWorker {
					continue
				}

				// If matching worker agent exists in this workspace
				if a.Name == expectedWorkerName && inWorkspace {
					if a.AgentStatus == "blocked" {
						_ = exec.CommandContext(ctx, h.config.Command, "agent", "send-keys", a.Name, "esc").Run()
					}
					return a.Name, a.PaneID, false, a.AgentSession.Value, nil
				}

				// If an old worker with a different model exists in this workspace, clean it up
				if isSpyJoWorker && inWorkspace && a.Name != expectedWorkerName {
					_ = exec.CommandContext(ctx, h.config.Command, "pane", "close", a.PaneID).Run()
				}
			}
		}
	}

	// 3. Spawn a dedicated worker pane via split
	splitArgs := []string{"pane", "split", "--direction", "right", "--ratio", "0.5", "--cwd", absCwd, "--no-focus"}
	if callerPane != "" {
		splitArgs = append(splitArgs, "--pane", callerPane)
	}

	splitCmd := exec.CommandContext(ctx, h.config.Command, splitArgs...)
	splitOut, splitErr := splitCmd.Output()
	if splitErr != nil {
		return "", "", false, "", fmt.Errorf("failed to split dedicated worker pane: %w (out: %s)", splitErr, string(splitOut))
	}

	var splitResp herdrPaneSplitResponse
	if err := json.Unmarshal(splitOut, &splitResp); err != nil || splitResp.Result.Pane.PaneID == "" {
		return "", "", false, "", fmt.Errorf("failed to parse new pane ID from herdr: %s", string(splitOut))
	}
	newPaneID := splitResp.Result.Pane.PaneID

	// 4. Start the agent in the new pane
	startArgs := []string{"agent", "start", expectedWorkerName, "--kind", model, "--pane", newPaneID, "--timeout", "45000"}
	startCmd := exec.CommandContext(ctx, h.config.Command, startArgs...)
	startOut, startErr := startCmd.CombinedOutput()
	if startErr != nil {
		_ = exec.Command(h.config.Command, "pane", "close", newPaneID).Run()
		return "", "", false, "", fmt.Errorf("failed to start agent %s in pane %s: %w (out: %s)", model, newPaneID, startErr, string(startOut))
	}

	getCmd := exec.CommandContext(ctx, h.config.Command, "agent", "get", expectedWorkerName)
	if getOut, getErr := getCmd.Output(); getErr == nil {
		var getResp herdrAgentGetResponse
		if json.Unmarshal(getOut, &getResp) == nil {
			threadID = getResp.Result.Agent.AgentSession.Value
		}
	}
	// Brief settle pause to let agent terminal finish initialization and input binding
	time.Sleep(1500 * time.Millisecond)

	return expectedWorkerName, newPaneID, true, threadID, nil
}

func (h *Herdr) sendInternal(ctx context.Context, key, prompt string, cfg HarnessConfig, emit core.Emit) (string, bool, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", false, errors.New("harness prompt is empty")
	}

	lock := h.lockForKey(key)
	lock.Lock()
	defer lock.Unlock()

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return "", false, errors.New("Herdr harness is closed")
	}
	h.mu.Unlock()

	target, paneID, isDynamic, threadID, err := h.resolveTarget(ctx, cfg.Model, cfg.Cwd)
	if err != nil {
		return "", false, fmt.Errorf("herdr target resolution: %w", err)
	}

	if emit != nil {
		statusMsg := fmt.Sprintf("Herdr: routing work to dedicated worker %s in %s", target, paneID)
		if isDynamic {
			statusMsg = fmt.Sprintf("Herdr: spawned dedicated worker %s in %s (cwd: %s)", target, paneID, cfg.Cwd)
		}
		emit(core.Event{
			Kind: core.EventStatus,
			Text: statusMsg,
		})
	}

	turnContext, cancel := context.WithCancel(ctx)
	turn := &herdrTurn{
		target:    target,
		isDynamic: isDynamic,
		cancel:    cancel,
		done:      make(chan struct{}),
	}

	h.mu.Lock()
	h.active[key] = turn
	h.mu.Unlock()

	defer func() {
		cancel()
		h.mu.Lock()
		delete(h.active, key)
		h.mu.Unlock()
		close(turn.done)
	}()

	// Background status ticker while agent is processing prompt
	streamTicker := time.NewTicker(2 * time.Second)
	streamDone := make(chan struct{})
	go func() {
		defer streamTicker.Stop()
		startTime := time.Now()
		for {
			select {
			case <-streamDone:
				return
			case <-turnContext.Done():
				return
			case <-streamTicker.C:
				elapsed := time.Since(startTime).Truncate(time.Second)
				if emit != nil {
					emit(core.Event{
						Kind: core.EventStatus,
						Text: fmt.Sprintf("Herdr: %s working (%s)...", target, elapsed),
					})
				}
			}
		}
	}()

	// Execute prompt with --wait
	// Default timeout 15 minutes (900000 ms)
	promptCmd := exec.CommandContext(turnContext, h.config.Command, "agent", "prompt", target, prompt, "--wait", "--timeout", "900000")
	turn.cmd = promptCmd
	promptOut, promptErr := promptCmd.CombinedOutput()

	// If prompt stalled due to agent input loop not ready, retry once after a short delay
	if promptErr != nil && strings.Contains(string(promptOut), "agent_prompt_stalled") {
		time.Sleep(2000 * time.Millisecond)
		promptCmd = exec.CommandContext(turnContext, h.config.Command, "agent", "prompt", target, prompt, "--wait", "--timeout", "900000")
		turn.cmd = promptCmd
		promptOut, promptErr = promptCmd.CombinedOutput()
	}

	close(streamDone)

	// Inspect final agent state
	getCmd := exec.Command(h.config.Command, "agent", "get", target)
	getOut, getErr := getCmd.Output()
	var agentStatus string
	var agentKind string
	if getErr == nil {
		var getResp herdrAgentGetResponse
		if json.Unmarshal(getOut, &getResp) == nil {
			agentStatus = getResp.Result.Agent.AgentStatus
			agentKind = getResp.Result.Agent.Agent
			if getResp.Result.Agent.AgentSession.Value != "" {
				threadID = getResp.Result.Agent.AgentSession.Value
			}
		}
	}

	if threadID == "" {
		threadID = fmt.Sprintf("herdr:%s:%d", target, time.Now().Unix())
	}
	h.rememberSession(key, threadID)

	// Fetch and clean final response text
	var finalText string
	if agentKind == "opencode" || strings.HasPrefix(threadID, "ses_") {
		if text, err := extractOpencodeMessage(threadID); err == nil && strings.TrimSpace(text) != "" {
			finalText = text
		}
	}

	if finalText == "" {
		finalOut, _ := exec.Command(h.config.Command, "agent", "read", target, "--source", "recent-unwrapped", "--lines", "250").Output()
		raw := strings.TrimSpace(string(finalOut))
		if raw == "" {
			raw = strings.TrimSpace(string(promptOut))
		}
		finalText = cleanHerdrTerminalOutput(raw)
		if finalText == "" {
			finalText = raw
		}
	}

	// Evaluate completion
	if promptErr != nil {
		errMsg := strings.TrimSpace(string(promptOut))
		if errMsg == "" {
			errMsg = promptErr.Error()
		}
		if agentStatus == "blocked" || strings.Contains(errMsg, "agent_blocked") {
			readOut, _ := exec.Command(h.config.Command, "agent", "read", target, "--source", "recent-unwrapped", "--lines", "30").Output()
			blockedDetail := strings.TrimSpace(string(readOut))
			if blockedDetail == "" {
				blockedDetail = errMsg
			}
			if emit != nil {
				emit(core.Event{
					Kind:      core.EventStatus,
					Text:      fmt.Sprintf("Herdr worker %s is blocked in %s: %s", target, paneID, blockedDetail),
					ThreadID:  threadID,
					Execution: &core.ExecutionStatus{State: "blocked", Detail: blockedDetail},
				})
			}
			return threadID, false, fmt.Errorf("herdr agent %s blocked: %s", target, errMsg)
		}
		if emit != nil {
			emit(core.Event{
				Kind:      core.EventError,
				Text:      errMsg,
				ThreadID:  threadID,
				Done:      true,
				Execution: &core.ExecutionStatus{State: "error", Detail: errMsg},
			})
		}
		return threadID, false, fmt.Errorf("herdr prompt failed: %w (out: %s)", promptErr, errMsg)
	}

	if emit != nil {
		emit(core.Event{
			Kind:      core.EventFinal,
			Text:      finalText,
			FinalText: &finalText,
			ThreadID:  threadID,
			Done:      true,
			Execution: &core.ExecutionStatus{State: "finishing"},
		})
	}

	return threadID, false, nil
}

var (
	herdrFooterPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*▣\s+Build.*$`),
		regexp.MustCompile(`(?m)^\s*╹[▀─=]+.*$`),
		regexp.MustCompile(`(?m)^\s*✻\s+Churned for.*$`),
		regexp.MustCompile(`(?m)^.*•\s+OpenCode.*$`),
		regexp.MustCompile(`(?m)^.*ctrl\+p commands.*$`),
		regexp.MustCompile(`(?m)^\s*●\s+Login expired.*$`),
		regexp.MustCompile(`(?m)^\s*⏵⏵\s+bypass permissions.*$`),
		regexp.MustCompile(`(?m)^\s*Ask When Needed.*$`),
		regexp.MustCompile(`(?m)^\s*context:\s*\d+%.*$`),
		regexp.MustCompile(`(?m)^\s*⚕\s+k\d+.*$`),
		regexp.MustCompile(`(?m)^\s*❯\s+Ask anything.*$`),
		regexp.MustCompile(`(?m)^.*Message (?:Antigravity|agy|SpyJo).*$`),
		regexp.MustCompile(`(?m)^\s*▄▄[↵\^].*$`),
		regexp.MustCompile(`(?m)^\s*───{5,}.*$`),
	}
	herdrThoughtPattern   = regexp.MustCompile(`(?s)(?:^|\n)\s*Thought:\s*[^\n]+\n+(.*?)(?:\n\s*\n\s*([^\s].*)|$)`)
	hermesBoxPattern      = regexp.MustCompile(`(?s)╭─\s*⚕\s*Hermes[^\n]*\n(.*?)\n╰[─]+╯`)
	hermesReasoningBox    = regexp.MustCompile(`(?s)┌─\s*Reasoning[^\n]*\n.*?└[─]+┘\n*`)
	kimiInputBoxPattern   = regexp.MustCompile(`(?s)╭[─]+╮\s*\n\s*│\s*>\s*\n\s*╰[─]+╯`)
	agyInputBoxPattern    = regexp.MustCompile(`(?s)╭─+╮\s*\n\s*│\s*Message (?:Antigravity|agy|SpyJo)[^\n]*\n\s*╰─+╯`)
)

// cleanHerdrTerminalOutput strips prompt echoes, terminal footers, and internal thought blocks
// from raw terminal screen dumps across OpenCode, Kimi, and Hermes agents.
func cleanHerdrTerminalOutput(text string) string {
	// 0. If Hermes formatted response box is present, extract directly
	if m := hermesBoxPattern.FindStringSubmatch(text); len(m) >= 2 && strings.TrimSpace(m[1]) != "" {
		return strings.TrimSpace(m[1])
	}

	// Strip Hermes reasoning boxes if present
	text = hermesReasoningBox.ReplaceAllString(text, "")

	// Strip Kimi bottom input box if present
	text = kimiInputBoxPattern.ReplaceAllString(text, "")

	// Strip Antigravity / SpyJo bottom input box if present
	text = agyInputBoxPattern.ReplaceAllString(text, "")

	lines := strings.Split(text, "\n")
	var cleanedLines []string

	// 1. Strip prompt echo lines (vertical borders or Kimi prompt marker ✨)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "┃") || strings.HasPrefix(trimmed, "│") || strings.HasPrefix(trimmed, "|") {
			continue
		}
		if strings.HasPrefix(trimmed, "✨ ") || strings.HasPrefix(trimmed, "● respond in one sentence") {
			continue
		}
		cleanedLines = append(cleanedLines, line)
	}
	text = strings.TrimSpace(strings.Join(cleanedLines, "\n"))

	// 2. Cut off bottom status chrome
	for _, pat := range herdrFooterPatterns {
		loc := pat.FindStringIndex(text)
		if loc != nil {
			text = strings.TrimSpace(text[:loc[0]])
		}
	}

	// 3. If there is a Thought block followed by an answer, extract the actual answer
	if m := herdrThoughtPattern.FindStringSubmatch(text); m != nil && len(m) >= 3 && strings.TrimSpace(m[2]) != "" {
		text = strings.TrimSpace(m[2])
	}

	return strings.TrimSpace(text)
}

// extractOpencodeMessage queries OpenCode's local database for the latest assistant message in a session.
func extractOpencodeMessage(sessionID string) (string, error) {
	if !strings.HasPrefix(sessionID, "ses_") {
		return "", errors.New("not an opencode session")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dbPath := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		return "", err
	}

	// 1. Find the latest assistant message ID for this session
	msgCmd := exec.Command("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath),
		fmt.Sprintf("SELECT m.id FROM message m WHERE m.session_id = '%s' AND json_extract(m.data, '$.role') = 'assistant' ORDER BY m.time_created DESC LIMIT 1;", sessionID),
	)
	msgOut, err := msgCmd.Output()
	if err != nil {
		return "", fmt.Errorf("sqlite message query: %w", err)
	}
	msgID := strings.TrimSpace(string(msgOut))
	if msgID == "" {
		return "", errors.New("no assistant message found")
	}

	// 2. Fetch all text parts for this message in order
	partCmd := exec.Command("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath),
		fmt.Sprintf("SELECT json_extract(p.data, '$.text') FROM part p WHERE p.message_id = '%s' AND json_extract(p.data, '$.type') = 'text' ORDER BY p.time_created ASC;", msgID),
	)
	partOut, err := partCmd.Output()
	if err != nil {
		return "", fmt.Errorf("sqlite part query: %w", err)
	}

	text := strings.TrimSpace(string(partOut))
	if text == "" {
		return "", errors.New("no text in message parts")
	}
	return text, nil
}

func (h *Herdr) rememberSession(key, threadID string) {
	h.mu.Lock()
	h.sessions[key] = threadID
	_ = h.saveSessionsLocked()
	h.mu.Unlock()
}

func (h *Herdr) loadSessions() error {
	if h.config.SessionsFile == "" {
		return nil
	}
	data, err := os.ReadFile(h.config.SessionsFile)
	if err != nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return json.Unmarshal(data, &h.sessions)
}

func (h *Herdr) saveSessionsLocked() error {
	if h.config.SessionsFile == "" {
		return nil
	}
	data, err := json.MarshalIndent(h.sessions, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.config.SessionsFile), 0700); err != nil {
		return err
	}
	return fsx.AtomicWriteFile(h.config.SessionsFile, data, 0600)
}

func (h *Herdr) Interrupt(_ context.Context, key string) (bool, error) {
	lock := h.lockForKey(key)
	lock.Lock()
	defer lock.Unlock()

	h.mu.Lock()
	turn := h.active[key]
	h.mu.Unlock()

	if turn == nil {
		return false, nil
	}

	turn.cancel()
	_ = exec.Command(h.config.Command, "agent", "send-keys", turn.target, "ctrl+c").Run()
	return true, nil
}

func (h *Herdr) ResetSession(key string) error {
	lock := h.lockForKey(key)
	lock.Lock()
	defer lock.Unlock()

	h.mu.Lock()
	delete(h.sessions, key)
	_ = h.saveSessionsLocked()
	h.mu.Unlock()
	return nil
}

func (h *Herdr) ThreadID(key string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[key]
}

func (h *Herdr) IsActive(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active[key] != nil
}

func (h *Herdr) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, turn := range h.active {
		turn.cancel()
	}
	return nil
}
