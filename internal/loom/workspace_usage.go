package loom

import (
	"math"
	"net/http"

	"time"
)

func usageSummaries() []ModelUsageSummary {
	rows := map[string]*ModelUsageSummary{}
	for _, c := range modelCatalog(workspaceSessions.providers()) {
		if c.Kind == "local" {
			continue
		}
		row := &ModelUsageSummary{ChoiceID: c.ID, Name: c.Model, Provider: c.ProviderName, RuntimeID: c.RuntimeID, Kind: c.Kind}
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
	sendJSON(w, 200, map[string]any{"ok": true, "quotas": []any{}, "models": usageSummaries(), "scope": "Loom only · tokens reported by runtimes"})
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
		sendJSON(w, 400, map[string]any{"ok": false, "error": "enter both prices; a missing value is not zero"})
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
		sendJSON(w, 400, map[string]any{"ok": false, "error": "cloud model and valid prices required (USD or EUR)"})
		return
	}
	p.UpdatedAt = time.Now().Unix()
	if putStoreJSON(bkUsagePrices, p.ChoiceID, p) != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "could not save"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

func usageVaultAccess(w http.ResponseWriter) bool {
	if memEncActive() && !memUnlocked() {
		sendJSON(w, 423, map[string]any{"ok": false, "error": "unlock the Loom vault to view accounts"})
		return false
	}
	return true
}
