package loom

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/tools"
)

type searchSettings struct {
	Provider string `json:"provider"`
	URL      string `json:"url,omitempty"`
}

var searchMu sync.Mutex
var searchHTTP = &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func readSearchSettings() searchSettings {
	p := searchSettings{Provider: "duckduckgo"}
	getStoreJSON(bkState, "web_search_settings", &p)
	return p
}
func validateSearchSettings(p searchSettings) (searchSettings, error) {
	switch p.Provider {
	case "duckduckgo", "brave", "tavily":
		p.URL = ""
	case "searxng":
		u, err := url.Parse(strings.TrimSpace(p.URL))
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(p.URL) > 2048 || u.Opaque != "" || (u.Scheme != "https" && !(u.Scheme == "http" && localNetworkHost(u.Hostname()))) {
			return p, errors.New("SearXNG needs an HTTPS URL, or HTTP on your LAN, without credentials/query/fragment")
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && (ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
			return p, errors.New("invalid SearXNG host")
		}
		u.Path = strings.TrimRight(u.Path, "/")
		if !strings.HasSuffix(u.Path, "/search") {
			u.Path += "/search"
		}
		p.URL = u.String()
	default:
		return p, errors.New("unknown search provider")
	}
	return p, nil
}
func configuredWebSearch(ctx context.Context, query string, limit int) ([]searchResult, error) {
	p := readSearchSettings()
	key, err := searchKey(p.Provider)
	if err != nil {
		return nil, errors.New("search credential unavailable; reconnect in Settings → Internet")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return tools.Search(ctx, searchHTTP, tools.SearchConfig{Provider: p.Provider, URL: p.URL, Key: key}, query, limit)
}
func searchKey(provider string) (string, error) {
	if provider == "duckduckgo" {
		return "", nil
	}
	id := "web-search-" + provider
	if _, err := os.Lstat(filepath.Join(providerSecretRoot(), id+".sealed")); os.IsNotExist(err) {
		return "", nil
	}
	return readProviderSecret(id)
}
func handleSearchSettings(w http.ResponseWriter, r *http.Request) {
	if !usageVaultAccess(w) {
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Provider string  `json:"provider"`
			URL      string  `json:"url"`
			Key      *string `json:"key"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		p, err := validateSearchSettings(searchSettings{req.Provider, req.URL})
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		searchMu.Lock()
		defer searchMu.Unlock()
		if req.Key != nil {
			id := "web-search-" + p.Provider
			if len(*req.Key) > 4096 || strings.ContainsAny(*req.Key, "\r\n") {
				sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid search key"})
				return
			}
			if strings.TrimSpace(*req.Key) == "" {
				err = deleteProviderSecret(id)
			} else {
				err = sealProviderSecret(id, strings.TrimSpace(*req.Key))
			}
			if err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": "could not store search credential"})
				return
			}
		}
		if putStoreJSON(bkState, "web_search_settings", p) != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": "could not save search configuration"})
			return
		}
	} else if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	p := readSearchSettings()
	key, err := searchKey(p.Provider)
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "search credential unavailable"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "provider": p.Provider, "url": p.URL, "key_set": key != ""})
}
func handleSearchTest(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	results, err := configuredWebSearch(r.Context(), req.Query, 3)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "results": results})
}

// Cloud inference gets only web_search. It cannot invoke the local shell,
// filesystem, private memory, MCP or arbitrary URL fetches through this seam.
type cloudWebSearch struct{}

// cloudTools is a cloud turn's toolbox: web_search and/or the MCP servers
// declared in Brain, as the discussion's Tools menu chose.
type cloudTools struct{ web, mcp bool }

func (c cloudTools) Definitions() any {
	defs := []Tool{}
	if c.web {
		defs = append(defs, webSearchTool())
	}
	if c.mcp {
		defs = append(defs, mcpTools()...)
	}
	return defs
}
func (c cloudTools) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	switch {
	case c.web && name == "web_search":
		return cloudWebSearch{}.Execute(ctx, name, args)
	case c.mcp && isMCPTool(name):
		return capWebOutput(mcpCall(name, args)), nil
	}
	return "", errors.New("tool not enabled for this discussion: " + name)
}

func (cloudWebSearch) Definitions() any { return []Tool{webSearchTool()} }

type cloudSearchSource struct {
	nativeWebSource
	ctx context.Context
}

func (s cloudSearchSource) SearchPages(query string, limit int) ([]searchResult, error) {
	return configuredWebSearch(s.ctx, query, limit)
}
func (cloudWebSearch) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	if name != "web_search" {
		return "", errors.New("this cloud connector only permits web_search")
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return capWebOutput(tools.ToolWebSearch(cloudSearchSource{ctx: ctx}, args)), nil
}
