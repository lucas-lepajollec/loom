package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPConfigSource leaves authoritative configuration and migration in Loom.
type MCPConfigSource interface {
	LoadMCPConfig() (map[string]resources.MCPServerConfig, error)
}
type MCPConnector func(context.Context, string, resources.MCPServerConfig) (*mcpsdk.ClientSession, error)

func NewMCPManager(config MCPConfigSource, connect MCPConnector) *MCPManager {
	return &MCPManager{config: config, connect: connect, sessions: map[string]*MCPSession{}, registry: map[string]mcpToolRef{}}
}
func (s *MCPSession) Ready() <-chan struct{} { return s.ready }
func (s *MCPSession) Error() error           { return s.err }

const (
	// mcpConnectTimeout borne l'établissement d'une session (handshake initialize
	// + tools/list). Un serveur qui rame ne doit pas figer le tour de chat.
	mcpConnectTimeout = 20 * time.Second
	// mcpCallTimeout borne un appel d'outil MCP.
	mcpCallTimeout = 120 * time.Second
	// mcpMaxOutput cap la sortie renvoyée au modèle (cohérent avec toolMaxOutput).
	mcpMaxOutput = 12000
)

// MCPSession est une connexion vivante à un serveur MCP.
//
// L'entrée est publiée dans le pool AVANT que la connexion aboutisse, pour qu'un
// second appelant se mette en attente au lieu de lancer un deuxième process. Les
// champs sess/tools/err ne doivent donc être lus qu'une fois `ready` fermé.
type MCPSession struct {
	name  string
	cfg   resources.MCPServerConfig
	sess  *mcpsdk.ClientSession
	tools []*mcpsdk.Tool
	err   error         // dernière erreur de connexion/liste, pour l'UI
	ready chan struct{} // fermé quand la tentative de connexion est terminée
}

// MCPManager détient le pool de sessions.
type MCPManager struct {
	config   MCPConfigSource
	connect  MCPConnector
	mu       sync.Mutex
	sessions map[string]*MCPSession
	// registry mappe un nom d'outil exposé au modèle → (serveur, outil réel).
	registry map[string]mcpToolRef
}

type mcpToolRef struct {
	server string
	tool   string
}

// Invalidate ferme et oublie la session d'un serveur (après un changement de
// config), pour qu'elle soit reconstruite avec la nouvelle config au prochain
// usage.
func (m *MCPManager) Invalidate(name string) {
	m.mu.Lock()
	s := m.sessions[name]
	delete(m.sessions, name)
	// Purge le registre des outils de ce serveur.
	for exposed, ref := range m.registry {
		if ref.server == name {
			delete(m.registry, exposed)
		}
	}
	m.mu.Unlock()
	// Une session encore en cours de connexion a sess == nil : c'est sa goroutine
	// qui refermera ce qu'elle vient d'ouvrir, en constatant qu'elle n'est plus
	// dans le pool.
	if s != nil && s.sess != nil {
		_ = s.sess.Close()
	}
}

// CloseAll ferme toutes les sessions (arrêt du service).
func (m *MCPManager) CloseAll() {
	m.mu.Lock()
	sessions := m.sessions
	m.sessions = map[string]*MCPSession{}
	m.registry = map[string]mcpToolRef{}
	m.mu.Unlock()
	for _, s := range sessions {
		if s.sess != nil {
			_ = s.sess.Close()
		}
	}
}

