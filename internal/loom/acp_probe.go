package loom

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// An ACP agent only reveals its models, modes and settings inside a session.
// The probe opens one in an empty temporary folder, sends no prompt (so it
// costs nothing), records what the agent announces, and closes it.

type acpProbe struct {
	At       int64            `json:"at"`
	Agent    map[string]any   `json:"agent,omitempty"` // agentInfo: name, version
	Auth     []map[string]any `json:"auth,omitempty"`  // authMethods
	Caps     map[string]any   `json:"capabilities,omitempty"`
	Modes    []map[string]any `json:"modes,omitempty"`
	Mode     string           `json:"mode,omitempty"`
	Config   []map[string]any `json:"config,omitempty"`
	Commands []map[string]any `json:"commands,omitempty"` // "/" commands announced by the agent
	Error    string           `json:"error,omitempty"`
	Duration float64          `json:"duration_seconds,omitempty"`
}

const acpProbeKey = "acp_probe_"

var acpProbeMu sync.Mutex

func loadACPProbe(id string) (acpProbe, bool) {
	var p acpProbe
	ok := getStoreJSON(bkState, acpProbeKey+id, &p) && p.At > 0
	return p, ok
}

func probeACPAgent(ctx context.Context, agent acpAgent) acpProbe {
	started := time.Now()
	out := acpProbe{At: started.UnixMilli()}
	fail := func(err error) acpProbe {
		out.Error = err.Error()
		out.Duration = time.Since(started).Seconds()
		return out
	}
	if !agent.available() {
		return fail(errors.New("CLI du harness ou lanceur ACP indisponible"))
	}
	dir, err := os.MkdirTemp("", "loom-acp-probe-")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(dir)
	cwd := dir
	if agent.Remote {
		cwd, _ = os.UserHomeDir()
	}
	c, err := startACPClient(agent.Command, agent.Args, cwd)
	if err != nil {
		return fail(err)
	}
	defer c.close()
	// The probe never grants anything: every client request is refused.
	c.handler = func(*acpFrame) (any, error) { return nil, errors.New("sonde Loom") }
	commands := make(chan []map[string]any, 4)
	c.notify = func(f acpFrame) {
		var params struct {
			Update map[string]any `json:"update"`
		}
		if json.Unmarshal(f.Params, &params) != nil || params.Update["sessionUpdate"] != "available_commands_update" {
			return
		}
		list, _ := params.Update["availableCommands"].([]any)
		out := []map[string]any{}
		for _, raw := range list {
			if m, ok := raw.(map[string]any); ok {
				out = append(out, m)
			}
		}
		select {
		case commands <- out:
		default:
		}
	}
	if err := c.start(); err != nil {
		return fail(err)
	}
	initCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var init struct {
		ProtocolVersion   int              `json:"protocolVersion"`
		AgentCapabilities map[string]any   `json:"agentCapabilities"`
		AgentInfo         map[string]any   `json:"agentInfo"`
		AuthMethods       []map[string]any `json:"authMethods"`
	}
	if err := c.call(initCtx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false}, "clientInfo": map[string]string{"name": "loom", "version": Version}}, &init); err != nil || init.ProtocolVersion != 1 {
		return fail(errors.New("l’agent ne répond pas au protocole ACP"))
	}
	out.Agent, out.Auth, out.Caps = init.AgentInfo, init.AuthMethods, init.AgentCapabilities
	if agent.Remote {
		// The remote folder is unknown before the user picks one.
		out.Duration = time.Since(started).Seconds()
		return out
	}
	var session acpSessionResponse
	if err := c.call(initCtx, "session/new", map[string]any{"cwd": dir, "mcpServers": []any{}}, &session); err != nil || session.SessionID == "" {
		return fail(errors.New("session refusée : vérifie que le CLI est connecté à ton compte"))
	}
	if session.Modes != nil {
		out.Modes, out.Mode = session.Modes.Available, session.Modes.Current
	}
	out.Config = session.Config
	// Agents announce their commands right after session/new.
	select {
	case out.Commands = <-commands:
	case <-time.After(2 * time.Second):
	}
	out.Duration = time.Since(started).Seconds()
	return out
}

func refreshACPProbe(ctx context.Context, agent acpAgent) acpProbe {
	acpProbeMu.Lock()
	defer acpProbeMu.Unlock()
	p := probeACPAgent(ctx, agent)
	if old, ok := loadACPProbe(agent.ID); ok && p.Error != "" && len(old.Config) > 0 {
		// Keep the last good catalog; only report the new failure.
		old.Error, old.At = p.Error, p.At
		p = old
	}
	_ = putStoreJSON(bkState, acpProbeKey+agent.ID, p)
	return p
}

// probeMissingACPAgents runs once in the background at startup for available
// agents that have never been probed.
func probeMissingACPAgents() {
	for _, d := range runtimeCatalog() {
		agent, ok := acpAgentFor(d.ID)
		if !ok || !agent.available() {
			continue
		}
		if _, done := loadACPProbe(agent.ID); done {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		refreshACPProbe(ctx, agent)
		cancel()
	}
}

// acpModelOption returns the agent's model selector, if it announced one.
func acpModelOption(config []map[string]any) map[string]any {
	for _, o := range config {
		if o["category"] == "model" {
			return o
		}
	}
	for _, o := range config {
		if id, _ := o["id"].(string); strings.EqualFold(id, "model") {
			return o
		}
	}
	return nil
}

type acpOptionValue struct{ Value, Name, Description string }

func acpOptionValues(option map[string]any) []acpOptionValue {
	out := []acpOptionValue{}
	var walk func(any)
	walk = func(raw any) {
		list, _ := raw.([]any)
		for _, item := range list {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			if inner, ok := m["options"]; ok {
				walk(inner)
				continue
			}
			v, _ := m["value"].(string)
			n, _ := m["name"].(string)
			d, _ := m["description"].(string)
			if v != "" {
				out = append(out, acpOptionValue{v, n, d})
			}
		}
	}
	if option != nil {
		walk(option["options"])
	}
	return out
}

// GET: cached probe. POST: run a new probe now.
func handleACPProbe(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	agent, ok := acpAgentFor(id)
	if !ok {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "harness ACP introuvable"})
		return
	}
	if r.Method == http.MethodGet {
		p, _ := loadACPProbe(id)
		sendJSON(w, 200, map[string]any{"ok": true, "probe": p})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct{}
	if !workspaceDecode(w, r, &req) {
		return
	}
	p := refreshACPProbe(r.Context(), agent)
	if p.Error != "" && len(p.Config) == 0 {
		sendJSON(w, 502, map[string]any{"ok": false, "error": p.Error, "probe": p})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "probe": p})
}
