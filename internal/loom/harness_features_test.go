package loom

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/antigravity"
)

// Expectations are independent of the descriptor. The source markers tie each
// advertised path to its wire call/launch consumer; fixture tests exercise it.
func TestHarnessFeaturePaths(t *testing.T) {
	testHome(t)
	for _, tt := range []struct {
		id, protocol                           string
		permissions, modes, sources            []string
		effort, questions, forms, mcp, history bool
		file                                   string
		paths                                  []string
	}{
		{"codex", "app-server", []string{"ask", "full"}, []string{}, []string{"native", "loom"}, true, true, true, false, true, "runtime/codexapp/session.go", []string{"model/list", "thread/resume", "approvalPolicy", "p.Effort", "p.SandboxPolicy"}},
		{"pi", "pi-rpc", []string{}, []string{}, []string{"native", "loom"}, true, true, false, false, true, "runtime/pirpc/session.go", []string{"get_available_models", "switch_session", "set_thinking_level", "extension_ui_request"}},
		{"opencode", "opencode-http", []string{}, []string{}, []string{"native", "loom"}, false, true, false, false, true, "runtime/opencodehttp/session.go", []string{"question", "permission", "Model"}},
		{"antigravity", "agy-stream-json", []string{}, []string{"accept-edits", "plan", "full"}, []string{"native"}, true, false, false, false, false, "runtime/antigravity/stream.go", []string{"--effort", "--sandbox", "--conversation", "--mode"}},
		{"claude-code", "acp", []string{"ask", "edits", "full"}, []string{}, []string{"native", "loom"}, false, true, true, true, false, "acp_session.go", []string{"session/set_mode", "session/set_model", "session/set_config_option"}},
		{"hermes", "acp", []string{"ask", "edits", "full"}, []string{}, []string{"native"}, false, false, true, true, false, "acp_permissions.go", []string{"session/request_permission", "session/create_elicitation"}},
		{"openclaw", "acp", []string{"ask", "edits", "full"}, []string{}, []string{"native"}, false, false, true, true, false, "acp_permissions.go", []string{"session/request_permission", "session/create_elicitation"}},
		{"deepseek-harness", "acp", []string{"ask", "edits", "full"}, []string{}, []string{"native"}, true, false, false, true, false, "acp_session.go", []string{"session/resume", "session/new", "session/set_config_option"}},
		{"registry-example", "acp", []string{"ask", "edits", "full"}, []string{}, []string{"native"}, false, false, true, true, false, "acp_session.go", []string{"session/new", "session/set_config_option"}},
		{"custom-example", "acp", []string{"ask", "edits", "full"}, []string{}, []string{"native"}, false, false, true, true, false, "acp_session.go", []string{"session/new", "session/set_config_option"}},
	} {
		t.Run(tt.id+"/"+tt.protocol, func(t *testing.T) {
			f := harnessFeatures(acpAgent{ID: tt.id}, tt.protocol, acpProbe{})
			if !reflect.DeepEqual(f.Permissions, tt.permissions) || !reflect.DeepEqual(f.Modes, tt.modes) || !reflect.DeepEqual(f.ModelSources, tt.sources) {
				t.Fatalf("unexpected controls: %+v", f)
			}
			if f.Effort != tt.effort || f.Questions != tt.questions || f.Forms != tt.forms || f.MCPSelection != tt.mcp || f.HistoryImport != tt.history {
				t.Fatalf("unsupported path advertised: %+v", f)
			}
			source, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range tt.paths {
				if !strings.Contains(string(source), path) {
					t.Fatalf("advertised %s path missing: %s", tt.id, path)
				}
			}
			for _, mode := range f.Modes {
				if tt.protocol == "agy-stream-json" {
					args, err := (antigravity.TurnConfig{Mode: mode}).Args()
					if err != nil || len(args) == 0 {
						t.Fatal(mode, args, err)
					}
				}
			}
			for _, policy := range f.Permissions {
				if !slices.Contains([]string{"ask", "edits", "full"}, policy) {
					t.Fatal("permission has no handler", policy)
				}
			}
		})
	}
}

