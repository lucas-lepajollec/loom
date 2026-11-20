package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
)

// Model watch: what is out there and how good it is. Two public sources:
//   - OpenRouter's catalogue (no key): every hosted model with its release
//     time, context, prices and, for open weights, its Hugging Face id.
//   - Artificial Analysis' free API (the user's own key, 100 requests a day):
//     intelligence index, output speed, time to first token and prices. Its
//     data must be credited to Artificial Analysis wherever it is shown.
// Both are cached, so opening the page never spends the daily quota.

const (
	openRouterModelsURL  = "https://openrouter.ai/api/v1/models"
	aaModelsURL          = "https://artificialanalysis.ai/api/v2/language/models/free"
	marketCatalogFor     = 6 * time.Hour
	marketBenchFor       = 12 * time.Hour
	marketSecretID       = "market-artificialanalysis"
	marketSeenKey        = "market_seen_models"
	marketWatchKey       = "market_watch"
	marketNewWindowDays  = 30
	marketMaxCatalogSize = 4000
)

// Tests point these at local servers.
var (
	marketOpenRouterURL = openRouterModelsURL
	marketAAURL         = aaModelsURL
)

type marketModel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Creator     string  `json:"creator"`
	Created     int64   `json:"created"` // unix seconds
	Context     int     `json:"context,omitempty"`
	InputPrice  float64 `json:"input_price"`  // USD per 1M tokens, -1 unknown
	OutputPrice float64 `json:"output_price"` // USD per 1M tokens, -1 unknown
	HFID        string  `json:"hf_id,omitempty"`
	Free        bool    `json:"free,omitempty"`
	Modalities  string  `json:"modalities,omitempty"`
	Description string  `json:"description,omitempty"`
}

type benchModel struct {
	Slug         string   `json:"slug"`
	Name         string   `json:"name"`
	Creator      string   `json:"creator"`
	Released     string   `json:"released,omitempty"`
	Intelligence *float64 `json:"intelligence,omitempty"`
	Speed        *float64 `json:"speed,omitempty"` // output tokens per second
	FirstToken   *float64 `json:"first_token,omitempty"`
	InputPrice   *float64 `json:"input_price,omitempty"`
	OutputPrice  *float64 `json:"output_price,omitempty"`
}

type marketCache struct {
	mu         sync.Mutex
	catalog    []marketModel
	catalogAt  time.Time
	catalogErr string
	bench      []benchModel
	benchAt    time.Time
	benchErr   string
	benchIndex int
	refreshing bool
}

var market = &marketCache{}

type marketWatch struct {
	Notify   bool `json:"notify"`
	OpenOnly bool `json:"open_only"`
}

func loadMarketWatch() marketWatch {
	w := marketWatch{Notify: true}
	getStoreJSON(bkState, marketWatchKey, &w)
	return w
}

func marketHTTP(ctx context.Context, url string, header map[string]string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "loom-model-watch")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("the key was refused")
	case resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("the daily quota of this key is used up; try again tomorrow")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("the source returned %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(into)
}

func perMillion(s string) float64 {
	var v float64
	if _, err := fmt.Sscan(strings.TrimSpace(s), &v); err != nil || v < 0 {
		return -1
	}
	return v * 1e6
}

