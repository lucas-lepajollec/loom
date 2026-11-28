package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	"github.com/lucas-lepajollec/loom/internal/loom/resources"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpListTool = "loom_mcp_list"

var mcpEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type mcpRegistration struct {
	Action        string   `json:"action,omitempty"`
	Name          string   `json:"name"`
	Transport     string   `json:"transport,omitempty"`
	Command       *string  `json:"command,omitempty"`
	Args          []string `json:"args,omitempty"`
	URL           *string  `json:"url,omitempty"`
	EnvNames      []string `json:"env_names,omitempty"`
	Exposure      *string  `json:"exposure,omitempty"`
	Description   *string  `json:"description,omitempty"`
	Enabled       *bool    `json:"enabled,omitempty"`
	DisabledTools []string `json:"disabled_tools,omitempty"`
}

func registerMCPManagement(s *mcp.Server) {
	s.AddTool(&mcp.Tool{Name: mcpListTool, Description: "List configured external servers, exposure, available tool counts, latency and credential key names.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		rows, all, err := mcpMgr.StatusWithTools()
		if err != nil {
			return nil, err
		}
		counts := map[string]int{}
		for _, t := range all {
			counts[t.Server]++
		}
		settings, err := loadGatewayState()
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, r := range rows {
			out = append(out, map[string]any{"name": r.Name, "transport": r.Transport, "enabled": r.Enabled, "exposure": mcpExposure(r.Exposure, settings.ToolMode), "connected": r.Connected, "cached": r.Cached, "tools_count": counts[r.Name], "latency_ms": r.LatencyMS, "error": r.Error, "env_names": r.EnvNames, "header_names": r.HeaderNames})
		}
		return mcpJSONResult(map[string]any{"servers": out})
	})
	s.AddTool(&mcp.Tool{Name: mcpRegisterTool, Description: "Propose a server change for owner approval. action is create (default), update, toggle, set_exposure or delete. Specify transport on create. Updates patch supplied fields. env_names adds empty placeholders. disabled_tools replaces the hidden tool list.", InputSchema: mcpObjectSchema([]string{"name"}, map[string]string{"action": "string", "name": "string", "transport": "string", "command": "string", "args": "strings", "url": "string", "env_names": "strings", "exposure": "string", "description": "string", "enabled": "boolean", "disabled_tools": "strings"})}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return registerProposedMCP(ctx, req.Params.Arguments)
	})
}

func (a mcpRegistration) proposed(old MCPServerConfig) (MCPServerConfig, error) {
	cfg := old
	switch a.Action {
	case "delete":
		return old, nil
	case "toggle":
		if a.Enabled == nil {
			return cfg, errors.New("toggle requires enabled")
		}
		cfg.Enabled = *a.Enabled
	case "set_exposure":
		if a.Exposure == nil {
			return cfg, errors.New("set_exposure requires exposure")
		}
		cfg.Exposure = *a.Exposure
	case "create", "update":
		if a.Action == "create" {
			if a.Transport == "" {
				return cfg, errors.New("transport required on create")
			}
			cfg = MCPServerConfig{Enabled: true, Exposure: resources.MCPOnDemand}
		}
		if a.Transport != "" && a.Transport != cfg.Transport() {
			switch a.Transport {
			case "http":
				cfg.Command, cfg.Args, cfg.Env, cfg.Type = "", nil, nil, ""
			case "stdio":
				cfg.URL, cfg.Headers, cfg.Type = "", nil, ""
			default:
				return cfg, errors.New("invalid transport")
			}
		}
		if a.Enabled != nil {
			cfg.Enabled = *a.Enabled
		}
		if a.Exposure != nil {
			cfg.Exposure = *a.Exposure
		}
		if a.Description != nil {
			cfg.Description = *a.Description
		}
		if a.Command != nil {
			cfg.Command = strings.TrimSpace(*a.Command)
		}
		if a.URL != nil {
			cfg.URL = strings.TrimSpace(*a.URL)
		}
		if a.Args != nil {
			cfg.Args = a.Args
		}
		if a.DisabledTools != nil {
			cfg.DisabledTools = a.DisabledTools
		}
		if len(a.EnvNames) > 0 {
			cfg.Env = maps.Clone(cfg.Env)
			if cfg.Env == nil {
				cfg.Env = map[string]string{}
			}
			for _, key := range a.EnvNames {
				if !mcpEnvName.MatchString(key) {
					return cfg, errors.New("invalid environment variable name")
				}
				if _, exists := cfg.Env[key]; !exists {
					cfg.Env[key] = ""
				}
			}
		}
		if a.Transport != "" && cfg.Transport() != a.Transport {
			return cfg, errors.New("transport must match command or URL")
		}
	default:
		return cfg, errors.New("unknown MCP management action")
	}
	if cfg.Transport() == "http" && (!resources.ValidMCPURL(cfg.URL) || len(cfg.Args) > 0 || len(cfg.Env) > 0) {
		return cfg, errors.New("invalid HTTP MCP configuration")
	}
	return cfg, cfg.Validate()
}