func TestHarnessDiscoveryAndProbeProjection(t *testing.T) {
	testHome(t)
	p := acpProbe{Caps: map[string]any{"loadSession": true, "sessionCapabilities": map[string]any{"list": map[string]any{}}}, Modes: []map[string]any{{"id": "plan"}, {"id": "code"}}, Config: []map[string]any{{"id": "model", "category": "model"}, {"id": "effort", "category": "thought_level"}}}
	f := harnessFeatures(acpAgent{ID: "registry-example", Custom: true}, "acp", p)
	if !f.Models || !f.Effort || !f.Resume || !f.SessionList || !f.HistoryImport || !slices.Contains(f.Modes, "plan") || !slices.Contains(f.ConfigOptions, "effort") {
		t.Fatal(f)
	}
	p.Modes = append(p.Modes, map[string]any{"id": "default"})
	p.Caps["sessionCapabilities"].(map[string]any)["additionalDirectories"] = map[string]any{}
	p.Config = append(p.Config, map[string]any{"id": "sandbox"})
	// A cached ACP fallback announcement must not resurrect dead native controls.
	a := acpAgent{ID: "deepseek-harness", Custom: true}
	projectHarnessProbe(a, &p)
	if len(p.Modes) != 0 || p.Features.Forms || p.Features.HistoryImport || p.Features.AdditionalDirs || p.Features.Sandbox || len(p.Config) != 1 {
		t.Fatal(p)
	}
	for _, absent := range []any{nil, false} {
		f := harnessFeatures(acpAgent{ID: "registry-example"}, "acp", acpProbe{Caps: map[string]any{"sessionCapabilities": map[string]any{"list": absent, "additionalDirectories": absent}}})
		if f.SessionList || f.AdditionalDirs {
			t.Fatal("absent capability advertised", f)
		}
	}
	bridge := harnessFeatures(acpAgent{ID: "custom-agy", Command: "loom", Args: []string{"agy-acp"}, Custom: true}, "acp", p)
	if bridge.Approvals || bridge.Forms || bridge.Questions || bridge.MCPSelection || len(bridge.Permissions) != 0 || bridge.DefaultMode != "accept-edits" {
		t.Fatal("custom Antigravity bridge advertised interactive controls", bridge)
	}
}

