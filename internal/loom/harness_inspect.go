package loom

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Harness inspection: what a harness already has on this machine (version,
// account, MCP servers, plugins, skills, API keys present in the environment),
// read from its own CLI and folders as described in harness/inspect.json.
// Read-only, except the explicit update action. Secret values are never read
// into responses: environment variables and auth files report presence only.

//go:embed harness/inspect.json
var harnessInspectJSON []byte

type inspectCmd struct {
	Cmd       []string `json:"cmd"`
	File      string   `json:"file"`
	Format    string   `json:"format"`
	Connected string   `json:"connected"`
	Method    string   `json:"method"`
	Account   string   `json:"account"`
}

type inspectSpec struct {
	NativeRoots   []string            `json:"native_roots"`
	RepairPaths   []string            `json:"repair_paths"`
	NativeUpdate  []string            `json:"native_update"`
	BrewNames     []string            `json:"brew_names"`
	Install       map[string][]string `json:"install"`
	Latest        *harnessLatestSpec  `json:"latest"`
	Requires      []string            `json:"requires"`
	RequiresOS    map[string][]string `json:"requires_os"`
	UpdateInstall bool                `json:"update_install"`
	Unverified    bool                `json:"unverified"`
	Source        string              `json:"source"`
	Binary        string              `json:"binary"`
	Version       []string            `json:"version"`
	Update        []string            `json:"update"`
	Auth          *inspectCmd         `json:"auth"`
	MCP           *inspectCmd         `json:"mcp"`
	Plugins       *inspectCmd         `json:"plugins"`
	Skills        []string            `json:"skills"`
	Env           []string            `json:"env"`
}

type HarnessMCP struct {
	Name    string `json:"name"`
	Target  string `json:"target,omitempty"`
	Status  string `json:"status,omitempty"` // connected | disabled | needs-auth | error | ""
	Enabled *bool  `json:"enabled,omitempty"`
	// Enough to recreate the server in Loom ("adopt"). Environment variable
	// names only: their values stay in the harness's own configuration.
	Command  string   `json:"command,omitempty"`
	Args     []string `json:"args,omitempty"`
	URL      string   `json:"url,omitempty"`
	EnvNames []string `json:"env_names,omitempty"`
}

type HarnessSkill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Folder      string `json:"folder"`
	FromLoom    bool   `json:"from_loom"`
}

type HarnessInspection struct {
	At        int64          `json:"at"`
	Installed bool           `json:"installed"`
	Path      string         `json:"path,omitempty"`
	Version   string         `json:"version,omitempty"`
	CanUpdate bool           `json:"can_update"`
	Auth      map[string]any `json:"auth,omitempty"` // connected, method, account, providers
	MCP       []HarnessMCP   `json:"mcp"`
	MCPKnown  bool           `json:"mcp_known"`
	Plugins   []string       `json:"plugins"`
	Skills    []HarnessSkill `json:"skills"`
	Env       []string       `json:"env"` // names of API key variables that are set
	Errors    []string       `json:"errors,omitempty"`
}

var (
	inspectSpecsOnce sync.Once
	inspectSpecs     map[string]inspectSpec
	inspectMu        sync.Mutex
	inspectCache     = map[string]HarnessInspection{}
)

func harnessInspectSpec(id string) (inspectSpec, bool) {
	inspectSpecsOnce.Do(func() {
		if json.Unmarshal(harnessInspectJSON, &inspectSpecs) != nil {
			panic("invalid harness/inspect.json")
		}
	})
	s, ok := inspectSpecs[id]
	return s, ok
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

func runInspect(ctx context.Context, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("empty command")
	}
	native, err := harnessNativeArgv(argv)
	if err != nil {
		return "", err
	}
	c, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(c, native[0], native[1:]...)
	cmd.Env = append(os.Environ(), "PATH="+lifecycleLocalPath())
	cmd.Stdin = nil
	out := &harnessTail{}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	s := out.String()
	return s, err
}

var claudeMCPLine = regexp.MustCompile(`^(.+?):\s+(.+?)\s+-\s+(.+)$`)

