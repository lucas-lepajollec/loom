package loom

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ACPState is runtime-private display state, never part of prepared messages.
// MCP definitions/env values are deliberately absent from this persisted state.
type ACPState struct {
	MCPServers             *[]string         `json:"mcp_servers,omitempty"`
	NativeSessionID        string            `json:"native_session_id,omitempty"`
	NativeRuntimeID        string            `json:"native_runtime_id,omitempty"`
	NativeContext          string            `json:"native_context,omitempty"`
	Workdir                string            `json:"workdir,omitempty"`
	AdditionalDirs         []string          `json:"additional_dirs,omitempty"`
	Permission             string            `json:"permission,omitempty"`
	Mode                   string            `json:"mode,omitempty"`
	ConfigOptions          map[string]any    `json:"config_options,omitempty"`
	AvailableModes         []map[string]any  `json:"available_modes,omitempty"`
	AvailableConfigOptions []map[string]any  `json:"available_config_options,omitempty"`
	FileBaselines          map[string]string `json:"file_baselines,omitempty"`
	Files                  []ACPChangedFile  `json:"changed_files,omitempty"`
	ACPUsage               map[string]any    `json:"harness_usage,omitempty"`
	Commands               []map[string]any  `json:"commands,omitempty"`
	AgentCapabilities      map[string]any    `json:"agent_capabilities,omitempty"`
}

func cloneACPState(s ACPState) ACPState {
	b, _ := json.Marshal(s)
	var out ACPState
	_ = json.Unmarshal(b, &out)
	return out
}

type acpApproval struct {
	options []map[string]any
	answer  chan acpDecision
}
type acpDecision struct {
	option string
	auto   bool
}
type acpBinding struct {
	mu            sync.Mutex
	emitMu        sync.Mutex
	fsMu          sync.Mutex
	client        *acpClient
	state         ACPState
	roots         []*os.Root
	remoteRoot    string // remote agents: folder on the other machine, never opened locally
	tools         map[string]map[string]any
	approvals     map[string]*acpApproval
	ctx           context.Context
	emit          ChatCallback
	manager       *runtimeSessions
	id            string
	idle          *time.Timer
	mcpRevision   string
	loomModel     bool // the harness runs a Loom model through its launch environment
	loading       bool
	answer        string
	approvalGrace time.Duration
	requestWG     sync.WaitGroup
	active        bool
}

