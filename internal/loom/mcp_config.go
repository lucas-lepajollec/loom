package loom

import (
	"fmt"
	"strings"
	"sync"
)

// Configuration des serveurs MCP (Model Context Protocol).
//
// loom peut se connecter à des serveurs MCP tiers pour enrichir la palette
// d'outils de l'IA (accès fichiers rapide type desktop-commander, bases de
// données, APIs métier…). Deux transports :
//
//   - stdio : loom lance un process local (command + args) et parle en
//     JSON-RPC sur stdin/stdout. C'est le cas de desktop-commander, du serveur
//     filesystem officiel, etc. — lancés via npx/uvx/un binaire.
//   - http  : loom parle à un serveur MCP distant en Streamable HTTP (url +
//     éventuels en-têtes d'auth).
//
// Le format du fichier mcp.json reprend celui de Claude Desktop (clé
// "mcpServers" indexée par nom) pour que les utilisateurs puissent copier-coller
// leurs configs existantes. On ajoute un champ "enabled" par serveur.
//
// IMPORTANT (sécurité) : un serveur MCP stdio exécute un process arbitraire sur
// la machine où tourne loom — même niveau de confiance que l'outil `bash` du
// mode agent. La configuration MCP est donc réservée au propriétaire local de la
// machine et ne doit JAMAIS être pilotable depuis le relais/accès distant.

// mcpConfigMu sérialise les accès concurrents à la déclaration des serveurs
// MCP (l'UI web et les tours de chat peuvent lire/écrire en parallèle).
var mcpConfigMu sync.Mutex

// LoadMCPConfig lit les serveurs MCP déclarés. Aucun => map vide, pas d'erreur.
func LoadMCPConfig() (map[string]MCPServerConfig, error) {
	mcpConfigMu.Lock()
	defer mcpConfigMu.Unlock()
	return loadMCPConfigLocked()
}

// SetMCPServer ajoute ou remplace un serveur nommé, puis invalide le pool de
// sessions pour que le changement prenne effet au prochain tour.
func SetMCPServer(name string, cfg MCPServerConfig) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("nom de serveur vide")
	}
	if strings.Contains(name, "__") {
		return fmt.Errorf("le nom ne peut pas contenir '__' (réservé au namespacing des outils)")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	mcpConfigMu.Lock()
	servers, err := loadMCPConfigForWriteLocked()
	if err != nil {
		mcpConfigMu.Unlock()
		return err
	}
	if cfg.Type == "" {
		cfg.Type = servers[name].Type
	}
	servers[name] = cfg
	err = saveMCPConfigLocked(servers)
	mcpConfigMu.Unlock()
	if err != nil {
		return err
	}
	mcpInvalidate(name)
	return nil
}

// DeleteMCPServer retire un serveur et ferme sa session si ouverte.
func DeleteMCPServer(name string) error {
	mcpConfigMu.Lock()
	servers, err := loadMCPConfigForWriteLocked()
	if err != nil {
		mcpConfigMu.Unlock()
		return err
	}
	if _, ok := servers[name]; !ok {
		mcpConfigMu.Unlock()
		return fmt.Errorf("serveur MCP inconnu: %s", name)
	}
	delete(servers, name)
	err = saveMCPConfigLocked(servers)
	mcpConfigMu.Unlock()
	if err != nil {
		return err
	}
	mcpInvalidate(name)
	return nil
}

// SetMCPServerEnabled active/désactive un serveur existant.
func SetMCPServerEnabled(name string, on bool) error {
	mcpConfigMu.Lock()
	servers, err := loadMCPConfigForWriteLocked()
	if err != nil {
		mcpConfigMu.Unlock()
		return err
	}
	cfg, ok := servers[name]
	if !ok {
		mcpConfigMu.Unlock()
		return fmt.Errorf("serveur MCP inconnu: %s", name)
	}
	cfg.Enabled = on
	servers[name] = cfg
	err = saveMCPConfigLocked(servers)
	mcpConfigMu.Unlock()
	if err != nil {
		return err
	}
	mcpInvalidate(name)
	return nil
}

// SetMCPToolEnabled masque/démasque un outil précis d'un serveur (via sa liste
// DisabledTools), puis invalide la session pour recalculer les outils exposés.
func SetMCPToolEnabled(server, tool string, on bool) error {
	mcpConfigMu.Lock()
	servers, err := loadMCPConfigForWriteLocked()
	if err != nil {
		mcpConfigMu.Unlock()
		return err
	}
	cfg, ok := servers[server]
	if !ok {
		mcpConfigMu.Unlock()
		return fmt.Errorf("serveur MCP inconnu: %s", server)
	}
	// Reconstruit la liste sans l'outil concerné, puis l'ajoute si on désactive.
	next := cfg.DisabledTools[:0:0]
	for _, t := range cfg.DisabledTools {
		if t != tool {
			next = append(next, t)
		}
	}
	if !on {
		next = append(next, tool)
	}
	cfg.DisabledTools = next
	servers[server] = cfg
	err = saveMCPConfigLocked(servers)
	mcpConfigMu.Unlock()
	if err != nil {
		return err
	}
	mcpInvalidate(server)
	return nil
}
