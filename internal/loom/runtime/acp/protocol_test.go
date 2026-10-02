package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

// This process uses only stdio, so transport coverage needs no sockets or Loom state.
func TestProtocolAgentHelper(t *testing.T) {
	if os.Getenv("LOOM_TEST_LEAF_ACP") != "1" {
		return
	}
	send := func(v any) { b, _ := json.Marshal(v); _, _ = os.Stdout.Write(append(b, '\n')) }
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var f Frame
		if json.Unmarshal(scan.Bytes(), &f) != nil {
			os.Exit(2)
		}
		switch f.Method {
		case "permission":
			send(map[string]any{"jsonrpc": "2.0", "id": "agent-request", "method": "session/request_permission", "params": map[string]any{}})
			send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": map[string]any{}})
		case "ordered":
			for _, text := range []string{"first", "last"} {
				send(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"text": text}})
			}
			send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": map[string]any{"ok": true}})
		case "notify":
			send(map[string]any{"jsonrpc": "2.0", "method": "notification-received"})
		case "rpc-error":
			send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "error": map[string]any{"code": 42, "message": "private diagnostic"}})
		case "bad-result":
			send(map[string]any{"jsonrpc": "2.0", "id": f.ID, "result": false})
		case "wait": // Deliberately leave this call pending until its context expires.
		case "malformed":
			_, _ = os.Stdout.Write([]byte("not json\n"))
		case "":
			// Report the client's response, including its sanitized error code/text.
			send(map[string]any{"jsonrpc": "2.0", "method": "reply-received", "params": f.Error})
		}
	}
	os.Exit(0)
}

func TestClientBidirectionalOrderingCancellationAndSanitizedErrors(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestProtocolAgentHelper$")
	cmd.Env = append(os.Environ(), "LOOM_TEST_LEAF_ACP=1")
	c, err := NewClient(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	entered, release, replied := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
	notices := make(chan Frame, 16)
	c.Notify = func(f Frame) { notices <- f }
	c.Handler = func(f *Frame) (any, error) {
		f.Replied = func() { close(replied) }
		close(entered)
		<-release
		return nil, &RPCError{Code: -32601, Message: "private permission data"}
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Call(ctx, "permission", nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("request handler missing")
	}
	// A blocked client request must leave the reader available and preserve update order.
	var result struct {
		OK bool `json:"ok"`
	}
	if err := c.Call(ctx, "ordered", nil, &result); err != nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	texts := []string{}
	for range 2 {
		select {
		case f := <-notices:
			var p struct{ Text string }
			_ = json.Unmarshal(f.Params, &p)
			texts = append(texts, p.Text)
		case <-ctx.Done():
			t.Fatal("ordered update missing")
		}
	}
	if !reflect.DeepEqual(texts, []string{"first", "last"}) {
		t.Fatal(texts)
	}
	if err := c.Notification("notify", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-notices:
		if f.Method != "notification-received" {
			t.Fatal(f)
		}
	case <-ctx.Done():
		t.Fatal("notification missing")
	}
	short, stop := context.WithTimeout(ctx, 20*time.Millisecond)
	if err := c.Call(short, "wait", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	stop()
	err = c.Call(ctx, "rpc-error", nil, nil)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != 42 || strings.Contains(err.Error(), "private") {
		t.Fatalf("%v", err)
	}
	if err := c.Call(ctx, "bad-result", nil, &result); err == nil || err.Error() != "réponse ACP invalide" {
		t.Fatal(err)
	}
	if err := c.Write(strings.Repeat("x", MaxFrame)); err == nil || err.Error() != "message ACP trop long" {
		t.Fatal(err)
	}
	// Unblock the handler and check the deferred callback runs after its reply.
	release <- struct{}{}
	select {
	case <-replied:
	case <-ctx.Done():
		t.Fatal("reply callback missing")
	}
	select {
	case f := <-notices:
		var got RPCError
		if err := json.Unmarshal(f.Params, &got); err != nil {
			t.Fatal(err)
		}
		if got.Code != -32601 || got.Message != "requête client refusée" {
			t.Fatalf("%+v", got)
		}
	case <-ctx.Done():
		t.Fatal("client reply missing")
	}
	if err := c.Notification("malformed", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-ctx.Done():
		t.Fatal("malformed frame did not disconnect")
	}
	if err := c.Call(ctx, "ordered", nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	// The local completion hook never appears on the wire.
	b, err := json.Marshal(Frame{JSONRPC: "2.0", Replied: func() {}})
	if err != nil || string(b) != `{"jsonrpc":"2.0"}` {
		t.Fatalf("%s %v", b, err)
	}
}
