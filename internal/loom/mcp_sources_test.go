package loom

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func mcpSourceRequest(t *testing.T, h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h(rr, r)
	return rr
}

func TestLinkedMCPFormatsAndNoSecretValues(t *testing.T) {
	testHome(t)
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"claude", `{"mcpServers":{"global":{"command":"node","env":{"TOKEN":"env-private"}}},"projects":{"/project/one":{"mcpServers":{"global":{"command":"python","env":{"KEY":"project-private"}}}},"/project/two":{"mcpServers":{"remote":{"url":"https://example.test/mcp","headers":{"Authorization":"header-private"},"type":"http"}}}}}`, 3},
		{"standard", `{"mcpServers":{"local":{"command":"node","args":["argument-private"],"env":{"TOKEN":"env-private"}}}}`, 1},
		{"cursor", `{"mcpServers":{"remote":{"url":"https://example.test/mcp","headers":{"Authorization":"header-private"},"env":{"KEY":"env-private"}}}}`, 1},
		{"vscode", `{"inputs":[{"id":"secret","type":"promptString"}],"servers":{"remote":{"type":"http","url":"https://example.test/mcp","headers":{"Authorization":"header-private"}},"local":{"type":"stdio","command":"node","env":{"TOKEN":"env-private"}}}}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mcp.json")
			writeMCPTestFile(t, path, tc.body)
			if err := linkMCPSource(MCPSource{Path: path, Label: tc.name}, false); err != nil {
				t.Fatal(err)
			}
			entries, err := readMCPSource(MCPSource{Path: path, Label: tc.name})
			if err != nil || len(entries) != tc.count {
				t.Fatal("format parse failed", err)
			}
			rr := mcpSourceRequest(t, handleMCPSources, http.MethodGet, "/api/mcp/sources", "")
			if rr.Code != 200 || rr.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("listing failed")
			}
			for _, secret := range []string{"env-private", "header-private", "project-private", "argument-private"} {
				if bytes.Contains(rr.Body.Bytes(), []byte(secret)) {
					t.Fatal("listing leaked a value")
				}
			}
			var response struct {
				Sources []MCPSourceStatus `json:"sources"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			var status *MCPSourceStatus
			for i := range response.Sources {
				if response.Sources[i].Path == path {
					status = &response.Sources[i]
				}
			}
			if status == nil || len(status.Servers) != tc.count {
				t.Fatal("missing linked metadata")
			}
			for _, entry := range status.Servers {
				if !entry.ReadOnly || entry.Source == "" || entry.Label != tc.name || entry.Name == "" || entry.Transport == "" {
					t.Fatal("incomplete read-only metadata")
				}
			}
			if tc.name == "claude" && status.Servers[0].Source == status.Servers[1].Source {
				t.Fatal("project scopes conflated")
			}
			before, _ := os.ReadFile(path)
			if err := linkMCPSource(MCPSource{Path: path, Label: ""}, true); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("linked file modified")
			}
		})
	}
}

func TestMCPSourceAdoptAndExplicitEnvironment(t *testing.T) {
	for _, withEnv := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "explicit"}[withEnv], func(t *testing.T) {
			testHome(t)
			path := filepath.Join(t.TempDir(), "external.json")
			original := `{"mcpServers":{"my.server":{"command":"node","args":["server.js"],"env":{"TOKEN":"env-private","PATH":"path-private"},"type":"stdio"},"remote":{"url":"https://example.test/mcp","headers":{"Authorization":"header-private"},"env":{"TOKEN":"env-private"},"type":"http"}}}`
			writeMCPTestFile(t, path, original)
			if err := linkMCPSource(MCPSource{Path: path, Label: "External"}, false); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"my.server", "remote"} {
				body, _ := json.Marshal(map[string]any{"source": path, "name": name, "with_env": withEnv})
				rr := mcpSourceRequest(t, handleMCPSourceAdopt, http.MethodPost, "/api/mcp/sources/adopt", string(body))
				if rr.Code != 200 {
					t.Fatal("adoption failed", rr.Body.String())
				}
				if bytes.Contains(rr.Body.Bytes(), []byte("env-private")) || bytes.Contains(rr.Body.Bytes(), []byte("header-private")) {
					t.Fatal("adopt response leaked secrets")
				}
				var response struct {
					Name    string   `json:"name"`
					Missing []string `json:"env_to_fill"`
				}
				_ = json.Unmarshal(rr.Body.Bytes(), &response)
				cfgs, err := LoadMCPConfig()
				if err != nil {
					t.Fatal(err)
				}
				cfg := cfgs[response.Name]
				if cfg.Enabled || cfg.Type == "" {
					t.Fatal("adopted server enabled or type lost")
				}
				expected := ""
				if withEnv {
					expected = "env-private"
				}
				if cfg.Env["TOKEN"] != expected {
					t.Fatal("explicit environment choice ignored")
				}
				if withEnv && len(response.Missing) != 0 || !withEnv && len(response.Missing) == 0 {
					t.Fatal("wrong placeholders")
				}
				for _, v := range cfg.Headers {
					if v != "" {
						t.Fatal("header credential copied")
					}
				}
				if rr := mcpSourceRequest(t, handleMCPSourceAdopt, http.MethodPost, "/api/mcp/sources/adopt", string(body)); rr.Code != 409 {
					t.Fatal("collision overwrote Loom server")
				}
			}
			data, _ := os.ReadFile(path)
			if string(data) != original {
				t.Fatal("adopt modified source file")
			}
			if _, _, err := adoptLinkedMCP(filepath.Join(t.TempDir(), "unlinked.json"), "my.server", true); err == nil {
				t.Fatal("unlinked source read")
			}
		})
	}
}

