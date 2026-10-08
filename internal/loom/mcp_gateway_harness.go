package loom

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var gatewayMu sync.Mutex
var gatewayToken struct{ home, token string }

const gatewayStateKey = "mcp_gateway"
const gatewayBegin = "# loom-gateway begin"
const gatewayEnd = "# loom-gateway end"

type gatewayOwnership struct {
	File string `json:"file"`
	Hash string `json:"hash"`
}
type gatewayState struct {
	TokenHash string                      `json:"token_hash"`
	Entries   map[string]gatewayOwnership `json:"entries"`
}
type gatewayHarness struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Supported  bool   `json:"supported"`
	Registered bool   `json:"registered"`
	File       string `json:"file"`
}
type gatewayInfo struct {
	URL       string           `json:"url"`
	TokenSet  bool             `json:"token_set"`
	Harnesses []gatewayHarness `json:"harnesses"`
}
type gatewayHarnessSpec struct{ id, name, file, key string }

var gatewayHarnessSpecs = []gatewayHarnessSpec{
	{"claude-code", "Claude Code", "~/.claude.json", "mcpServers"},
	{"codex", "Codex", "~/.codex/config.toml", ""},
	{"opencode", "OpenCode", "~/.config/opencode/opencode.json", "mcp"},
	{"gemini", "Gemini CLI", "~/.gemini/settings.json", "mcpServers"},
}

func gatewaySpec(id string) (gatewayHarnessSpec, error) {
	for _, h := range gatewayHarnessSpecs {
		if h.id == id {
			return h, nil
		}
	}
	return gatewayHarnessSpec{}, errors.New("unsupported harness")
}
func loadGatewayState() (gatewayState, error) {
	state := gatewayState{Entries: map[string]gatewayOwnership{}}
	data, err := getBytesErr(bkState, gatewayStateKey)
	if err == nil && len(data) != 0 {
		err = json.Unmarshal(data, &state)
	}
	if state.Entries == nil {
		state.Entries = map[string]gatewayOwnership{}
	}
	return state, err
}

type gatewayConfig struct {
	spec       gatewayHarnessSpec
	root       *os.Root
	rel        string
	before     []byte
	existed    bool
	top        map[string]json.RawMessage
	entries    map[string]json.RawMessage
	entry      []byte
	start, end int
}

func openGatewayConfig(h gatewayHarnessSpec) (*gatewayConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	c := &gatewayConfig{spec: h, root: root, start: -1, end: -1}
	c.rel, err = filepath.Rel(home, expandHome(h.file))
	if err != nil {
		root.Close()
		return nil, err
	}
	fail := func(err error) (*gatewayConfig, error) { root.Close(); return nil, err }
	info, err := root.Lstat(c.rel)
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	if err == nil && !info.Mode().IsRegular() {
		return fail(errors.New("harness config must be a regular file"))
	}
	c.existed = err == nil
	if c.existed {
		c.before, err = root.ReadFile(c.rel)
		if err != nil {
			return fail(err)
		}
	}
	if h.id == "codex" {
		c.start, c.end, err = gatewayTOMLBlock(string(c.before))
		if err != nil {
			return fail(err)
		}
		if c.start >= 0 {
			c.entry = c.before[c.start:c.end]
			for _, line := range strings.Split(string(c.before[c.end:]), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if !strings.HasPrefix(line, "[") {
					return fail(errors.New("gateway TOML table edited outside its markers"))
				}
				break
			}
		}
		outside := string(c.before)
		if c.start >= 0 {
			outside = outside[:c.start] + outside[c.end:]
		}
		if gatewayForeignTOML(outside) {
			return fail(errors.New("refusing to modify an unowned Codex loom entry"))
		}
	} else {
		c.top = map[string]json.RawMessage{}
		c.entries = map[string]json.RawMessage{}
		if c.existed {
			if err = json.Unmarshal(c.before, &c.top); err != nil || c.top == nil {
				return fail(errors.New("invalid harness JSON config"))
			}
		}
		if raw, ok := c.top[h.key]; ok {
			if err = json.Unmarshal(raw, &c.entries); err != nil || c.entries == nil {
				return fail(errors.New("invalid harness MCP config"))
			}
		}
		if raw, ok := c.entries["loom"]; ok {
			var value any
			if err = json.Unmarshal(raw, &value); err != nil {
				return fail(err)
			}
			c.entry, err = json.Marshal(value)
			if err != nil {
				return fail(err)
			}
		}
	}
	return c, nil
}
func gatewayTOMLBlock(text string) (int, int, error) {
	start, end, offset := -1, -1, 0
	for _, line := range strings.SplitAfter(text, "\n") {
		switch strings.TrimSpace(line) {
		case gatewayBegin:
			if start >= 0 || end >= 0 {
				return -1, -1, errors.New("duplicate gateway TOML markers")
			}
			start = offset
		case gatewayEnd:
			if start < 0 || end >= 0 {
				return -1, -1, errors.New("invalid gateway TOML markers")
			}
			end = offset + len(line)
		}
		offset += len(line)
	}
	if (start < 0) != (end < 0) {
		return -1, -1, errors.New("incomplete gateway TOML markers")
	}
	return start, end, nil
}

