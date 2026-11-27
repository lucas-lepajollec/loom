package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Internal Go HTTP client for Codeman's official /api/v1/* endpoints.
// Reuses Loom's saved Codeman configuration, credentials and transport.

var codemanInputSeq atomic.Int64

type codemanSessionInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Mode           string `json:"mode"`
	WorkingDir     string `json:"workingDir"`
	Status         string `json:"status"`
	PID            *int   `json:"pid"`
	IsWorking      bool   `json:"isWorking"`
	LastActivityAt int64  `json:"lastActivityAt"`
	CreatedAt      int64  `json:"createdAt"`
}

type codemanCreateSessionReq struct {
	WorkingDir   string            `json:"workingDir"`
	Mode         string            `json:"mode"`
	Name         string            `json:"name,omitempty"`
	Model        string            `json:"model,omitempty"`
	Effort       string            `json:"effort,omitempty"`
	EnvOverrides map[string]string `json:"envOverrides,omitempty"`
	CodexConfig  map[string]any    `json:"codexConfig,omitempty"`
	OpenCodeCfg  map[string]any    `json:"openCodeConfig,omitempty"`
	GeminiCfg    map[string]any    `json:"geminiConfig,omitempty"`
	AntigravCfg  map[string]any    `json:"antigravityConfig,omitempty"`
	PiCfg        map[string]any    `json:"piConfig,omitempty"`
	GrokCfg      map[string]any    `json:"grokConfig,omitempty"`
	CopilotCfg   map[string]any    `json:"copilotConfig,omitempty"`
	DeepSeekCfg  map[string]any    `json:"deepSeekConfig,omitempty"`
	OMPCfg       map[string]any    `json:"ompConfig,omitempty"`
}

type codemanWaitState struct {
	Signal    string `json:"signal"`
	Immediate bool   `json:"immediate"`
	TimedOut  bool   `json:"timedOut"`
	Ended     bool   `json:"ended"`
	Matched   bool   `json:"matched"`
	Snippet   string `json:"snippet"`
	TimeoutMs int    `json:"timeoutMs"`
}

type codemanWaitResponse struct {
	Wait        codemanWaitState `json:"wait"`
	Status      string           `json:"status"`
	LimitPaused bool             `json:"limitPaused"`
	Delivered   *bool            `json:"delivered,omitempty"`
	Duplicate   bool             `json:"duplicate,omitempty"`
}

