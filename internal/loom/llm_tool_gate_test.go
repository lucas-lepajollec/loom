package loom

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// A model can name a tool the discussion never offered (here bash with the
// agent mode off, as text injected by a web page could ask). The loop must
// refuse it instead of running a shell command.
func TestLocalLoopRefusesToolsNotOffered(t *testing.T) {
	testHome(t)
	marker := filepath.Join(t.TempDir(), "ran")
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			args := fmt.Sprintf(`{\"command\":\"touch %s\"}`, marker)
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"%s\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n", args)
		} else {
			fmt.Fprint(w, sseChunk("done"))
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	if err := SetConfigKey("PORT", u.Port()); err != nil {
		t.Fatal(err)
	}
	var refused string
	_, err := runChat(context.Background(), []Message{{Role: "user", Content: "hi"}}, 0.7, Caps{}, func(ev StreamEvent) bool {
		if ev.ToolUsed != nil && ev.ToolUsed.Done {
			refused = ev.ToolUsed.Result
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("bash ran although the discussion did not offer it")
	}
	if !strings.Contains(refused, "not enabled") {
		t.Fatalf("expected a refusal, got %q", refused)
	}
}
