package ajean

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// backend_server.go — surface « Serveur » : llama-server expose déjà /v1
// (OpenAI), /slots et /health. Loom observe, il ne réimplémente pas
// l'inférence. Un llama-server suffit ; le dépôt llama.cpp n'est pas requis.
//
// GET  /api/server  → état (écoute, modèle, slots live, NP, stats, récentes)
// POST /api/server  {np} → slots parallèles (NP), puis redémarrage

const srvHistMax = 40

type llamaSlot struct {
	ID           int
	IDTask       int
	NCtx         int
	IsProcessing bool
	NDecoded     int
	NRemain      int
	PromptN      int
	PredTokS     float64
}

type slotWatch struct {
	slot     llamaSlot
	started  time.Time
	lastN    int
	lastT    time.Time
	emaTokS  float64
	recorded bool
}

type srvRecent struct {
	Slot    int     `json:"slot"`
	Task    int     `json:"task"`
	Tokens  int     `json:"tokens"`
	Prompt  int     `json:"prompt"`
	Ended   int64   `json:"ended"`
	Started int64   `json:"started,omitempty"`
	Ms      int64   `json:"ms"`
	TokS    float64 `json:"toks"`
	State   string  `json:"state"`
}

var (
	srvMu        sync.Mutex
	srvWatch     = map[int]slotWatch{}
	srvRecentQ   []srvRecent
	srvCompleted int
	srvGenTok    int
	srvPromptTok int
	srvTokSSum   float64
	srvSessionAt = time.Now()
)

func resetServerWatch() {
	srvMu.Lock()
	defer srvMu.Unlock()
	srvWatch = map[int]slotWatch{}
	srvRecentQ = nil
	srvCompleted = 0
	srvGenTok = 0
	srvPromptTok = 0
	srvTokSSum = 0
	srvSessionAt = time.Now()
}

func llamaGET(path string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", LLMPort(), path), nil)
	if err != nil {
		return nil, 0, err
	}
	authHeader(req)
	resp, err := healthClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return b, resp.StatusCode, err
}

func anyInt(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case int:
		return t
	case int64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	case bool:
		if t {
			return 1
		}
	}
	return 0
}

func anyFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	case int:
		return float64(t)
	case int64:
		return float64(t)
	}
	return 0
}

func anyBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case json.Number:
		n, _ := t.Int64()
		return n != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "1" || s == "yes"
	}
	return false
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func extractNextToken(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	if arr, ok := v.([]any); ok && len(arr) > 0 {
		if m, ok := arr[0].(map[string]any); ok {
			return m
		}
	}
	return nil
}

func slotFromMap(m map[string]any) llamaSlot {
	nt := extractNextToken(m["next_token"])
	tm := asMap(m["timings"])
	s := llamaSlot{
		ID:           anyInt(m["id"]),
		NCtx:         anyInt(m["n_ctx"]),
		IsProcessing: anyBool(m["is_processing"]),
	}
	if v, ok := m["id_task"]; ok {
		s.IDTask = anyInt(v)
	} else {
		s.IDTask = anyInt(m["task_index"])
	}
	s.NDecoded = anyInt(m["n_decoded"])
	if s.NDecoded == 0 && nt != nil {
		s.NDecoded = anyInt(nt["n_decoded"])
	}
	if s.NDecoded == 0 && tm != nil {
		s.NDecoded = anyInt(tm["predicted_n"])
	}
	if nt != nil {
		s.NRemain = anyInt(nt["n_remain"])
	}
	s.PromptN = anyInt(m["n_prompt_tokens"])
	if s.PromptN == 0 {
		s.PromptN = anyInt(m["n_prompt_tokens_processed"])
	}
	if s.PromptN == 0 && tm != nil {
		s.PromptN = anyInt(tm["prompt_n"])
	}
	if tm != nil {
		s.PredTokS = anyFloat(tm["predicted_per_second"])
	}
	return s
}

func parseLlamaSlots(raw []byte) []llamaSlot {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '{' {
		var wrap map[string]any
		if json.Unmarshal(raw, &wrap) != nil {
			return nil
		}
		if inner, ok := wrap["slots"]; ok {
			b, err := json.Marshal(inner)
			if err != nil {
				return nil
			}
			return parseLlamaSlots(b)
		}
		return nil
	}
	if raw[0] != '[' {
		return nil
	}
	var arr []map[string]any
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	out := make([]llamaSlot, 0, len(arr))
	for _, m := range arr {
		out = append(out, slotFromMap(m))
	}
	return out
}

