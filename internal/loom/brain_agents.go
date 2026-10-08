package loom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const brainAgentsStateKey = "brain_agents"
const brainAgentBegin = "<!-- loom-brain begin -->"
const brainAgentEnd = "<!-- loom-brain end -->"

var brainAgentsMu sync.Mutex
var errBrainAgentNoPrimary = errors.New("no writable primary brain is configured")

type brainAgentOwnership struct {
	Hash       string          `json:"hash"`
	Previous   json.RawMessage `json:"previous,omitempty"`
	Separator  bool            `json:"separator,omitempty"`
	PrefixHash string          `json:"prefix_hash,omitempty"`
	Created    bool            `json:"created,omitempty"`
}
type brainAgentLink struct {
	Enabled bool                           `json:"enabled"`
	Files   map[string]brainAgentOwnership `json:"files"`
}
type brainAgentState map[string]brainAgentLink

type brainAgentInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Supported bool   `json:"supported"`
	Linked    bool   `json:"linked"`
	File      string `json:"file"`
	Note      string `json:"note"`
}
type brainAgentsInfo struct {
	MemoryDir string           `json:"memory_dir"`
	Agents    []brainAgentInfo `json:"agents"`
}
type brainAgentSpec struct{ id, name, file, note string }

// Pi's path is accepted only when the installed package documents it. No CLI
// is launched for discovery, and no guessed fallback is written.
func piBrainInstructions() string {
	dirs := []string{}
	if dir := os.Getenv("PI_PACKAGE_DIR"); dir != "" {
		dirs = append(dirs, dir)
	}
	if binary, err := lifecycleLookPath("pi"); err == nil {
		if binary, err = filepath.EvalSymlinks(binary); err == nil {
			dir := filepath.Dir(binary)
			for i := 0; i < 4; i++ {
				dirs = append(dirs, dir)
				dir = filepath.Dir(dir)
			}
		}
	}
	for _, dir := range dirs {
		data, err := os.ReadFile(filepath.Join(dir, "README.md"))
		if err != nil || !strings.Contains(string(data), "~/.pi/agent/AGENTS.md") {
			continue
		}
		home, _ := os.UserHomeDir()
		config := filepath.Join(home, ".pi", "agent")
		if override := os.Getenv("PI_CODING_AGENT_DIR"); override != "" {
			if !strings.Contains(string(data), "PI_CODING_AGENT_DIR") {
				return ""
			}
			config = expandHome(override)
		}
		return filepath.Join(config, "AGENTS.md")
	}
	return ""
}
func brainAgentSpecs() []brainAgentSpec {
	home, _ := os.UserHomeDir()
	codex := os.Getenv("CODEX_HOME")
	if codex == "" {
		codex = filepath.Join(home, ".codex")
	}
	pi := piBrainInstructions()
	note := ""
	if pi == "" {
		note = "installed Pi documentation does not identify a global instructions file"
	}
	return []brainAgentSpec{
		{"claude-code", "Claude Code", filepath.Join(home, ".claude", "settings.json"), ""},
		{"codex", "Codex", filepath.Join(expandHome(codex), "AGENTS.md"), ""},
		{"opencode", "OpenCode", filepath.Join(home, ".config", "opencode", "AGENTS.md"), ""},
		{"gemini", "Gemini CLI", filepath.Join(home, ".gemini", "GEMINI.md"), ""},
		{"pi", "Pi", pi, note},
	}
}
func loadBrainAgentState() (brainAgentState, error) {
	state := brainAgentState{}
	data, err := getBytesErr(bkState, brainAgentsStateKey)
	if err == nil && len(data) > 0 {
		err = json.Unmarshal(data, &state)
	}
	if state == nil {
		state = brainAgentState{}
	}
	return state, err
}

type brainAgentProject struct {
	Slug      string `json:"slug"`
	Directory string `json:"directory"`
	Memory    string `json:"memory"`
}
type brainAgentLayout struct {
	Dir      string
	Projects map[string]brainAgentProject
}

