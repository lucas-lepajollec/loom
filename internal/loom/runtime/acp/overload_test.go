package acp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func TestProtocolFloodHelper(t *testing.T) {
	if os.Getenv("LOOM_TEST_ACP_FLOOD") != "1" {
		return
	}
	for i := 0; i < maxInboundRequests+1; i++ {
		fmt.Fprintf(os.Stdout, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"method\":\"session/request_permission\"}\n", i)
	}
	time.Sleep(10 * time.Second)
	os.Exit(0)
}
func TestInboundPermissionFloodDisconnectsWithoutUnboundedHandlers(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestProtocolFloodHelper$")
	cmd.Env = append(os.Environ(), "LOOM_TEST_ACP_FLOOD=1")
	c, err := NewClient(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var entered atomic.Int32
	c.Handler = func(*Frame) (any, error) { entered.Add(1); <-c.Done(); return nil, context.Canceled }
	if err = c.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("overloaded agent stayed connected")
	}
	if entered.Load() > maxInboundRequests {
		t.Fatal("unbounded handlers")
	}
}
