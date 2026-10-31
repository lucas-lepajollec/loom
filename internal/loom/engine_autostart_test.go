package loom

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"
)

func TestEngineStartsForAnInferenceRequest(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	u := &url.URL{Scheme: "http", Host: addr}
	old := engineStartHook
	t.Cleanup(func() { engineStartHook = old; engineAutoStart.err = nil })
	engineAutoStart.err = nil
	starts := 0
	var started net.Listener
	engineStartHook = func() error {
		starts++
		var err error
		started, err = net.Listen("tcp", addr)
		return err
	}
	if err := ensureEngineForRequest(context.Background(), u); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer started.Close()
	if err := ensureEngineForRequest(context.Background(), u); err != nil || starts != 1 {
		t.Fatalf("a running engine must not start again: %v, %d starts", err, starts)
	}
}

func TestEngineStartFailureIsNotRetriedInALoop(t *testing.T) {
	u := &url.URL{Scheme: "http", Host: "127.0.0.1:1"}
	old := engineStartHook
	t.Cleanup(func() { engineStartHook = old; engineAutoStart.err = nil })
	engineAutoStart.err = nil
	starts := 0
	engineStartHook = func() error { starts++; return errors.New("no model configured") }
	for i := 0; i < 3; i++ {
		if err := ensureEngineForRequest(context.Background(), u); err == nil {
			t.Fatal("a failed start must be reported")
		}
	}
	if starts != 1 {
		t.Fatalf("%d starts within the retry delay", starts)
	}
	engineAutoStart.failedAt = time.Now().Add(-engineAutoStartRetry - time.Second)
	_ = ensureEngineForRequest(context.Background(), u)
	if starts != 2 {
		t.Fatalf("expected a new attempt after the delay, got %d", starts)
	}
}
