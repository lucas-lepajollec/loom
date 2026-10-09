package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/brain"
	"github.com/lucas-lepajollec/loom/internal/loom/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func gatewayTestHome(t *testing.T) {
	t.Helper()
	testHome(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
}
func gatewayTestRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestGatewayHarnessWriters(t *testing.T) {
	for _, h := range gatewayHarnessSpecs {
		t.Run(h.id, func(t *testing.T) {
			gatewayTestHome(t)
			path := expandHome(h.file)
			original := `{"keep":{"number":9007199254740993},"` + h.key + `":{"other":{"command":"owned-by-user"}}}`
			if h.id == "codex" {
				original = "model = \"user-model\"\n\n[mcp_servers.other]\ncommand = \"user-server\"\n"
			}
			writeMCPTestFile(t, path, original)
			if err := setGatewayHarness(h.id, true); err != nil {
				t.Fatal(err)
			}
			first := gatewayTestRead(t, path)
			if !gatewayRegistered(h.id) {
				t.Fatal("registration missing")
			}
			if !bytes.Equal(gatewayTestRead(t, path+".loom-backup"), []byte(original)) {
				t.Fatal("backup changed")
			}
			if err := setGatewayHarness(h.id, true); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, gatewayTestRead(t, path)) {
				t.Fatal("writer not idempotent")
			}
			c, err := openGatewayConfig(h)
			if err != nil {
				t.Fatal(err)
			}
			oldToken := c.token()
			c.root.Close()
			if oldToken == "" {
				t.Fatal("header token missing")
			}
			if h.id == "codex" {
				if strings.Count(string(first), gatewayBegin) != 1 || strings.Count(string(first), gatewayEnd) != 1 || !bytes.HasPrefix(first, []byte(original)) || !bytes.Contains(first, []byte("http_headers = { \"Authorization\" = \"Bearer ")) {
					t.Fatal("TOML edit", string(first))
				}
			} else {
				var doc map[string]json.RawMessage
				if err := json.Unmarshal(first, &doc); err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(doc["keep"], []byte("9007199254740993")) {
					t.Fatal("other key lost precision")
				}
				var entries map[string]map[string]any
				if err := json.Unmarshal(doc[h.key], &entries); err != nil {
					t.Fatal(err)
				}
				if entries["other"]["command"] != "owned-by-user" {
					t.Fatal("foreign entry changed")
				}
				entry := entries["loom"]
				switch h.id {
				case "claude-code":
					if entry["type"] != "http" || entry["url"] != gatewayURL() {
						t.Fatal(entry)
					}
				case "opencode":
					if entry["type"] != "remote" || entry["enabled"] != true {
						t.Fatal(entry)
					}
				case "antigravity":
					if entry["url"] != gatewayURL() {
						t.Fatal(entry)
					}
				}
			}
			token, err := rotateGatewayToken()
			if err != nil || token == oldToken {
				t.Fatal("rotation", err)
			}
			c, err = openGatewayConfig(h)
			if err != nil {
				t.Fatal(err)
			}
			if c.token() != token {
				t.Fatal("registered header not rotated")
			}
			c.root.Close()
			if bytes.Contains(getBytes(bkState, gatewayStateKey), []byte(token)) {
				t.Fatal("plaintext token in store")
			}
			if !bytes.Equal(gatewayTestRead(t, path+".loom-backup"), []byte(original)) {
				t.Fatal("backup overwritten")
			}
			if err := setGatewayHarness(h.id, false); err != nil {
				t.Fatal(err)
			}
			removed := gatewayTestRead(t, path)
			if gatewayRegistered(h.id) {
				t.Fatal("still registered")
			}
			if h.id == "codex" && !bytes.Equal(removed, []byte(original)) {
				t.Fatal("TOML removal touched other blocks")
			}
			if h.id != "codex" {
				var doc map[string]json.RawMessage
				_ = json.Unmarshal(removed, &doc)
				var entries map[string]json.RawMessage
				_ = json.Unmarshal(doc[h.key], &entries)
				if len(entries) != 1 || entries["other"] == nil || doc["keep"] == nil {
					t.Fatal("JSON removal touched other entries")
				}
			}
			if err := setGatewayHarness(h.id, false); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(removed, gatewayTestRead(t, path)) {
				t.Fatal("removal not idempotent")
			}
		})
	}
}
func TestGatewayOwnershipAndConfinement(t *testing.T) {
	for _, h := range gatewayHarnessSpecs {
		t.Run(h.id, func(t *testing.T) {
			gatewayTestHome(t)
			original := `{"` + h.key + `":{"loom":{"command":"user-owned"}}}`
			if h.id == "codex" {
				original = "[mcp_servers.\"loom\"]\ncommand = \"user-owned\"\n"
			}
			path := expandHome(h.file)
			writeMCPTestFile(t, path, original)
			for _, enabled := range []bool{true, false} {
				if err := setGatewayHarness(h.id, enabled); err == nil {
					t.Fatal("foreign loom entry changed")
				}
			}
			if string(gatewayTestRead(t, path)) != original {
				t.Fatal("foreign config changed")
			}
			if _, err := os.Stat(path + ".loom-backup"); !os.IsNotExist(err) {
				t.Fatal("foreign backup created")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := setGatewayHarness(h.id, true); err != nil {
				t.Fatal(err)
			}
			changed := append(gatewayTestRead(t, path), []byte(" ")...)
			if h.id == "codex" {
				changed = bytes.ReplaceAll(changed, []byte("http_headers"), []byte("env_http_headers"))
			} else {
				changed = bytes.ReplaceAll(changed, []byte("Bearer loom-gateway-"), []byte("Bearer edited-"))
			}
			writeMCPTestFile(t, path, string(changed))
			if gatewayRegistered(h.id) {
				t.Fatal("edited entry considered registered")
			}
			if _, err := rotateGatewayToken(); err == nil {
				t.Fatal("rotation overwrote user edit")
			}
			if !bytes.Equal(changed, gatewayTestRead(t, path)) {
				t.Fatal("rotation changed edited entry")
			}
		})
	}
	t.Run("symlink", func(t *testing.T) {
		gatewayTestHome(t)
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(os.Getenv("HOME"), ".gemini")); err != nil {
			t.Skip(err)
		}
		if err := setGatewayHarness("antigravity", true); err == nil {
			t.Fatal("escaped home")
		}
		if entries, _ := os.ReadDir(outside); len(entries) != 0 {
			t.Fatal("wrote outside harness home")
		}
	})
}
func TestGatewayTokenReuseRotationAndURLs(t *testing.T) {
	gatewayTestHome(t)
	token, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range gatewayHarnessSpecs {
		if err := setGatewayHarness(h.id, true); err != nil {
			t.Fatal(err)
		}
		c, err := openGatewayConfig(h)
		if err != nil {
			t.Fatal(err)
		}
		if c.token() != token {
			t.Fatal("did not reuse dedicated token")
		}
		c.root.Close()
	}
	// Restart can recover the token from an owned config; the store holds hashes.
	gatewayToken.home, gatewayToken.token = "", ""
	if err := setGatewayHarness("codex", true); err != nil {
		t.Fatal("restart", err)
	}
	beforeRevision := gatewayRevision("codex")
	rotated, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	if beforeRevision == "" || beforeRevision == gatewayRevision("codex") {
		t.Fatal("rotation did not change ACP launch revision")
	}
	for _, h := range gatewayHarnessSpecs {
		c, err := openGatewayConfig(h)
		if err != nil {
			t.Fatal(err)
		}
		if c.token() != rotated {
			t.Fatal("rotation skipped", h.id)
		}
		c.root.Close()
	}
	oldHost, oldPort := webBound.host, webBound.port
	defer func() { webBound.host, webBound.port = oldHost, oldPort }()
	for host, want := range map[string]string{"0.0.0.0": "127.0.0.1", "::": "127.0.0.1", "192.0.2.1": "192.0.2.1", "::1": "[::1]"} {
		webBound.host, webBound.port = host, 2594
		if gatewayURL() != "http://"+want+":2594/mcp/loom" {
			t.Fatal(gatewayURL())
		}
	}
}
func TestGatewayToolNaming(t *testing.T) {
	upstream := []tools.MCPGatewayTool{}
	for _, pair := range [][2]string{{"a b", "read"}, {"a.b", "read"}, {"a_b", "read"}, {strings.Repeat("x", 100), "first"}, {strings.Repeat("x", 100), "second"}, {"🧠", "🧠"}, {"a", "b.c/d"}} {
		upstream = append(upstream, tools.MCPGatewayTool{Server: pair[0], Tool: &mcp.Tool{Name: pair[1]}})
	}
	names := gatewayToolNames(upstream)
	allowed := regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	seen := map[string]bool{}
	for _, name := range names {
		if !allowed.MatchString(name) || seen[name] {
			t.Fatal("invalid or duplicate", name)
		}
		seen[name] = true
	}
	if names[2] != "a_b__read" || names[1] == names[0] || !reflect.DeepEqual(names, gatewayToolNames(upstream)) {
		t.Fatal(names)
	}
}
func gatewayFixturePool(t *testing.T) {
	t.Helper()
	cfg := map[string]MCPServerConfig{
		"ok-server": {Command: "in-memory", Enabled: true, DisabledTools: []string{"hidden"}},
		"down":      {Command: "in-memory", Enabled: true},
		"disabled":  {Command: "in-memory", Enabled: false},
	}
	old := mcpMgr
	mgr := tools.NewMCPManager(gatewayConfigFixture(cfg), func(ctx context.Context, name string, cfg MCPServerConfig) (*mcp.ClientSession, error) {
		if name == "down" {
			return nil, errors.New("private-token must never reach logs or status")
		}
		if name == "disabled" {
			t.Error("disabled upstream connected")
		}
		server := mcp.NewServer(&mcp.Implementation{Name: "fixture"}, nil)
		for _, name := range []string{"echo", "hidden"} {
			server.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(req.Params.Arguments)}, &mcp.ImageContent{Data: []byte{1, 2, 3}, MIMEType: "image/png"}}, StructuredContent: map[string]any{"kept": true}}, nil
			})
		}
		st, ct := mcp.NewInMemoryTransports()
		session, err := server.Connect(ctx, st, nil)
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { _ = session.Close() })
		return mcp.NewClient(&mcp.Implementation{Name: "loom"}, nil).Connect(ctx, ct, nil)
	})
	mcpMgr = &mcpManager{mgr}
	t.Cleanup(func() { mgr.CloseAll(); mcpMgr = old })
}

