package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// headerRoundTripper injecte des en-têtes statiques (auth) sur chaque requête
// HTTP vers un serveur MCP distant.
type headerRoundTripper struct {
	base           http.RoundTripper
	headers        map[string]string
	origin         *url.URL
	sameOriginOnly bool
}

func (h headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != h.origin.Scheme || !strings.EqualFold(req.URL.Host, h.origin.Host) {
		if h.sameOriginOnly {
			return nil, fmt.Errorf("MCP SSE endpoint changed origin")
		}
		return h.base.RoundTrip(req)
	}
	req = req.Clone(req.Context())
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.base.RoundTrip(req)
}

// The SDK's SSE transport uses the connect context for the lifetime of its
// stream. Bound connection setup, then leave the established stream open
// until the session closes it.
type mcpSSETransport struct{ *mcpsdk.SSEClientTransport }

func (t mcpSSETransport) Connect(ctx context.Context) (mcpsdk.Connection, error) {
	stream, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(ctx, cancel)
	conn, err := t.SSEClientTransport.Connect(stream)
	stop()
	if err != nil {
		cancel()
		return nil, err
	}
	return mcpSSEConnection{conn, cancel}, nil
}

type mcpSSEConnection struct {
	mcpsdk.Connection
	cancel context.CancelFunc
}

func (c mcpSSEConnection) Close() error {
	c.cancel()
	return c.Connection.Close()
}

// connect établit une session pour la config donnée.
func ConnectMCP(ctx context.Context, name string, cfg resources.MCPServerConfig, version string, cmd *exec.Cmd, httpClient *http.Client) (*mcpsdk.ClientSession, error) {
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "loom", Version: version}, nil)

	var transport mcpsdk.Transport
	switch cfg.Transport() {
	case "stdio":
		transport = &mcpsdk.CommandTransport{Command: cmd}
	case "http":
		if !resources.ValidMCPURL(cfg.URL) {
			return nil, fmt.Errorf("invalid MCP URL")
		}
		endpoint, err := url.Parse(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid MCP URL")
		}
		sse := cfg.Type == "sse"
		httpCopy := *httpClient
		redirect := httpCopy.CheckRedirect
		httpCopy.CheckRedirect = func(r *http.Request, via []*http.Request) error {
			if !strings.EqualFold(r.URL.Host, endpoint.Host) || r.URL.Scheme != endpoint.Scheme || len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			if redirect != nil {
				return redirect(r, via)
			}
			return nil
		}
		if len(cfg.Headers) > 0 || sse {
			base := httpCopy.Transport
			if base == nil {
				base = http.DefaultTransport
			}
			httpCopy.Transport = headerRoundTripper{base: base, headers: cfg.Headers, origin: endpoint, sameOriginOnly: sse}
		}
		if sse {
			transport = mcpSSETransport{&mcpsdk.SSEClientTransport{Endpoint: cfg.URL, HTTPClient: &httpCopy}}
		} else {
			transport = &mcpsdk.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: &httpCopy}
		}
	default:
		return nil, fmt.Errorf("MCP server '%s' misconfigured (neither command nor url)", name)
	}

	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

var mcpSanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// mcpSanitize rend une chaîne compatible avec les noms d'outils (^[A-Za-z0-9_-]+$).
func MCPSanitize(s string) string {
	s = mcpSanitizeRe.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

// mcpExposedName construit le nom d'outil vu par le modèle.
func MCPExposedName(server, tool string) string {
	return "mcp__" + MCPSanitize(server) + "__" + MCPSanitize(tool)
}

// mcpNormalizeSchema convertit le InputSchema du SDK (any → map) en map[string]any
// pour notre type Tool. Un schéma vide devient un objet sans propriété.
func MCPNormalizeSchema(schema any) map[string]any {
	if m, ok := schema.(map[string]any); ok && m != nil {
		return m
	}
	// Le SDK peut renvoyer une struct/RawMessage : passe par un round-trip JSON.
	if schema != nil {
		if b, err := json.Marshal(schema); err == nil {
			var m map[string]any
			if json.Unmarshal(b, &m) == nil && m != nil {
				return m
			}
		}
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// mcpArgLabel produit un libellé court à partir des arguments d'un appel MCP,
// pour l'affichage « outil utilisé » dans l'UI (les outils MCP n'ont pas de
// champ canonique connu à l'avance).
func MCPArgLabel(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	// Privilégie les clés parlantes courantes.
	for _, k := range []string{"path", "file", "query", "command", "url", "name"} {
		if v, ok := args[k].(string); ok && v != "" {
			return v
		}
	}
	b, _ := json.Marshal(args)
	s := string(b)
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

// isMCPTool indique si un nom d'outil est routé vers MCP.
func IsMCPTool(name string) bool {
	return strings.HasPrefix(name, "mcp__")
}

// flattenMCPContent aplatit le contenu d'un CallToolResult en texte. Les blocs
// texte sont concaténés ; le contenu structuré est rendu en JSON s'il n'y a pas
// de texte ; les autres types (image/audio) sont résumés.
func FlattenMCPContent(res *mcpsdk.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		switch v := c.(type) {
		case *mcpsdk.TextContent:
			parts = append(parts, v.Text)
		case *mcpsdk.ImageContent:
			parts = append(parts, "[image "+v.MIMEType+"]")
		case *mcpsdk.AudioContent:
			parts = append(parts, "[audio "+v.MIMEType+"]")
		default:
			if b, err := json.Marshal(c); err == nil {
				parts = append(parts, string(b))
			}
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" && res.StructuredContent != nil {
		if b, err := json.MarshalIndent(res.StructuredContent, "", "  "); err == nil {
			return string(b)
		}
	}
	return text
}
