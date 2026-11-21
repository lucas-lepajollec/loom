package loom

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/events"
)

// Model watch: what is out there and how good it is. Two public sources:
//   - OpenRouter's catalogue (no key): every hosted model with its release
//     time, context, prices and, for open weights, its Hugging Face id.
//   - Epoch AI's public benchmark archive (no key, CC BY 4.0): the Epoch
//     Capabilities Index, one score per model combining many benchmarks, with
//     its confidence interval, release date and weights access. It must be
//     credited to Epoch AI wherever it is shown.
// Both feeds are cached between refreshes.

const (
	openRouterModelsURL  = "https://openrouter.ai/api/v1/models"
	epochArchiveURL      = "https://epoch.ai/data/benchmark_data.zip"
	marketCatalogFor     = 6 * time.Hour
	marketBenchFor       = 24 * time.Hour
	marketSeenKey        = "market_seen_models"
	marketWatchKey       = "market_watch"
	marketNewWindowDays  = 30
	marketMaxCatalogSize = 4000
)

var marketTransport http.RoundTripper = &http.Transport{
	DialContext: publicWebDial, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 90 * time.Second,
}

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
	Name     string   `json:"name"`
	Creator  string   `json:"creator"`
	Released string   `json:"released,omitempty"`
	Score    *float64 `json:"score"` // Epoch Capabilities Index
	Low      *float64 `json:"low,omitempty"`
	High     *float64 `json:"high,omitempty"`
	Open     bool     `json:"open,omitempty"` // open weights
}

type marketCache struct {
	mu         sync.Mutex
	catalog    []marketModel
	catalogAt  time.Time
	catalogErr string
	bench      []benchModel
	benchAt    time.Time
	benchErr   string
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

func marketHTTP(ctx context.Context, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "loom-model-watch")
	resp, err := (&http.Client{Transport: marketTransport, Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
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
	if err := marketHTTP(ctx, openRouterModelsURL, &body); err != nil {
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

func fetchEpoch(ctx context.Context) ([]benchModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, epochArchiveURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "loom-model-watch")
	resp, err := (&http.Client{Transport: marketTransport, Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the source returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 40<<20))
	if err != nil {
		return nil, err
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, errors.New("unreadable archive")
	}
	var file *zip.File
	for _, f := range archive.File {
		if path.Base(f.Name) == "eci_scores.csv" {
			file = f
		}
	}
	if file == nil {
		return nil, errors.New("the archive has no capabilities index")
	}
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	rows, err := csv.NewReader(io.LimitReader(rc, 8<<20)).ReadAll()
	if err != nil || len(rows) < 2 {
		return nil, errors.New("unreadable capabilities index")
	}
	col := map[string]int{}
	for i, name := range rows[0] {
		col[strings.TrimSpace(name)] = i
	}
	cell := func(row []string, name string) string {
		if i, ok := col[name]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}
	num := func(v string) *float64 {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
		return &f
	}
	out := []benchModel{}
	for _, row := range rows[1:] {
		name := cell(row, "Display name")
		if name == "" {
			name = cell(row, "Model")
		}
		score := num(cell(row, "eci"))
		if name == "" || score == nil {
			continue
		}
		out = append(out, benchModel{Name: name, Creator: cell(row, "Organization"), Released: cell(row, "date"), Score: score,
			Low: num(cell(row, "eci_ci_low")), High: num(cell(row, "eci_ci_high")), Open: strings.HasPrefix(cell(row, "Accessibility group"), "Open weights")})
	}
	if len(out) == 0 {
		return nil, errors.New("empty capabilities index")
	}
	sort.SliceStable(out, func(i, j int) bool { return *out[i].Score > *out[j].Score })
	return out, nil
}

// refreshMarket updates whatever is stale. force ignores the cache ages.
func refreshMarket(ctx context.Context, force bool) {
	market.mu.Lock()
	if market.refreshing {
		market.mu.Unlock()
		return
	}
	needCatalog := force || time.Since(market.catalogAt) > marketCatalogFor
	needBench := force && time.Since(market.benchAt) > time.Hour || time.Since(market.benchAt) > marketBenchFor
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
		list, err := fetchEpoch(ctx)
		market.mu.Lock()
		market.benchAt = time.Now()
		if err != nil {
			market.benchErr = err.Error()
		} else {
			market.bench, market.benchErr = list, ""
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
		"watch": map[string]bool{"notify": watch.Notify, "open_only": watch.OpenOnly},
	})
}

// handleMarketSettings: POST {notify?, open_only?}.
func handleMarketSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Notify   *bool `json:"notify"`
		OpenOnly *bool `json:"open_only"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		sendJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	watch := loadMarketWatch()
	if body.Notify != nil {
		watch.Notify = *body.Notify
	}
	if body.OpenOnly != nil {
		watch.OpenOnly = *body.OpenOnly
	}
	if err := putStoreJSON(bkState, marketWatchKey, watch); err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": "could not save market settings"})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}