func (p *acpBinding) publish(e DiscussionEvent) bool {
	p.emitMu.Lock()
	defer p.emitMu.Unlock()
	p.mu.Lock()
	emit := p.emit
	state := cloneACPState(p.state)
	p.mu.Unlock()
	if emit == nil {
		return false
	}
	return emit(StreamEvent{ACPEvent: e, ACPState: &state})
}
func (p *acpBinding) close() {
	p.mu.Lock()
	if p.idle != nil {
		p.idle.Stop()
	}
	p.mu.Unlock()
	p.client.close()
	p.fsMu.Lock()
	defer p.fsMu.Unlock()
	for _, root := range p.roots {
		_ = root.Close()
	}
}
func (m *runtimeSessions) closeACP(id string) {
	m.acpMu.Lock()
	p := m.acp[id]
	delete(m.acp, id)
	m.acpMu.Unlock()
	if p != nil {
		p.close()
	}
}
func (m *runtimeSessions) shutdownACP() {
	m.acpMu.Lock()
	ps := m.acp
	m.acp = map[string]*acpBinding{}
	m.acpMu.Unlock()
	for _, p := range ps {
		p.close()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, run := range m.runs {
		run.cancel()
	}
}
func acpContextHash(messages []Message) string {
	b, _ := json.Marshal(messages)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func (m *runtimeSessions) runACP(ctx context.Context, agent acpAgent, s RuntimeSession, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	if !agent.available() {
		return nil, errors.New("CLI du harness ou lanceur ACP indisponible")
	}
	check := acpDirectory
	if agent.Remote {
		check = remoteWorkdir
	}
	canonical, err := check(s.Workdir)
	if err != nil {
		return nil, err
	}
	if canonical != s.Workdir {
		return nil, errors.New("le dossier choisi a changé ; configurez-le à nouveau")
	}
	definitions, err := acpSessionMCPDefinitions(s)
	if err != nil {
		return nil, err
	}
	if agent.Remote {
		// Local MCP commands would run on the other machine: not passed.
		definitions = map[string]MCPServerConfig{}
	}
	encoded, _ := json.Marshal(definitions)
	// A Loom model is passed in the launch environment: changing it (or going
	// back to a native model) restarts the adapter, the native session resumes.
	env := acpLaunchEnv(agent.ID, s.Model)
	encoded = append(encoded, []byte(strings.Join(env, "\n"))...)
	mcpRevision := fmt.Sprintf("%x", sha256.Sum256(encoded))
	if len(turn.Messages) == 0 {
		return nil, errors.New("message ACP requis")
	}
	requestCtx, requestCancel := context.WithCancel(ctx)
	defer requestCancel()
	prefix := acpContextHash(turn.Messages[:len(turn.Messages)-1])
	m.acpMu.Lock()
	p := m.acp[s.ID]
	m.acpMu.Unlock()
	if p != nil {
		p.mu.Lock()
		valid := p.state.NativeRuntimeID == agent.ID && p.state.NativeContext == prefix && p.state.Workdir == s.Workdir && p.mcpRevision == mcpRevision && !p.active
		if p.idle != nil {
			p.idle.Stop()
		}
		p.mu.Unlock()
		select {
		case <-p.client.done:
			valid = false
		default:
		}
		if !valid {
			m.closeACP(s.ID)
			p = nil
		}
	}
	fresh := false
	if p == nil {
		processDir := s.Workdir
		if agent.Remote {
			processDir, _ = os.UserHomeDir()
		}
		c, err := startACPClient(agent.Command, agent.Args, processDir, env...)
		if err != nil {
			return nil, err
		}
		p = &acpBinding{client: c, state: cloneACPState(s.ACPState), tools: map[string]map[string]any{}, approvals: map[string]*acpApproval{}, manager: m, id: s.ID, ctx: requestCtx, emit: emit, active: true, mcpRevision: mcpRevision, loomModel: env != nil}
		if p.state.Permission == "" {
			p.state.Permission = "ask"
		}
		p.state.NativeRuntimeID = agent.ID
		p.remoteRoot = ""
		if agent.Remote {
			p.remoteRoot = s.Workdir
		}
		for _, dir := range append([]string{s.Workdir}, s.AdditionalDirs...) {
			if agent.Remote {
				break
			}
			canonical, err := acpDirectory(dir)
			if err != nil || canonical != dir {
				p.close()
				return nil, errors.New("dossier autorisé inaccessible ou modifié")
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				p.close()
				return nil, errors.New("dossier ACP inaccessible")
			}
			p.roots = append(p.roots, root)
		}
		c.handler = p.handleRequest
		c.notify = p.handleNotification
		if err := c.start(); err != nil {
			p.close()
			return nil, err
		}
		m.acpMu.Lock()
		m.acp[s.ID] = p
		m.acpMu.Unlock()
		initCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		var init struct {
			ProtocolVersion   int            `json:"protocolVersion"`
			AgentCapabilities map[string]any `json:"agentCapabilities"`
		}
		err = c.call(initCtx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": !agent.Remote, "writeTextFile": !agent.Remote}, "terminal": false}, "clientInfo": map[string]string{"name": "loom", "version": Version}}, &init)
		cancel()
		if err != nil || init.ProtocolVersion != 1 {
			m.closeACP(s.ID)
			return nil, errors.New("initialisation ACP incompatible ou échouée")
		}
		p.mu.Lock()
		p.state.AgentCapabilities = init.AgentCapabilities
		p.mu.Unlock()
		servers, err := acpMCPServersFromDefinitions(init.AgentCapabilities, definitions)
		if err != nil {
			m.closeACP(s.ID)
			return nil, err
		}
		params := map[string]any{"cwd": s.Workdir, "additionalDirectories": append([]string{}, s.AdditionalDirs...), "mcpServers": servers}
		var response acpSessionResponse
		load, _ := init.AgentCapabilities["loadSession"].(bool)
		if registered, ok := registeredRuntimes.lookup(agent.ID); ok {
			if adapter, ok := registered.(*acpAdapter); ok {
				adapter.negotiatedMu.Lock()
				adapter.loadSession = load
				adapter.negotiatedMu.Unlock()
			}
		}
		if load && s.NativeSessionID != "" && s.NativeRuntimeID == agent.ID && s.NativeContext == prefix {
			params["sessionId"] = s.NativeSessionID
			p.mu.Lock()
			p.loading = true
			p.mu.Unlock()
			loadCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			err = c.call(loadCtx, "session/load", params, &response)
			cancel()
			p.mu.Lock()
			p.loading = false
			p.mu.Unlock()
			if err == nil {
				response.SessionID = s.NativeSessionID
			}
		} else {
			fresh = true
		}
		if fresh || err != nil {
			fresh = true
			delete(params, "sessionId")
			newCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			err = c.call(newCtx, "session/new", params, &response)
			cancel()
		}
		if err != nil || response.SessionID == "" {
			m.closeACP(s.ID)
			return nil, errors.New("création de session ACP échouée ; vérifiez l’authentification native")
		}
		p.mu.Lock()
		p.state.NativeSessionID = response.SessionID
		p.applySessionResponse(response)
		p.mu.Unlock()
		config := map[string]any{}
		for k, v := range s.ConfigOptions {
			config[k] = v
		}
		// The model picked in Loom's selector wins over the agent's default.
		if s.Model != "" && s.Model != "default" {
			p.mu.Lock()
			option := acpModelOption(p.state.AvailableConfigOptions)
			p.mu.Unlock()
			if id, _ := option["id"].(string); id != "" && option["currentValue"] != s.Model && acpConfigValueAllowed(option, s.Model) {
				config[id] = s.Model
			}
		}
		if err := p.configure(ctx, s.Mode, config); err != nil {
			m.closeACP(s.ID)
			return nil, err
		}
	} else {
		p.mu.Lock()
		p.ctx = requestCtx
		p.answer = ""
		p.tools = map[string]map[string]any{}
		p.emit = emit
		p.active = true
		p.state.MCPServers = cloneACPState(s.ACPState).MCPServers
		p.state.Permission = s.Permission
		if p.state.Permission == "" {
			p.state.Permission = "ask"
		}
		option := acpModelOption(p.state.AvailableConfigOptions)
		p.mu.Unlock()
		// A model picked in Loom since the last turn applies to the live session.
		if id, _ := option["id"].(string); id != "" && s.Model != "" && s.Model != "default" && option["currentValue"] != s.Model && acpConfigValueAllowed(option, s.Model) {
			if err := p.configure(ctx, "", map[string]any{id: s.Model}); err != nil {
				m.closeACP(s.ID)
				return nil, err
			}
		}
	}
	defer func() {
		p.mu.Lock()
		p.active = false
		p.emit = nil
		p.ctx = nil
		p.idle = time.AfterFunc(10*time.Minute, func() {
			m.acpMu.Lock()
			if m.acp[s.ID] == p {
				delete(m.acp, s.ID)
			}
			m.acpMu.Unlock()
			p.close()
		})
		p.mu.Unlock()
	}()
	p.mu.Lock()
	state := cloneACPState(p.state)
	p.mu.Unlock()
	p.publish(DiscussionEvent{"type": "mode", "current": state.Mode, "modes": state.AvailableModes})
	p.publish(DiscussionEvent{"type": "config", "options": state.AvailableConfigOptions})
	var prompt string
	last, _ := turn.Messages[len(turn.Messages)-1].Content.(string)
	// A "/" command must reach the agent exactly as typed, or it is not
	// recognised. On a fresh native session the history is then not sent; the
	// next ordinary message hands it over (see NativeContext below).
	command := strings.HasPrefix(strings.TrimSpace(last), "/")
	if fresh && !command {
		b, _ := json.Marshal(turn.Messages)
		prompt = "Loom portable discussion (role/content; tools are not replayed):\n" + string(b)
	} else {
		prompt = last
	}
	// A cancelled prompt cannot leave an unknown native transcript reusable.
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-stopped:
			return
		case <-ctx.Done():
			_ = p.client.notification("session/cancel", map[string]any{"sessionId": state.NativeSessionID})
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				p.client.close()
			}
		}
	}()
	var result struct {
		StopReason string `json:"stopReason"`
	}
	err = p.client.call(ctx, "session/prompt", map[string]any{"sessionId": state.NativeSessionID, "prompt": []any{map[string]any{"type": "text", "text": prompt}}}, &result)
	requestCancel()
	p.mu.Lock()
	p.active = false
	p.mu.Unlock()
	p.requestWG.Wait()
	if err != nil || result.StopReason == "cancelled" {
		if ctx.Err() != nil {
			_ = p.client.notification("session/cancel", map[string]any{"sessionId": state.NativeSessionID})
		}
		p.mu.Lock()
		p.state.NativeContext = ""
		p.mu.Unlock()
		if err == nil {
			err = context.Canceled
		}
		p.publish(nil)
		p.client.close()
		return nil, err
	}
	// Text captured by the callback is the canonical Loom answer. Hash exactly
	// that portable history so a route/context change starts a fresh handoff.
	p.mu.Lock()
	answer := p.answer
	p.mu.Unlock()
	history := append(append([]Message{}, turn.Messages...), Message{Role: "assistant", Content: answer})
	p.mu.Lock()
	p.state.NativeContext = acpContextHash(history)
	if fresh && command && len(turn.Messages) > 1 {
		// The agent never received the portable history: hand it over next time.
		p.state.NativeContext = ""
	}
	p.mu.Unlock()
	p.publish(nil)
	return []Message{{Role: "assistant", Content: answer}}, nil
}