// Ensure renvoie une session vivante pour un serveur activé, en la (re)créant si
// besoin. Doit être appelé sans détenir mgr.mu (il gère le lock lui-même).
//
// L'entrée est réservée dans le pool avant la connexion : deux appelants
// simultanés (le pré-chauffage et un tour de chat, par exemple) partagent la même
// tentative au lieu de lancer deux process stdio.
func (m *MCPManager) Ensure(name string, cfg resources.MCPServerConfig) *MCPSession {
	m.mu.Lock()
	if s, ok := m.sessions[name]; ok {
		m.mu.Unlock()
		<-s.ready // connexion peut-être encore en cours : on attend son issue
		return s
	}
	s := &MCPSession{name: name, cfg: cfg, ready: make(chan struct{})}
	m.sessions[name] = s
	m.mu.Unlock()

	// Connexion hors-lock (peut être lente).
	ctx, cancel := context.WithTimeout(context.Background(), mcpConnectTimeout)
	defer cancel()
	sess, err := m.connect(ctx, name, cfg)
	if err != nil {
		s.err = err
	} else {
		s.sess = sess
		lt, lerr := sess.ListTools(ctx, nil)
		if lerr != nil {
			s.err = lerr
		} else {
			s.tools = lt.Tools
		}
	}

	m.mu.Lock()
	// La session a pu être invalidée pendant la connexion (changement de config,
	// arrêt du service) : dans ce cas on ne la republie pas, on referme.
	stale := m.sessions[name] != s
	if !stale {
		// (Re)peuple le registre pour ce serveur.
		for exposed, ref := range m.registry {
			if ref.server == name {
				delete(m.registry, exposed)
			}
		}
		for _, t := range s.tools {
			m.registry[MCPExposedName(name, t.Name)] = mcpToolRef{server: name, tool: t.Name}
		}
	}
	m.mu.Unlock()
	close(s.ready)
	if stale && s.sess != nil {
		_ = s.sess.Close()
		s.sess = nil
	}
	return s
}

// EnsureAll connecte en parallèle tous les serveurs activés et attend qu'ils
// soient tous fixés (connectés ou en erreur). C'est ce qui évite d'additionner
// les temps de démarrage : le coût total est celui du serveur le plus lent.
func (m *MCPManager) EnsureAll(servers map[string]resources.MCPServerConfig) {
	var wg sync.WaitGroup
	for _, name := range resources.SortedServerNames(servers) {
		cfg := servers[name]
		if !cfg.Enabled || cfg.Transport() == "" {
			continue
		}
		wg.Add(1)
		go func(name string, cfg resources.MCPServerConfig) {
			defer wg.Done()
			m.Ensure(name, cfg)
		}(name, cfg)
	}
	wg.Wait()
}

// Tools connecte les serveurs activés et renvoie leurs outils, prêts à être
// annoncés au modèle. Réservé au mode agent (appelé par EnabledTools).
func (m *MCPManager) Tools() []Tool {
	servers, err := m.config.LoadMCPConfig()
	if err != nil || len(servers) == 0 {
		return nil
	}
	m.EnsureAll(servers) // connexions en parallèle, puis lecture du pool (instantanée)
	var out []Tool
	for _, name := range resources.SortedServerNames(servers) {
		cfg := servers[name]
		if !cfg.Enabled || cfg.Transport() == "" {
			continue
		}
		s := m.Ensure(name, cfg)
		if s.sess == nil {
			continue // connexion échouée : signalé dans l'UI, pas au modèle
		}
		for _, t := range s.tools {
			if cfg.ToolDisabled(t.Name) {
				continue // outil masqué par l'utilisateur : pas annoncé au modèle
			}
			desc := t.Description
			if desc == "" {
				desc = t.Title
			}
			// Préfixe le serveur d'origine dans la description : aide le modèle à
			// choisir entre outils similaires de serveurs différents.
			desc = "[MCP: " + name + "] " + desc
			out = append(out, Tool{
				Type: "function",
				Function: ToolFunction{
					Name:        MCPExposedName(name, t.Name),
					Description: desc,
					Parameters:  MCPNormalizeSchema(t.InputSchema),
				},
			})
		}
	}
	return out
}

// Call exécute un outil MCP à partir de son nom exposé et renvoie une chaîne
// résultat (texte aplati), cappée. Reconnecte une fois si la session est morte.
func (m *MCPManager) Call(name string, args map[string]any) string {
	m.mu.Lock()
	ref, ok := m.registry[name]
	m.mu.Unlock()
	if !ok {
		return "[erreur] outil MCP inconnu ou serveur non connecté: " + name
	}

	result, err := m.callOnce(ref, args)
	if err != nil {
		// Session peut-être morte (process stdio tombé) : on l'invalide et on
		// retente une fois avec une reconnexion fraîche.
		servers, _ := m.config.LoadMCPConfig()
		cfg, exists := servers[ref.server]
		if exists && cfg.Enabled {
			m.Invalidate(ref.server)
			m.Ensure(ref.server, cfg)
			if result2, err2 := m.callOnce(ref, args); err2 == nil {
				return result2
			} else {
				err = err2
			}
		}
		return "[erreur MCP] " + err.Error()
	}
	return result
}

