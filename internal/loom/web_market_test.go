package loom

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	f, _ := z.Create("benchmark_data/epoch_capabilities_index/eci_scores.csv")
	_, _ = f.Write([]byte("Model,Display name,eci,eci_ci_low,eci_ci_high,date,Organization,Country (of organization),Model accessibility,Accessibility group,model_versions\n" +
		"B,B,140.5,138,143,2026-01-02,Lab,X,Open weights (unrestricted),Open weights,\n" +
		"A,A Pro,170.2,168,172,2026-11-01,Lab,X,API access,Closed weights,\n" +
		"C,,not-a-number,,,2026-01-01,Lab,X,API access,Closed weights,\n"))
	_ = z.Close()
	old := marketTransport
	marketTransport = codemanTestTransport(func(req *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		switch req.URL.String() {
		case openRouterModelsURL:
			w.WriteString(*catalog)
		case epochArchiveURL:
			w.Write(buf.Bytes())
		default:
			return nil, fmt.Errorf("unexpected market URL %s", req.URL)
		}
		return w.Result(), nil
	})
	market = &marketCache{}
	t.Cleanup(func() {
		marketTransport = old
		market = &marketCache{}
	})
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

// The leaderboard comes from Epoch AI's public archive, without a key,
// sorted by capabilities index; unreadable rows are skipped.
func TestMarketEpochIndex(t *testing.T) {
	catalog := `{"data":[]}`
	marketFixture(t, &catalog)
	w := httptest.NewRecorder()
	handleMarket(w, httptest.NewRequest(http.MethodGet, "/api/market", nil))
	var out struct {
		Bench      []benchModel `json:"bench"`
		BenchError string       `json:"bench_error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.BenchError != "" || len(out.Bench) != 2 || out.Bench[0].Name != "A Pro" || out.Bench[1].Open != true || *out.Bench[0].Low != 168 {
		t.Fatalf("bench %+v", out)
	}
}

func i64s(n int64) string { b, _ := json.Marshal(n); return string(b) }
