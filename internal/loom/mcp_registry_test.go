package loom

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type registryRoundTripFunc func(*http.Request) (*http.Response, error)

func (f registryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func registryFixture(t *testing.T, handler http.HandlerFunc) *mcpRegistry {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	local, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := srv.Client()
	base := client.Transport
	client.Timeout = 8 * time.Second
	client.CheckRedirect = officialMCPRegistry.client.CheckRedirect
	client.Transport = registryRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Scheme != "https" || req.URL.Host != "registry.modelcontextprotocol.io" || req.URL.Path != "/v0/servers" {
			t.Errorf("unexpected registry target: %s", req.URL)
			return nil, fmt.Errorf("unexpected registry target")
		}
		rewrite := req.Clone(req.Context())
		rewrite.URL.Scheme, rewrite.URL.Host = local.Scheme, local.Host
		return base.RoundTrip(rewrite)
	})
	return &mcpRegistry{client: client}
}

func TestRegistryParsesPresetsAndCachesNormalizedQuery(t *testing.T) {
	var requests atomic.Int32
	registry := registryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		q := r.URL.Query()
		if q.Get("search") != "tools & files" || q.Get("limit") != "25" || q.Get("version") != "latest" {
			t.Errorf("query: %v", q)
		}
		if r.Header.Get("Authorization") != "" || r.Method != http.MethodGet {
			t.Errorf("unexpected request headers or method")
		}
		_, _ = io.WriteString(w, `{"servers":[{"server":{
			"name":"org.example/My__Tool!", "description":"Fixture description", "version":"1.2.3",
			"websiteUrl":"https://example.test", "repository":{"url":"https://git.example.test/fixture"},
			"remotes":[
				{"type":"streamable-http","url":"https://example.test/mcp","headers":[{"name":"Authorization","isSecret":true,"isRequired":true,"value":"fixture-secret"},{"name":"X-Mode","value":"app"}]},
				{"type":"sse","url":"https://example.test/sse"},
				{"type":"sse","url":"https://user:password@example.test/sse"},
				{"type":"unsupported","url":"https://example.test/unsupported"}
			],
			"packages":[
				{"registryType":"npm","identifier":"@scope/fixture","version":"1.2.0","transport":{"type":"stdio"},"environmentVariables":[{"name":"TOKEN","isSecret":true}],
				 "packageArguments":[{"type":"positional","value":"workspace folder"},{"type":"named","name":"--config","isRequired":true}]},
				{"registryType":"pypi","identifier":"fixture-py","version":"1.1"},
				{"registryType":"oci","identifier":"ghcr.io/org/fixture","version":"v2","environmentVariables":[{"name":"API_KEY","isRequired":true}]},
				{"registryType":"oci","identifier":"--unsafe"},
				{"registryType":"npm","identifier":"not-stdio","transport":{"type":"sse"}},
				{"registryType":"unsupported","identifier":"ignore"}
			]
		}}]}`)
	})
	rows, err := registry.search(t.Context(), "   Tools  &  FILES ", 500)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	row := rows[0]
	if row.ID != "org.example/My__Tool!" || row.Title != "My__Tool!" || row.SuggestedName != "my_tool" ||
		row.Description != "Fixture description" || row.Version != "1.2.3" || row.WebsiteURL != "https://example.test" ||
		row.RepositoryURL != "https://git.example.test/fixture" || len(row.Presets) != 5 {
		t.Fatal(row)
	}
	if p := row.Presets[0]; p.Transport != "http" || p.Type != "streamable-http" || p.URL != "https://example.test/mcp" ||
		len(p.Headers) != 2 || p.Headers[0].Value != "" || !p.Headers[0].Secret || p.Headers[1].Value != "app" {
		t.Fatal(p)
	}
	if p := row.Presets[1]; p.Transport != "http" || p.Type != "sse" || p.URL != "https://example.test/sse" {
		t.Fatal(p)
	}
	for i, want := range []struct {
		command string
		args    []string
		env     string
	}{
		{"npx", []string{"-y", "@scope/fixture@1.2.0", "workspace folder", "--config", "{config}"}, "TOKEN"},
		{"uvx", []string{"fixture-py==1.1"}, ""},
		{"docker", []string{"run", "-i", "--rm", "-e", "API_KEY", "ghcr.io/org/fixture:v2"}, "API_KEY"},
	} {
		p := row.Presets[i+2]
		if p.Transport != "stdio" || p.Command != want.command || !reflect.DeepEqual(p.Args, want.args) ||
			want.env != "" && (len(p.EnvVars) != 1 || p.EnvVars[0].Name != want.env) {
			t.Fatalf("unexpected package preset: %+v", p)
		}
	}
	again, err := registry.search(t.Context(), "tools & files", 25)
	if err != nil || !reflect.DeepEqual(rows, again) || requests.Load() != 1 {
		t.Fatal(again, err, requests.Load())
	}
	key := registryCacheKey{"tools & files", 25}
	registry.mu.Lock()
	old := registry.cache[key]
	old.expires = time.Now().Add(-time.Second)
	registry.cache[key] = old
	registry.mu.Unlock()
	if _, err := registry.search(t.Context(), "tools & files", 25); err != nil || requests.Load() != 2 {
		t.Fatal(err, requests.Load())
	}
}

func TestRegistrySafeErrorsAndEmptyResults(t *testing.T) {
	var requests atomic.Int32
	registry := registryFixture(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("limit") != "1" {
			t.Errorf("limit not bounded: %s", r.URL.RawQuery)
		}
		switch r.URL.Query().Get("search") {
		case "redirect":
			http.Redirect(w, r, "http://127.0.0.1:1/private", http.StatusFound)
		case "status":
			http.Error(w, "private diagnostic", http.StatusServiceUnavailable)
		case "malformed":
			_, _ = io.WriteString(w, `{"private":"diagnostic"`)
		case "shape":
			_, _ = io.WriteString(w, `{"error":"private diagnostic"}`)
		case "oversized":
			_, _ = io.WriteString(w, `{"servers":[]}`+strings.Repeat(" ", 2<<20))
		default:
			_, _ = io.WriteString(w, `{"servers":[]}`)
		}
	})
	for _, query := range []string{"redirect", "status", "status", "malformed", "shape", "oversized"} {
		if rows, err := registry.search(t.Context(), query, -1); len(rows) != 0 || err == nil ||
			strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "127.0.0.1") {
			t.Fatal(query, rows, err)
		}
	}
	if rows, err := registry.search(t.Context(), "", 1); rows == nil || len(rows) != 0 || err != nil || requests.Load() != 7 {
		t.Fatal(rows, err, requests.Load())
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := registry.search(ctx, "canceled", 1); err == nil {
		t.Fatal("registry search ignored cancellation")
	}
	rec := httptest.NewRecorder()
	handleMCPRegistrySearch(rec, httptest.NewRequest(http.MethodPost, "/api/mcp/registry/search", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatal(rec.Code)
	}
	rec = httptest.NewRecorder()
	handleMCPRegistrySearch(rec, httptest.NewRequest(http.MethodGet, "/api/mcp/registry/search?limit=invalid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatal(rec.Code)
	}
}
