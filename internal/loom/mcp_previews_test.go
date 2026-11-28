package loom

import (
	"net/http"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPPreviewSuggestionSessionBus(t *testing.T) {
	testHome(t)
	id := "preview-test"
	token, err := mintSessionToken(SessionScope{SessionID: id, RootID: id})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	client := mcp.NewClient(&mcp.Implementation{Name: "preview-fixture", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: "http://localhost/mcp/loom", HTTPClient: &http.Client{Transport: brainMuxTransport{mux, token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call := func(args map[string]any, wantErr bool) {
		t.Helper()
		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "loom_preview_open", Arguments: args})
		if err != nil || res.IsError != wantErr {
			t.Fatalf("tool %v, %v", res, err)
		}
	}
	ch := orchHub.subscribe()
	defer orchHub.unsubscribe(ch)
	call(map[string]any{"kind": "port", "port": 5173}, false)
	select {
	case event := <-ch:
		if event.Name != "preview" {
			t.Fatal(event)
		}
	default:
		t.Fatal("missing notification")
	}
	call(map[string]any{"kind": "file", "path": "render.png", "session_id": "elsewhere"}, true)
	call(map[string]any{"kind": "port", "port": 22}, true)
	call(map[string]any{"kind": "file", "path": "../other.png"}, true)
	code, out := filesCall(t, handleSessionPreviews, "GET", "/api/previews/session?session_id="+id, "")
	if code != 200 || len(out["previews"].([]any)) != 1 {
		t.Fatal(out)
	}
	if code, _ := capCall(t, handleSessionPreviews, "/api/previews/session", `{"session_id":"preview-test","kind":"port","port":5173,"label":"Studio"}`); code != 200 {
		t.Fatal(code)
	}
	if list := sessionPreviews.list(id); len(list) != 1 || list[0].Label != "Studio" {
		t.Fatal(list)
	}
	body := `{"session_id":"preview-test","kind":"port","port":5173,"action":"remove"}`
	if code, _ := capCall(t, handleSessionPreviews, "/api/previews/session", body); code != 200 {
		t.Fatal(code)
	}
	if len(sessionPreviews.list(id)) != 0 {
		t.Fatal("remove did not clear suggestion")
	}
}