func roundTokS(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*10) / 10
}

func noteServerSlots(slots []llamaSlot) ([]srvRecent, []map[string]any) {
	now := time.Now()
	srvMu.Lock()
	defer srvMu.Unlock()
	seen := map[int]bool{}
	items := make([]map[string]any, 0, len(slots))
	for _, s := range slots {
		seen[s.ID] = true
		prev, ok := srvWatch[s.ID]
		finished := ok && prev.slot.IDTask != 0 && !prev.recorded && (s.IDTask != prev.slot.IDTask || (!s.IsProcessing && prev.slot.IsProcessing))
		if finished {
			prev.recorded = true
			ms := int64(0)
			if !prev.started.IsZero() {
				ms = now.Sub(prev.started).Milliseconds()
				if ms < 0 {
					ms = 0
				}
			}
			decoded := prev.slot.NDecoded
			if s.NDecoded > decoded {
				decoded = s.NDecoded
			}
			promptN := prev.slot.PromptN
			if s.PromptN > promptN {
				promptN = s.PromptN
			}
			toks := 0.0
			if prev.slot.PredTokS > 0 {
				toks = prev.slot.PredTokS
			} else if prev.emaTokS > 0 {
				toks = prev.emaTokS
			} else if ms > 0 && decoded > 0 {
				toks = float64(decoded) / (float64(ms) / 1000)
			}
			toks = roundTokS(toks)
			startedMs := int64(0)
			if !prev.started.IsZero() {
				startedMs = prev.started.UnixMilli()
			}
			rec := srvRecent{
				Slot:    prev.slot.ID,
				Task:    prev.slot.IDTask,
				Tokens:  decoded,
				Prompt:  promptN,
				Ended:   now.UnixMilli(),
				Started: startedMs,
				Ms:      ms,
				TokS:    toks,
				State:   "terminé",
			}
			srvRecentQ = append(srvRecentQ, rec)
			if len(srvRecentQ) > srvHistMax {
				srvRecentQ = srvRecentQ[len(srvRecentQ)-srvHistMax:]
			}
			srvCompleted++
			srvGenTok += decoded
			srvPromptTok += promptN
			if toks > 0 {
				srvTokSSum += toks
			}
		}
		newTask := !ok || finished || (s.IDTask != 0 && s.IDTask != prev.slot.IDTask)
		startedBusy := s.IsProcessing && (!ok || !prev.slot.IsProcessing || newTask)
		if startedBusy {
			prev.started = now
			prev.lastN = s.NDecoded
			prev.lastT = now
			prev.emaTokS = 0
			prev.recorded = false
		} else if ok && s.IsProcessing {
			dt := now.Sub(prev.lastT).Seconds()
			if dt > 0.04 && s.NDecoded >= prev.lastN {
				inst := float64(s.NDecoded-prev.lastN) / dt
				if prev.emaTokS <= 0 {
					prev.emaTokS = inst
				} else {
					prev.emaTokS = 0.45*inst + 0.55*prev.emaTokS
				}
			}
			prev.lastN = s.NDecoded
			prev.lastT = now
		}
		prev.slot = s
		if !s.IsProcessing {
			prev.emaTokS = 0
		}
		srvWatch[s.ID] = prev

		toks := 0.0
		if s.IsProcessing {
			if s.PredTokS > 0 {
				toks = s.PredTokS
			} else {
				toks = prev.emaTokS
			}
		}
		elapsed := int64(0)
		if s.IsProcessing && !prev.started.IsZero() {
			elapsed = now.Sub(prev.started).Milliseconds()
		}
		items = append(items, map[string]any{
			"id":         s.ID,
			"task":       s.IDTask,
			"busy":       s.IsProcessing,
			"tokens":     s.NDecoded,
			"prompt":     s.PromptN,
			"ctx":        s.NCtx,
			"remaining":  s.NRemain,
			"toks":       roundTokS(toks),
			"elapsed_ms": elapsed,
		})
	}
	for id := range srvWatch {
		if !seen[id] {
			delete(srvWatch, id)
		}
	}
	out := make([]srvRecent, len(srvRecentQ))
	copy(out, srvRecentQ)
	return out, items
}

