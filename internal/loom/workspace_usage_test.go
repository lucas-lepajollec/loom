package loom

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"strings"
	"testing"
)

func TestUsageTotalsRemainLoomScopedAndIncomplete(t *testing.T) {
	testHome(t)
	old := workspaceSessions
	workspaceSessions = newRuntimeSessions()
	t.Cleanup(func() { workspaceSessions = old })
	p, err := workspaceSessions.saveProvider(CloudProvider{Name: "Fixture", Endpoint: "https://example.com/v1", Model: "m"}, "")
	if err != nil {
		t.Fatal(err)
	}
	id := cloudChoiceID(p.ID, "m")
	if putStoreJSON(bkUsagePrices, id, UsagePrice{ChoiceID: id, Input: 1, Output: 2, Currency: "USD"}) != nil {
		t.Fatal("write")
	}
	s := RuntimeSession{ID: "fixture", Status: "complete", Turns: []RuntimeTurnRecord{{RuntimeID: "openai-compatible", ProviderID: p.ID, ProviderName: p.Name, Model: "m", Usage: &RuntimeUsage{Input: 100, Output: 50, Total: 150}}, {RuntimeID: "openai-compatible", ProviderID: p.ID, ProviderName: p.Name, Model: "m"}}}
	if putStoreJSON(bkRuntimeSessions, s.ID, s) != nil {
		t.Fatal("write")
	}
	// Installed ACP harnesses add their own zero-turn rows; only the cloud row is under test.
	var row *ModelUsageSummary
	all := usageSummaries()
	for i := range all {
		if all[i].ChoiceID == id {
			row = &all[i]
		} else if all[i].Turns != 0 {
			t.Fatalf("unexpected usage outside the fixture: %+v", all[i])
		}
	}
	if row == nil || row.Turns != 2 || row.Reported != 1 || row.Usage.Total != 150 || row.EstimatedCost == nil || *row.EstimatedCost != 0.0002 {
		t.Fatalf("%+v", all)
	}
}
func TestUsageAPIReadOnlyAndPriceValidation(t *testing.T) {
	testHome(t)
	w := httptest.NewRecorder()
	handleUsage(w, httptest.NewRequest(http.MethodPost, "/api/usage", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	handleUsage(w, httptest.NewRequest(http.MethodGet, "/api/usage", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing private boundary")
	}
	var result map[string]any
	if json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatal("json")
	}
	for _, body := range []string{`{"choice_id":"unknown","input_per_million":1,"output_per_million":1,"currency":"USD"}`, `{"choice_id":"unknown","input_per_million":-1,"output_per_million":1,"currency":"USD"}`} {
		r := httptest.NewRequest(http.MethodPost, "/api/usage/price", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		handleUsagePrice(w, r)
		if w.Code != 400 {
			t.Fatal("invalid price accepted")
		}
	}
}
