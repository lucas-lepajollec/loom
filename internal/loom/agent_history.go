package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agent "github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/codexapp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/pirpc"
)

func nativeHistoryClient(ctx context.Context, a acpAgent, cwd string) (*codexapp.Session, func(), error) {
	c, err := startNativeAgent(a, RuntimeSession{ACPState: ACPState{Workdir: cwd}}, true)
	if err != nil {
		return nil, func() {}, err
	}
	sink := func(agent.AgentEvent) bool { return true }
	broker := agent.NewRequestBroker(a.ID, sink)
	s := codexapp.New(c, broker, sink)
	cleanup := func() { broker.Cancel(); c.Close() }
	if err = c.Start(); err == nil {
		err = s.Initialize(ctx)
	}
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	return s, cleanup, nil
}
func listNativeAgentSessions(ctx context.Context, a acpAgent) ([]acpSessionInfo, error) {
	if a.ID == "antigravity" {
		return nil, errors.New("Antigravity stream-json has no session-list API; use the native CLI/IDE conversation catalog")
	}
	if a.ID == "opencode" {
		return listOpenCodeSessions(ctx, a)
	}
	if a.ID == "pi" {
		return listPiNativeSessions()
	}
	dir, err := os.MkdirTemp("", "loom-native-history-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	s, cleanup, err := nativeHistoryClient(ctx, a, dir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	rows := []acpSessionInfo{}
	cursor := ""
	for page := 0; page < 5; page++ {
		var r codexapp.ThreadListResponse
		if err := s.Client.Call(ctx, "thread/list", codexapp.ThreadListParams{Cursor: cursor}, false, &r); err != nil {
			return nil, err
		}
		var data []struct {
			ID        string `json:"id"`
			Cwd       string `json:"cwd"`
			Name      string `json:"name"`
			Preview   string `json:"preview"`
			UpdatedAt int64  `json:"updatedAt"`
		}
		if err := json.Unmarshal(r.Data, &data); err != nil {
			return nil, err
		}
		for _, d := range data {
			rows = append(rows, acpSessionInfo{SessionID: d.ID, Cwd: d.Cwd, Title: firstNonEmpty(d.Name, d.Preview), UpdatedAt: time.Unix(d.UpdatedAt, 0).UTC().Format(time.RFC3339)})
		}
		if r.NextCursor == "" || r.NextCursor == cursor {
			break
		}
		cursor = r.NextCursor
	}
	return markNativeImports(a, rows), nil
}
func markNativeImports(a acpAgent, rows []acpSessionInfo) []acpSessionInfo {
	for _, s := range workspaceSessions.list() {
		for i := range rows {
			if nativeImportMatches(s, a, rows[i].SessionID) {
				rows[i].Imported = s.ID
			}
		}
	}
	return rows
}
func piSessionsRoot() string {
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return filepath.Join(dir, "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent", "sessions")
}
func listPiNativeSessions() ([]acpSessionInfo, error) {
	// The native RPC has no session/list command. Inspect only bounded headers
	// under Pi's configured session root; get_messages supplies the active branch.
	root := piSessionsRoot()
	paths, err := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	direct, _ := filepath.Glob(filepath.Join(root, "*.jsonl"))
	paths = append(paths, direct...)
	rows := []acpSessionInfo{}
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 4096), 64<<10)
		var h struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Cwd       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
		}
		ok := sc.Scan() && json.Unmarshal(sc.Bytes(), &h) == nil && h.Type == "session" && h.ID != ""
		_ = f.Close()
		if !ok {
			continue
		}
		rows = append(rows, acpSessionInfo{SessionID: h.ID, SessionFile: path, Cwd: h.Cwd, Title: h.ID, UpdatedAt: h.Timestamp})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].UpdatedAt > rows[j].UpdatedAt })
	if len(rows) > 200 {
		rows = rows[:200]
	}
	return rows, nil
}
func readNativeAgentHistory(ctx context.Context, a acpAgent, info acpSessionInfo, cwd string) ([]Message, []RuntimeTurnRecord, string, error) {
	messages := []Message{}
	turns := []RuntimeTurnRecord{}
	sessionFile := ""
	if a.ID == "antigravity" {
		return nil, nil, "", errors.New("Antigravity stream-json has no history-read API; native conversation resume is supported")
	}
	if a.ID == "opencode" {
		messages, err := readOpenCodeHistory(ctx, info.SessionID, cwd)
		if err != nil {
			return nil, nil, "", err
		}
		turns := []RuntimeTurnRecord{}
		for i, msg := range messages {
			if msg.Role == "assistant" {
				turns = append(turns, RuntimeTurnRecord{MessageIndex: i, RuntimeID: a.ID, ProviderName: a.Name, Model: "default", NativeSessionID: info.SessionID})
			}
		}
		return messages, turns, "", nil
	}
	if a.ID == "pi" {
		// Resolve the ID to Pi's own file; an API caller cannot supply an arbitrary
		// local path as a session. Pi itself restores branch/compaction semantics.
		rows, err := listPiNativeSessions()
		if err != nil {
			return nil, nil, "", err
		}
		for _, r := range rows {
			if r.SessionID == info.SessionID {
				sessionFile = r.SessionFile
				break
			}
		}
		if sessionFile == "" {
			return nil, nil, "", errors.New("Pi native session not found")
		}
		c, err := startNativeAgent(a, RuntimeSession{ACPState: ACPState{Workdir: cwd}}, true)
		if err != nil {
			return nil, nil, "", err
		}
		defer c.Close()
		sink := func(agent.AgentEvent) bool { return true }
		b := agent.NewRequestBroker(a.ID, sink)
		defer b.Cancel()
		s := pirpc.New(c, b, sink)
		if err := c.Start(); err != nil {
			return nil, nil, "", err
		}
		var loaded struct {
			Cancelled bool `json:"cancelled"`
		}
		if err := c.Call(ctx, "switch_session", map[string]any{"sessionPath": sessionFile}, true, &loaded); err != nil {
			return nil, nil, "", err
		}
		if loaded.Cancelled {
			return nil, nil, "", errors.New("Pi session resume cancelled")
		}
		st, err := s.State(ctx)
		if err != nil {
			return nil, nil, "", err
		}
		if st.IsStreaming {
			return nil, nil, "", errors.New("session open in another Pi window")
		}
		var r struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := c.Call(ctx, "get_messages", nil, true, &r); err != nil {
			return nil, nil, "", err
		}
		for _, msg := range r.Messages {
			if msg.Role != "user" && msg.Role != "assistant" {
				continue
			}
			text := nativeMessageText(msg.Content)
			if text != "" {
				messages = append(messages, Message{Role: msg.Role, Content: text})
			}
		}
	} else {
		s, cleanup, err := nativeHistoryClient(ctx, a, cwd)
		if err != nil {
			return nil, nil, "", err
		}
		defer cleanup()
		var r codexapp.ThreadReadResponse
		if err := s.Client.Call(ctx, "thread/read", codexapp.ThreadReadParams{ThreadId: info.SessionID, IncludeTurns: true}, false, &r); err != nil {
			return nil, nil, "", codexapp.SessionError(err)
		}
		var thread struct {
			ID    string `json:"id"`
			Turns []struct {
				Items []struct {
					Type    string          `json:"type"`
					Text    string          `json:"text"`
					Content json.RawMessage `json:"content"`
				} `json:"items"`
			} `json:"turns"`
		}
		if err := json.Unmarshal(r.Thread, &thread); err != nil {
			return nil, nil, "", err
		}
		for _, turn := range thread.Turns {
			for _, item := range turn.Items {
				switch item.Type {
				case "userMessage":
					messages = append(messages, Message{Role: "user", Content: nativeMessageText(item.Content)})
				case "agentMessage":
					messages = append(messages, Message{Role: "assistant", Content: item.Text})
				}
			}
		}
	}
	for i, msg := range messages {
		if msg.Role == "assistant" {
			turns = append(turns, RuntimeTurnRecord{MessageIndex: i, RuntimeID: a.ID, ProviderName: a.Name, Model: "default", NativeSessionID: info.SessionID})
		}
	}
	return messages, turns, sessionFile, nil
}
func nativeMessageText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &content)
	var out strings.Builder
	for _, v := range content {
		if v.Type == "text" {
			out.WriteString(v.Text)
		}
	}
	return out.String()
}
