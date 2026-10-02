package loom

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"
)

type QuotaWindow struct {
	Group     string   `json:"group"`
	Name      string   `json:"name"`
	Remaining *float64 `json:"remaining_percent"`
	ResetAt   *int64   `json:"reset_at"`
}
type QuotaSnapshot struct {
	RuntimeID    string             `json:"runtime_id"`
	Name         string             `json:"name"`
	Source       string             `json:"source"`
	FetchedAt    int64              `json:"fetched_at"`
	Windows      []QuotaWindow      `json:"windows"`
	ResetCredits *int               `json:"reset_credits"`
	Credits      *float64           `json:"credits"`
	Error        string             `json:"error,omitempty"`
	Note         string             `json:"note,omitempty"`
	ResetDetails []QuotaResetCredit `json:"reset_details,omitempty"`
}
type QuotaResetCredit struct {
	Title     *string `json:"title"`
	ExpiresAt *int64  `json:"expiresAt"`
	Status    string  `json:"status"`
}

var quotaCache = struct {
	sync.Mutex
	items    map[string]QuotaSnapshot
	attempts map[string]time.Time
}{items: map[string]QuotaSnapshot{}, attempts: map[string]time.Time{}}

// Read-only native JSON-RPC. Never login/logout, start a turn, send a nudge or
// redeem a credit. Separate process: does not attach to the desktop's thread.
func readCodexQuota(ctx context.Context) (QuotaSnapshot, error) {
	q := QuotaSnapshot{RuntimeID: "codex", Name: "Codex", Source: "Codex app-server · compte natif", Windows: []QuotaWindow{}}
	path, err := exec.LookPath("codex")
	if err != nil {
		return q, errors.New("CLI Codex absent du PATH de Loom")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "loom-codex-quota-")
	if err != nil {
		return q, errors.New("dossier temporaire indisponible")
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, path, "app-server")
	cmd.Dir = dir
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return q, errors.New("lecture Codex indisponible")
	}
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return q, errors.New("lecture Codex indisponible")
	}
	stopRead := context.AfterFunc(ctx, func() { _ = out.Close() })
	defer stopRead()
	defer func() { _ = in.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(io.LimitReader(out, 2<<20))
	scanner.Buffer(make([]byte, 4096), 512<<10)
	encoder := json.NewEncoder(in)
	if encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "loom", "title": "Loom quotas", "version": "0.1"}}}) != nil {
		return q, errors.New("initialisation Codex impossible")
	}
	if _, err = readRPCResult(scanner, 1); err != nil {
		return q, err
	}
	if encoder.Encode(map[string]any{"method": "initialized"}) != nil || encoder.Encode(map[string]any{"id": 2, "method": "account/rateLimits/read"}) != nil {
		return q, errors.New("lecture Codex impossible")
	}
	data, err := readRPCResult(scanner, 2)
	if err != nil {
		return q, err
	}
	return parseCodexQuota(data)
}
func readRPCResult(scanner *bufio.Scanner, id int) (json.RawMessage, error) {
	for scanner.Scan() {
		var msg struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			return nil, errors.New("protocole Codex non reconnu")
		}
		if msg.ID != nil && *msg.ID == id {
			if len(msg.Error) > 0 && string(msg.Error) != "null" {
				return nil, errors.New("quotas Codex indisponibles pour cette authentification/version")
			}
			return msg.Result, nil
		}
	}
	return nil, errors.New("lecture Codex interrompue")
}
func parseCodexQuota(data []byte) (QuotaSnapshot, error) {
	q := QuotaSnapshot{RuntimeID: "codex", Name: "Codex", Source: "Codex app-server · compte natif", Windows: []QuotaWindow{}}
	type window struct {
		Used    *float64 `json:"usedPercent"`
		Minutes *int     `json:"windowDurationMins"`
		Reset   *int64   `json:"resetsAt"`
	}
	type bucket struct {
		Name      *string `json:"limitName"`
		Primary   *window `json:"primary"`
		Secondary *window `json:"secondary"`
	}
	var r struct {
		RateLimits *bucket           `json:"rateLimits"`
		ByID       map[string]bucket `json:"rateLimitsByLimitId"`
		Reset      *struct {
			Count   *int               `json:"availableCount"`
			Details []QuotaResetCredit `json:"credits"`
		} `json:"rateLimitResetCredits"`
	}
	if json.Unmarshal(data, &r) != nil || len(r.ByID) > 128 {
		return q, errors.New("format des quotas Codex non reconnu")
	}
	if len(r.ByID) == 0 && r.RateLimits != nil {
		r.ByID = map[string]bucket{"codex": *r.RateLimits}
	}
	keys := []string{}
	for key := range r.ByID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		b := r.ByID[key]
		group := key
		if b.Name != nil {
			group = *b.Name
		}
		for i, w := range []*window{b.Primary, b.Secondary} {
			if w == nil {
				continue
			}
			name := []string{"Fenêtre principale", "Fenêtre secondaire"}[i]
			if w.Minutes != nil {
				switch *w.Minutes {
				case 300:
					name = "5 heures"
				case 10080:
					name = "7 jours"
				default:
					name = fmt.Sprintf("%d minutes", *w.Minutes)
				}
			}
			v := QuotaWindow{Group: group, Name: name, ResetAt: w.Reset}
			if w.Used != nil && *w.Used >= 0 && *w.Used <= 100 {
				p := 100 - *w.Used
				v.Remaining = &p
			}
			q.Windows = append(q.Windows, v)
		}
	}
	if r.Reset != nil {
		if r.Reset.Count != nil && *r.Reset.Count >= 0 {
			q.ResetCredits = r.Reset.Count
		}
		if len(r.Reset.Details) <= 128 {
			q.ResetDetails = r.Reset.Details
		}
	}
	if len(q.Windows) == 0 {
		return q, errors.New("aucune fenêtre de quota Codex communiquée")
	}
	q.FetchedAt = time.Now().Unix()
	return q, nil
}

