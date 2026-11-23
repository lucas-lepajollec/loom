package loom

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
)

var gatewayMu sync.Mutex
var gatewayToken struct{ home, token string }

const gatewayStateKey = "mcp_gateway"

// Retained only to read old ownership metadata. Loom no longer writes native
// agent configurations; existing files and credentials belong to their owners.
type gatewayOwnership struct {
	File string `json:"file"`
	Hash string `json:"hash"`
}
type gatewayState struct {
	TokenHash string                      `json:"token_hash"`
	Entries   map[string]gatewayOwnership `json:"entries,omitempty"`
	// Last use of the current token, shown in Settings so the person sees
	// that an agent is connected: time (Unix seconds) and MCP client name.
	LastUsed   int64  `json:"last_used,omitempty"`
	LastClient string `json:"last_client,omitempty"`
}

// gatewayLastNote keeps the last recorded use in memory, so most gateway
// requests do not touch the database.
var gatewayLastNote struct {
	home, client string
	at           int64
}

// noteGatewayUse records that an agent used the gateway. A client name comes
// from the MCP request; calls by the same client refresh the time at most once
// a minute.
func noteGatewayUse(client string) {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	now, home := time.Now().Unix(), LoomHome()
	if n := gatewayLastNote; n.home == home && (client == "" || client == n.client) && now-n.at < 60 {
		return
	}
	state, err := loadGatewayState()
	if err != nil || state.TokenHash == "" {
		return
	}
	if client == "" {
		client = state.LastClient
	}
	if client != state.LastClient || now-state.LastUsed >= 60 {
		state.LastUsed, state.LastClient = now, client
		if putJSON(bkState, gatewayStateKey, state) != nil {
			return
		}
	}
	gatewayLastNote.home, gatewayLastNote.client, gatewayLastNote.at = home, state.LastClient, state.LastUsed
}

func loadGatewayState() (gatewayState, error) {
	var s gatewayState
	b, err := getBytesErr(bkState, gatewayStateKey)
	if err == nil && len(b) > 0 {
		err = json.Unmarshal(b, &s)
	}
	return s, err
}
func rotateGatewayToken() (string, error) {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	state, err := loadGatewayState()
	if err != nil {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	state.TokenHash = hashWebKey(token)
	state.LastUsed, state.LastClient = 0, "" // a new token has not been used yet
	gatewayLastNote.at = 0
	if err = putJSON(bkState, gatewayStateKey, state); err != nil {
		return "", err
	}
	gatewayToken.home, gatewayToken.token = LoomHome(), token
	return token, nil
}
func revokeGatewayToken() error {
	gatewayMu.Lock()
	defer gatewayMu.Unlock()
	state, err := loadGatewayState()
	if err != nil {
		return err
	}
	state.TokenHash = ""
	state.LastUsed, state.LastClient = 0, ""
	gatewayLastNote.at = 0
	if err = putJSON(bkState, gatewayStateKey, state); err != nil {
		return err
	}
	if gatewayToken.home == LoomHome() {
		gatewayToken.token = ""
	}
	return nil
}
func handleMCPGateway(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	state, err := loadGatewayState()
	brainResponse(w, map[string]any{"url": gatewayURL(), "token_set": state.TokenHash != "", "agents": agentsState(), "last_used": state.LastUsed, "last_client": state.LastClient}, err)
}
func handleMCPGatewayToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, DELETE")
		sendJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	if !usageVaultAccess(w) {
		return
	}
	if r.Method == http.MethodDelete {
		if err := revokeGatewayToken(); err != nil {
			brainResponse(w, nil, err)
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "token_set": false, "url": gatewayURL()})
		return
	}
	var req struct {
		Rotate bool `json:"rotate"`
		Revoke bool `json:"revoke"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Rotate == req.Revoke {
		brainResponse(w, nil, errors.New("set either rotate or revoke to true"))
		return
	}
	if req.Revoke {
		if err := revokeGatewayToken(); err != nil {
			brainResponse(w, nil, err)
			return
		}
		sendJSON(w, 200, map[string]any{"ok": true, "token_set": false, "url": gatewayURL()})
		return
	}
	token, err := rotateGatewayToken()
	if err != nil {
		brainResponse(w, nil, err)
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "token": token, "token_set": true, "url": gatewayURL()})
}