func codemanAPIRequest(ctx context.Context, method, apiPath string, body any, extraHeaders map[string]string, out any) error {
	cfg := savedCodemanConfig()
	if cfg == nil || strings.TrimSpace(cfg.URL) == "" {
		return errors.New("Codeman is not connected in Loom")
	}
	var reader io.Reader = http.NoBody
	var contentLen int64
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
		contentLen = int64(len(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(cfg.URL, "/")+"/api/v1"+apiPath, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = contentLen
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Codeman-CSRF", "1")
	if auth := codemanAuthHeader(*cfg); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for k, v := range extraHeaders {
		if strings.TrimSpace(v) != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := codemanHTTP(cfg.URL).Do(req)
	if err != nil {
		return fmt.Errorf("codeman unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var env struct {
		Success   bool            `json:"success"`
		Data      json.RawMessage `json:"data"`
		Error     string          `json:"error"`
		ErrorCode string          `json:"errorCode"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		if resp.StatusCode >= 400 {
			return fmt.Errorf("codeman HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		}
		return err
	}
	if resp.StatusCode >= 400 || !env.Success {
		msg := strings.TrimSpace(env.Error)
		if msg == "" {
			msg = fmt.Sprintf("codeman HTTP %d", resp.StatusCode)
		}
		if env.ErrorCode != "" {
			return fmt.Errorf("%s (%s)", msg, env.ErrorCode)
		}
		return errors.New(msg)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return err
		}
	}
	return nil
}

func codemanListSessions(ctx context.Context) ([]codemanSessionInfo, error) {
	var list []codemanSessionInfo
	if err := codemanAPIRequest(ctx, http.MethodGet, "/sessions", nil, nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func codemanGetSession(ctx context.Context, id string) (*codemanSessionInfo, error) {
	var info codemanSessionInfo
	if err := codemanAPIRequest(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id), nil, nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func codemanCreateSession(ctx context.Context, req codemanCreateSessionReq, parentSessionID string) (*codemanSessionInfo, error) {
	headers := map[string]string{}
	if strings.TrimSpace(parentSessionID) != "" {
		headers["X-Codeman-Parent-Session"] = strings.TrimSpace(parentSessionID)
	}
	var resp struct {
		Session codemanSessionInfo `json:"session"`
		ID      string             `json:"id"`
	}
	if err := codemanAPIRequest(ctx, http.MethodPost, "/sessions", req, headers, &resp); err != nil {
		return nil, err
	}
	if resp.Session.ID == "" && resp.ID != "" {
		resp.Session.ID = resp.ID
	}
	if resp.Session.ID == "" {
		return nil, errors.New("codeman did not return a session id")
	}
	return &resp.Session, nil
}

func codemanStartSession(ctx context.Context, id string, shell bool) error {
	suffix := "/interactive"
	if shell {
		suffix = "/shell"
	}
	return codemanAPIRequest(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+suffix, map[string]any{}, nil, nil)
}

func codemanDeleteSession(ctx context.Context, id string, killMux bool) error {
	q := "true"
	if !killMux {
		q = "false"
	}
	return codemanAPIRequest(ctx, http.MethodDelete, "/sessions/"+url.PathEscape(id)+"?killMux="+q, nil, nil, nil)
}

func codemanSendPrompt(ctx context.Context, id, text string, wait bool, waitTimeoutMs int) (*codemanWaitResponse, error) {
	line := strings.TrimRight(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), " \t\n")
	if line == "" {
		return nil, errors.New("prompt text is empty")
	}
	if len(line) > 64*1024-2 {
		return nil, errors.New("prompt exceeds Codeman 64 KiB input limit")
	}
	seq := codemanInputSeq.Add(1)
	payload := map[string]any{
		"input":    line + "\r",
		"useMux":   true,
		"clientId": "loom-orch",
		"seq":      seq,
	}
	if wait {
		if waitTimeoutMs < 1000 {
			waitTimeoutMs = 60000
		}
		if waitTimeoutMs > 600000 {
			waitTimeoutMs = 600000
		}
		payload["wait"] = true
		payload["waitTimeout"] = waitTimeoutMs
	}
	var res codemanWaitResponse
	if err := codemanAPIRequest(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/input", payload, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func codemanInterruptSession(ctx context.Context, id string) error {
	seq := codemanInputSeq.Add(1)
	payload := map[string]any{
		"input":    "\x1b",
		"useMux":   true,
		"clientId": "loom-orch-esc",
		"seq":      seq,
	}
	return codemanAPIRequest(ctx, http.MethodPost, "/sessions/"+url.PathEscape(id)+"/input", payload, nil, nil)
}

func codemanWaitSession(ctx context.Context, id, until string, timeoutMs int, fresh bool) (*codemanWaitResponse, error) {
	if timeoutMs < 1000 {
		timeoutMs = 1000
	}
	if timeoutMs > 600000 {
		timeoutMs = 600000
	}
	q := url.Values{}
	if strings.TrimSpace(until) != "" {
		q.Set("until", until)
	}
	q.Set("timeout", strconv.Itoa(timeoutMs))
	if fresh {
		q.Set("fresh", "1")
	}
	var res codemanWaitResponse
	if err := codemanAPIRequest(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/wait?"+q.Encode(), nil, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func codemanWaitOutput(ctx context.Context, id, match, from string, timeoutMs int) (*codemanWaitResponse, error) {
	if timeoutMs < 1000 {
		timeoutMs = 1000
	}
	if timeoutMs > 600000 {
		timeoutMs = 600000
	}
	q := url.Values{}
	q.Set("match", match)
	if from != "" {
		q.Set("from", from)
	}
	q.Set("timeout", strconv.Itoa(timeoutMs))
	var res codemanWaitResponse
	if err := codemanAPIRequest(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/wait-output?"+q.Encode(), nil, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

func codemanReadLastResponse(ctx context.Context, id string) (string, string, error) {
	// Poll briefly because Claude/DeepSeek flush their transcript shortly after stop.
	for attempt := 0; attempt < 3; attempt++ {
		var resp struct {
			Text string `json:"text"`
		}
		if err := codemanAPIRequest(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/last-response", nil, nil, &resp); err == nil {
			if txt := strings.TrimSpace(resp.Text); txt != "" {
				return txt, "transcript", nil
			}
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return "", "", ctx.Err()
			case <-time.After(450 * time.Millisecond):
			}
		}
	}
	// Fallback for hookless CLIs (Gemini, OpenCode, Pi, Shell, etc.): ANSI-stripped terminal tail.
	var term struct {
		TerminalBuffer string `json:"terminalBuffer"`
	}
	if err := codemanAPIRequest(ctx, http.MethodGet, "/sessions/"+url.PathEscape(id)+"/terminal?tail=16000", nil, nil, &term); err != nil {
		return "", "", err
	}
	clean, _ := cleanTerminalOutput([]byte(term.TerminalBuffer))
	return clean, "terminal", nil
}

// codemanWaitReady gives a freshly spawned CLI a short window to draw its
// interactive composer so the first prompt lands reliably.
func codemanWaitReady(ctx context.Context, id, cli string, maxWait time.Duration) {
	if maxWait <= 0 {
		maxWait = 12 * time.Second
	}
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		info, err := codemanGetSession(ctx, id)
		if err == nil && info.PID != nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
	rem := int(time.Until(deadline).Milliseconds())
	if rem < 1000 {
		return
	}
	switch strings.ToLower(strings.TrimSpace(cli)) {
	case "claude":
		_, _ = codemanWaitOutput(ctx, id, "shift+tab", "buffer", min(rem, 10000))
	case "deepseek":
		_, _ = codemanWaitOutput(ctx, id, "❯", "buffer", min(rem, 8000))
	case "shell":
		select {
		case <-ctx.Done():
		case <-time.After(400 * time.Millisecond):
		}
	default:
		select {
		case <-ctx.Done():
		case <-time.After(min(time.Duration(rem)*time.Millisecond, 1800*time.Millisecond)):
		}
	}
}