func harnessRuntime(id string) bool {
	a, ok := registeredRuntimes.lookup(id)
	return ok && a.Descriptor().Kind == "harness"
}

func usageSummaries() []ModelUsageSummary {
	rows := map[string]*ModelUsageSummary{}
	for _, c := range modelCatalog(workspaceSessions.providers()) {
		if c.Kind == "local" {
			continue
		}
		row := &ModelUsageSummary{ChoiceID: c.ID, Name: c.Model, Provider: c.ProviderName, RuntimeID: c.RuntimeID}
		var price UsagePrice
		if getStoreJSON(bkUsagePrices, c.ID, &price) {
			row.Price = &price
		}
		rows[c.ID] = row
	}
	accumulateDiscussionUsage(rows, workspaceSessions.list())
	accumulateDiscussionCosts(rows, workspaceSessions.list())
	return discussionUsageSummaries(rows)
}

func handleUsage(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	if !usageVaultAccess(w) {
		return
	}
	quotaCache.Lock()
	quotas := []QuotaSnapshot{}
	for _, d := range runtimeCatalog() {
		if d.Kind != "harness" {
			continue
		}
		id := d.ID
		q, ok := quotaCache.items[id]
		if !ok {
			q = QuotaSnapshot{RuntimeID: id, Name: d.Name, Windows: []QuotaWindow{}}
		}
		quotas = append(quotas, q)
	}
	quotaCache.Unlock()
	if !usageVaultAccess(w) {
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "quotas": quotas, "models": usageSummaries(), "scope": "Loom uniquement · tokens communiqués par les runtimes"})
}
func handleUsageRefresh(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	if !usageVaultAccess(w) {
		return
	}
	var req struct {
		RuntimeID string `json:"runtime_id"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	adapter, ok := registeredRuntimes.lookup(req.RuntimeID)
	if !ok {
		// Preserve this legacy endpoint's status for an unsupported body ID.
		sendJSON(w, 400, map[string]any{"ok": false, "error": "lecture des quotas non disponible pour ce harness"})
		return
	}
	refreshRuntimeQuota(w, r.Context(), adapter)
}
func handleUsagePrice(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		ChoiceID string   `json:"choice_id"`
		Input    *float64 `json:"input_per_million"`
		Output   *float64 `json:"output_per_million"`
		Currency string   `json:"currency"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if req.Input == nil || req.Output == nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "renseignez les deux tarifs ; une valeur absente n’est pas zéro"})
		return
	}
	p := UsagePrice{ChoiceID: req.ChoiceID, Input: *req.Input, Output: *req.Output, Currency: req.Currency}
	valid := false
	for _, c := range modelCatalog(workspaceSessions.providers()) {
		if c.ID == p.ChoiceID && c.Kind == "cloud" {
			valid = true
		}
	}
	if !valid || (p.Currency != "USD" && p.Currency != "EUR") || math.IsNaN(p.Input) || math.IsInf(p.Input, 0) || p.Input < 0 || p.Input > 10000 || math.IsNaN(p.Output) || math.IsInf(p.Output, 0) || p.Output < 0 || p.Output > 10000 {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "modèle cloud et tarifs valides requis (USD ou EUR)"})
		return
	}
	p.UpdatedAt = time.Now().Unix()
	if putStoreJSON(bkUsagePrices, p.ChoiceID, p) != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "enregistrement impossible"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// Legacy alias: consent, persistence and response share the generic handler.
func handleAgyConnect(w http.ResponseWriter, r *http.Request) {
	r.SetPathValue("id", "antigravity")
	handleRuntimeConnect(w, r)
}

func usageVaultAccess(w http.ResponseWriter) bool {
	if memEncActive() && !memUnlocked() {
		sendJSON(w, 423, map[string]any{"ok": false, "error": "déverrouillez le coffre Loom pour consulter les comptes"})
		return false
	}
	return true
}
