package runtime_test

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/agentstdio"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/codexapp"
	"github.com/lucas-lepajollec/loom/internal/loom/runtime/pirpc"
)

// Explicit read-only acceptance, never enabled by the ordinary suite. No
// thread/start, prompt, login, quota reset, tool execution or account mutation.
func TestNativeAgentHandshakes(t *testing.T) {
	if os.Getenv("LOOM_AGENT_HANDSHAKES") != "1" {
		t.Skip("opt-in installed CLI initialize/list check")
	}
	for _, name := range []string{"codex", "pi"} {
		t.Run(name, func(t *testing.T) {
			path, err := exec.LookPath(name)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"app-server", "-c", "sqlite_home=" + strconv.Quote(t.TempDir())}
			if name == "pi" {
				args = []string{"--mode", "rpc", "--no-session"}
			}
			cmd := exec.Command(path, args...)
			cmd.Dir = t.TempDir()
			c, err := agentstdio.New(cmd)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			sink := func(runtime.AgentEvent) bool { return true }
			b := runtime.NewRequestBroker(name, sink)
			defer b.Cancel()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if name == "codex" {
				s := codexapp.New(c, b, sink)
				if err := c.Start(); err != nil {
					t.Fatal(err)
				}
				if err := s.Initialize(ctx); err != nil {
					t.Fatal(err)
				}
				models, err := s.Models(ctx)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("initialize/initialized + model/list: %d model descriptors", len(models))
			} else {
				s := pirpc.New(c, b, sink)
				if err := c.Start(); err != nil {
					t.Fatal(err)
				}
				if _, err := s.State(ctx); err != nil {
					t.Fatal(err)
				}
				models, err := s.Models(ctx)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("get_state + get_available_models: %d model descriptors", len(models))
			}
		})
	}
}
