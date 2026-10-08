package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPEnsureDeduplicatesConcurrentConnects vérifie que N appelants simultanés
// (pré-chauffage + tour de chat + panneau MCP de l'UI) partagent une seule
// tentative de connexion : sans ça, connecter en parallèle multiplierait les
// process stdio lancés pour un même serveur.
func TestMCPEnsureDeduplicatesConcurrentConnects(t *testing.T) {
	var connects atomic.Int32
	mgr := NewMCPManager(nil, func(context.Context, string, resources.MCPServerConfig) (*mcpsdk.ClientSession, error) {
		connects.Add(1)
		return nil, fmt.Errorf("commande inexistante")
	})
	t.Cleanup(mgr.CloseAll)

	// Commande inexistante : la connexion échoue, mais l'entrée du pool est bien
	// partagée — c'est ce qu'on teste.
	cfg := resources.MCPServerConfig{Command: "loom-binaire-inexistant-pour-test", Enabled: true}

	const callers = 8
	var wg sync.WaitGroup
	got := make([]*MCPSession, callers)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = mgr.Ensure("bidon", cfg)
		}(i)
	}
	wg.Wait()

	if connects.Load() != 1 {
		t.Fatalf("connexions: %d", connects.Load())
	}
	for i, s := range got {
		if s != got[0] {
			t.Fatalf("appelant %d a obtenu une session différente : %p vs %p", i, s, got[0])
		}
		select {
		case <-s.ready:
		default:
			t.Fatalf("appelant %d a reçu une session pas encore prête", i)
		}
	}
	if got[0].err == nil {
		t.Fatal("une commande inexistante devrait produire une erreur de connexion")
	}
}

type mcpConfigFixture map[string]resources.MCPServerConfig

func (s mcpConfigFixture) LoadMCPConfig() (map[string]resources.MCPServerConfig, error) {
	return s, nil
}

func TestMCPPoolDiscoveryCallAndSingleReconnect(t *testing.T) {
	var connects atomic.Int32
	config := mcpConfigFixture{"test.server": {Command: "in-memory", Enabled: true, DisabledTools: []string{"hidden"}}}
	mgr := NewMCPManager(config, func(ctx context.Context, _ string, _ resources.MCPServerConfig) (*mcpsdk.ClientSession, error) {
		connects.Add(1)
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fixture"}, nil)
		for _, name := range []string{"echo", "hidden"} {
			server.AddTool(&mcpsdk.Tool{Name: name, Title: "Echo", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
				return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "bonjour-loom"}}}, nil
			})
		}
		st, ct := mcpsdk.NewInMemoryTransports()
		ss, err := server.Connect(ctx, st, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = ss.Close() })
		return mcpsdk.NewClient(&mcpsdk.Implementation{Name: "loom"}, nil).Connect(ctx, ct, nil)
	})
	t.Cleanup(mgr.CloseAll)
	tools := mgr.Tools()
	if len(tools) != 1 || tools[0].Function.Name != "mcp__test_server__echo" || tools[0].Function.Description != "[MCP: test.server] Echo" {
		t.Fatalf("discovery: %+v", tools)
	}
	if got := mgr.Call(tools[0].Function.Name, map[string]any{}); got != "bonjour-loom" {
		t.Fatal(got)
	}
	if line := mgr.PromptLine(); !strings.Contains(line, "test.server (2)") {
		t.Fatal(line)
	}
	// Retain the registry while closing the connection, as a dropped stdio process would.
	session := mgr.Ensure("test.server", config["test.server"])
	_ = session.sess.Close()
	if got := mgr.Call(tools[0].Function.Name, map[string]any{}); got != "bonjour-loom" || connects.Load() != 2 {
		t.Fatalf("reconnect: %q (%d connections)", got, connects.Load())
	}
	status, err := mgr.Status()
	if err != nil || len(status) != 1 || !status[0].Connected || len(status[0].Tools) != 2 || len(status[0].Disabled) != 1 {
		t.Fatalf("status: %+v %v", status, err)
	}
}

func TestMCPPoolInvalidationDuringDiscovery(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	mgr := NewMCPManager(nil, func(ctx context.Context, _ string, _ resources.MCPServerConfig) (*mcpsdk.ClientSession, error) {
		close(started)
		<-release
		server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fixture"}, nil)
		st, ct := mcpsdk.NewInMemoryTransports()
		ss, err := server.Connect(ctx, st, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = ss.Close() })
		return mcpsdk.NewClient(&mcpsdk.Implementation{Name: "fixture-client"}, nil).Connect(ctx, ct, nil)
	})
	t.Cleanup(mgr.CloseAll)
	done := make(chan *MCPSession, 1)
	go func() { done <- mgr.Ensure("changing", resources.MCPServerConfig{Command: "fixture", Enabled: true}) }()
	<-started
	// Observation, cancellation and config invalidation can overlap discovery.
	_ = mgr.PromptLine()
	mgr.Invalidate("changing")
	mgr.CloseAll()
	close(release)
	s := <-done
	<-s.Ready()
	if s.sess != nil {
		t.Fatal("invalidated connection republished")
	}
}