func TestMCPClaudeProjectAdopt(t *testing.T) {
	testHome(t)
	path := filepath.Join(t.TempDir(), ".claude.json")
	writeMCPTestFile(t, path, `{"mcpServers":{"same":{"command":"global"}},"projects":{"/one":{"mcpServers":{"same":{"command":"project"}}},"/two":{"mcpServers":{"same":{"command":"other"}}}}}`)
	if err := linkMCPSource(MCPSource{Path: path, Label: "Claude"}, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := adoptLinkedMCP(mcpSourceSelector(path, "/one"), "same", false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadMCPConfig()
	if cfg["same"].Command != "project" {
		t.Fatal("wrong project adopted")
	}
}

func TestMCPSourcesLinkUnlinkSuggestReloadAndErrors(t *testing.T) {
	testHome(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	path := filepath.Join(home, ".claude.json")
	writeMCPTestFile(t, path, `{"mcpServers":{"old":{"command":"node","env":{"TOKEN":"env-private"}}}}`)
	list, suggested, err := mcpSourcesSnapshot()
	if err != nil || len(list) != 0 {
		t.Fatal("initial list", err)
	}
	found := false
	for _, s := range suggested {
		if s.Path == path {
			found = true
		}
	}
	if !found {
		t.Fatal("existing candidate not suggested")
	}
	body, _ := json.Marshal(map[string]string{"path": path, "label": "Claude", "action": "link"})
	rr := mcpSourceRequest(t, handleMCPSources, http.MethodPost, "/api/mcp/sources", string(body))
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	// Relinking updates the label, without duplicating the source.
	if err := linkMCPSource(MCPSource{Path: path, Label: "Updated"}, false); err != nil {
		t.Fatal(err)
	}
	list, suggested, err = mcpSourcesSnapshot()
	if err != nil || len(list) != 1 || list[0].Label != "Updated" {
		t.Fatal("source persistence")
	}
	for _, s := range suggested {
		if s.Path == path {
			t.Fatal("linked candidate suggested")
		}
	}
	writeMCPTestFile(t, path, `{"mcpServers":{"new":{"command":"python"}}}`)
	list, _, _ = mcpSourcesSnapshot()
	if len(list[0].Servers) != 1 || list[0].Servers[0].Name != "new" {
		t.Fatal("linked external edit not loaded")
	}
	writeMCPTestFile(t, path, `{"secret-private":`)
	list, _, _ = mcpSourcesSnapshot()
	if list[0].Error == "" || len(list[0].Servers) != 0 || bytes.Contains([]byte(list[0].Error), []byte("secret-private")) {
		t.Fatal("linked parse error not redacted")
	}
	body, _ = json.Marshal(map[string]string{"path": path, "action": "unlink"})
	if rr := mcpSourceRequest(t, handleMCPSources, http.MethodPost, "/api/mcp/sources", string(body)); rr.Code != 200 {
		t.Fatal("unlink failed")
	}
	list, _, _ = mcpSourcesSnapshot()
	if len(list) != 0 {
		t.Fatal("unlink not persisted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("unlink deleted user file")
	}
	if err := linkMCPSource(MCPSource{Path: mcpFilePath(), Label: "Loom"}, false); err == nil {
		t.Fatal("linked Loom to itself")
	}
}

func TestMCPSourceHandlersRejectBadRequests(t *testing.T) {
	testHome(t)
	for _, h := range []http.HandlerFunc{handleMCPFile, handleMCPSourceAdopt} {
		rr := mcpSourceRequest(t, h, http.MethodPut, "/api/mcp/file", `{}`)
		if rr.Code != 405 {
			t.Fatal("method accepted")
		}
	}
	for _, body := range []string{`{"path":"x","unexpected":true}`, `{} {}`, `{"action":"erase"}`} {
		rr := mcpSourceRequest(t, handleMCPSources, http.MethodPost, "/api/mcp/sources", body)
		if rr.Code != 400 {
			t.Fatal("bad request accepted")
		}
	}
}
