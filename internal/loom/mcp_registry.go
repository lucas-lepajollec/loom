package loom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/resources"
)

const mcpRegistryEndpoint = "https://registry.modelcontextprotocol.io/v0/servers"

type registryInput struct {
	Name     string `json:"name"`
	Value    string `json:"value,omitempty"`
	Required bool   `json:"isRequired,omitempty"`
	Secret   bool   `json:"isSecret,omitempty"`
}

type registryPreset struct {
	Transport string          `json:"transport"`
	Type      string          `json:"type,omitempty"`
	URL       string          `json:"url,omitempty"`
	Headers   []registryInput `json:"headers,omitempty"`
	Command   string          `json:"command,omitempty"`
	Args      []string        `json:"args,omitempty"`
	EnvVars   []registryInput `json:"env_vars,omitempty"`
}

type registryTemplate struct {
	ID            string           `json:"id"`
	Title         string           `json:"title"`
	Description   string           `json:"description"`
	Version       string           `json:"version"`
	WebsiteURL    string           `json:"website_url"`
	RepositoryURL string           `json:"repository_url"`
	SuggestedName string           `json:"suggested_name"`
	Presets       []registryPreset `json:"presets"`
}

type registryServer struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
	WebsiteURL  string `json:"websiteUrl"`
	Repository  struct {
		URL string `json:"url"`
	} `json:"repository"`
	Remotes []struct {
		Type    string          `json:"type"`
		URL     string          `json:"url"`
		Headers []registryInput `json:"headers"`
	} `json:"remotes"`
	Packages []registryPackage `json:"packages"`
}

type registryArgument struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Value     string `json:"value"`
	ValueHint string `json:"valueHint"`
	Required  bool   `json:"isRequired"`
	Secret    bool   `json:"isSecret"`
}

type registryPackage struct {
	RegistryType string `json:"registryType"`
	Identifier   string `json:"identifier"`
	Version      string `json:"version"`
	Transport    struct {
		Type string `json:"type"`
	} `json:"transport"`
	EnvVars          []registryInput    `json:"environmentVariables"`
	RuntimeArguments []registryArgument `json:"runtimeArguments"`
	PackageArguments []registryArgument `json:"packageArguments"`
}

func registryArgs(args []registryArgument) []string {
	out := []string{}
	for _, a := range args {
		value := a.Value
		if a.Secret || value == "" {
			if !a.Required {
				continue
			}
			value = "{" + firstNonEmpty(a.ValueHint, strings.TrimLeft(a.Name, "-"), "value") + "}"
		}
		switch a.Type {
		case "positional":
			out = append(out, value)
		case "named":
			if strings.HasPrefix(a.Name, "-") {
				out = append(out, a.Name, value)
			}
		}
	}
	return out
}

func registryInputs(inputs []registryInput) []registryInput {
	out := make([]registryInput, 0, len(inputs))
	for _, input := range inputs {
		if input.Name == "" || strings.ContainsAny(input.Name, ":=\r\n") {
			continue
		}
		if input.Secret {
			input.Value = ""
		}
		out = append(out, input)
	}
	return out
}

func (p registryPackage) preset() (registryPreset, bool) {
	id := strings.TrimSpace(p.Identifier)
	if id == "" || strings.HasPrefix(id, "-") || strings.ContainsAny(id, " \t\r\n") || (p.Transport.Type != "" && p.Transport.Type != "stdio") {
		return registryPreset{}, false
	}
	preset := registryPreset{Transport: "stdio", EnvVars: registryInputs(p.EnvVars)}
	switch p.RegistryType {
	case "npm":
		preset.Command, preset.Args = "npx", []string{"-y"}
		if p.Version != "" && !strings.Contains(strings.TrimPrefix(id, "@"), "@") {
			id += "@" + p.Version
		}
	case "pypi":
		preset.Command = "uvx"
		if p.Version != "" && !strings.ContainsAny(id, "=<>~!@") {
			id += "==" + p.Version
		}
	case "oci":
		preset.Command, preset.Args = "docker", []string{"run", "-i", "--rm"}
		if p.Version != "" && !strings.Contains(path.Base(id), ":") && !strings.Contains(id, "@") {
			id += ":" + p.Version
		}
		for _, env := range preset.EnvVars {
			preset.Args = append(preset.Args, "-e", env.Name)
		}
	default:
		return registryPreset{}, false
	}
	preset.Args = append(preset.Args, registryArgs(p.RuntimeArguments)...)
	preset.Args = append(preset.Args, id)
	preset.Args = append(preset.Args, registryArgs(p.PackageArguments)...)
	return preset, true
}

