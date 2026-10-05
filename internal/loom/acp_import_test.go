package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/lucas-lepajollec/loom/internal/loom/project"
	"os"
	"strings"
	"testing"
	"time"
)

// A native replay fixture, never a real account or model process.
func TestNativeImportHelperProcess(t *testing.T) {
	if os.Getenv("LOOM_IMPORT_FIXTURE") != "1" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scan.Bytes(), &req) != nil {
			os.Exit(2)
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": true, "sessionCapabilities": map[string]any{"list": map[string]any{}}}}
		case "session/list":
			cwd, _ := os.Getwd()
			if os.Getenv("LOOM_IMPORT_FIXTURE_CWD") != "" {
				cwd = os.Getenv("LOOM_IMPORT_FIXTURE_CWD")
			}
			result = map[string]any{"sessions": []acpSessionInfo{{SessionID: "native-fixture", Cwd: cwd, Title: "Source transcript"}}}
		case "session/load":
			for _, row := range []struct{ kind, text string }{{"user_message_chunk", "Original question"}, {"agent_message_chunk", "Original answer"}} {
				frame := map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": "native-fixture", "update": map[string]any{"sessionUpdate": row.kind, "content": map[string]string{"type": "text", "text": row.text}}}}
				json.NewEncoder(os.Stdout).Encode(frame)
			}
			result = map[string]string{"sessionId": "native-fixture"}
		}
		if len(req.ID) > 0 {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		}
	}
	os.Exit(0)
}

func TestNativeImportFreshProvenanceDedupAndContext(t *testing.T) {
	testHome(t)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = old })
	t.Setenv("LOOM_IMPORT_FIXTURE", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	agent := acpAgent{ID: "fixture-native", Name: "Fixture", Command: binary, Args: []string{"-test.run=TestNativeImportHelperProcess"}}
	p, err := saveProjectContext(ChatProject{Name: "Portable", Continuity: &project.Continuity{Core: project.Core{Purpose: "Keep the rationale"}, WorkingState: "Continue safely"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info := acpSessionInfo{SessionID: "native-fixture", Cwd: t.TempDir(), Title: "Imported"}
	s, err := importACPSession(ctx, agent, info, p.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Messages) != 2 || s.Messages[0].Content != "Original question" || s.NativeSessionID != "" || s.NativeContext != "" || s.ImportSource == nil || s.ImportSource.SessionID != info.SessionID {
		t.Fatalf("fresh import: %+v", s)
	}
	preview := prepareDiscussion(s, "Continue")
	if !strings.Contains(preview.Context.System, p.Continuity.Core.Purpose) || preview.Messages[1].Content != "Original question" {
		t.Fatal("fresh executor lost portable history or project purpose")
	}
	s.RuntimeID = "another-executor"
	s.NativeSessionID = "new-native"
	if err = putStoreJSON(bkRuntimeSessions, s.ID, s); err != nil {
		t.Fatal(err)
	}
	again, err := importACPSession(ctx, agent, info, p.ID, true)
	if err != nil || again.ID != s.ID || len(workspaceSessions.list()) != 1 {
		t.Fatalf("route change broke import dedup: %v", err)
	}
	otherMachine := agent
	otherMachine.Machine = "another-machine"
	if nativeImportMatches(s, otherMachine, info.SessionID) {
		t.Fatal("native ID collided across machines")
	}
}