func TestOnlyUsableLocalResourceAgents(t *testing.T) {
	testHome(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if !localHarnessUsable("antigravity") || localHarnessUsable("hermes") {
		t.Fatal("local executable discovery")
	}
	for _, target := range visibleSkillSinks(skillSinkDefs()) {
		for _, id := range target.Harnesses {
			if !localHarnessUsable(id) || id == "gemini" {
				t.Fatal(target)
			}
		}
	}
	for _, h := range gatewayHarnessSpecs {
		if h.id == "gemini" {
			t.Fatal(h)
		}
	}
	for _, h := range brainAgentSpecs() {
		if h.id == "gemini" {
			t.Fatal(h)
		}
	}
}

func TestDeepSeekNativeLauncherPreference(t *testing.T) {
	testHome(t)
	bin := t.TempDir()
	marker := filepath.Join(bin, "args")
	t.Setenv("PATH", bin)
	t.Setenv("LOOM_DSH_ARGS", marker)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$LOOM_DSH_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(bin, "dsh"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	c, err := startACPClient("npx", []string{"-y", "@deepseek-ai/dsh@0.2.0-rc.2", "--profile", "acp"}, bin)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	if err = c.start(); err != nil {
		t.Fatal(err)
	}
	<-c.done
	args, err := os.ReadFile(marker)
	if err != nil || string(args) != "--profile\nacp\n" {
		t.Fatal(string(args), err)
	}
}

func TestGeminiOffersAreRejected(t *testing.T) {
	testHome(t)
	for _, a := range []acpAgent{{Name: "Gemini CLI", Command: "gemini"}, {Name: "Custom", Command: "npx", Args: []string{"-y", "@google/gemini-cli@1"}}, {Name: "Remote", Command: "ssh", Args: []string{"host", "gemini", "--acp"}}} {
		if _, err := validCustomACPAgent(a); err == nil {
			t.Fatal("Gemini custom offer accepted", a)
		}
	}
	a := registryAgent{ID: "alias", Name: "Other name"}
	a.Distribution.NPX = &registryLaunch{Package: "@google/gemini-cli@1"}
	if rows := catalogEntries([]registryAgent{a}, nil, lifecycleLookPath); len(rows) != 0 {
		t.Fatal(rows)
	}
	if _, err := a.agent("linux-x86_64", lifecycleLookPath); err == nil {
		t.Fatal("Gemini registry alias accepted")
	}
}

func TestHarnessAncillaryFeaturePaths(t *testing.T) {
	testHome(t)
	// Every advertised boolean has an independently enumerated consumer. Adding
	// a descriptor flag therefore requires updating this evidence, not only copy.
	evidence := map[string]struct{ file, marker string }{
		"models": {"acp_session.go", "session/set_model"}, "effort": {"acp_session.go", "session/set_config_option"},
		"workdir": {"acp_session.go", "\"cwd\""}, "additional_dirs": {"acp_session.go", "additionalDirectories"},
		"sandbox": {"runtime/antigravity/stream.go", "--sandbox"}, "resume": {"acp_session.go", "session/load"},
		"session_list": {"acp_import.go", "session/list"}, "history_import": {"acp_import.go", "readNativeACPSession"},
		"questions": {"acp_permissions.go", "AskUserQuestions"}, "forms": {"acp_permissions.go", "session/create_elicitation"},
		"approvals": {"acp_permissions.go", "session/request_permission"}, "plan": {"acp_events.go", "\"plan\""},
		"usage": {"acp_events.go", "usage_update"}, "quota": {"acp_registry.go", "readHarnessQuota"},
		"mcp_selection": {"acp_session.go", "acpMCPServersFromDefinitions"}, "mcp_gateway": {"mcp_gateway_harness.go", "setGatewayHarness"},
		"skills": {"skills_sink.go", "SyncSkillSinks"}, "memory": {"brain_agents.go", "applyBrainAgent"},
		"terminal": {"harness_resume.go", "nativeResumeCommand"}, "remote": {"node_bridge.go", "nodeMachineAccess"},
	}
	expected := map[string][]string{
		"codex":            {"models", "effort", "workdir", "additional_dirs", "resume", "session_list", "history_import", "questions", "forms", "approvals", "plan", "usage", "quota", "mcp_gateway", "skills", "memory", "terminal"},
		"pi":               {"models", "effort", "workdir", "resume", "session_list", "history_import", "questions", "usage", "skills", "terminal"},
		"opencode":         {"models", "workdir", "resume", "session_list", "history_import", "questions", "approvals", "plan", "usage", "mcp_gateway", "memory", "terminal"},
		"antigravity":      {"models", "effort", "workdir", "additional_dirs", "sandbox", "resume", "plan", "usage", "quota", "mcp_gateway", "skills", "memory", "terminal"},
		"claude-code":      {"workdir", "questions", "forms", "approvals", "plan", "usage", "quota", "mcp_selection", "mcp_gateway", "skills", "memory", "terminal"},
		"hermes":           {"workdir", "forms", "approvals", "plan", "usage", "quota", "mcp_selection", "terminal"},
		"openclaw":         {"workdir", "forms", "approvals", "plan", "usage", "mcp_selection"},
		"deepseek-harness": {"models", "effort", "workdir", "resume", "session_list", "approvals", "usage", "mcp_selection"},
		"custom-example":   {"workdir", "forms", "approvals", "plan", "usage", "remote"},
	}
	if piBrainInstructions() != "" {
		expected["pi"] = append(expected["pi"], "memory")
	}
	for _, a := range append(builtinACPAgents(), acpAgent{ID: "antigravity"}, acpAgent{ID: "custom-example", Custom: true, Remote: true}) {
		protocol := "acp"
		switch a.ID {
		case "codex":
			protocol = "app-server"
		case "pi":
			protocol = "pi-rpc"
		case "opencode":
			protocol = "opencode-http"
		case "antigravity":
			protocol = "agy-stream-json"
		}
		f := harnessFeatures(a, protocol, acpProbe{})
		raw, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		for key, value := range fields {
			if on, ok := value.(bool); ok && on != slices.Contains(expected[a.ID], key) {
				t.Fatalf("%s/%s advertises %v without a path for this protocol", a.ID, key, on)
			}
			if on, ok := value.(bool); ok && on {
				path, ok := evidence[key]
				if !ok {
					t.Fatalf("%s advertises %s without consumer evidence", a.ID, key)
				}
				source, err := os.ReadFile(path.file)
				if err != nil || !strings.Contains(string(source), path.marker) {
					t.Fatalf("%s/%s consumer missing: %s/%s (%v)", a.ID, key, path.file, path.marker, err)
				}
			}
		}
	}
}
