package loom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
)

var ollamaModelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*){0,2}(:[A-Za-z0-9][A-Za-z0-9._-]*)?$`)

func handleOllamaPull(w http.ResponseWriter, r *http.Request)   { handleOllamaAction(w, r, true) }
func handleOllamaUnload(w http.ResponseWriter, r *http.Request) { handleOllamaAction(w, r, false) }

func handleOllamaAction(w http.ResponseWriter, r *http.Request, pull bool) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	fail := func(code int, message string) { sendJSON(w, code, map[string]any{"ok": false, "error": message}) }
	var input struct {
		Model string          `json:"model"`
		URL   json.RawMessage `json:"url"`
	}
	if !workspaceDecode(w, r, &input) {
		return
	}
	if len(input.URL) != 0 || len(input.Model) > 200 || !ollamaModelName.MatchString(input.Model) || pull && !isPublicOllamaModel(input.Model) {
		fail(400, "invalid Ollama model or target")
		return
	}
	n := currentEngineNode()
	if n == nil || !n.Direct || n.Kind != "ollama" {
		fail(409, "activate a saved Ollama connection first")
		return
	}
	timeout := 15 * time.Second
	if pull {
		timeout = 60 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	if pull {
		if err := workspaceSessions.authorizePolicy(ctx, policy.Input{Subject: "node.install", MachineID: "local", Fallback: policy.Allow}, false); err != nil {
			fail(403, err.Error())
			return
		}
	}
	if currentEngineNode() != n {
		fail(409, "active engine changed")
		return
	}
	endpoint := "/api/generate"
	payload := map[string]any{"model": input.Model, "keep_alive": 0, "stream": false}
	if pull {
		endpoint = "/api/pull"
		payload = map[string]any{"model": input.Model, "stream": true}
	}
	// Old direct links are also persisted in engine_node. Revalidate the saved
	// origin and use fixed native paths. nodeClient follows no redirects.
	base, _, err := cleanDirectURL(n.V1)
	if err != nil {
		fail(409, "invalid saved Ollama connection")
		return
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+endpoint, bytes.NewReader(body))
	if err != nil {
		fail(409, "invalid saved Ollama connection")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if n.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+n.APIKey)
	}
	// Pull streams progress to avoid a response-header timeout during a long
	// download. The browser awaits completion and receives only the result.
	client := *nodeClient
	if pull {
		client.Timeout = timeout
	}
	resp, err := client.Do(req)
	if err != nil {
		fail(502, "Ollama request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fail(502, fmt.Sprintf("Ollama returned HTTP %d", resp.StatusCode))
		return
	}
	var result struct{ Status, Error string }
	dec := json.NewDecoder(io.LimitReader(resp.Body, 32<<20))
	for {
		result = struct{ Status, Error string }{}
		err := dec.Decode(&result)
		if err != nil || result.Error != "" {
			fail(502, "Ollama did not complete the request")
			return
		}
		if !pull || result.Status == "success" {
			break
		}
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// An Ollama name may include a registry. Native pulls without an explicit URL
// still must not be allowed to redirect Ollama to an arbitrary host.
func isPublicOllamaModel(model string) bool {
	first, _, hasNamespace := strings.Cut(model, "/")
	if !hasNamespace || strings.Count(model, "/") == 1 && !strings.Contains(first, ".") {
		return true
	}
	return first == "registry.ollama.ai"
}