func (m *MCPManager) callOnce(ref mcpToolRef, args map[string]any) (string, error) {
	m.mu.Lock()
	s := m.sessions[ref.server]
	m.mu.Unlock()
	if s == nil || s.sess == nil {
		return "", fmt.Errorf("serveur %s non connecté", ref.server)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpCallTimeout)
	defer cancel()
	res, err := s.sess.CallTool(ctx, &mcpsdk.CallToolParams{Name: ref.tool, Arguments: args})
	if err != nil {
		return "", err
	}
	out := FlattenMCPContent(res)
	if r := []rune(out); len(r) > mcpMaxOutput {
		out = string(r[:mcpMaxOutput]) + "\n…[tronqué]"
	}
	if res.IsError {
		return "[l'outil a renvoyé une erreur]\n" + out, nil
	}
	if strings.TrimSpace(out) == "" {
		return "[ok] (aucune sortie)", nil
	}
	return out, nil
}

// PromptLine renvoie une ligne système listant les serveurs MCP connectés et
// leur nombre d'outils, pour situer le modèle. Ne force PAS de connexion : lit
// seulement l'état déjà établi par Tools() (appelé par EnabledTools sur le
// même tour), pour ne pas payer deux fois le handshake.
func (m *MCPManager) PromptLine() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for name, s := range m.sessions {
		if s.sess != nil && len(s.tools) > 0 {
			names = append(names, fmt.Sprintf("%s (%d)", name, len(s.tools)))
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return "\n\nMCP servers connected (their tools are prefixed mcp__<server>__<tool>): " + strings.Join(names, ", ") + "."
}

// MCPServerStatus est l'état d'un serveur pour l'UI web.
type MCPServerStatus struct {
	Name      string   `json:"name"`
	Transport string   `json:"transport"`
	Enabled   bool     `json:"enabled"`
	Connected bool     `json:"connected"`
	Error     string   `json:"error,omitempty"`
	Tools     []string `json:"tools"`    // tous les outils découverts sur le serveur
	Disabled  []string `json:"disabled"` // sous-ensemble masqué (non exposé à l'IA)
	// Détail de config (pour pré-remplir le formulaire d'édition).
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Status renvoie l'état de tous les serveurs configurés, en tentant de
// connecter ceux qui sont activés (pour refléter l'état réel dans l'UI).
func (m *MCPManager) Status() ([]MCPServerStatus, error) {
	servers, err := m.config.LoadMCPConfig()
	if err != nil {
		return nil, err
	}
	m.EnsureAll(servers) // en parallèle : le panneau MCP de l'UI n'attend plus la somme
	var out []MCPServerStatus
	for _, name := range resources.SortedServerNames(servers) {
		cfg := servers[name]
		st := MCPServerStatus{
			Name:      name,
			Transport: cfg.Transport(),
			Enabled:   cfg.Enabled,
			Command:   cfg.Command,
			Args:      cfg.Args,
			Env:       cfg.Env,
			URL:       cfg.URL,
			Headers:   cfg.Headers,
			Tools:     []string{},
			Disabled:  cfg.DisabledTools,
		}
		if st.Disabled == nil {
			st.Disabled = []string{}
		}
		if cfg.Enabled && cfg.Transport() != "" {
			s := m.Ensure(name, cfg)
			if s.sess != nil {
				st.Connected = true
				for _, t := range s.tools {
					st.Tools = append(st.Tools, t.Name)
				}
			}
			if s.err != nil {
				st.Error = s.err.Error()
			}
		}
		out = append(out, st)
	}
	return out, nil
}