func (s registryServer) template() registryTemplate {
	base := path.Base(s.Name)
	slug := strings.Trim(mcpNameRe.ReplaceAllString(strings.ToLower(base), "-"), "-_")
	for strings.Contains(slug, "__") {
		slug = strings.ReplaceAll(slug, "__", "_")
	}
	row := registryTemplate{
		ID: s.Name, Title: firstNonEmpty(s.Title, base), Description: s.Description, Version: s.Version,
		SuggestedName: firstNonEmpty(slug, "mcp-server"), Presets: []registryPreset{},
	}
	if resources.ValidMCPURL(s.WebsiteURL) {
		row.WebsiteURL = s.WebsiteURL
	}
	if resources.ValidMCPURL(s.Repository.URL) {
		row.RepositoryURL = s.Repository.URL
	}
	for _, r := range s.Remotes {
		if (r.Type == "streamable-http" || r.Type == "sse") && resources.ValidMCPURL(r.URL) {
			row.Presets = append(row.Presets, registryPreset{Transport: "http", Type: r.Type, URL: r.URL, Headers: registryInputs(r.Headers)})
		}
	}
	for _, p := range s.Packages {
		if preset, ok := p.preset(); ok {
			row.Presets = append(row.Presets, preset)
		}
	}
	return row
}

type mcpRegistry struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[registryCacheKey]registryCacheEntry
}

type registryCacheKey struct {
	query string
	limit int
}
type registryCacheEntry struct {
	expires time.Time
	rows    []registryTemplate
}

var officialMCPRegistry = mcpRegistry{
	client: &http.Client{
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	},
}

func (r *mcpRegistry) search(ctx context.Context, q string, limit int) ([]registryTemplate, error) {
	query := strings.ToLower(strings.Join(strings.Fields(q), " "))
	key := registryCacheKey{query, min(max(1, limit), 25)}
	now := time.Now()
	r.mu.Lock()
	cached, found := r.cache[key]
	r.mu.Unlock()
	if found && now.Before(cached.expires) {
		return cached.rows, nil
	}

	endpoint := mcpRegistryEndpoint + "?version=latest&limit=" + strconv.Itoa(key.limit) + "&search=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("could not prepare MCP Registry search")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, errors.New("MCP Registry unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("MCP Registry unavailable")
	}
	const maxBytes = 2 << 20
	limited := &io.LimitedReader{R: resp.Body, N: maxBytes}
	body, err := io.ReadAll(limited)
	if err != nil || limited.N == 0 {
		return nil, errors.New("MCP Registry response unreadable or too large")
	}
	var payload struct {
		Servers []struct {
			Server registryServer `json:"server"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Servers == nil {
		return nil, errors.New("invalid MCP Registry response")
	}
	rows := []registryTemplate{}
	for _, entry := range payload.Servers {
		if entry.Server.Name != "" {
			rows = append(rows, entry.Server.template())
			if len(rows) == key.limit {
				break
			}
		}
	}
	r.store(key, rows)
	return rows, nil
}

func (r *mcpRegistry) store(key registryCacheKey, rows []registryTemplate) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		r.cache = make(map[registryCacheKey]registryCacheEntry)
	}
	var oldest registryCacheKey
	for k, entry := range r.cache {
		if !now.Before(entry.expires) {
			delete(r.cache, k)
		} else if oldest.limit == 0 || entry.expires.Before(r.cache[oldest].expires) {
			oldest = k
		}
	}
	if len(r.cache) >= 64 {
		delete(r.cache, oldest)
	}
	r.cache[key] = registryCacheEntry{now.Add(5 * time.Minute), rows}
}

func handleMCPRegistrySearch(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	q := r.URL.Query().Get("q")
	if len(q) > 512 {
		sendJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "MCP Registry query is too long"})
		return
	}
	limit := 12
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.Atoi(raw)
		if err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid MCP Registry limit"})
			return
		}
	}
	rows, err := officialMCPRegistry.search(r.Context(), q, limit)
	if err != nil {
		sendJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, http.StatusOK, map[string]any{"ok": true, "servers": rows})
}