func (s *brainService) brainAgentLayout(create bool) (brainAgentLayout, error) {
	out := brainAgentLayout{Projects: map[string]brainAgentProject{}}
	if err := brainAvailable(); err != nil {
		return out, err
	}
	sources, err := s.storage.LoadSources()
	if err != nil {
		return out, err
	}
	primaryPath := ""
	for _, src := range sources {
		if src.Primary && !src.ReadOnly && src.Permission == "write" {
			primaryPath = src.Path
			break
		}
	}
	if primaryPath == "" {
		return out, errBrainAgentNoPrimary
	}
	root, err := os.OpenRoot(primaryPath)
	if err != nil {
		return out, err
	}
	defer root.Close()
	out.Dir, err = filepath.Abs(primaryPath)
	if err != nil {
		return out, err
	}
	for _, p := range listProjects() {
		if p.Machine != "" && p.Machine != "local" || p.Directory == "" {
			continue
		}
		slug := skillDirSlug(p.ID)
		out.Projects[p.ID] = brainAgentProject{slug, p.Directory, filepath.ToSlash(filepath.Join("Projects", slug, "memory"))}
	}
	if !create {
		return out, nil
	}
	for _, rel := range append([]string{"Memory"}, brainAgentProjectFolders(out)...) {
		if err = root.MkdirAll(rel, 0700); err != nil {
			return out, err
		}
		file := filepath.Join(rel, "MEMORY.md")
		f, openErr := root.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(openErr) {
			info, err := root.Stat(file)
			if err != nil {
				return out, err
			}
			if !info.Mode().IsRegular() {
				return out, errors.New("memory index must be a regular file")
			}
			continue
		}
		if openErr != nil {
			return out, openErr
		}
		_, err = f.WriteString("# Memory\n\n")
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return out, err
		}
	}
	// Preserve other brain metadata; this field is the local project-to-memory map.
	metadata := map[string]json.RawMessage{}
	if data, readErr := root.ReadFile(".loom/brain.json"); readErr == nil {
		if json.Unmarshal(data, &metadata) != nil || metadata == nil {
			return out, errors.New("invalid .loom/brain.json")
		}
	} else if !os.IsNotExist(readErr) {
		return out, readErr
	}
	metadata["agent_projects"], _ = json.Marshal(out.Projects)
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err == nil {
		err = gatewayRootWrite(root, ".loom/brain.json", append(data, '\n'))
	}
	return out, err
}
func brainAgentProjectFolders(layout brainAgentLayout) []string {
	folders := []string{}
	for _, p := range layout.Projects {
		folders = append(folders, filepath.FromSlash(p.Memory))
	}
	sort.Strings(folders)
	return folders
}
func brainAgentBlock(layout brainAgentLayout) string {
	// Quote paths so unusual filenames cannot introduce instructions or markers.
	quote := func(path string) string { data, _ := json.Marshal(path); return string(data) }
	return brainAgentBegin + "\n" +
		"Loom shared memory index: " + quote(filepath.Join(layout.Dir, "Memory", "MEMORY.md")) + ".\n" +
		"At session start, read this index and relevant linked topic files.\n" +
		"For the current project, use Projects/<slug>/memory/ under " + quote(layout.Dir) + "; read its MEMORY.md too.\n" +
		"Find the project directory, slug and memory folder in agent_projects in " + quote(filepath.Join(layout.Dir, ".loom", "brain.json")) + ".\n" +
		"Write durable memories as Markdown topic files with YAML frontmatter: name, description, type (user|feedback|project|reference).\n" +
		"For feedback and project memories, include Why: and How to apply: in the body.\n" +
		"Update the appropriate MEMORY.md index with one link line and a short description for each topic file.\n" +
		"Never store secrets. Prefer updating existing memory files over creating duplicates.\n" + brainAgentEnd + "\n"
}