func serverStats(busy int, liveTokS float64) map[string]any {
	srvMu.Lock()
	defer srvMu.Unlock()
	avg := 0.0
	if srvCompleted > 0 && srvTokSSum > 0 {
		avg = srvTokSSum / float64(srvCompleted)
	}
	return map[string]any{
		"inflight":  busy,
		"completed": srvCompleted,
		"tokens":    srvGenTok,
		"prompt":    srvPromptTok,
		"avg_toks":  roundTokS(avg),
		"live_toks": roundTokS(liveTokS),
		"since":     srvSessionAt.UnixMilli(),
	}
}

func serverNP() int {
	if v := oaiRuntimeGet("NP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			return n
		}
	}
	n, _ := strconv.Atoi(strings.TrimSpace(ReadConfig()["NP"]))
	if n < 1 {
		return 4
	}
	return n
}

func setServerNP(n int) error {
	if n < 1 {
		n = 1
	}
	if n > 32 {
		n = 32
	}
	clearOAIRuntime()
	return SetConfigKey("NP", strconv.Itoa(n))
}

func handleServer(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req struct {
			NP *int `json:"np"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if req.NP != nil {
			if err := setServerNP(*req.NP); err != nil {
				sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if serviceIsActive() {
				go func() {
					if err := restartLlamaEngine(); err != nil {
						fmt.Printf("%s redémarrage après NP: %v\n", red("[ERREUR]"), err)
					}
				}()
			}
		}
	}

	cfg := ReadConfig()
	net := networkStatus()
	port := LLMPort()
	loop := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	url := loop
	if net.Exposed {
		url = net.URL
	}
	model := strings.TrimSpace(cfg["MODEL"])
	if ownedLlamaManaged() && !ownedLlamaRunning() {
		model = ""
	}
	modelName := ""
	if model != "" {
		modelName = filepath.Base(model)
	}
	presetID := getStr(bkState, "active_preset")
	if model == "" {
		presetID = ""
	}
	presetName := ""
	if presetID != "" {
		if body, err := ReadPreset(presetID); err == nil {
			presetName = presetDisplayName(body, presetID)
		}
	}
	key := readAPIKey()

	out := map[string]any{
		"ok":           true,
		"running":      serviceIsActive(),
		"health":       false,
		"port":         port,
		"host":         net.Host,
		"lan":          net.Exposed,
		"url":          url,
		"url_local":    loop,
		"np":           serverNP(),
		"model":        model,
		"model_name":   modelName,
		"preset_id":    presetID,
		"preset_name":  presetName,
		"key_set":      key != "",
		"key_required": apiKeyRequired(),
		"slots":        map[string]any{"n": 0, "busy": 0, "items": []any{}, "ok": false},
		"recent":       []srvRecent{},
		"stats":        serverStats(0, 0),
	}
	if !serviceIsActive() {
		sendJSON(w, 200, out)
		return
	}
	healthy := healthCheck()
	out["health"] = healthy
	if !healthy {
		sendJSON(w, 200, out)
		return
	}
	raw, code, err := llamaGET("/slots")
	if err != nil || code != 200 {
		out["slots"] = map[string]any{"n": 0, "busy": 0, "items": []any{}, "ok": false}
		if code > 0 {
			out["slots_http"] = code
		}
		sendJSON(w, 200, out)
		return
	}
	slots := parseLlamaSlots(raw)
	recent, items := noteServerSlots(slots)
	busy := 0
	liveTokS := 0.0
	for _, it := range items {
		if b, _ := it["busy"].(bool); b {
			busy++
			if v, ok := it["toks"].(float64); ok {
				liveTokS += v
			}
		}
	}
	out["slots"] = map[string]any{"n": len(slots), "busy": busy, "items": items, "ok": true}
	out["recent"] = recent
	out["stats"] = serverStats(busy, liveTokS)
	if n := len(slots); n > 0 && serverNP() != n {
		out["np_live"] = n
	}
	sendJSON(w, 200, out)
}