func registerProposedMCP(ctx context.Context, raw json.RawMessage) (*mcp.CallToolResult, error) {
	var a mcpRegistration
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&a) != nil {
		return nil, errors.New("invalid MCP proposal; use env_names for names only")
	}
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" || mcpNameRe.MatchString(a.Name) || strings.Contains(a.Name, "__") {
		return nil, errors.New("invalid MCP server name")
	}
	if a.Action == "" {
		a.Action = "create"
	}
	config, err := LoadMCPConfig()
	if err != nil {
		return nil, err
	}
	old, exists := config[a.Name]
	if !exists && a.Action != "create" {
		return nil, errors.New("unknown MCP server")
	}
	cfg, err := a.proposed(old)
	if err != nil {
		return nil, err
	}
	mgr, session := policyTurn(ctx)
	in := sessionPolicyInput(session, "mcp.manage", policy.Confirm)
	in.Command, in.Endpoint = cfg.Command, cfg.URL
	// Only proposed values and saved credential names belong in the prompt.
	changes, _ := json.Marshal(a)
	in.Detail = fmt.Sprintf("%s MCP server %q: %s", a.Action, a.Name, changes)
	if len(cfg.Env)+len(cfg.Headers) > 0 {
		in.Detail += fmt.Sprintf(" (credential keys in resulting configuration: env %v, headers %v)", slices.Sorted(maps.Keys(cfg.Env)), slices.Sorted(maps.Keys(cfg.Headers)))
	}
	if err := authorizeMCPPolicy(ctx, mgr, in, true); err != nil {
		return nil, err
	}
	err = applyMCPProposal(a, old, exists, cfg)
	if err != nil {
		return nil, err
	}
	return mcpJSONResult(map[string]any{"name": a.Name, "action": a.Action, "enabled": a.Action != "delete" && cfg.Enabled, "exposure": cfg.Exposure, "env_to_fill": a.EnvNames})
}

// Compare the exact reviewed revision after confirmation, under the same lock
// used by Connections and file edits. A concurrent change needs fresh review.
func applyMCPProposal(a mcpRegistration, old MCPServerConfig, exists bool, cfg MCPServerConfig) error {
	mcpConfigMu.Lock()
	defer mcpConfigMu.Unlock()
	config, err := loadMCPConfigForWriteLocked()
	if err != nil {
		return err
	}
	current, found := config[a.Name]
	if a.Action == "create" && found {
		return errMCPAdoptConflict
	}
	if found != exists || !reflect.DeepEqual(old, current) {
		return errors.New("MCP server changed during review")
	}
	if a.Action == "delete" {
		delete(config, a.Name)
	} else {
		config[a.Name] = cfg
	}
	if err := saveMCPConfigLocked(config); err != nil {
		return err
	}
	mcpInvalidate(a.Name)
	return nil
}
