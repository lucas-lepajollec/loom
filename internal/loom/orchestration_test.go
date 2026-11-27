package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Loom Test",
			"GIT_AUTHOR_EMAIL=test@loom.local",
			"GIT_COMMITTER_NAME=Loom Test",
			"GIT_COMMITTER_EMAIL=test@loom.local",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, string(out))
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "init")
	return dir
}

func TestWorktreeLifecycleAndInclude(t *testing.T) {
	ctx := context.Background()
	repo := initTestGitRepo(t)

	// Untracked local bootstrap file listed in .worktreeinclude (T3 Code pattern).
	if err := os.WriteFile(filepath.Join(repo, ".env.local"), []byte("LOCAL_FLAG=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".worktreeinclude"), []byte("# bootstrap\n.env.local\n../escape\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := createLoomWorktree(ctx, repo, "backend dev", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Branch, "loom/backend-dev-") {
		t.Fatalf("unexpected branch %q", info.Branch)
	}
	if len(info.CopiedFiles) != 1 || info.CopiedFiles[0] != ".env.local" {
		t.Fatalf("expected .env.local copied, got %v", info.CopiedFiles)
	}
	if raw, err := os.ReadFile(filepath.Join(info.WorktreePath, ".env.local")); err != nil || string(raw) != "LOCAL_FLAG=1\n" {
		t.Fatalf("copied .env.local mismatch: %q %v", string(raw), err)
	}
	mcpRaw, err := os.ReadFile(filepath.Join(info.WorktreePath, ".mcp.json"))
	if err != nil || !strings.Contains(string(mcpRaw), "mcp-bridge") || strings.Contains(string(mcpRaw), "lm_ses_") {
		t.Fatalf("expected secret-free .mcp.json with mcp-bridge, got %q %v", string(mcpRaw), err)
	}
	// .loom-worktrees and .mcp.json must stay excluded from git status.
	if st := worktreeStatus(ctx, info.WorktreePath); !st.Exists || st.DirtyFiles != 1 {
		// Only .env.local is untracked in the worktree (unless ignored by git); .mcp.json is excluded.
		t.Logf("worktree status: %+v", st)
	}
	// Create an uncommitted tracked change and verify safe cleanup refuses without force.
	if err := os.WriteFile(filepath.Join(info.WorktreePath, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeLoomWorktree(ctx, repo, info.WorktreePath, false); err == nil {
		t.Fatal("expected refusal to remove dirty worktree without force")
	}
	if err := removeLoomWorktree(ctx, repo, info.WorktreePath, true); err != nil {
		t.Fatalf("expected forced worktree removal to succeed: %v", err)
	}
	if _, err := os.Stat(info.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists after removal")
	}
}

func TestScopedTokensAndTreeIsolation(t *testing.T) {
	gatewayTestHome(t)

	tokA, err := mintPendingSessionToken("", "", 0, "/repo-a")
	if err != nil {
		t.Fatal(err)
	}
	bindSessionToken(tokA, "sess-root-a", "sess-root-a")
	_, _ = orchRegisterNode(OrchestrationNode{SessionID: "sess-root-a", RootID: "sess-root-a", Depth: 0, Name: "Lead A"})
	_, _ = orchRegisterNode(OrchestrationNode{SessionID: "sess-child-a", ParentID: "sess-root-a", RootID: "sess-root-a", Depth: 1, Role: "worker"})
	_, _ = orchRegisterNode(OrchestrationNode{SessionID: "sess-root-b", RootID: "sess-root-b", Depth: 0, Name: "Other User Session"})

	req := httptest.NewRequest(http.MethodPost, "/mcp/loom", nil)
	req.Header.Set("Authorization", "Bearer "+tokA)
	scope, ok := lookupScopedBearer(req)
	if !ok || scope.SessionID != "sess-root-a" || scope.RootID != "sess-root-a" || scope.Depth != 0 {
		t.Fatalf("unexpected scoped bearer lookup: %+v ok=%v", scope, ok)
	}

	if _, err := orchCanTarget(scope, "sess-child-a"); err != nil {
		t.Fatalf("expected parent to target child in same tree: %v", err)
	}
	if _, err := orchCanTarget(scope, "sess-root-b"); err == nil {
		t.Fatal("expected cross-tree targeting to be rejected")
	}

	orchMarkStopped("sess-root-a")
	if _, ok := lookupScopedBearer(req); ok {
		t.Fatal("expected scoped token to be revoked after session stop")
	}
}

func TestMCPOrchestrationEndToEndAndGuardrails(t *testing.T) {
	gatewayTestHome(t)
	repo := initTestGitRepo(t)

	var mu sync.Mutex
	sessions := map[string]codemanSessionInfo{
		"root-1": {ID: "root-1", Name: "Lead", Mode: "claude", WorkingDir: repo, Status: "idle"},
	}
	createdParents := map[string]string{}
	seq := 0

	fakeCodeman := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := strings.TrimPrefix(r.URL.Path, "/api/v1")
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.Method == http.MethodGet && p == "/sessions":
			list := make([]codemanSessionInfo, 0, len(sessions))
			for _, s := range sessions {
				list = append(list, s)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": list})
		case r.Method == http.MethodPost && p == "/sessions":
			var body struct {
				WorkingDir   string            `json:"workingDir"`
				Mode         string            `json:"mode"`
				Name         string            `json:"name"`
				EnvOverrides map[string]string `json:"envOverrides"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			seq++
			id := "worker-" + string(rune('0'+seq))
			pid := 1000 + seq
			info := codemanSessionInfo{ID: id, Name: body.Name, Mode: body.Mode, WorkingDir: body.WorkingDir, Status: "idle", PID: &pid}
			sessions[id] = info
			createdParents[id] = r.Header.Get("X-Codeman-Parent-Session")
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"session": info}})
		case r.Method == http.MethodGet && p == "/clis":
			tr := true
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []codemanCLIInfo{
				{ID: "claude", Label: "Claude Code", Kind: "agent", Installed: &tr, Enabled: &tr},
				{ID: "codex", Label: "Codex", Kind: "agent", Installed: &tr, Enabled: &tr},
			}})
		case r.Method == http.MethodGet && p == "/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
				"claudeMode": "normal", "codexDangerouslyBypassApprovals": false,
			}})
		case r.Method == http.MethodPut && p == "/settings":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
		case r.Method == http.MethodGet && p == "/approvals":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"approvals": []any{}}})
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/sessions/") && strings.HasSuffix(p, "/edits"):
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"entries": []map[string]any{
				{"id": "e1", "relPath": "api/handler.go", "added": 12, "removed": 2},
			}}})
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/sessions/") && strings.HasSuffix(p, "/last-response"):
			id := strings.TrimSuffix(strings.TrimPrefix(p, "/sessions/"), "/last-response")
			if r.URL.Query().Get("context") == "full" {
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
					"text": "Finished work for " + id,
					"messages": []map[string]any{
						{"role": "user", "text": "Inspect mobile views"},
						{"role": "assistant", "text": "Finished work for " + id},
					},
				}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"text": "Finished work for " + id}})
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/sessions/") && strings.HasSuffix(p, "/wait-output"):
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"wait": map[string]any{"matched": true}}})
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/sessions/") && strings.HasSuffix(p, "/wait"):
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"wait": map[string]any{"signal": "stop"}, "status": "idle"}})
		case r.Method == http.MethodPost && strings.HasPrefix(p, "/sessions/") && (strings.HasSuffix(p, "/interactive") || strings.HasSuffix(p, "/shell")):
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
		case r.Method == http.MethodPost && strings.HasPrefix(p, "/sessions/") && strings.HasSuffix(p, "/input"):
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"delivered": true, "wait": map[string]any{"signal": "stop"}, "status": "idle"}})
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/sessions/"):
			id := strings.TrimPrefix(p, "/sessions/")
			if s, ok := sessions[id]; ok {
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": s})
				return
			}
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "not found", "errorCode": "NOT_FOUND"})
		case r.Method == http.MethodDelete && strings.HasPrefix(p, "/sessions/"):
			id := strings.TrimPrefix(p, "/sessions/")
			delete(sessions, id)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
		default:
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "unhandled " + r.Method + " " + p})
		}
	}))
	defer fakeCodeman.Close()

	if err := saveCodemanConfig(codemanConfig{URL: fakeCodeman.URL, Username: "admin"}); err != nil {
		t.Fatal(err)
	}
	// Tighten limits to exercise guardrails cleanly.
	if _, err := saveOrchestrationSettings(OrchestrationSettings{
		AutoInjectMCP:        true,
		AutoWriteWorktreeMCP: true,
		MaxDepth:             1,
		MaxChildrenPerRoot:   2,
	}); err != nil {
		t.Fatal(err)
	}

	rootTok, err := mintSessionToken(SessionScope{SessionID: "root-1", RootID: "root-1", Depth: 0, WorkingDir: repo})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = orchRegisterNode(OrchestrationNode{SessionID: "root-1", RootID: "root-1", Depth: 0, Name: "Lead", CLI: "claude", WorkingDir: repo})

	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	registerOrchestrationRoutes(mux, webAPI(mux))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "orchestrator-test", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             "http://localhost/mcp/loom",
		HTTPClient:           &http.Client{Transport: brainMuxTransport{mux, rootTok}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	// 0. Dynamic catalog reports live CLIs & models.
	catRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "loom_subagent_catalog", Arguments: map[string]any{}})
	if err != nil || catRes.IsError {
		t.Fatalf("catalog failed: %v %+v", err, catRes)
	}
	rawCat, _ := json.Marshal(catRes.StructuredContent)
	if !strings.Contains(string(rawCat), `"codex"`) || !strings.Contains(string(rawCat), `"opus"`) {
		t.Fatalf("unexpected catalog: %s", string(rawCat))
	}

	// 1. Spawn a worktree subagent via MCP.
	spawnRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "loom_subagent_spawn",
		Arguments: map[string]any{
			"role":      "api-dev",
			"task":      "Implement endpoint",
			"cli":       "codex",
			"workspace": "worktree",
		},
	})
	if err != nil || spawnRes.IsError {
		t.Fatalf("spawn failed: err=%v res=%+v", err, spawnRes)
	}
	spawnBytes, _ := json.Marshal(spawnRes.StructuredContent)
	var spawned subagentSpawnResult
	_ = json.Unmarshal(spawnBytes, &spawned)
	if spawned.SessionID != "worker-1" || spawned.ParentID != "root-1" || spawned.Depth != 1 || spawned.WorktreePath == "" {
		t.Fatalf("unexpected spawn result: %+v", spawned)
	}
	sf, ok := readSessionBridgeFile("", "worker-1")
	mu.Lock()
	parentHdr := createdParents["worker-1"]
	mu.Unlock()
	if !ok || !strings.HasPrefix(sf.Token, "lm_ses_") || parentHdr != "root-1" {
		t.Fatalf("expected 0600 session bridge file & X-Codeman-Parent-Session, got ok=%v sf=%+v parent=%q", ok, sf, parentHdr)
	}
	childTok := sf.Token

	// 2. Child at depth=1 (max_depth=1) must be blocked from spawning deeper.
	childClient := mcp.NewClient(&mcp.Implementation{Name: "worker-test", Version: "1.0"}, nil)
	childSession, err := childClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             "http://localhost/mcp/loom",
		HTTPClient:           &http.Client{Transport: brainMuxTransport{mux, childTok}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer childSession.Close()

	deepRes, err := childSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "loom_subagent_spawn",
		Arguments: map[string]any{"role": "too-deep", "task": "recurse"},
	})
	if err == nil && !deepRes.IsError {
		t.Fatalf("expected max_depth guardrail to reject nested spawn: %+v", deepRes)
	}

	// 3. Child posts to shared team board; orchestrator reads it.
	if postRes, err := childSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "loom_board_post",
		Arguments: map[string]any{"kind": "result", "text": "Endpoint implemented and tested"},
	}); err != nil || postRes.IsError {
		t.Fatalf("board post failed: %v %+v", err, postRes)
	}
	readBoardRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "loom_board_read", Arguments: map[string]any{}})
	if err != nil || readBoardRes.IsError {
		t.Fatalf("board read failed: %v %+v", err, readBoardRes)
	}
	rawBoard, _ := json.Marshal(readBoardRes.StructuredContent)
	if !strings.Contains(string(rawBoard), "Endpoint implemented and tested") {
		t.Fatalf("board message missing: %s", string(rawBoard))
	}

	// 4. Orchestrator reads full conversation + edits and stops worker with worktree cleanup.
	readRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "loom_subagent_read",
		Arguments: map[string]any{"session_id": "worker-1", "mode": "full", "include_diff": true, "include_edits": true},
	})
	if err != nil || readRes.IsError {
		t.Fatalf("subagent read failed: %v %+v", err, readRes)
	}
	rawRead, _ := json.Marshal(readRes.StructuredContent)
	if !strings.Contains(string(rawRead), "Finished work for worker-1") || !strings.Contains(string(rawRead), "Inspect mobile views") || !strings.Contains(string(rawRead), "api/handler.go") {
		t.Fatalf("unexpected full read payload: %s", string(rawRead))
	}

	// Verify reparent cycle protection + detach/re-attach.
	if _, err := orchReparent("root-1", "worker-1", nil); err == nil {
		t.Fatal("expected cycle protection to reject attaching parent under child")
	}

	// Fail-closed self-delete check.
	selfStop, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "loom_subagent_stop",
		Arguments: map[string]any{"session_id": "root-1"},
	})
	if err == nil && !selfStop.IsError {
		t.Fatal("expected self-stop to be rejected")
	}

	stopRes, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "loom_subagent_stop",
		Arguments: map[string]any{
			"session_id":       "worker-1",
			"mode":             "close",
			"cleanup_worktree": true,
		},
	})
	if err != nil || stopRes.IsError {
		t.Fatalf("stop failed: %v %+v", err, stopRes)
	}
	if _, err := os.Stat(spawned.WorktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree %s to be removed on stop", spawned.WorktreePath)
	}
}