// Use the gateway's confined, checked, atomic writer and backup-once policy.
// Global configs are rooted at HOME; explicit config-home overrides and project
// files are rooted at their existing directory (or its nearest existing parent).
func openBrainAgentFile(file string) (*gatewayConfig, error) {
	if !filepath.IsAbs(file) {
		return nil, errors.New("agent config path must be absolute")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	base := home
	if filepath.Base(filepath.Dir(file)) == ".claude" && (filepath.Base(file) == "settings.local.json" || filepath.Base(file) == ".gitignore") {
		base = filepath.Dir(filepath.Dir(file))
	}
	rel, err := filepath.Rel(base, file)
	if err != nil || !filepath.IsLocal(rel) {
		base = filepath.Dir(file)
		for {
			_, statErr := os.Stat(base)
			if statErr == nil {
				break
			}
			if !os.IsNotExist(statErr) {
				return nil, statErr
			}
			parent := filepath.Dir(base)
			if parent == base {
				return nil, statErr
			}
			base = parent
		}
		rel, err = filepath.Rel(base, file)
	}
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	c := &gatewayConfig{root: root, rel: rel, start: -1, end: -1}
	info, err := root.Lstat(rel)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("agent config must be a regular file")
	}
	if err == nil {
		c.existed = true
		c.before, err = root.ReadFile(rel)
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	return c, nil
}
func brainAgentMarkers(text, begin, end string) (int, int, error) {
	if strings.Count(text, begin) > 1 || strings.Count(text, end) > 1 {
		return -1, -1, errors.New("duplicate loom-brain markers")
	}
	start, stop, offset := -1, -1, 0
	for _, line := range strings.SplitAfter(text, "\n") {
		switch strings.TrimSpace(line) {
		case begin:
			if start >= 0 || stop >= 0 {
				return -1, -1, errors.New("duplicate loom-brain markers")
			}
			start = offset
		case end:
			if start < 0 || stop >= 0 {
				return -1, -1, errors.New("invalid loom-brain markers")
			}
			stop = offset + len(line)
		}
		offset += len(line)
	}
	if (start < 0) != (stop < 0) || strings.Contains(text, begin) && start < 0 || strings.Contains(text, end) && stop < 0 {
		return -1, -1, errors.New("incomplete loom-brain markers")
	}
	return start, stop, nil
}
func brainAgentEntry(c *gatewayConfig, kind string) ([]byte, error) {
	if kind == "json" {
		c.top = map[string]json.RawMessage{}
		if c.existed && (json.Unmarshal(c.before, &c.top) != nil || c.top == nil) {
			return nil, errors.New("invalid Claude Code settings JSON")
		}
		return c.top["autoMemoryDirectory"], nil
	}
	begin, end := brainAgentBegin, brainAgentEnd
	if kind == "ignore" {
		begin, end = "# loom-brain begin", "# loom-brain end"
	}
	var err error
	c.start, c.end, err = brainAgentMarkers(string(c.before), begin, end)
	if err != nil {
		return nil, err
	}
	if c.start >= 0 {
		return c.before[c.start:c.end], nil
	}
	return nil, nil
}
func brainAgentOwned(entry []byte, record brainAgentOwnership) bool {
	if record.Hash == "" || len(entry) == 0 {
		return false
	}
	// JSON strings may be reformatted by the native client.
	var value string
	if json.Unmarshal(entry, &value) == nil {
		entry, _ = json.Marshal(value)
	}
	return hashWebKey(string(entry)) == record.Hash
}

type brainAgentChange struct {
	c      *gatewayConfig
	data   []byte
	remove bool
}

// Config edits compare whole files before publication, but ownership compares
// only Loom's entry. User edits elsewhere survive re-pointing and removal.
func (s *brainService) applyBrainAgent(state brainAgentState, spec brainAgentSpec, enabled bool, layout brainAgentLayout) error {
	link := state[spec.id]
	if link.Files == nil {
		link.Files = map[string]brainAgentOwnership{}
	}
	desired := map[string]string{}
	kinds := map[string]string{}
	if enabled && layout.Dir != "" {
		if spec.id == "claude-code" {
			desired[spec.file] = filepath.Join(layout.Dir, "Memory")
			kinds[spec.file] = "json"
			for _, p := range layout.Projects {
				file := filepath.Join(p.Directory, ".claude", "settings.local.json")
				if previous, ok := desired[file]; ok && previous != filepath.Join(layout.Dir, filepath.FromSlash(p.Memory)) {
					return errors.New("multiple projects use the same Claude Code settings directory")
				}
				desired[file] = filepath.Join(layout.Dir, filepath.FromSlash(p.Memory))
				kinds[file] = "json"
				ignore := filepath.Join(p.Directory, ".claude", ".gitignore")
				desired[ignore] = "# loom-brain begin\n/settings.local.json\n/settings.local.json.loom-backup\n/.gitignore\n/.gitignore.loom-backup\n# loom-brain end\n"
				kinds[ignore] = "ignore"
			}
		} else {
			desired[spec.file] = brainAgentBlock(layout)
		}
	}
	files := map[string]bool{}
	for file := range link.Files {
		files[file] = true
	}
	for file := range desired {
		files[file] = true
	}
	ordered := []string{}
	for file := range files {
		ordered = append(ordered, file)
	}
	sort.Strings(ordered)
	changes := []brainAgentChange{}
	defer func() {
		for _, ch := range changes {
			ch.c.root.Close()
		}
	}()
	next := brainAgentLink{Enabled: enabled, Files: map[string]brainAgentOwnership{}}
	for _, file := range ordered {
		c, err := openBrainAgentFile(file)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		changes = append(changes, brainAgentChange{c: c})
		kind := kinds[file]
		if kind == "" && spec.id == "claude-code" {
			if filepath.Base(file) == ".gitignore" {
				kind = "ignore"
			} else {
				kind = "json"
			}
		}
		entry, err := brainAgentEntry(c, kind)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		record, recorded := link.Files[file]
		target, keep := desired[file]
		if recorded && len(entry) > 0 && !brainAgentOwned(entry, record) {
			return fmt.Errorf("%s: refusing to modify a brain link edited outside Loom", file)
		}
		if !recorded && len(entry) > 0 && kind != "json" {
			return fmt.Errorf("%s: refusing to modify unowned loom-brain markers", file)
		}
		if !recorded {
			record = brainAgentOwnership{Created: !c.existed}
			if kind == "json" {
				record.Previous = append(json.RawMessage{}, entry...)
			}
		}
		data := c.before
		if kind == "json" {
			if keep {
				entry, _ = json.Marshal(target)
				c.top["autoMemoryDirectory"] = entry
			} else if len(entry) > 0 {
				if len(record.Previous) > 0 {
					c.top["autoMemoryDirectory"] = record.Previous
				} else {
					delete(c.top, "autoMemoryDirectory")
				}
			}
			if keep || len(entry) > 0 {
				data, err = json.MarshalIndent(c.top, "", "  ")
				data = append(data, '\n')
			}
		} else {
			text := string(c.before)
			replacement := ""
			if keep {
				replacement = target
			}
			if c.start >= 0 {
				start := c.start
				if !keep && record.Separator && start > 0 && text[start-1] == '\n' && hashWebKey(text[:start-1]) == record.PrefixHash {
					start--
				}
				text = text[:start] + replacement + text[c.end:]
			} else if keep {
				record.Separator = text != "" && !strings.HasSuffix(text, "\n")
				record.PrefixHash = hashWebKey(text)
				if record.Separator {
					text += "\n"
				}
				text += replacement
			}
			data, entry = []byte(text), []byte(replacement)
		}
		if err != nil {
			return err
		}
		if keep {
			record.Hash = hashWebKey(string(entry))
			next.Files[file] = record
		}
		changes[len(changes)-1].data = data
		changes[len(changes)-1].remove = !keep && record.Created && (len(data) == 0 || kind == "json" && len(c.top) == 0)
	}
	applied := 0
	rollback := func() {
		for i := applied - 1; i >= 0; i-- {
			_ = changes[i].c.restore()
		}
	}
	for _, ch := range changes {
		if err := ch.c.write(ch.data); err != nil {
			rollback()
			return err
		}
		applied++
		if ch.remove && ch.c.existed {
			if err := ch.c.root.Remove(ch.c.rel); err != nil {
				rollback()
				return err
			}
		}
	}
	state[spec.id] = next
	if err := putJSON(bkState, brainAgentsStateKey, state); err != nil {
		rollback()
		state[spec.id] = link
		return err
	}
	return nil
}

func (s *brainService) syncBrainAgents() error {
	brainAgentsMu.Lock()
	defer brainAgentsMu.Unlock()
	state, err := loadBrainAgentState()
	if err != nil {
		return err
	}
	active := false
	for _, link := range state {
		active = active || link.Enabled
	}
	if !active {
		return nil
	}
	layout, err := s.brainAgentLayout(true)
	if errors.Is(err, errBrainAgentNoPrimary) {
		layout, err = brainAgentLayout{}, nil
	}
	if err != nil {
		return err
	}
	var firstErr error
	for _, spec := range brainAgentSpecs() {
		if state[spec.id].Enabled && spec.file != "" {
			if err = s.applyBrainAgent(state, spec, true, layout); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
func (s *brainService) brainAgentsInfo(state brainAgentState) brainAgentsInfo {
	info := brainAgentsInfo{Agents: []brainAgentInfo{}}
	layout, layoutErr := s.brainAgentLayout(false)
	if layoutErr == nil {
		info.MemoryDir = filepath.Join(layout.Dir, "Memory")
	}
	for _, spec := range brainAgentSpecs() {
		row := brainAgentInfo{ID: spec.id, Name: spec.name, Supported: spec.file != "", File: spec.file, Note: spec.note}
		link := state[spec.id]
		row.Linked = link.Enabled && len(link.Files) > 0
		for file, record := range link.Files {
			c, err := openBrainAgentFile(file)
			if err == nil {
				kind := ""
				if spec.id == "claude-code" {
					kind = "json"
					if filepath.Base(file) == ".gitignore" {
						kind = "ignore"
					}
				}
				var entry []byte
				entry, err = brainAgentEntry(c, kind)
				if err == nil && !brainAgentOwned(entry, record) {
					err = errors.New("brain link missing or edited outside Loom")
				}
				c.root.Close()
			}
			if err != nil {
				row.Linked = false
				row.Note = err.Error()
			}
		}
		if layoutErr != nil && row.Supported {
			row.Note = layoutErr.Error()
		}
		info.Agents = append(info.Agents, row)
	}
	for _, installation := range agentInstallations() {
		if installation.Machine == "local" {
			continue
		}
		info.Agents = append(info.Agents, brainAgentInfo{ID: installation.RuntimeID, Name: installation.Name + " (" + installation.MachineName + ")", Note: "local only for now"})
	}
	return info
}
func (s *brainService) agentsHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	brainAgentsMu.Lock()
	defer brainAgentsMu.Unlock()
	state, err := loadBrainAgentState()
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	if r.Method == "POST" {
		var req struct {
			ID      string `json:"id"`
			Enabled *bool  `json:"enabled"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if req.Enabled == nil {
			brainResponse(w, nil, errors.New("enabled is required"))
			return
		}
		var spec *brainAgentSpec
		for _, item := range brainAgentSpecs() {
			if item.id == req.ID {
				spec = &item
				break
			}
		}
		if spec == nil {
			note := "unknown agent"
			for _, agent := range s.brainAgentsInfo(state).Agents {
				if agent.ID == req.ID {
					note = agent.Note
					break
				}
			}
			brainResponse(w, nil, errors.New(note))
			return
		}
		if spec.file == "" && *req.Enabled {
			brainResponse(w, nil, errors.New(spec.note))
			return
		}
		layout := brainAgentLayout{}
		if *req.Enabled {
			layout, err = s.brainAgentLayout(true)
		}
		if err == nil {
			err = s.applyBrainAgent(state, *spec, *req.Enabled, layout)
		}
		if err != nil {
			brainResponse(w, nil, err)
			return
		}
	}
	brainResponse(w, s.brainAgentsInfo(state), nil)
}
