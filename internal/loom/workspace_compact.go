package loom

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/discussion"
)

var errCompactUnsupported = errors.New("this agent does not offer compaction")

func loomTranscript(s RuntimeSession) bool {
	return s.RuntimeID == "llama.cpp" || s.RuntimeID == "openai-compatible"
}
func offersCompact(s RuntimeSession) bool {
	for _, command := range s.Commands {
		if command["name"] == "compact" {
			return true
		}
	}
	return false
}
func portableMessages(s RuntimeSession) []Message {
	if s.PortableMessages != nil {
		return s.PortableMessages
	}
	return s.Messages
}
func promptTokens(messages []Message) int {
	n := 0
	for _, msg := range messages {
		n += 4 + brain.Tokens(msgText(msg))
		for _, call := range msg.ToolCalls {
			n += brain.Tokens(call.Function.Name + call.Function.Arguments)
		}
	}
	return n
}
func discussionWindow(s RuntimeSession) int {
	if s.RuntimeID == "openai-compatible" {
		var p CloudProvider
		if getStoreJSON(bkProviders, s.ProviderID, &p) {
			return max(0, p.ContextWindows[s.Model])
		}
		return 0
	}
	if s.RuntimeID != "llama.cpp" {
		return 0
	}
	if n := currentEngineNode(); n != nil {
		return max(0, n.Ctx)
	}
	if s.Model == "" {
		return 0
	}
	if !sameModelPath(s.Model, ReadConfig()["MODEL"]) {
		return max(0, modelNativeCtx(s.Model))
	}
	return max(0, effectiveCtx(ReadConfig()))
}
func discussionLimit(s RuntimeSession) discussion.ContextState {
	if !loomTranscript(s) && s.ACPUsage != nil {
		b, _ := s.ACPUsage["context"].(map[string]any)
		if b["used"] != nil {
			return discussion.ContextState{Used: contextNumber(b["used"]), Size: contextNumber(b["size"]), Source: "agent"}
		}
	}
	c := discussionContext(s)
	messages := discussion.PrepareDiscussion(s, "", c).Messages
	if s.RuntimeID == "llama.cpp" && s.NativeArchive != "" {
		if a, ok := loadArchive(s.NativeArchive); ok {
			native := a.Messages
			if sp := effectiveSysPrompt(); sp != "" {
				native = append([]Message{{Role: "system", Content: sp}}, native...)
			}
			messages = InjectSkills(discussion.WithContextExtras(withProjectContext(native, c.System), c.Extras), Caps{})
		}
	}
	return discussion.ContextState{Used: promptTokens(messages), Size: discussionWindow(s), Source: "estimate"}
}
func contextNumber(v any) int {
	switch n := v.(type) {
	case float64:
		return max(0, int(n))
	case int:
		return max(0, n)
	case int64:
		return max(0, int(n))
	}
	return 0
}
func contextWarning(s RuntimeSession, c discussion.ContextState) bool {
	return c.Size > 0 && float64(c.Used)/float64(c.Size) >= .85 && (!compactEnabled() || !loomTranscript(s) && !offersCompact(s))
}
func (m *runtimeSessions) compactSnapshot(ctx context.Context, s RuntimeSession, key string) (RuntimeSession, string, bool, error) {
	endpoint, model := strings.TrimRight(s.Endpoint, "/")+"/chat/completions", s.Model
	if s.RuntimeID == "llama.cpp" {
		if !sameModelPath(s.Model, ReadConfig()["MODEL"]) {
			return s, "", false, errors.New("load the selected model before compacting")
		}
		endpoint, key, model = engineBase()+"/v1/chat/completions", engineAPIKey(), engineRequestModel()
	} else if key == "" {
		return s, "", false, errors.New("missing key: reconnect the provider in Models → Providers")
	}
	messages := portableMessages(s)
	var archive *convArchive
	if s.RuntimeID == "llama.cpp" && s.NativeArchive != "" {
		var ok bool
		archive, ok = loadArchive(s.NativeArchive)
		if !ok {
			return s, "", false, errors.New("local discussion archive unavailable")
		}
		messages = archive.Messages
	}
	window := discussionWindow(s)
	if window == 0 {
		window = 32768
	}
	out, summary, changed, err := compactMessagesWith(ctx, messages, window, func(ctx context.Context, transcript string) (string, error) {
		return summarizeTranscriptAt(ctx, transcript, endpoint, key, model, s.RuntimeID == "llama.cpp")
	})
	if err != nil {
		return s, "", false, err
	}
	if !changed || strings.TrimSpace(summary) == "" {
		return s, "", false, nil
	}
	s = cloneRuntimeSession(s)
	s.PortableMessages = out
	s.Compactions = append(s.Compactions, discussion.CompactionRecord{At: time.Now().UnixMilli(), RuntimeID: s.RuntimeID, ProviderID: s.ProviderID, Model: s.Model, Before: promptTokens(messages), After: promptTokens(out)})
	s.UpdatedAt = time.Now().UnixMilli()
	return s, summary, true, nil
}
func (m *runtimeSessions) compactNow(ctx context.Context, id string) (RuntimeSession, bool, error) {
	m.mu.Lock()
	s, ok := m.getLocked(id)
	if !ok {
		m.mu.Unlock()
		return s, false, errors.New("discussion not found or locked")
	}
	if m.preparing[id] || m.runs[id] != nil || m.nativeRunning(s) {
		m.mu.Unlock()
		return s, false, errors.New("a response is already in progress")
	}
	if !loomTranscript(s) {
		m.mu.Unlock()
		if !offersCompact(s) {
			return s, false, errCompactUnsupported
		}
		if err := m.startPrepared(id, newSessionID(), "/compact", func(current RuntimeSession, text string) DiscussionPreview {
			p := prepareDiscussion(current, text)
			if current.RuntimeID != s.RuntimeID || !offersCompact(current) {
				p.Problem = "the agent or its compact command changed; try again"
			}
			return p
		}); err != nil {
			return s, false, err
		}
		s, _ = m.get(id)
		return s, false, nil
	}
	m.preparing[id] = true
	key := m.keys[s.ProviderID]
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.preparing, id); m.mu.Unlock() }()
	release, err := reserveNativeDiscussion(s)
	if err != nil {
		return s, false, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	next, _, changed, err := m.compactSnapshot(ctx, s, key)
	if err != nil || !changed {
		return s, false, err
	}
	theBrain().queueSessionTranscript(next)
	err = func() error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if next.RuntimeID == "llama.cpp" && next.NativeArchive != "" {
			a, ok := loadArchive(next.NativeArchive)
			if !ok {
				return errors.New("local discussion archive unavailable")
			}
			a.Messages, a.CtxUsed = next.PortableMessages, promptTokens(next.PortableMessages)
			a.CompactCount++
			if err = saveArchive(a); err != nil {
				return err
			}
			conv.mu.Lock()
			if conv.ID == a.ID {
				conv.Messages = append([]Message{}, a.Messages...)
				conv.CtxUsed, conv.CompactCount = a.CtxUsed, a.CompactCount
			}
			conv.mu.Unlock()
			next.PortableMessages = portableText(a.Messages)
		}
		if err = putStoreJSON(bkRuntimeSessions, id, next); err != nil {
			return err
		}
		m.publishLocked(id, DiscussionEvent{"type": "compacted", "session": next})
		return nil
	}()
	if err != nil {
		return s, false, err
	}
	if next.RuntimeID == "llama.cpp" && next.NativeArchive != "" {
		conv.mu.Lock()
		active, epoch := conv.ID == next.NativeArchive, conv.epoch
		conv.mu.Unlock()
		if active {
			conv.appendDelta(epoch, map[string]any{"compacted": true, "ctx_used": promptTokens(next.PortableMessages)})
			conv.persist()
		}
	}
	return next, true, nil
}
func handleRuntimeSessionCompact(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	s, changed, err := workspaceSessions.compactNow(r.Context(), req.ID)
	if err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error(), "unsupported": errors.Is(err, errCompactUnsupported)})
		return
	}
	status := 200
	if !loomTranscript(s) {
		status = 202
	}
	sendJSON(w, status, map[string]any{"ok": true, "compacted": changed, "session": clientSession(s)})
}

func portableText(messages []Message) []Message {
	out := []Message{}
	for _, msg := range messages {
		if (msg.Role == "user" || msg.Role == "assistant") && msgText(msg) != "" {
			out = append(out, Message{Role: msg.Role, Content: msgText(msg)})
		}
	}
	return out
}
func reserveNativeDiscussion(s RuntimeSession) (func(), error) {
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if s.RuntimeID != "llama.cpp" || s.NativeArchive == "" || conv.ID != s.NativeArchive {
		return func() {}, nil
	}
	if conv.Generating {
		return nil, errors.New("a response is already in progress")
	}
	conv.Generating = true
	epoch := conv.epoch
	return func() {
		conv.mu.Lock()
		if conv.epoch == epoch {
			conv.Generating = false
		}
		conv.mu.Unlock()
	}, nil
}
