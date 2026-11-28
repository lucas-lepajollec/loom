package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPConfigSource interface {
	LoadMCPConfig() (map[string]resources.MCPServerConfig, error)
}
type MCPConnector func(context.Context, string, resources.MCPServerConfig) (*mcp.ClientSession, error)

// The application owns persistence. Each record is scoped to the named server
// and only usable with the same configuration (including credentials).
type MCPCatalogStore interface {
	LoadCatalog(name, hash string) []*mcp.Tool
	SaveCatalog(name, hash string, cfg resources.MCPServerConfig, definitions []*mcp.Tool)
	ForgetCatalog(name string)
}

const (
	mcpConnectTimeout = 12 * time.Second
	catalogLifetime   = 5 * time.Minute
	catalogRetry      = 15 * time.Second
	mcpMaxOutput      = 12000
)

type MCPSession struct {
	hash   string
	ready  chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	// Immutable once ready closes.
	sess *mcp.ClientSession
	err  error
	// Protected by the manager lock. Readers retain immutable tool definitions.
	tools              []*mcp.Tool
	schemaBytes        []int
	cached, refreshing bool
	next               time.Time
	latency            int64
	failed             bool
}

func (s *MCPSession) Ready() <-chan struct{} { return s.ready }
func (s *MCPSession) Error() error           { return s.err }

type MCPManager struct {
	config   MCPConfigSource
	connect  MCPConnector
	mu       sync.Mutex
	sessions map[string]*MCPSession
	workers  int
	pending  []func()
	store    MCPCatalogStore
	storeMu  sync.Mutex // serialize snapshot commits with invalidation
}

func NewMCPManager(config MCPConfigSource, connect MCPConnector) *MCPManager {
	store, _ := config.(MCPCatalogStore)
	return &MCPManager{config: config, connect: connect, store: store, sessions: map[string]*MCPSession{}}
}

func configHash(cfg resources.MCPServerConfig) string {
	data, _ := json.Marshal(cfg)
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (m *MCPManager) Probe(ctx context.Context, name string) (bool, bool, error) {
	m.mu.Lock()
	s := m.sessions[name]
	m.mu.Unlock()
	if s == nil {
		return false, false, nil
	}
	select {
	case <-s.ready:
	case <-ctx.Done():
		return true, false, ctx.Err()
	}
	if s.err != nil || s.sess == nil {
		return true, false, errors.New("upstream unavailable")
	}
	err := s.sess.Ping(ctx, nil)
	return true, err == nil, err
}

func retireSession(s *MCPSession) {
	if s == nil {
		return
	}
	s.cancel()
	select {
	case <-s.ready:
		if s.sess != nil {
			go s.sess.Close()
		}
	default: // The connecting worker owns cleanup until publication.
	}
}

func (m *MCPManager) Invalidate(name string) {
	m.storeMu.Lock()
	defer m.storeMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	retireSession(m.sessions[name])
	delete(m.sessions, name)
	if m.store != nil {
		m.store.ForgetCatalog(name)
	}
}

func (m *MCPManager) CloseAll() {
	m.storeMu.Lock()
	defer m.storeMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		retireSession(s)
	}
	clear(m.sessions)
}

