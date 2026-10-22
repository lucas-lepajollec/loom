package loom

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Suivi des quotas d'abonnement relevés par Codeman. Codeman ne garde que la
// dernière mesure (Claude : fenêtres 5 h et 7 j lues dans la statusline ;
// Codex : ses propres limites) ; Loom en relève une toutes les cinq minutes
// tant que Codeman est configuré, pour tracer leur évolution. Seuls des
// pourcentages et des heures de remise à zéro sont gardés.

const (
	codemanQuotaEvery   = 5 * time.Minute
	codemanQuotaKeep    = 14 * 24 * time.Hour
	codemanQuotaMax     = 6000
	codemanQuotaRefresh = 30 * time.Second
	codemanQuotaIdleGap = 30 * time.Minute
)

type quotaWindow struct {
	Used    float64 `json:"used"`
	ResetAt int64   `json:"reset_at,omitempty"`
}

type quotaSample struct {
	At     int64                  `json:"at"`
	Claude map[string]quotaWindow `json:"claude,omitempty"`
	Codex  map[string]quotaWindow `json:"codex,omitempty"`
}

func (s quotaSample) empty() bool { return len(s.Claude) == 0 && len(s.Codex) == 0 }

// Même mesure : mêmes fenêtres, mêmes pourcentages, même remise à zéro.
func (s quotaSample) same(o quotaSample) bool {
	eq := func(a, b map[string]quotaWindow) bool {
		if len(a) != len(b) {
			return false
		}
		for k, v := range a {
			if w, ok := b[k]; !ok || w != v {
				return false
			}
		}
		return true
	}
	return eq(s.Claude, o.Claude) && eq(s.Codex, o.Codex)
}

var codemanQuotas struct {
	sync.Mutex
	loaded  bool
	samples []quotaSample
	latest  quotaSample
	probed  time.Time
}

func codemanQuotaPath() string { return filepath.Join(LoomHome(), "codeman-quotas.json") }

// Fenêtres d'une mesure Codeman ({fiveHour,sevenDay:{usedPercentage,resetAt}}).
func quotaWindows(raw map[string]any) map[string]quotaWindow {
	out := map[string]quotaWindow{}
	for key, name := range map[string]string{"fiveHour": "5h", "sevenDay": "7d"} {
		w, _ := raw[key].(map[string]any)
		used, ok := w["usedPercentage"].(float64)
		if !ok || math.IsNaN(used) || math.IsInf(used, 0) {
			continue
		}
		q := quotaWindow{Used: math.Max(0, math.Min(100, used))}
		if r, ok := w["resetAt"].(float64); ok && r > 0 {
			q.ResetAt = int64(r)
		}
		out[name] = q
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parsePlanUsage(raw map[string]any) quotaSample {
	s := quotaSample{Claude: quotaWindows(raw)}
	if codex, ok := raw["codex"].(map[string]any); ok {
		s.Codex = quotaWindows(codex)
	}
	return s
}

func loadQuotaHistoryLocked() {
	if codemanQuotas.loaded {
		return
	}
	codemanQuotas.loaded = true
	b, err := os.ReadFile(codemanQuotaPath())
	if err != nil {
		return
	}
	var saved []quotaSample
	if json.Unmarshal(b, &saved) == nil {
		codemanQuotas.samples = saved
		if n := len(saved); n > 0 {
			codemanQuotas.latest = saved[n-1]
		}
	}
}

// Ajoute une mesure si elle change, ou au moins toutes les 30 minutes pour
// que la courbe reste lisible pendant une période calme.
func recordQuotaSample(s quotaSample, now time.Time) {
	if s.empty() {
		return
	}
	s.At = now.UnixMilli()
	codemanQuotas.Lock()
	defer codemanQuotas.Unlock()
	loadQuotaHistoryLocked()
	codemanQuotas.latest = s
	list := codemanQuotas.samples
	if n := len(list); n > 0 && list[n-1].same(s) && now.Sub(time.UnixMilli(list[n-1].At)) < codemanQuotaIdleGap {
		return
	}
	list = append(list, s)
	cut := now.Add(-codemanQuotaKeep).UnixMilli()
	start := 0
	for start < len(list) && (list[start].At < cut || len(list)-start > codemanQuotaMax) {
		start++
	}
	codemanQuotas.samples = append([]quotaSample(nil), list[start:]...)
	if b, err := json.Marshal(codemanQuotas.samples); err == nil {
		if os.MkdirAll(LoomHome(), 0700) == nil {
			_ = memWriteFileAtomic(codemanQuotaPath(), b, 0600)
		}
	}
}

// Lit la dernière mesure de Codeman (champ planUsage de /api/v1/status).
func probeCodemanQuota(ctx context.Context) (quotaSample, bool) {
	if _, err := os.Stat(codemanConfigPath()); err != nil {
		return quotaSample{}, false
	}
	c := loadCodemanConfig()
	ctx, cancel := context.WithTimeout(ctx, codemanProbeDeadline)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/v1/status", nil)
	if h := codemanAuthHeader(c); h != "" {
		req.Header.Set("Authorization", h)
	}
	resp, err := codemanHTTP(c.URL).Do(req)
	if err != nil {
		return quotaSample{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return quotaSample{}, false
	}
	var st struct {
		Data struct {
			PlanUsage map[string]any `json:"planUsage"`
		} `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&st) != nil {
		return quotaSample{}, false
	}
	return parsePlanUsage(st.Data.PlanUsage), true
}

func sampleCodemanQuota(ctx context.Context) {
	if s, ok := probeCodemanQuota(ctx); ok {
		recordQuotaSample(s, time.Now())
	}
	codemanQuotas.Lock()
	codemanQuotas.probed = time.Now()
	codemanQuotas.Unlock()
}

func codemanQuotaLoop(ctx context.Context) {
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			sampleCodemanQuota(ctx)
			timer.Reset(codemanQuotaEvery)
		}
	}
}

func handleCodemanQuotas(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	codemanQuotas.Lock()
	stale := time.Since(codemanQuotas.probed) > codemanQuotaRefresh
	codemanQuotas.Unlock()
	if stale {
		sampleCodemanQuota(r.Context())
	}
	codemanQuotas.Lock()
	defer codemanQuotas.Unlock()
	loadQuotaHistoryLocked()
	samples := codemanQuotas.samples
	if samples == nil {
		samples = []quotaSample{}
	}
	var latest any
	if !codemanQuotas.latest.empty() {
		latest = codemanQuotas.latest
	}
	sendJSON(w, 200, map[string]any{"ok": true, "latest": latest, "samples": samples, "every_ms": codemanQuotaEvery.Milliseconds()})
}