func parseMCP(format, text string) []HarnessMCP {
	out := []HarnessMCP{}
	switch format {
	case "codex":
		var list []struct {
			Name      string `json:"name"`
			Enabled   bool   `json:"enabled"`
			Transport struct {
				Type    string   `json:"type"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
				URL     string   `json:"url"`
				EnvVars []string `json:"env_vars"`
			} `json:"transport"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(text)), &list) == nil {
			for _, m := range list {
				target := m.Transport.URL
				if target == "" {
					target = filepath.Base(m.Transport.Command)
				}
				st, en := "disabled", m.Enabled
				if m.Enabled {
					st = "enabled"
				}
				out = append(out, HarnessMCP{Name: m.Name, Target: target, Status: st, Enabled: &en, Command: m.Transport.Command, Args: m.Transport.Args, URL: m.Transport.URL, EnvNames: m.Transport.EnvVars})
			}
		}
	case "claude":
		sc := bufio.NewScanner(strings.NewReader(text))
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			m := claudeMCPLine.FindStringSubmatch(line)
			if m == nil || strings.HasPrefix(line, "Checking") {
				continue
			}
			state := strings.ToLower(m[3])
			st := "error"
			switch {
			case strings.Contains(state, "connected"):
				st = "connected"
			case strings.Contains(state, "auth"):
				st = "needs-auth"
			case strings.Contains(state, "pending"):
				st = "disabled"
			}
			entry := HarnessMCP{Name: m[1], Target: m[2], Status: st}
			target := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[2]), "(HTTP)"))
			target = strings.TrimSpace(strings.TrimSuffix(target, "(SSE)"))
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
				entry.URL = target
			} else if f := strings.Fields(target); len(f) > 0 {
				entry.Command, entry.Args = f[0], f[1:]
			}
			out = append(out, entry)
		}
	default: // one entry per line; "No … configured" means none
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(strings.ToLower(line), "no ") {
				continue
			}
			out = append(out, HarnessMCP{Name: line})
		}
	}
	return out
}

func parseLines(text string) []string {
	out := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		low := strings.ToLower(line)
		if line == "" || strings.HasPrefix(low, "no ") || strings.HasSuffix(low, ":") {
			continue
		}
		out = append(out, line)
	}
	return out
}

var frontField = regexp.MustCompile(`(?m)^(name|description):\s*"?(.*?)"?\s*$`)