func fetchOpenRouter(ctx context.Context) ([]marketModel, error) {
	var body struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Created       int64  `json:"created"`
			ContextLength int    `json:"context_length"`
			HuggingFaceID string `json:"hugging_face_id"`
			Description   string `json:"description"`
			Architecture  struct {
				Modality string `json:"modality"`
			} `json:"architecture"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := marketHTTP(ctx, marketOpenRouterURL, nil, &body); err != nil {
		return nil, err
	}
	out := make([]marketModel, 0, len(body.Data))
	for _, m := range body.Data {
		// Routers and per-provider variants (":free", ":batch"…) are not models.
		if m.ID == "" || strings.HasPrefix(m.ID, "openrouter/") || strings.Contains(m.ID, ":") {
			continue
		}
		creator, _, _ := strings.Cut(m.ID, "/")
		desc := m.Description
		if len(desc) > 400 {
			desc = desc[:400]
		}
		in, outP := perMillion(m.Pricing.Prompt), perMillion(m.Pricing.Completion)
		out = append(out, marketModel{ID: m.ID, Name: m.Name, Creator: creator, Created: m.Created, Context: m.ContextLength,
			InputPrice: in, OutputPrice: outP, HFID: m.HuggingFaceID, Free: in == 0 && outP == 0,
			Modalities: m.Architecture.Modality, Description: desc})
		if len(out) >= marketMaxCatalogSize {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

func fetchArtificialAnalysis(ctx context.Context, key string) ([]benchModel, int, error) {
	var out []benchModel
	version := 0
	for page := 1; page <= 3; page++ {
		var body struct {
			Version    int `json:"intelligence_index_version"`
			Pagination struct {
				HasMore bool `json:"has_more"`
			} `json:"pagination"`
			Data []struct {
				Slug         string `json:"slug"`
				Name         string `json:"name"`
				ReleaseDate  string `json:"release_date"`
				ModelCreator struct {
					Name string `json:"name"`
				} `json:"model_creator"`
				Evaluations struct {
					Intelligence *float64 `json:"artificial_analysis_intelligence_index"`
				} `json:"evaluations"`
				Performance struct {
					Speed      *float64 `json:"mean_output_tokens_per_second"`
					FirstToken *float64 `json:"mean_time_to_first_token_seconds"`
				} `json:"performance_v3"`
				Pricing struct {
					Input  *float64 `json:"price_1m_input_tokens"`
					Output *float64 `json:"price_1m_output_tokens"`
				} `json:"pricing"`
			} `json:"data"`
		}
		url := marketAAURL
		if page > 1 {
			url += fmt.Sprintf("?page=%d", page)
		}
		if err := marketHTTP(ctx, url, map[string]string{"x-api-key": key}, &body); err != nil {
			return nil, 0, err
		}
		version = body.Version
		for _, m := range body.Data {
			out = append(out, benchModel{Slug: m.Slug, Name: m.Name, Creator: m.ModelCreator.Name, Released: m.ReleaseDate,
				Intelligence: m.Evaluations.Intelligence, Speed: m.Performance.Speed, FirstToken: m.Performance.FirstToken,
				InputPrice: m.Pricing.Input, OutputPrice: m.Pricing.Output})
		}
		if !body.Pagination.HasMore {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Intelligence, out[j].Intelligence
		return a != nil && (b == nil || *a > *b)
	})
	return out, version, nil
}

// refreshMarket updates whatever is stale. force ignores the cache ages.
func refreshMarket(ctx context.Context, force bool) {
	market.mu.Lock()
	if market.refreshing {
		market.mu.Unlock()
		return
	}
	needCatalog := force || time.Since(market.catalogAt) > marketCatalogFor
	key, _ := readProviderSecret(marketSecretID)
	needBench := key != "" && (force || time.Since(market.benchAt) > marketBenchFor)
	if !needCatalog && !needBench {
		market.mu.Unlock()
		return
	}
	market.refreshing = true
	market.mu.Unlock()
	defer func() { market.mu.Lock(); market.refreshing = false; market.mu.Unlock() }()
	if needCatalog {
		list, err := fetchOpenRouter(ctx)
		market.mu.Lock()
		market.catalogAt = time.Now()
		if err != nil {
			market.catalogErr = err.Error()
		} else {
			market.catalog, market.catalogErr = list, ""
		}
		market.mu.Unlock()
		if err == nil {
			announceNewModels(list)
		}
	}
	if needBench {
		list, version, err := fetchArtificialAnalysis(ctx, key)
		market.mu.Lock()
		market.benchAt = time.Now()
		if err != nil {
			market.benchErr = err.Error()
		} else {
			market.bench, market.benchIndex, market.benchErr = list, version, ""
		}
		market.mu.Unlock()
	}
}

// announceNewModels publishes one event per model not seen before. The first
// refresh only remembers the catalogue, so installing Loom does not announce
// hundreds of models.
func announceNewModels(list []marketModel) {
	seen := map[string]bool{}
	first := !getStoreJSON(bkState, marketSeenKey, &seen)
	watch := loadMarketWatch()
	fresh := []marketModel{}
	for _, m := range list {
		if !seen[m.ID] {
			seen[m.ID] = true
			if !first && (!watch.OpenOnly || m.HFID != "") {
				fresh = append(fresh, m)
			}
		}
	}
	_ = putStoreJSON(bkState, marketSeenKey, seen)
	if !watch.Notify || len(fresh) == 0 || workspaceSessions == nil || workspaceSessions.events == nil {
		return
	}
	for i, m := range fresh {
		if i == 5 {
			workspaceSessions.events.Publish(events.Event{Type: events.ModelNew, Title: fmt.Sprintf("%d new models", len(fresh)), Status: "see Activity › Model watch"})
			break
		}
		status := m.Creator
		if m.HFID != "" {
			status += " · open weights"
		}
		workspaceSessions.events.Publish(events.Event{Type: events.ModelNew, Title: "New model: " + m.Name, Status: status})
	}
}

// watchMarket refreshes the catalogue in the background.
func watchMarket(ctx context.Context) {
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			refreshMarket(ctx, false)
			timer.Reset(marketCatalogFor)
		}
	}
}

// handleMarket:
//
//	GET  → {catalog, new, bench, ...}
//	POST {refresh} → refreshes now (the benchmark only if its cache is older than an hour)
func handleMarket(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		refreshMarket(r.Context(), true)
	} else {
		refreshMarket(r.Context(), false)
	}
	key, _ := readProviderSecret(marketSecretID)
	watch := loadMarketWatch()
	market.mu.Lock()
	defer market.mu.Unlock()
	cutoff := time.Now().AddDate(0, 0, -marketNewWindowDays).Unix()
	fresh := []marketModel{}
	for _, m := range market.catalog {
		if m.Created >= cutoff {
			fresh = append(fresh, m)
		}
	}
	sendJSON(w, 200, map[string]any{
		"ok": true, "catalog": market.catalog, "new": fresh,
		"catalog_at": market.catalogAt.UnixMilli(), "catalog_error": market.catalogErr,
		"bench": market.bench, "bench_at": market.benchAt.UnixMilli(), "bench_error": market.benchErr,
		"intelligence_index_version": market.benchIndex, "has_key": key != "",
		"watch": map[string]bool{"notify": watch.Notify, "open_only": watch.OpenOnly},
	})
}

// handleMarketSettings: POST {key?, forget_key?, notify?, open_only?}.
func handleMarketSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Key       *string `json:"key"`
		ForgetKey bool    `json:"forget_key"`
		Notify    *bool   `json:"notify"`
		OpenOnly  *bool   `json:"open_only"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if body.ForgetKey {
		_ = deleteProviderSecret(marketSecretID)
		market.mu.Lock()
		market.bench, market.benchAt, market.benchErr = nil, time.Time{}, ""
		market.mu.Unlock()
	}
	if body.Key != nil && strings.TrimSpace(*body.Key) != "" {
		key := strings.TrimSpace(*body.Key)
		if _, _, err := fetchArtificialAnalysis(r.Context(), key); err != nil {
			sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if err := sealProviderSecret(marketSecretID, key); err != nil {
			sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		market.mu.Lock()
		market.benchAt = time.Time{}
		market.mu.Unlock()
	}
	watch := loadMarketWatch()
	if body.Notify != nil {
		watch.Notify = *body.Notify
	}
	if body.OpenOnly != nil {
		watch.OpenOnly = *body.OpenOnly
	}
	_ = putStoreJSON(bkState, marketWatchKey, watch)
	sendJSON(w, 200, map[string]any{"ok": true})
}