var gatewayTOMLWhitespace = regexp.MustCompile(`[\s"']`)

func gatewayForeignTOML(text string) bool {
	inServers := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		compact := gatewayTOMLWhitespace.ReplaceAllString(line, "")
		if strings.HasPrefix(compact, "[") {
			inServers = compact == "[mcp_servers]"
			if strings.HasPrefix(compact, "[mcp_servers.loom]") || strings.HasPrefix(compact, "[mcp_servers.loom.") || strings.HasPrefix(compact, "[[mcp_servers.loom") {
				return true
			}
		} else if strings.HasPrefix(compact, "mcp_servers.loom=") || strings.HasPrefix(compact, "mcp_servers.loom.") || (inServers && (strings.HasPrefix(compact, "loom=") || strings.HasPrefix(compact, "loom."))) || strings.HasPrefix(compact, "mcp_servers=") {
			return true
		}
	}
	return false
}
func (c *gatewayConfig) owned(state gatewayState) bool {
	record, ok := state.Entries[c.spec.id]
	return ok && record.File == expandHome(c.spec.file) && len(c.entry) != 0 && record.Hash == hashWebKey(string(c.entry))
}
func (c *gatewayConfig) token() string {
	if c.spec.id == "codex" {
		for _, line := range strings.Split(string(c.entry), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "http_headers = ") {
				var headers map[string]string
				raw := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "http_headers = "))
				raw = strings.Replace(raw, `"Authorization" =`, `"Authorization":`, 1)
				if json.Unmarshal([]byte(raw), &headers) == nil {
					return strings.TrimPrefix(headers["Authorization"], "Bearer ")
				}
			}
		}
		return ""
	}
	var entry struct {
		Headers map[string]string `json:"headers"`
	}
	_ = json.Unmarshal(c.entry, &entry)
	return strings.TrimPrefix(entry.Headers["Authorization"], "Bearer ")
}
func (c *gatewayConfig) render(url, token string, enabled bool) ([]byte, []byte, error) {
	if c.spec.id == "codex" {
		text := string(c.before)
		block := ""
		if enabled {
			block = gatewayBegin + "\n[mcp_servers.loom]\nurl = " + strconv.Quote(url) + "\nhttp_headers = { \"Authorization\" = " + strconv.Quote("Bearer "+token) + " }\n" + gatewayEnd + "\n"
		}
		if c.start >= 0 {
			text = text[:c.start] + block + text[c.end:]
		} else if enabled {
			if text != "" && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += block
		}
		return []byte(text), []byte(block), nil
	}
	var entry []byte
	if enabled {
		value := map[string]any{"url": url, "headers": map[string]string{"Authorization": "Bearer " + token}}
		switch c.spec.id {
		case "claude-code":
			value["type"] = "http"
		case "opencode":
			value["type"] = "remote"
			value["enabled"] = true
		case "gemini":
			delete(value, "url")
			value["httpUrl"] = url
		}
		entry, _ = json.Marshal(value)
		c.entries["loom"] = entry
	} else {
		delete(c.entries, "loom")
	}
	entries, err := json.Marshal(c.entries)
	if err != nil {
		return nil, nil, err
	}
	c.top[c.spec.key] = entries
	data, err := json.MarshalIndent(c.top, "", "  ")
	return append(data, '\n'), entry, err
}
func gatewayRootWrite(root *os.Root, rel string, data []byte) error {
	if current, err := root.ReadFile(rel); err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err := root.MkdirAll(filepath.Dir(rel), 0700); err != nil {
		return err
	}
	tmpName := filepath.Join(filepath.Dir(rel), ".loom-gateway-"+rand.Text())
	f, err := root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmpName)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = root.Rename(tmpName, rel)
	}
	return err
}
func (c *gatewayConfig) write(data []byte) error {
	current, err := c.root.ReadFile(c.rel)
	if err != nil && !(os.IsNotExist(err) && !c.existed) {
		return err
	}
	if err == nil && !c.existed {
		return errors.New("harness config appeared during editing")
	}
	if !bytes.Equal(current, c.before) {
		return errors.New("harness config changed during editing")
	}
	if bytes.Equal(data, c.before) {
		return nil
	}
	if c.existed {
		f, err := c.root.OpenFile(c.rel+".loom-backup", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil && !os.IsExist(err) {
			return err
		}
		if err == nil {
			_, err = f.Write(c.before)
			if err == nil {
				err = f.Sync()
			}
			if closeErr := f.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				_ = c.root.Remove(c.rel + ".loom-backup")
				return err
			}
		}
	}
	return gatewayRootWrite(c.root, c.rel, data)
}
func gatewayInfoFromState(state gatewayState) gatewayInfo {
	info := gatewayInfo{URL: gatewayURL(), TokenSet: state.TokenHash != "", Harnesses: []gatewayHarness{}}
	for _, h := range gatewayHarnessSpecs {
		registered := false
		if c, err := openGatewayConfig(h); err == nil {
			registered = c.owned(state)
			c.root.Close()
		}
		info.Harnesses = append(info.Harnesses, gatewayHarness{h.id, h.name, true, registered, expandHome(h.file)})
	}
	return info
}
func gatewayRegistered(id string) bool { return gatewayRevision(id) != "" }
func gatewayRevision(id string) string {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	state, err := loadGatewayState()
	if err != nil {
		return ""
	}
	h, err := gatewaySpec(id)
	if err != nil {
		return ""
	}
	c, err := openGatewayConfig(h)
	if err != nil {
		return ""
	}
	defer c.root.Close()
	if c.owned(state) {
		return state.Entries[id].Hash
	}
	return ""
}
func gatewayExistingToken(state gatewayState) (string, error) {
	if gatewayToken.home == LoomHome() && gatewayToken.token != "" && hashWebKey(gatewayToken.token) == state.TokenHash {
		return gatewayToken.token, nil
	}
	for _, h := range gatewayHarnessSpecs {
		c, err := openGatewayConfig(h)
		if err != nil {
			continue
		}
		owned := c.owned(state)
		token := c.token()
		c.root.Close()
		if owned && hashWebKey(token) == state.TokenHash {
			return token, nil
		}
	}
	if state.TokenHash != "" {
		return "", errors.New("gateway token unavailable; rotate the token before registering")
	}
	return "loom-gateway-" + rand.Text(), nil
}
func setGatewayHarness(id string, enabled bool) error {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	state, err := loadGatewayState()
	if err != nil {
		return err
	}
	h, err := gatewaySpec(id)
	if err != nil {
		return err
	}
	c, err := openGatewayConfig(h)
	if err != nil {
		return err
	}
	defer c.root.Close()
	if len(c.entry) != 0 && !c.owned(state) {
		return errors.New("refusing to modify a loom entry Loom did not create or which was edited")
	}
	if !enabled && len(c.entry) == 0 {
		delete(state.Entries, id)
		return putJSON(bkState, gatewayStateKey, state)
	}
	token := ""
	if enabled {
		token, err = gatewayExistingToken(state)
		if err != nil {
			return err
		}
	}
	data, entry, err := c.render(gatewayURL(), token, enabled)
	if err != nil {
		return err
	}
	if err = c.write(data); err != nil {
		return err
	}
	if enabled {
		state.TokenHash = hashWebKey(token)
		state.Entries[id] = gatewayOwnership{expandHome(h.file), hashWebKey(string(entry))}
	} else {
		delete(state.Entries, id)
	}
	if err = putJSON(bkState, gatewayStateKey, state); err != nil {
		_ = c.restore()
	} else if enabled {
		gatewayToken.home, gatewayToken.token = LoomHome(), token
	}
	return err
}
func (c *gatewayConfig) restore() error {
	if !c.existed {
		return c.root.Remove(c.rel)
	}
	return gatewayRootWrite(c.root, c.rel, c.before)
}
func rotateGatewayToken() (string, error) {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	state, err := loadGatewayState()
	if err != nil {
		return "", err
	}
	token := "loom-gateway-" + rand.Text()
	type change struct {
		config *gatewayConfig
		data   []byte
		entry  []byte
	}
	changes := []change{}
	defer func() {
		for _, ch := range changes {
			ch.config.root.Close()
		}
	}()
	for _, h := range gatewayHarnessSpecs {
		if _, ok := state.Entries[h.id]; !ok {
			continue
		}
		c, err := openGatewayConfig(h)
		if err != nil {
			return "", err
		}
		if !c.owned(state) {
			c.root.Close()
			return "", errors.New("registered loom entry changed; remove it from the harness config and unregister before rotating")
		}
		data, entry, err := c.render(gatewayURL(), token, true)
		if err != nil {
			c.root.Close()
			return "", err
		}
		changes = append(changes, change{c, data, entry})
	}
	applied := 0
	rollback := func() {
		for i := applied - 1; i >= 0; i-- {
			_ = changes[i].config.restore()
		}
	}
	for _, ch := range changes {
		if err = ch.config.write(ch.data); err != nil {
			rollback()
			return "", err
		}
		applied++
		state.Entries[ch.config.spec.id] = gatewayOwnership{expandHome(ch.config.spec.file), hashWebKey(string(ch.entry))}
	}
	state.TokenHash = hashWebKey(token)
	if err = putJSON(bkState, gatewayStateKey, state); err != nil {
		rollback()
		return "", err
	}
	gatewayToken.home, gatewayToken.token = LoomHome(), token
	return token, nil
}
func handleMCPGateway(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		sendJSON(w, 405, map[string]any{"error": "method not allowed"})
		return
	}
	if r.Method == "POST" {
		var req struct {
			Harness string `json:"harness"`
			Enabled bool   `json:"enabled"`
		}
		if !workspaceDecode(w, r, &req) {
			return
		}
		if err := setGatewayHarness(req.Harness, req.Enabled); err != nil {
			brainResponse(w, nil, err)
			return
		}
	}
	state, err := loadGatewayState()
	brainResponse(w, gatewayInfoFromState(state), err)
}
func handleMCPGatewayToken(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, "POST") {
		return
	}
	var req struct {
		Rotate bool `json:"rotate"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if !req.Rotate {
		brainResponse(w, nil, errors.New("rotate must be true"))
		return
	}
	token, err := rotateGatewayToken()
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "token": token, "token_set": true, "url": gatewayURL()})
}
