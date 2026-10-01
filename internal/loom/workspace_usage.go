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

func readAgyQuota(ctx context.Context) (QuotaSnapshot, error) {
	q := QuotaSnapshot{RuntimeID: "antigravity", Name: "Antigravity", Source: "agy /usage · compte natif", Windows: []QuotaWindow{}}
	out, err := agyRead(ctx, "-p", "/usage", "--output-format", "json", "--print-timeout", "15s")
	if err != nil {
		return q, err
	}
	var envelope struct {
		Status  string `json:"status"`
		Command struct {
			Name string `json:"name"`
			Data struct {
				Groups []struct {
					Name    string `json:"name"`
					Buckets []struct {
						Name      string   `json:"name"`
						Remaining *float64 `json:"remaining_fraction"`
						Reset     string   `json:"reset_time"`
					} `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if json.Unmarshal(out, &envelope) != nil || envelope.Status != "SUCCESS" || envelope.Command.Name != "usage" || len(envelope.Command.Data.Groups) > 128 {
		return q, errors.New("format des quotas Antigravity non reconnu")
	}
	for _, g := range envelope.Command.Data.Groups {
		if len(g.Buckets) > 32 {
			return q, errors.New("trop de fenêtres de quota")
		}
		for _, b := range g.Buckets {
			w := QuotaWindow{Group: g.Name, Name: b.Name}
			if b.Remaining != nil && *b.Remaining >= 0 && *b.Remaining <= 1 {
				p := *b.Remaining * 100
				w.Remaining = &p
			}
			if date, err := time.Parse(time.RFC3339, b.Reset); err == nil {
				t := date.Unix()
				w.ResetAt = &t
			}
			q.Windows = append(q.Windows, w)
		}
	}
	if len(q.Windows) == 0 {
		return q, errors.New("aucune fenêtre de quota communiquée")
	}
	// AI credits are not reset credits. Keep the two fields distinct.
	if out, err := agyRead(ctx, "-p", "/credits", "--output-format", "json", "--print-timeout", "15s"); err == nil {
		var c struct {
			Status  string `json:"status"`
			Command struct {
				Name string `json:"name"`
				Data struct {
					Remaining *float64 `json:"remaining_credits"`
				} `json:"data"`
			} `json:"command"`
		}
		if json.Unmarshal(out, &c) == nil && c.Status == "SUCCESS" && c.Command.Name == "credits" {
			q.Credits = c.Command.Data.Remaining
		}
	}
	q.FetchedAt = time.Now().Unix()
	return q, nil
}

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

const bkUsagePrices = "workspace_usage_prices"

type UsagePrice struct {
	ChoiceID  string  `json:"choice_id"`
	Input     float64 `json:"input_per_million"`
	Output    float64 `json:"output_per_million"`
	Currency  string  `json:"currency"`
	UpdatedAt int64   `json:"updated_at"`
}
type ModelUsageSummary struct {
	ChoiceID      string       `json:"choice_id"`
	Name          string       `json:"name"`
	Provider      string       `json:"provider"`
	RuntimeID     string       `json:"runtime_id"`
	Turns         int          `json:"turns"`
	Reported      int          `json:"reported_turns"`
	Usage         RuntimeUsage `json:"usage"`
	Price         *UsagePrice  `json:"price"`
	EstimatedCost *float64     `json:"estimated_cost"`
	// Cost declared by the harness itself (ACP usage_update), summed over
	// discussions; nil when no harness reported one.
	ReportedCost *float64 `json:"reported_cost"`
	Currency     string   `json:"currency,omitempty"`
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
	for _, s := range workspaceSessions.list() {
		for i, t := range s.Turns {
			if t.RuntimeID == "llama.cpp" {
				continue
			}
			id := cloudChoiceID(t.ProviderID, t.Model)
			if harnessRuntime(t.RuntimeID) {
				id = t.RuntimeID + ":" + t.Model
			}
			row := rows[id]
			if row == nil {
				row = &ModelUsageSummary{ChoiceID: id, Name: t.Model, Provider: t.ProviderName, RuntimeID: t.RuntimeID}
				rows[id] = row
			}
			row.Turns++
			u := t.Usage
			if u == nil && i == len(s.Turns)-1 {
				u = s.Usage
			}
			if u != nil && u.Input >= 0 && u.Output >= 0 && u.Total >= 0 {
				row.Reported++
				row.Usage.Input += u.Input
				row.Usage.Output += u.Output
				row.Usage.Total += u.Total
				row.Usage.Thinking += u.Thinking
				row.Usage.Cached += u.Cached
			}
		}
	}
	// The harness cost is cumulative per native session: attribute it to the
	// choice of the discussion's last harness turn.
	for _, s := range workspaceSessions.list() {
		cost, _ := s.ACPUsage["cost"].(map[string]any)
		amount, ok := cost["amount"].(float64)
		if !ok || len(s.Turns) == 0 {
			continue
		}
		t := s.Turns[len(s.Turns)-1]
		if row := rows[t.RuntimeID+":"+t.Model]; row != nil && harnessRuntime(t.RuntimeID) {
			v := amount
			if row.ReportedCost != nil {
				v += *row.ReportedCost
			}
			row.ReportedCost = &v
			row.Currency, _ = cost["currency"].(string)
		}
	}
	out := []ModelUsageSummary{}
	for _, r := range rows {
		if harnessRuntime(r.RuntimeID) && r.Turns == 0 {
			continue
		}
		if r.Price != nil && r.Reported > 0 {
			v := (float64(r.Usage.Input)*r.Price.Input + float64(r.Usage.Output)*r.Price.Output) / 1e6
			r.EstimatedCost = &v
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider+out[i].Name < out[j].Provider+out[j].Name })
	return out
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
