package resources

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// MCPServerConfig décrit un serveur MCP configuré. Un seul des deux transports
// est renseigné : Command => stdio, URL => http.
type MCPServerConfig struct {
	Type string `json:"type,omitempty"`
	// Transport stdio.
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`

	// Transport http (Streamable HTTP).
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// Enabled : le serveur n'est connecté et ses outils exposés que s'il est
	// activé. Un serveur nouvellement ajouté est actif par défaut (voir
	// UnmarshalJSON) pour coller à l'intuition « je l'ajoute, il marche ».
	Enabled bool `json:"enabled"`

	// DisabledTools : outils du serveur à NE PAS exposer à l'IA (par nom réel,
	// non namespacé). Permet de garder un serveur connecté tout en masquant
	// certains de ses outils. Vide = tous les outils exposés.
	DisabledTools []string `json:"disabledTools,omitempty"`
}

// enabledDefaultTrue est un alias utilisé pour appliquer enabled=true par défaut
// quand le champ est absent du JSON (compat configs Claude Desktop sans
// "enabled").
type mcpServerConfigAlias MCPServerConfig

// ToolDisabled indique si un outil (nom réel) est masqué pour ce serveur.
func (c MCPServerConfig) ToolDisabled(tool string) bool {
	for _, t := range c.DisabledTools {
		if t == tool {
			return true
		}
	}
	return false
}

// UnmarshalJSON applique enabled=true par défaut lorsque la clé est absente,
// pour rester compatible avec les fichiers mcp.json qui ne connaissent pas ce
// champ (Claude Desktop, etc.).
func (c *MCPServerConfig) UnmarshalJSON(b []byte) error {
	// Sonde la présence de la clé "enabled".
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(b, &probe); err != nil {
		return err
	}
	alias := mcpServerConfigAlias{}
	if err := json.Unmarshal(b, &alias); err != nil {
		return err
	}
	if _, ok := probe["enabled"]; !ok {
		alias.Enabled = true
	}
	*c = MCPServerConfig(alias)
	return nil
}

// Transport renvoie "stdio", "http" ou "" (mal configuré : ni command ni url).
func (c MCPServerConfig) Transport() string {
	switch {
	case strings.TrimSpace(c.Command) != "":
		return "stdio"
	case strings.TrimSpace(c.URL) != "":
		return "http"
	default:
		return ""
	}
}

// Validate vérifie qu'exactement un transport est renseigné.
func (c MCPServerConfig) Validate() error {
	hasCmd := strings.TrimSpace(c.Command) != ""
	hasURL := strings.TrimSpace(c.URL) != ""
	switch {
	case hasCmd && hasURL:
		return fmt.Errorf("an MCP server cannot have both 'command' (stdio) and 'url' (http)")
	case !hasCmd && !hasURL:
		return fmt.Errorf("an MCP server must have either 'command' (stdio) or 'url' (http)")
	}
	return nil
}

// SortedServerNames renvoie les noms triés, pour un ordre d'affichage/itération
// stable.
func SortedServerNames(servers map[string]MCPServerConfig) []string {
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