func readSkills(dirs []string) []HarnessSkill {
	out := []HarnessSkill{}
	seen := map[string]bool{}
	for _, d := range dirs {
		dir := expandHome(d)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name(), "SKILL.md"))
			if err != nil {
				continue
			}
			if len(b) > 16<<10 {
				b = b[:16<<10]
			}
			s := HarnessSkill{Name: e.Name(), Folder: d, FromLoom: strings.HasPrefix(e.Name(), "loom-")}
			for _, m := range frontField.FindAllStringSubmatch(string(b), -1) {
				if m[1] == "description" && s.Description == "" {
					s.Description = m[2]
				}
			}
			key := d + "/" + e.Name()
			if !seen[key] {
				seen[key] = true
				out = append(out, s)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func inspectHarness(ctx context.Context, id string) (HarnessInspection, error) {
	spec, ok := harnessInspectSpec(id)
	if !ok {
		return HarnessInspection{}, errors.New("inspection not described for this harness")
	}
	r := HarnessInspection{At: time.Now().UnixMilli(), MCP: []HarnessMCP{}, Plugins: []string{}, Skills: []HarnessSkill{}, Env: []string{}}
	path, err := lifecycleLookPath(spec.Binary)
	if err != nil {
		return r, nil
	}
	r.Installed, r.Path, r.CanUpdate = true, path, len(spec.Update) > 0 || spec.UpdateInstall
	var wg sync.WaitGroup
	var mu sync.Mutex
	fail := func(what string, err error) {
		mu.Lock()
		r.Errors = append(r.Errors, what+" : "+err.Error())
		mu.Unlock()
	}
	wg.Add(4)
	go func() {
		defer wg.Done()
		if out, err := runInspect(ctx, spec.Version); err == nil {
			mu.Lock()
			r.Version = strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
			mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		if spec.Auth == nil {
			return
		}
		auth := map[string]any{}
		switch {
		case spec.Auth.File != "":
			// Only the provider names: never the stored credentials.
			b, err := os.ReadFile(expandHome(spec.Auth.File))
			var m map[string]any
			if err == nil && json.Unmarshal(b, &m) == nil {
				names := []string{}
				for k := range m {
					names = append(names, k)
				}
				sort.Strings(names)
				auth["providers"], auth["connected"] = names, len(names) > 0
			}
		case spec.Auth.Format == "json":
			out, err := runInspect(ctx, spec.Auth.Cmd)
			var m map[string]any
			if i := strings.Index(out, "{"); i >= 0 && json.Unmarshal([]byte(out[i:]), &m) == nil {
				auth["connected"], auth["method"], auth["account"] = m[spec.Auth.Connected], m[spec.Auth.Method], m[spec.Auth.Account]
			} else if err != nil {
				fail("account", err)
			}
		default:
			out, err := runInspect(ctx, spec.Auth.Cmd)
			line := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
			auth["status"] = line
			auth["connected"] = err == nil && !strings.Contains(strings.ToLower(line), "not logged")
		}
		mu.Lock()
		r.Auth = auth
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		if spec.MCP == nil {
			return
		}
		out, err := runInspect(ctx, spec.MCP.Cmd)
		list := parseMCP(spec.MCP.Format, out)
		if err != nil && len(list) == 0 {
			fail("MCP", err)
			return
		}
		mu.Lock()
		r.MCP, r.MCPKnown = list, true
		mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		if spec.Plugins == nil {
			return
		}
		plugins := []string{}
		if spec.Plugins.File != "" {
			b, err := os.ReadFile(expandHome(spec.Plugins.File))
			var m struct {
				Plugins map[string][]struct {
					Version string `json:"version"`
				} `json:"plugins"`
			}
			if err == nil && json.Unmarshal(b, &m) == nil {
				for name, v := range m.Plugins {
					label := strings.SplitN(name, "@", 2)[0]
					if len(v) > 0 && v[0].Version != "" {
						label += " " + v[0].Version
					}
					plugins = append(plugins, label)
				}
			}
		} else if out, err := runInspect(ctx, spec.Plugins.Cmd); err == nil {
			plugins = parseLines(out)
		}
		sort.Strings(plugins)
		mu.Lock()
		r.Plugins = plugins
		mu.Unlock()
	}()
	wg.Wait()
	r.Skills = readSkills(spec.Skills)
	for _, name := range spec.Env {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			r.Env = append(r.Env, name)
		}
	}
	return r, nil
}

// GET: cached inspection (refreshed when older than 10 minutes or ?refresh=1).
func handleHarnessInspect(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	id := r.PathValue("id")
	inspectMu.Lock()
	cached, ok := inspectCache[id]
	inspectMu.Unlock()
	if ok && r.URL.Query().Get("refresh") == "" && time.Since(time.UnixMilli(cached.At)) < 10*time.Minute {
		sendJSON(w, 200, map[string]any{"ok": true, "inspection": cached})
		return
	}
	res, err := inspectHarness(r.Context(), id)
	if err != nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	inspectMu.Lock()
	inspectCache[id] = res
	inspectMu.Unlock()
	sendJSON(w, 200, map[string]any{"ok": true, "inspection": res})
}

// POST: run the harness's own update command (explicit user action).
func handleHarnessUpdate(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct{}
	if !workspaceDecode(w, r, &req) {
		return
	}
	target, id := harnessLifecycleRuntimeTarget(r.PathValue("id"))
	state, err := harnessLifecycle.action(r.Context(), target, id, "update")
	if err != nil {
		sendHarnessLifecycle(w, state, err)
		return
	}
	var inspection any
	if target == "local" {
		res, _ := inspectHarness(r.Context(), id)
		inspection = res
	}
	sendJSON(w, 200, map[string]any{"ok": true, "state": state, "log": state.Log, "inspection": inspection})
}
