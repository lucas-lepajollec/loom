package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// SearchConfig contains operator-selected search inputs. Page rendering remains
// a separate concern; search results never select endpoints or credentials.
type SearchConfig struct {
	Provider string
	URL      string
	Key      string
}

type contextHTTP struct {
	context.Context
	HTTPDoer
}

func (h contextHTTP) Do(r *http.Request) (*http.Response, error) {
	return h.HTTPDoer.Do(r.WithContext(h.Context))
}

func Search(ctx context.Context, client HTTPDoer, cfg SearchConfig, query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 2048 {
		return nil, errors.New("search query must contain 1–2048 bytes")
	}
	limit = max(1, min(limit, 20))
	if cfg.Provider == "duckduckgo" {
		return GoSearch(contextHTTP{ctx, client}, query, limit)
	}
	var target string
	method := http.MethodGet
	var body io.Reader
	params := url.Values{}
	switch cfg.Provider {
	case "searxng":
		target = cfg.URL
		params.Set("q", query)
		params.Set("format", "json")
		params.Set("categories", "general")
	case "brave":
		target = "https://api.search.brave.com/res/v1/web/search"
		params.Set("q", query)
		params.Set("count", fmt.Sprint(limit))
	case "tavily":
		target, method = "https://api.tavily.com/search", http.MethodPost
		data, _ := json.Marshal(map[string]any{"query": query, "max_results": limit, "search_depth": "basic", "include_answer": false, "include_raw_content": false})
		body = bytes.NewReader(data)
	default:
		return nil, errors.New("unknown search provider")
	}
	if cfg.Provider != "searxng" && strings.TrimSpace(cfg.Key) == "" {
		return nil, errors.New("configure this search provider's API key in Settings → Internet")
	}
	u, err := url.Parse(target)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid search endpoint")
	}
	u.RawQuery = params.Encode()
	r, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, errors.New("invalid search request")
	}
	r.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
	}
	if cfg.Key != "" {
		if cfg.Provider == "brave" {
			r.Header.Set("X-Subscription-Token", cfg.Key)
		} else {
			r.Header.Set("Authorization", "Bearer "+cfg.Key)
		}
	}
	response, err := client.Do(r)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("search provider unreachable or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if cfg.Provider == "searxng" && response.StatusCode == 403 {
			return nil, errors.New("SearXNG rejected JSON access: enable json in search.formats and check instance access rules")
		}
		return nil, fmt.Errorf("search provider rejected the request (HTTP %d)", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return nil, errors.New("search response unreadable or too large")
	}
	var wire struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if json.Unmarshal(data, &wire) != nil {
		return nil, errors.New("search provider did not return compatible JSON; check JSON format support")
	}
	if (cfg.Provider == "brave" && wire.Web.Results == nil) || (cfg.Provider != "brave" && wire.Results == nil) {
		return nil, errors.New("search provider JSON has no results list")
	}
	results := []SearchResult{}
	seen := map[string]bool{}
	add := func(title, rawURL, snippet string) {
		u, err := url.Parse(rawURL)
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || len(rawURL) > 4096 || seen[rawURL] || len(results) >= limit {
			return
		}
		seen[rawURL] = true
		clip := func(s string, n int) string {
			rs := []rune(strings.TrimSpace(s))
			if len(rs) > n {
				rs = rs[:n]
			}
			return string(rs)
		}
		results = append(results, SearchResult{Title: clip(title, 512), URL: rawURL, Snippet: clip(snippet, 1500)})
	}
	if cfg.Provider == "brave" {
		for _, r := range wire.Web.Results {
			add(r.Title, r.URL, r.Description)
		}
	} else {
		for _, r := range wire.Results {
			add(r.Title, r.URL, r.Content)
		}
	}
	return results, nil
}
