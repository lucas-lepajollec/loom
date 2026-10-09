package loom

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const agentsRegistryURL = "https://cdn.agentclientprotocol.com/registry/v1/latest/registry.json"
const agentsRegistryState = "agents_registry_cache"
const agentsCatalogState = "agents_catalog_added"

//go:embed harness/acp_registry_snapshot.json
var agentsRegistrySnapshot []byte

type registryLaunch struct {
	Package string   `json:"package"`
	Args    []string `json:"args"`
	Archive string   `json:"archive"`
	Command string   `json:"cmd"`
}
type registryAgent struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	Repository   string `json:"repository"`
	Website      string `json:"website"`
	Icon         string `json:"icon,omitempty"`
	Distribution struct {
		NPX    *registryLaunch           `json:"npx"`
		UVX    *registryLaunch           `json:"uvx"`
		Binary map[string]registryLaunch `json:"binary"`
	} `json:"distribution"`
}

type catalogDistribution struct {
	Kind    string   `json:"kind"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Package string   `json:"package,omitempty"`
}
type catalogAgent struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Version          string              `json:"version"`
	Description      string              `json:"description"`
	Repository       string              `json:"repository"`
	Icon             string              `json:"icon,omitempty"`
	Distribution     catalogDistribution `json:"distribution"`
	Installed        bool                `json:"installed"`
	InstalledVersion string              `json:"installed_version,omitempty"`
	Added            bool                `json:"added"`
	Builtin          bool                `json:"builtin"`
}

// Reserved catalogue entries cannot create duplicate runtimes.
// Gemini is reserved by product policy; it is not registered as a builtin.
func registryBuiltin(id string) string {
	switch id {
	case "codex-acp":
		return "codex"
	case "claude-acp":
		return "claude-code"
	case "pi-acp":
		return "pi"
	case "opencode", "hermes", "openclaw", "deepseek-harness":
		return id
	case "antigravity-acp":
		return "antigravity"
	case "gemini", "gemini-cli":
		return "gemini"
	}
	return ""
}

var registryIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)
var registryVersionPattern = regexp.MustCompile(`^[0-9][0-9A-Za-z.+_-]{0,99}$`)
var registryPackagePattern = regexp.MustCompile(`^(?:@[a-zA-Z0-9._-]+/)?[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func parseAgentsRegistry(data []byte) ([]registryAgent, error) {
	var envelope struct {
		Agents []json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	agents := []registryAgent{}
	seen := map[string]bool{}
	for _, raw := range envelope.Agents {
		var a registryAgent
		if json.Unmarshal(raw, &a) != nil || !registryIDPattern.MatchString(a.ID) || a.Name == "" || !registryVersionPattern.MatchString(a.Version) || seen[a.ID] {
			continue
		}
		for _, binary := range a.Distribution.Binary {
			u, err := url.Parse(binary.Archive)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				return nil, errors.New("registry binary archive must use https: " + a.ID)
			}
		}
		seen[a.ID] = true
		agents = append(agents, a)
	}
	if len(agents) == 0 {
		return nil, errors.New("registry has no valid agents")
	}
	return agents, nil
}

func registryPlatform(goos, arch string) string {
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}
	return goos + "-" + arch
}
func registryPackage(value string) string {
	// Registry packages already carry pins; replace them with the entry version.
	if i := strings.Index(value, "=="); i >= 0 {
		return value[:i]
	}
	if i := strings.LastIndex(value, "@"); i > 0 {
		return value[:i]
	}
	return value
}
func (a registryAgent) launch(platform string) (catalogDistribution, error) {
	d := catalogDistribution{Kind: "unsupported", Args: []string{}}
	var launch registryLaunch
	switch {
	case a.Distribution.NPX != nil:
		launch = *a.Distribution.NPX
		d.Kind, d.Command, d.Package = "npx", "npx", registryPackage(launch.Package)
		d.Args = []string{"-y", d.Package + "@" + a.Version}
	case a.Distribution.UVX != nil:
		launch = *a.Distribution.UVX
		d.Kind, d.Command, d.Package = "uvx", "uvx", registryPackage(launch.Package)
		d.Args = []string{d.Package + "==" + a.Version}
	default:
		var ok bool
		launch, ok = a.Distribution.Binary[platform]
		if !ok {
			return d, errors.New("no supported distribution for this platform")
		}
		d.Kind = "binary"
		d.Command = path.Base(strings.ReplaceAll(launch.Command, `\`, "/"))
		if d.Command == "" || d.Command == "." || d.Command == ".." || strings.HasPrefix(d.Command, "-") || strings.ContainsAny(d.Command, "\r\n\x00") {
			return d, errors.New("invalid registry binary command")
		}
		d.Package = d.Command
	}
	if d.Kind != "binary" && !registryPackagePattern.MatchString(d.Package) {
		return d, errors.New("invalid registry package")
	}
	if len(launch.Args) > 32 {
		return d, errors.New("too many registry arguments")
	}
	for _, arg := range launch.Args {
		if len(arg) > 512 || strings.ContainsAny(arg, "\r\n\x00") {
			return d, errors.New("invalid registry argument")
		}
	}
	d.Args = append(d.Args, launch.Args...)
	return d, nil
}
func (a registryAgent) installHint() string {
	if a.Repository != "" {
		return a.Repository
	}
	if a.Website != "" {
		return a.Website
	}
	if binary, ok := a.Distribution.Binary[registryPlatform(runtime.GOOS, runtime.GOARCH)]; ok {
		return binary.Archive
	}
	return "Install the agent on PATH using its upstream instructions."
}
func (a registryAgent) agent(platform string, lookPath func(string) (string, error)) (acpAgent, error) {
	if registryBuiltin(a.ID) != "" || geminiRegistryAgent(a) {
		return acpAgent{}, errors.New("agent is reserved by Loom; use the existing runtime")
	}
	d, err := a.launch(platform)
	if err != nil {
		return acpAgent{}, err
	}
	if d.Kind == "binary" {
		if _, err := lookPath(d.Command); err != nil {
			return acpAgent{}, errors.New("binary must already be installed on PATH")
		}
	}
	return acpAgent{ID: "registry-" + a.ID, Name: a.Name, Command: d.Command, Args: d.Args, Docs: a.installHint(), Custom: true, RegistryID: a.ID, RegistryVersion: a.Version, RegistryPackage: d.Package, RegistryKind: d.Kind}, nil
}

type registryCache struct {
	CheckedAt time.Time       `json:"checked_at"`
	ETag      string          `json:"etag,omitempty"`
	Data      json.RawMessage `json:"data"`
}
type agentsCatalogue struct {
	mu         sync.Mutex
	cache      registryCache
	loaded     bool
	refreshing bool
	url        string
	client     *http.Client
}

var officialAgentsCatalog = &agentsCatalogue{url: agentsRegistryURL, client: &http.Client{Timeout: 10 * time.Second}}

func (c *agentsCatalogue) loadLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	_ = getStoreJSON(bkState, agentsRegistryState, &c.cache)
	if _, err := parseAgentsRegistry(c.cache.Data); err != nil {
		c.cache = registryCache{Data: agentsRegistrySnapshot}
	}
}
func (c *agentsCatalogue) entries() []registryAgent {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	entries, _ := parseAgentsRegistry(c.cache.Data)
	return entries
}

// Reserve an attempt before starting I/O, including failed attempts, so offline
// clients cannot cause startup/API requests to retry more than once per day.
func (c *agentsCatalogue) refresh(ctx context.Context, now time.Time) {
	c.mu.Lock()
	c.loadLocked()
	if c.refreshing || (!c.cache.CheckedAt.IsZero() && now.Sub(c.cache.CheckedAt) < 24*time.Hour) {
		c.mu.Unlock()
		return
	}
	c.refreshing = true
	c.cache.CheckedAt = now
	etag := c.cache.ETag
	_ = putStoreJSON(bkState, agentsRegistryState, c.cache)
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.refreshing = false; c.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return
	}
	if resp.StatusCode != http.StatusOK {
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20+1))
	if err != nil || len(data) > 4<<20 {
		return
	}
	if _, err := parseAgentsRegistry(data); err != nil {
		return
	}
	c.mu.Lock()
	c.cache.Data, c.cache.ETag = data, resp.Header.Get("ETag")
	_ = putStoreJSON(bkState, agentsRegistryState, c.cache)
	c.mu.Unlock()
}

var catalogMutationMu sync.Mutex

func loadCatalogAgents() []acpAgent {
	list := []acpAgent{}
	_ = getStoreJSON(bkState, agentsCatalogState, &list)
	return list
}
func registerCatalogAgents() {
	for _, a := range loadCatalogAgents() {
		if registryBuiltin(a.RegistryID) == "" && a.ID == "registry-"+a.RegistryID {
			registeredRuntimes.upsert(&acpAdapter{agent: a})
		}
	}
}
func catalogEntries(entries []registryAgent, added []acpAgent, lookPath func(string) (string, error)) []catalogAgent {
	out := make([]catalogAgent, 0, len(entries))
	for _, a := range entries {
		if geminiRegistryAgent(a) {
			continue
		}
		d, _ := a.launch(registryPlatform(runtime.GOOS, runtime.GOARCH))
		entry := catalogAgent{ID: a.ID, Name: a.Name, Version: a.Version, Description: a.Description, Repository: a.Repository, Icon: a.Icon, Distribution: d, Builtin: registryBuiltin(a.ID) != ""}
		runtimeID := "registry-" + a.ID
		for _, saved := range added {
			if saved.RegistryID == a.ID {
				entry.Added = true
			}
		}
		if id := registryBuiltin(a.ID); id != "" {
			runtimeID = id
			binary := id
			if id == "claude-code" {
				binary = "claude"
			}
			if id == "antigravity" {
				binary = "agy"
			}
			_, err := lookPath(binary)
			entry.Installed = err == nil
		} else if d.Kind == "binary" || d.Kind == "npx" || d.Kind == "uvx" {
			_, err := lookPath(d.Command)
			entry.Installed = err == nil
		}
		var compat runtimeCompatibilityRecord
		if getStoreJSON(bkState, "agent_compat_"+runtimeID, &compat) && compat.Version != "" {
			entry.InstalledVersion = compat.Version
		}
		out = append(out, entry)
	}
	return out
}
func handleAgentsCatalog(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	go officialAgentsCatalog.refresh(context.Background(), time.Now())
	sendJSON(w, 200, catalogEntries(officialAgentsCatalog.entries(), loadCatalogAgents(), lifecycleLookPath))
}
func handleAgentsCatalogAdd(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	catalogMutationMu.Lock()
	defer catalogMutationMu.Unlock()
	for _, a := range officialAgentsCatalog.entries() {
		if a.ID != req.ID {
			continue
		}
		list := loadCatalogAgents()
		for _, saved := range list {
			if saved.RegistryID == a.ID {
				sendJSON(w, 200, map[string]any{"ok": true, "agent": saved})
				return
			}
		}
		built, err := a.agent(registryPlatform(runtime.GOOS, runtime.GOARCH), lifecycleLookPath)
		if err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error(), "install_hint": a.installHint()})
			return
		}
		if _, ok := registeredRuntimes.lookup(built.ID); ok {
			sendJSON(w, 409, map[string]any{"ok": false, "error": "runtime ID already registered"})
			return
		}
		list = append(list, built)
		if err := putStoreJSON(bkState, agentsCatalogState, list); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		registeredRuntimes.upsert(&acpAdapter{agent: built})
		sendJSON(w, 200, map[string]any{"ok": true, "agent": built})
		return
	}
	sendJSON(w, 404, map[string]any{"ok": false, "error": "catalogue agent not found"})
}
func handleAgentsCatalogRemove(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	catalogMutationMu.Lock()
	defer catalogMutationMu.Unlock()
	list, kept := loadCatalogAgents(), []acpAgent{}
	for _, a := range list {
		if a.RegistryID != req.ID {
			kept = append(kept, a)
		}
	}
	if len(list) == len(kept) {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "catalogue agent not added"})
		return
	}
	if err := putStoreJSON(bkState, agentsCatalogState, kept); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	registeredRuntimes.remove("registry-" + req.ID)
	sendJSON(w, 200, map[string]any{"ok": true})
}

func geminiRegistryAgent(a registryAgent) bool {
	if registryBuiltin(a.ID) == "gemini" {
		return true
	}
	return a.Distribution.NPX != nil && strings.HasPrefix(a.Distribution.NPX.Package, "@google/gemini-cli")
}
