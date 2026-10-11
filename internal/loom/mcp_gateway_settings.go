package loom

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
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
	if err = putJSON(bkState, gatewayStateKey, state); err != nil {
		return "", err
	}
	gatewayToken.home, gatewayToken.token = LoomHome(), token
	return token, nil
}
func handleMCPGateway(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) || !usageVaultAccess(w) {
		return
	}
	state, err := loadGatewayState()
	brainResponse(w, map[string]any{"url": gatewayURL(), "token_set": state.TokenHash != ""}, err)
}
func handleMCPGatewayToken(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
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
	w.Header().Set("Cache-Control", "no-store")
	sendJSON(w, 200, map[string]any{"ok": true, "token": token, "token_set": true, "url": gatewayURL()})
}
