package loom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
)

func marketFixture(t *testing.T, catalog *string) {
	t.Helper()
	testHome(t)
	or := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(*catalog)) }))
	aa := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"intelligence_index_version":5,"pagination":{"has_more":false},"data":[
			{"slug":"b","name":"B","model_creator":{"name":"Lab"},"evaluations":{"artificial_analysis_intelligence_index":40},"performance_v3":{"mean_output_tokens_per_second":90},"pricing":{"price_1m_input_tokens":1}},
			{"slug":"a","name":"A","model_creator":{"name":"Lab"},"evaluations":{"artificial_analysis_intelligence_index":70}}]}`))
	}))
	oldOR, oldAA := marketOpenRouterURL, marketAAURL
	marketOpenRouterURL, marketAAURL = or.URL, aa.URL
	market = &marketCache{}
	t.Cleanup(func() { or.Close(); aa.Close(); marketOpenRouterURL, marketAAURL = oldOR, oldAA; market = &marketCache{} })
}

// The catalogue drops routers and variants and sorts newest first; the first
// refresh announces nothing, a later new model is announced.
func TestMarketCatalogAndNewModels(t *testing.T) {
	now := time.Now().Unix()
	catalog := `{"data":[
		{"id":"lab/old","name":"Old","created":` + i64s(now-90*86400) + `,"pricing":{"prompt":"0.000001","completion":"0.000002"}},
		{"id":"lab/new","name":"New","created":` + i64s(now-86400) + `,"hugging_face_id":"lab/new","pricing":{"prompt":"0","completion":"0"}},
		{"id":"openrouter/auto","name":"Auto","created":` + i64s(now) + `},
		{"id":"lab/new:free","name":"New (free)","created":` + i64s(now) + `}]}`
	marketFixture(t, &catalog)
	ch, unsubscribe := workspaceSessions.events.Subscribe()
	defer unsubscribe()
	refreshMarket(context.Background(), true)
	if len(market.catalog) != 2 || market.catalog[0].ID != "lab/new" || !market.catalog[0].Free || market.catalog[1].InputPrice != 1 {
		t.Fatalf("catalog %+v", market.catalog)
	}
	catalog = strings.Replace(catalog, `{"id":"openrouter/auto"`, `{"id":"lab/brand-new","name":"Brand new","created":`+i64s(now)+`},{"id":"openrouter/auto"`, 1)
	refreshMarket(context.Background(), true)
	got := []string{}
	deadline := time.After(time.Second)
	for len(got) < 1 {
		select {
		case e := <-ch:
			if e.Type == events.ModelNew {
				got = append(got, e.Title)
			}
		case <-deadline:
			t.Fatalf("no announcement, got %v", got)
		}
	}
	if len(got) != 1 || !strings.Contains(got[0], "Brand new") {
		t.Fatalf("announced %v", got)
	}
}

// A refused key is not stored; a good one is, and the leaderboard is sorted
// by intelligence index.
func TestMarketBenchmarkKey(t *testing.T) {
	catalog := `{"data":[]}`
	marketFixture(t, &catalog)
	post := func(body string) int {
		w := httptest.NewRecorder()
		handleMarketSettings(w, httptest.NewRequest(http.MethodPost, "/api/market/settings", strings.NewReader(body)))
		return w.Code
	}
	if post(`{"key":"bad"}`) != 400 {
		t.Fatal("refused key accepted")
	}
	if key, _ := readProviderSecret(marketSecretID); key != "" {
		t.Fatal("refused key stored")
	}
	if post(`{"key":"good"}`) != 200 {
		t.Fatal("good key rejected")
	}
	w := httptest.NewRecorder()
	handleMarket(w, httptest.NewRequest(http.MethodGet, "/api/market", nil))
	var out struct {
		HasKey bool         `json:"has_key"`
		Bench  []benchModel `json:"bench"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if !out.HasKey || len(out.Bench) != 2 || out.Bench[0].Slug != "a" {
		t.Fatalf("bench %+v", out)
	}
}

func i64s(n int64) string { b, _ := json.Marshal(n); return string(b) }
