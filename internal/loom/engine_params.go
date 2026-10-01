package loom

import (
	_ "embed"
	"net/http"
	"strings"
)

// immutable embedded source, not runtime state

func (llamaCppEngine) Params() []ParamSpec {
	bin := strings.TrimSpace(ReadConfig()["BIN"])
	var flags []LlamaFlag
	if bin != "" {
		flags = llamaFlagsPublic(bin)
	}
	return mergeEngineParams(readCuratedParams(), flags)
}

func handleEngineParams(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		sendJSON(w, 405, map[string]any{"ok": false, "error": "méthode non autorisée"})
		return
	}
	e := localEngine()
	sendJSON(w, 200, map[string]any{"ok": true, "engine_id": e.ID(), "params": e.Params()})
}