// schedule returns immediately. A cold handshake and subsequent tools/list
// refreshes share the same bounded worker pool across all request paths.
func (m *MCPManager) schedule(name string, cfg resources.MCPServerConfig) *MCPSession {
	hash := configHash(cfg)
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[name]
	if s != nil && s.hash != hash {
		retireSession(s)
		s = nil
	}
	if s != nil && (s.refreshing || time.Now().Before(s.next)) {
		return s
	}
	if s != nil && s.sess != nil && !s.failed {
		s.refreshing = true
		m.enqueueLocked(func() { m.discover(name, cfg, s, s.ctx, s.sess) })
		return s
	}
	var cached []*mcp.Tool
	if s != nil {
		cached = s.tools
		retireSession(s)
	} else if m.store != nil {
		cached = m.store.LoadCatalog(name, hash)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s = &MCPSession{hash: hash, ready: make(chan struct{}), ctx: ctx, cancel: cancel, tools: cached, cached: cached != nil, refreshing: true}
	s.schemaBytes = measureSchemas(cached)
	m.sessions[name] = s
	m.enqueueLocked(func() { m.discover(name, cfg, s, ctx, nil) })
	return s
}

// Workers exit when the queue drains, including after shutdown. An entry
// invalidated while queued is canceled before its connector can start.
func (m *MCPManager) enqueueLocked(job func()) {
	if m.workers >= 8 {
		m.pending = append(m.pending, job)
		return
	}
	m.workers++
	go func() {
		for {
			job()
			m.mu.Lock()
			if len(m.pending) == 0 {
				m.workers--
				m.mu.Unlock()
				return
			}
			job = m.pending[0]
			m.pending[0] = nil
			m.pending = m.pending[1:]
			m.mu.Unlock()
		}
	}()
}

func (m *MCPManager) discover(name string, cfg resources.MCPServerConfig, s *MCPSession, parent context.Context, live *mcp.ClientSession) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, mcpConnectTimeout)
	defer cancel()
	session := live
	err := ctx.Err()
	// Recheck the authoritative config after queueing. In particular, an old
	// reader racing an edit must never launch the superseded command.
	if err == nil && m.config != nil {
		latest, loadErr := m.config.LoadMCPConfig()
		current, exists := latest[name]
		if loadErr != nil || !exists || !current.Enabled || configHash(current) != s.hash {
			err = errors.New("MCP configuration changed")
		}
	}
	if err == nil && session == nil {
		session, err = m.connect(ctx, name, cfg)
	}
	definitions := []*mcp.Tool{}
	if err == nil {
		for t, listErr := range session.Tools(ctx, nil) {
			if listErr != nil {
				err = listErr
				break
			}
			if t == nil || t.Name == "" {
				err = errors.New("invalid MCP tool definition")
				break
			}
			if len(definitions) >= 8192 {
				err = errors.New("MCP catalog too large")
				break
			}
			definitions = append(definitions, t)
		}
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	sizes := measureSchemas(definitions)
	persist := err == nil && m.store != nil
	if persist {
		m.storeMu.Lock()
		defer m.storeMu.Unlock()
	}
	m.mu.Lock()
	current := m.sessions[name] == s
	if live == nil {
		if current && err == nil {
			s.sess = session
		} else if session != nil {
			go session.Close()
		}
		s.err = err
		close(s.ready)
	}
	if current {
		s.refreshing = false
		s.failed = err != nil
		s.latency = time.Since(start).Milliseconds()
		s.next = time.Now().Add(catalogRetry)
		if err == nil {
			s.tools, s.schemaBytes = definitions, sizes
			s.cached = false
			s.next = time.Now().Add(catalogLifetime)
		} else {
			s.cached = s.tools != nil
		}
	}
	m.mu.Unlock()
	if current && persist {
		m.store.SaveCatalog(name, s.hash, cfg, definitions)
	}
}

func (m *MCPManager) Ensure(name string, cfg resources.MCPServerConfig) *MCPSession {
	s := m.schedule(name, cfg)
	<-s.ready
	return s
}

func (m *MCPManager) EnsureAll(servers map[string]resources.MCPServerConfig) {
	var pending []*MCPSession
	for name, cfg := range servers {
		if cfg.Enabled && cfg.Transport() != "" {
			pending = append(pending, m.schedule(name, cfg))
		}
	}
	for _, s := range pending {
		<-s.ready
	}
}

type MCPServerStatus struct {
	Name        string   `json:"name"`
	Transport   string   `json:"transport"`
	Enabled     bool     `json:"enabled"`
	Connected   bool     `json:"connected"`
	Cached      bool     `json:"cached,omitempty"`
	LatencyMS   int64    `json:"latency_ms,omitempty"`
	Error       string   `json:"error,omitempty"`
	Tools       []string `json:"tools"`
	Disabled    []string `json:"disabled"`
	Exposure    string   `json:"exposure,omitempty"`
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	URL         string   `json:"url,omitempty"`
	EnvNames    []string `json:"env_names,omitempty"`
	HeaderNames []string `json:"header_names,omitempty"`
}

