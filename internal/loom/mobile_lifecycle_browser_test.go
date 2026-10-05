package loom

// Opt-in disposable UI acceptance. All services, machines and accounts are
// synthetic; no systemctl, SSH or real credential store is used by this fixture.
import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMobileLifecycleBrowser(t *testing.T) {
	if os.Getenv("LOOM_TEST_MOBILE_BROWSER") != "1" {
		t.Skip("disposable browser acceptance")
	}
	home := testHome(t)
	t.Setenv("LOOM_IMPORT_FIXTURE", "1")
	t.Setenv("LOOM_IMPORT_FIXTURE_CWD", home)
	oldRegistry := registeredRuntimes
	registeredRuntimes = newRuntimeRegistry()
	defer func() { registeredRuntimes = oldRegistry }()
	registerRuntime(llamaRuntimeAdapter{})
	registerRuntime(cloudRuntimeAdapter{})
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	defer func() { workspaceSessions = old }()
	binary, _ := os.Executable()
	for _, id := range []string{"fixture-one", "fixture-two"} {
		registeredRuntimes.upsert(&acpAdapter{agent: acpAgent{ID: id, Name: id, Custom: true, Command: binary, Args: []string{"-test.run=TestNativeImportHelperProcess"}}})
		defer registeredRuntimes.remove(id)
	}
	oldCommand := startupCommand
	defer func() { startupCommand = oldCommand }()
	var enabled atomic.Bool
	startupCommand = func(_ context.Context, _ bool, write bool, args ...string) ([]byte, error) {
		if write {
			enabled.Store(args[0] == "enable")
			return nil, nil
		}
		switch args[0] {
		case "show":
			return []byte("loaded"), nil
		case "is-enabled":
			if enabled.Load() {
				return []byte("enabled"), nil
			}
			return []byte("disabled"), nil
		default:
			return []byte("inactive"), nil
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:2601")
	if err != nil {
		t.Fatal(err)
	}
	mux := newWebMux()
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: mux}
	mux.HandleFunc("/fixture/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		w.WriteHeader(204)
		go server.Close()
	})
	mux.HandleFunc("/fixture/stall", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	// Do not probe a real engine or send a generation while browsing the fixture.
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status" {
			sendJSON(w, 200, map[string]any{"active": false, "health": false})
			return
		}
		if r.URL.Path == "/api/vram" {
			sendJSON(w, 200, []any{})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/send") {
			sendJSON(w, 403, map[string]any{"ok": false, "error": "generation disabled in fixture"})
			return
		}
		mux.ServeHTTP(w, r)
	})
	t.Log("disposable mobile acceptance http://127.0.0.1:2601")
	if err = server.Serve(listener); err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