type acpSessionResponse struct {
	SessionID string `json:"sessionId"`
	Modes     *struct {
		Current   string           `json:"currentModeId"`
		Available []map[string]any `json:"availableModes"`
	} `json:"modes"`
	Config []map[string]any `json:"configOptions"`
	// Older "session model" API (still used by Hermes and others): a model
	// list outside configOptions, changed with session/set_model.
	Models *struct {
		Current   string `json:"currentModelId"`
		Available []struct {
			ID          string `json:"modelId"`
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"availableModels"`
	} `json:"models"`
}

// acpLegacyModelKey marks the model option Loom built from the older API.
const acpLegacyModelKey = "loomLegacyModel"

// options returns configOptions plus, when the agent only uses the older
// model API, an equivalent "model" option so the rest of Loom sees one shape.
func (r acpSessionResponse) options() []map[string]any {
	out := r.Config
	if r.Models == nil || len(r.Models.Available) == 0 || acpModelOption(out) != nil {
		return out
	}
	values := []any{}
	for _, m := range r.Models.Available {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		values = append(values, map[string]any{"value": m.ID, "name": name, "description": m.Description})
	}
	return append(append([]map[string]any{}, out...), map[string]any{"id": "model", "name": "Model", "category": "model", "type": "select",
		"currentValue": r.Models.Current, "options": values, acpLegacyModelKey: true})
}

func (p *acpBinding) applySessionResponse(r acpSessionResponse) {
	if r.Modes != nil {
		p.state.Mode = r.Modes.Current
		p.state.AvailableModes = r.Modes.Available
	}
	if options := r.options(); options != nil {
		p.applyConfig(options)
	}
}
func (p *acpBinding) applyConfig(options []map[string]any) {
	// An update of configOptions keeps the model option built from the older API.
	if legacy := acpModelOption(p.state.AvailableConfigOptions); legacy != nil && legacy[acpLegacyModelKey] == true && acpModelOption(options) == nil {
		options = append(append([]map[string]any{}, options...), legacy)
	}
	p.state.AvailableConfigOptions = options
	p.state.ConfigOptions = map[string]any{}
	for _, option := range options {
		id, _ := option["id"].(string)
		p.state.ConfigOptions[id] = option["currentValue"]
	}
}
func (p *acpBinding) configure(ctx context.Context, mode string, config map[string]any) error {
	p.mu.Lock()
	sid := p.state.NativeSessionID
	state := cloneACPState(p.state)
	p.mu.Unlock()
	if mode != "" {
		found := false
		for _, m := range state.AvailableModes {
			if m["id"] == mode {
				found = true
			}
		}
		if !found {
			return errors.New("mode non annoncé par l’agent")
		}
		if err := p.client.call(ctx, "session/set_mode", map[string]any{"sessionId": sid, "modeId": mode}, nil); err != nil {
			return err
		}
		p.mu.Lock()
		p.state.Mode = mode
		p.mu.Unlock()
	}
	for id, value := range config {
		var found map[string]any
		for _, option := range state.AvailableConfigOptions {
			if option["id"] == id && acpConfigValueAllowed(option, value) {
				found = option
				break
			}
		}
		if found == nil {
			return errors.New("option ou valeur non annoncée par l’agent")
		}
		if found[acpLegacyModelKey] == true {
			if err := p.client.call(ctx, "session/set_model", map[string]any{"sessionId": sid, "modelId": value}, nil); err != nil {
				return err
			}
			p.mu.Lock()
			options := []map[string]any{}
			for _, option := range p.state.AvailableConfigOptions {
				if option["id"] == id {
					copied := map[string]any{}
					for k, v := range option {
						copied[k] = v
					}
					copied["currentValue"] = value
					option = copied
				}
				options = append(options, option)
			}
			p.applyConfig(options)
			p.mu.Unlock()
			continue
		}
		params := map[string]any{"sessionId": sid, "configId": id, "value": value}
		if _, ok := value.(bool); ok {
			params["type"] = "boolean"
		}
		var response struct {
			Options []map[string]any `json:"configOptions"`
		}
		if err := p.client.call(ctx, "session/set_config_option", params, &response); err != nil {
			return err
		}
		p.mu.Lock()
		p.applyConfig(response.Options)
		p.mu.Unlock()
	}
	return nil
}
func acpMCPServers(caps map[string]any) ([]any, error) {
	definitions, err := LoadMCPConfig()
	if err != nil {
		return nil, errors.New("définitions MCP indisponibles")
	}
	return acpMCPServersFromDefinitions(caps, definitions)
}
func acpMCPServersFromDefinitions(caps map[string]any, definitions map[string]MCPServerConfig) ([]any, error) {
	httpCaps, _ := caps["mcpCapabilities"].(map[string]any)
	servers := []any{}
	for _, name := range sortedServerNames(definitions) {
		d := definitions[name]
		if !d.Enabled {
			continue
		}
		// Per-tool masks cannot be enforced by passing a whole server to a harness.
		if len(d.DisabledTools) > 0 {
			return nil, errors.New("MCP ACP exige un serveur activé sans outils masqués")
		}
		pairs := func(values map[string]string) []any {
			out := []any{}
			for _, k := range sortedStringKeys(values) {
				out = append(out, map[string]string{"name": k, "value": values[k]})
			}
			return out
		}
		if d.Command != "" {
			command, err := resolveACPCommand(d.Command)
			if err != nil {
				return nil, errors.New("commande MCP indisponible")
			}
			servers = append(servers, map[string]any{"name": name, "command": command, "args": append([]string{}, d.Args...), "env": pairs(d.Env)})
		} else if httpCaps["http"] == true {
			servers = append(servers, map[string]any{"type": "http", "name": name, "url": d.URL, "headers": pairs(d.Headers)})
		} else {
			return nil, errors.New("cet agent ne prend pas en charge le serveur MCP HTTP activé")
		}
	}
	return servers, nil
}

func acpConfigValueAllowed(option map[string]any, value any) bool {
	if option["type"] == "boolean" {
		_, ok := value.(bool)
		return ok
	}
	v, ok := value.(string)
	if !ok || option["type"] != "select" {
		return false
	}
	var matches func(any) bool
	matches = func(raw any) bool {
		choices, ok := raw.([]any)
		if !ok {
			return false
		}
		for _, rawChoice := range choices {
			choice, ok := rawChoice.(map[string]any)
			if !ok {
				continue
			}
			if choice["value"] == v {
				return true
			}
			if matches(choice["options"]) {
				return true
			}
		}
		return false
	}
	return matches(option["options"])
}

// Explicit session selection overrides the project's selection. Nil inherits
// all globally enabled definitions; an empty selection intentionally sends none.
func acpSessionMCPDefinitions(s RuntimeSession) (map[string]MCPServerConfig, error) {
	definitions, err := LoadMCPConfig()
	if err != nil {
		return nil, errors.New("définitions MCP indisponibles")
	}
	selected := s.MCPServers
	if selected == nil && s.ProjectID != "" {
		if project, ok := getProject(s.ProjectID); ok {
			selected = project.MCPServers
		} else {
			return nil, errors.New("projet introuvable ou verrouillé")
		}
	}
	if selected == nil {
		selected = harnessMCPBinding(s.RuntimeID)
	}
	if selected == nil {
		// All servers enabled in Loom; disabled ones are never passed.
		out := map[string]MCPServerConfig{}
		for name, d := range definitions {
			if d.Enabled {
				out[name] = d
			}
		}
		return out, nil
	}
	out := map[string]MCPServerConfig{}
	for _, name := range *selected {
		definition, ok := definitions[name]
		if !ok {
			return nil, errors.New("serveur MCP sélectionné introuvable")
		}
		out[name] = definition
	}
	return out, nil
}
func validateACPMCPSelection(names []string) error {
	if len(names) > 128 {
		return errors.New("128 serveurs MCP maximum")
	}
	definitions, err := LoadMCPConfig()
	if err != nil {
		return errors.New("définitions MCP indisponibles")
	}
	for _, name := range names {
		if _, ok := definitions[name]; !ok {
			return errors.New("serveur MCP sélectionné introuvable")
		}
	}
	return nil
}