func (m *MCPManager) Status() ([]MCPServerStatus, error) {
	out, _, err := m.StatusWithTools()
	return out, err
}

func (m *MCPManager) StatusWithTools() ([]MCPServerStatus, []MCPGatewayTool, error) {
	servers, err := m.config.LoadMCPConfig()
	if err != nil {
		return nil, nil, err
	}
	out := make([]MCPServerStatus, 0, len(servers))
	var exposed []MCPGatewayTool
	for _, name := range resources.SortedServerNames(servers) {
		cfg := servers[name]
		st := MCPServerStatus{
			Name: name, Transport: cfg.Transport(), Enabled: cfg.Enabled, Type: cfg.Type,
			Exposure: cfg.Exposure, Description: cfg.Description, Command: cfg.Command, Args: cfg.Args, URL: cfg.URL,
			EnvNames: slices.Sorted(maps.Keys(cfg.Env)), HeaderNames: slices.Sorted(maps.Keys(cfg.Headers)),
			Tools: []string{}, Disabled: append([]string{}, cfg.DisabledTools...),
		}
		if cfg.Enabled && cfg.Transport() != "" {
			s := m.schedule(name, cfg)
			m.mu.Lock()
			st.Connected = s.sess != nil && !s.failed
			st.Cached = s.cached || s.refreshing && s.tools != nil
			st.LatencyMS = s.latency
			definitions, sizes := s.tools, s.schemaBytes
			if s.failed {
				st.Error = "upstream unavailable"
			}
			m.mu.Unlock()
			for i, t := range definitions {
				st.Tools = append(st.Tools, t.Name)
				if !cfg.ToolDisabled(t.Name) {
					exposed = append(exposed, MCPGatewayTool{name, t, sizes[i]})
				}
			}
		}
		out = append(out, st)
	}
	return out, exposed, nil
}

// Legacy blocking API for explicit discovery.
func (m *MCPManager) Tools() []Tool {
	servers, err := m.config.LoadMCPConfig()
	if err != nil {
		return nil
	}
	m.EnsureAll(servers)
	all, _, _ := m.GatewayTools()
	var out []Tool
	for _, item := range all {
		t := item.Tool
		desc := t.Description
		if desc == "" {
			desc = t.Title
		}
		out = append(out, Tool{Type: "function", Function: ToolFunction{Name: MCPExposedName(item.Server, t.Name), Description: "[MCP: " + item.Server + "] " + desc, Parameters: MCPNormalizeSchema(t.InputSchema)}})
	}
	return out
}
func (m *MCPManager) PromptLine() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for name, s := range m.sessions {
		if s.sess != nil && len(s.tools) > 0 {
			names = append(names, fmt.Sprintf("%s (%d)", name, len(s.tools)))
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return "\n\nMCP servers connected (their tools are prefixed mcp__<server>__<tool>): " + strings.Join(names, ", ") + "."
}

func (m *MCPManager) Call(name string, args map[string]any) string {
	all, _, _ := m.GatewayTools()
	for _, item := range all {
		if MCPExposedName(item.Server, item.Tool.Name) != name {
			continue
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return "[error] invalid MCP arguments"
		}
		result, err := m.GatewayCall(context.Background(), item.Server, item.Tool.Name, raw)
		if err != nil {
			result, err = m.GatewayCall(context.Background(), item.Server, item.Tool.Name, raw)
		}
		if err != nil {
			return "[MCP error] " + err.Error()
		}
		out := FlattenMCPContent(result)
		if runes := []rune(out); len(runes) > mcpMaxOutput {
			out = string(runes[:mcpMaxOutput]) + "\n…[truncated]"
		}
		if result.IsError {
			return "[the tool returned an error]\n" + out
		}
		if out == "" {
			return "[ok] (no output)"
		}
		return out
	}
	return "[error] MCP tool not enabled: " + name
}

func measureSchemas(definitions []*mcp.Tool) []int {
	sizes := make([]int, len(definitions))
	for i, t := range definitions {
		data, _ := json.Marshal(t)
		sizes[i] = len(data)
	}
	return sizes
}