type gatewayConfigFixture map[string]MCPServerConfig

func (c gatewayConfigFixture) LoadMCPConfig() (map[string]MCPServerConfig, error) { return c, nil }
func TestGatewayDiscoveryProxyAndStatus(t *testing.T) {
	gatewayTestHome(t)
	gatewayFixturePool(t)
	token, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := storeWebKey("control-only"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	client := mcp.NewClient(&mcp.Implementation{Name: "fixture-client"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: "http://localhost/mcp/loom", HTTPClient: &http.Client{Transport: brainMuxTransport{mux, token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal("down upstream broke discovery", err)
	}
	names := map[string]bool{}
	for _, tool := range list.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"brain_search", "brain_pack", "brain_read", "brain_write", "brain_edit", "memory_index", "memory_read", "memory_write", "memory_delete", "list_skills", "read_skill", "search_discussions", "loom_gateway_status", "ok-server__echo"} {
		if !names[name] {
			t.Fatal("missing", name)
		}
	}
	if len(names) != 14 || names["ok-server__hidden"] {
		t.Fatal(names)
	}
	args := json.RawMessage(`{"large":9007199254740993,"nested":{"value":"untouched"}}`)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ok-server__echo", Arguments: args})
	if err != nil || !result.IsError || result.Content[0].(*mcp.TextContent).Text != string(args) || !bytes.Equal(result.Content[1].(*mcp.ImageContent).Data, []byte{1, 2, 3}) || result.StructuredContent.(map[string]any)["kept"] != true {
		t.Fatal("raw proxy", result, err)
	}
	status, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "loom_gateway_status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(status)
	if bytes.Contains(raw, []byte("private-token")) || !bytes.Contains(raw, []byte("upstream unavailable")) {
		t.Fatal(string(raw))
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := mcpMgr.GatewayCall(canceled, "ok-server", "echo", args); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation", err)
	}
	if _, err := mcpMgr.GatewayCall(ctx, "ok-server", "hidden", args); err == nil {
		t.Fatal("disabled tool called")
	}
}
func TestGatewayAuthScopeOriginAndLimit(t *testing.T) {
	gatewayTestHome(t)
	token, err := rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := storeWebKey("control-only"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerBrainRoutes(mux, nil)
	webAPI(mux)("/api/mcp/gateway", handleMCPGateway)
	call := func(path, key, origin, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://localhost"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	for _, path := range []string{"/mcp/loom", "/mcp/brain"} {
		if w := call(path, token, "", initialize); w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		if w := call(path, "control-only", "", initialize); w.Code != 200 {
			t.Fatal("web auth", w.Code)
		}
		if w := call(path, "wrong", "", initialize); w.Code != 401 {
			t.Fatal("wrong token", w.Code)
		}
		if w := call(path, token, "https://foreign.example", initialize); w.Code != 403 {
			t.Fatal("cross origin", w.Code)
		}
		if w := call(path, token, "", strings.Repeat(" ", 128<<10)+initialize); w.Code == 200 {
			t.Fatal("body limit ignored")
		}
	}
	if w := call("/api/mcp/gateway", token, "", `{"harness":"codex","enabled":true}`); w.Code != 401 {
		t.Fatal("gateway token accepted by control API", w.Code)
	}
	old := token
	token, err = rotateGatewayToken()
	if err != nil {
		t.Fatal(err)
	}
	if w := call("/mcp/brain", old, "", initialize); w.Code != 401 {
		t.Fatal("old token survived rotation", w.Code)
	}
	if w := call("/mcp/brain", token, "", initialize); w.Code != 200 {
		t.Fatal("new token rejected", w.Code)
	}
}
func TestGatewayACPDuplicateAvoidance(t *testing.T) {
	gatewayTestHome(t)
	if err := SetMCPServer("selected", MCPServerConfig{Command: "test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := setGatewayHarness("codex", true); err != nil {
		t.Fatal(err)
	}
	s := RuntimeSession{RuntimeID: "codex"}
	defs, err := acpSessionMCPDefinitions(s)
	if err != nil || len(defs) != 0 {
		t.Fatal("global duplicates", defs, err)
	}
	selected := []string{"selected"}
	s.MCPServers = &selected
	defs, err = acpSessionMCPDefinitions(s)
	if err != nil || len(defs) != 1 {
		t.Fatal("explicit session selection lost", defs, err)
	}
	project, err := saveProjectContext(ChatProject{Name: "Gateway project", MCPServers: &selected})
	if err != nil {
		t.Fatal(err)
	}
	s.ProjectID, s.MCPServers = project.ID, nil
	defs, err = acpSessionMCPDefinitions(s)
	if err != nil || len(defs) != 1 {
		t.Fatal("explicit project selection lost", defs, err)
	}
	s.ProjectID, s.RuntimeID = "", "unregistered"
	defs, err = acpSessionMCPDefinitions(s)
	if err != nil || len(defs) != 1 {
		t.Fatal("unregistered inheritance changed", defs, err)
	}
}
func TestPortableMCPExportImportAndConfinement(t *testing.T) {
	gatewayTestHome(t)
	dir := t.TempDir()
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(brain.Source{ID: "primary", Label: "Primary", Path: dir, Kind: "context", Connector: "folder", Primary: true, Permission: "write"}); err != nil {
		t.Fatal(err)
	}
	cfg := MCPServerConfig{Command: "test", Args: []string{"argument"}, Enabled: true, Env: map[string]string{"TOKEN": "private-env"}, Headers: map[string]string{"Authorization": "private-header"}, DisabledTools: []string{"write"}}
	if err := SetMCPServer("local", cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, portableMCPPath)
	data := gatewayTestRead(t, path)
	if bytes.Contains(data, []byte("private-env")) || bytes.Contains(data, []byte("private-header")) || !bytes.Contains(data, []byte("${secret}")) {
		t.Fatal("secrets exported", string(data))
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	portable, err := readPortableMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	if portable["local"].Env["TOKEN"] != "${secret}" || portable["local"].Headers["Authorization"] != "${secret}" {
		t.Fatal(portable)
	}
	portable["missing"] = cfg
	foreign, _ := json.Marshal(map[string]any{"mcpServers": portable})
	if err := root.WriteFile(portableMCPPath, foreign, 0600); err != nil {
		t.Fatal(err)
	}
	list, err := missingPortableMCP()
	if err != nil || len(list) != 1 || list[0].Name != "missing" || list[0].Config.Env["TOKEN"] != "${secret}" {
		t.Fatal(list, err)
	}
	if _, err := importPortableMCP([]string{"local"}); err == nil {
		t.Fatal("local server overwritten")
	}
	if _, err := importPortableMCP([]string{"missing", "unknown"}); err == nil {
		t.Fatal("partial import accepted")
	}
	local, _ := LoadMCPConfig()
	if _, ok := local["missing"]; ok {
		t.Fatal("partial import wrote data")
	}
	imported, err := importPortableMCP([]string{"missing"})
	if err != nil || len(imported) != 1 {
		t.Fatal(imported, err)
	}
	local, err = LoadMCPConfig()
	c := local["missing"]
	if err != nil || c.Enabled || c.Env["TOKEN"] != "" || c.Headers["Authorization"] != "" || len(c.DisabledTools) != 1 {
		t.Fatal("unsafe import", c, err)
	}
	if err := SetMCPServerEnabled("local", false); err != nil {
		t.Fatal(err)
	}
	portable, err = readPortableMCP(root)
	if err != nil || portable["local"].Enabled {
		t.Fatal("toggle not exported", err)
	}
	if err := DeleteMCPServer("local"); err != nil {
		t.Fatal(err)
	}
	portable, err = readPortableMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := portable["local"]; ok {
		t.Fatal("deletion not exported")
	}
	if err := os.RemoveAll(filepath.Join(dir, ".loom")); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, ".loom")); err != nil {
		t.Skip(err)
	}
	if err := SetMCPServer("safe", cfg); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("export escaped primary brain")
	}
	if _, err := missingPortableMCP(); err == nil {
		t.Fatal("read escaped primary brain")
	}
}

func TestGatewayNamingSurvivesUnavailableUpstream(t *testing.T) {
	a := tools.MCPGatewayTool{Server: "a.b", Tool: &mcp.Tool{Name: "read"}}
	b := tools.MCPGatewayTool{Server: "a b", Tool: &mcp.Tool{Name: "read"}}
	both := gatewayToolNames([]tools.MCPGatewayTool{a, b})
	reverse := gatewayToolNames([]tools.MCPGatewayTool{b, a})
	if both[0] != reverse[1] || both[1] != reverse[0] || both[0] != gatewayToolNames([]tools.MCPGatewayTool{a})[0] || both[1] != gatewayToolNames([]tools.MCPGatewayTool{b})[0] {
		t.Fatal("name changed when upstream unavailable", both, reverse)
	}
}
func TestGatewayTOMLMarkersAndTableBoundaries(t *testing.T) {
	for _, text := range []string{gatewayBegin + "\n", gatewayEnd + "\n", gatewayBegin + "\n" + gatewayBegin + "\n" + gatewayEnd + "\n", gatewayBegin + "\n" + gatewayEnd + "\n" + gatewayEnd + "\n"} {
		if _, _, err := gatewayTOMLBlock(text); err == nil {
			t.Fatal("invalid markers accepted", text)
		}
	}
	gatewayTestHome(t)
	h, _ := gatewaySpec("codex")
	if err := setGatewayHarness(h.id, true); err != nil {
		t.Fatal(err)
	}
	path := expandHome(h.file)
	original := gatewayTestRead(t, path)
	modified := append(original, []byte("required = true\n")...)
	writeMCPTestFile(t, path, string(modified))
	if err := setGatewayHarness(h.id, false); err == nil {
		t.Fatal("unmarked table extension edited")
	}
	if !bytes.Equal(modified, gatewayTestRead(t, path)) {
		t.Fatal("unmarked table extension changed")
	}
	writeMCPTestFile(t, path, string(original)+"\n[features]\nuser_setting = true\n")
	if err := setGatewayHarness(h.id, false); err != nil {
		t.Fatal(err)
	}
	if string(gatewayTestRead(t, path)) != "\n[features]\nuser_setting = true\n" {
		t.Fatal("later table changed")
	}
}
func TestGatewayHTTPShapes(t *testing.T) {
	gatewayTestHome(t)
	if err := storeWebKey("control"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	api := webAPI(mux)
	api("/api/mcp/gateway", handleMCPGateway)
	api("/api/mcp/gateway/token", handleMCPGatewayToken)
	api("/api/mcp/portable", handleMCPPortable)
	api("/api/mcp/portable/import", handleMCPPortableImport)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer control")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		method, path, body string
		fields             []string
	}{
		{"GET", "/api/mcp/gateway", "", []string{"url", "token_set", "harnesses"}},
		{"POST", "/api/mcp/gateway", `{"harness":"codex","enabled":true}`, []string{"url", "token_set", "harnesses"}},
		{"POST", "/api/mcp/gateway/token", `{"rotate":true}`, []string{"ok", "url", "token", "token_set"}},
	} {
		w := call(tc.method, tc.path, tc.body)
		if w.Code != 200 {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		var result map[string]json.RawMessage
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		if len(result) != len(tc.fields) {
			t.Fatal("unexpected fields", result)
		}
		for _, field := range tc.fields {
			if result[field] == nil {
				t.Fatal("missing", field)
			}
		}
		if raw := result["harnesses"]; raw != nil {
			var harnesses []map[string]json.RawMessage
			_ = json.Unmarshal(raw, &harnesses)
			if len(harnesses) != 4 {
				t.Fatal(harnesses)
			}
			for _, h := range harnesses {
				if len(h) != 5 {
					t.Fatal("harness shape", h)
				}
			}
		}
	}
	dir := t.TempDir()
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(brain.Source{ID: "primary", Label: "Primary", Path: dir, Kind: "context", Connector: "folder", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".loom"), 0700); err != nil {
		t.Fatal(err)
	}
	writeMCPTestFile(t, filepath.Join(dir, portableMCPPath), `{"mcpServers":{"missing":{"command":"test","env":{"TOKEN":"${secret}"}}}}`)
	w := call("GET", "/api/mcp/portable", "")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"name":"missing"`)) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call("POST", "/api/mcp/portable/import", `{"names":["missing"]}`)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"imported":["missing"],"ok":true}` {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{"/api/mcp/gateway/token", "/api/mcp/portable/import"} {
		if w := call("GET", path, ""); w.Code != 405 {
			t.Fatal("unsafe method", path, w.Code)
		}
	}
}
func TestPortableMCPFreshInstallationRetainsMissingEntries(t *testing.T) {
	gatewayTestHome(t)
	dir := t.TempDir()
	e, err := theBrain().get()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Update(brain.Source{ID: "primary", Label: "Primary", Path: dir, Kind: "context", Connector: "folder", Permission: "write", Primary: true}); err != nil {
		t.Fatal(err)
	}
	writeMCPTestFile(t, filepath.Join(dir, portableMCPPath), `{"mcpServers":{"from-other-machine":{"command":"test","env":{"KEY":"${secret}"},"enabled":true}}}`)
	if _, err := LoadMCPConfig(); err != nil {
		t.Fatal(err)
	}
	entries, err := missingPortableMCP()
	if err != nil || len(entries) != 1 || entries[0].Name != "from-other-machine" {
		t.Fatal("fresh install erased portable definitions", entries, err)
	}
}
