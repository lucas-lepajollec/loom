package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, contentType, body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

type crawlFixture struct{}

func (crawlFixture) CrawlURL() string { return "https://crawl.test" }
func (crawlFixture) CrawlKey() string { return "test-key" }

func TestCrawlProtocolRequestAndMarkdownShapes(t *testing.T) {
	for _, body := range []string{
		`{"results":[{"success":true,"markdown":"# Page"}]}`,
		`{"success":true,"markdown":{"raw_markdown":"# Page","fit_markdown":"# Fit"}}`,
	} {
		client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != "POST" || r.URL.String() != "https://crawl.test/crawl" || r.Header.Get("Authorization") != "Bearer test-key" {
				t.Fatalf("request: %s %s", r.Method, r.URL)
			}
			var data map[string]any
			if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
				t.Fatal(err)
			}
			crawler := data["crawler_config"].(map[string]any)["params"].(map[string]any)
			if crawler["wait_for"] != "#content" || crawler["page_timeout"] != float64(45000) || data["urls"].([]any)[0] != "https://page.test" {
				t.Fatalf("payload: %+v", data)
			}
			return response(r, "application/json", body), nil
		})}
		got, err := RunCrwl(crawlFixture{}, client, "https://page.test", CrawlOptions{RawMarkdown: true, WaitFor: "#content", PageTimeoutMS: 45000})
		if err != nil || got != "# Page" {
			t.Fatalf("markdown: %q %v", got, err)
		}
	}
}

func TestGoFetchAndSearchUseSuppliedHTTP(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("DNT") != "1" || r.Header.Get("Sec-GPC") != "1" || r.Header.Get("User-Agent") == "" || len(r.Header.Values("Cookie")) != 5 {
			t.Fatal("fetch headers changed")
		}
		if r.URL.Host == "html.duckduckgo.com" {
			if r.URL.Query().Get("q") != "a & b" {
				t.Fatal("query escaping")
			}
			return response(r, "text/html", `<html><body><div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.test%2Fdocs">Docs</a><span class="result__snippet">A useful snippet</span></div><div class="result"><a class="result__a" href="https://example.test/docs">Duplicate</a></div></body></html>`), nil
		}
		return response(r, "text/markdown", "# Readme\n\nContent"), nil
	})}
	md, err := GoFetchMarkdown(client, "https://example.test/readme")
	if err != nil || md != "# Readme\n\nContent" {
		t.Fatalf("plain fetch: %q %v", md, err)
	}
	got, err := GoSearch(client, "a & b", 8)
	if err != nil || len(got) != 1 || got[0].Title != "Docs" || got[0].URL != "https://example.test/docs" || got[0].Snippet != "A useful snippet" {
		t.Fatalf("search: %+v %v", got, err)
	}
	_, _, _, err = GoFetch(client, context.Background(), "https://example.test")
	if err != nil {
		t.Fatal(err)
	}
}

type pageFixture struct {
	page    *Page
	options FetchOptions
}

func (*pageFixture) Engine() string                                  { return EngineGo }
func (*pageFixture) SearchPages(string, int) ([]SearchResult, error) { return nil, nil }
func (s *pageFixture) FindCached(string) *Page                       { return s.page }
func (s *pageFixture) GetPage(_ string, opts FetchOptions) (*Page, error) {
	s.options = opts
	return s.page, nil
}
func TestWebReadGrepAndOpenBoundary(t *testing.T) {
	source := &pageFixture{page: &Page{URL: "https://page.test", Lines: []string{"# Title", "alpha", "match", "omega", "match"}}}
	got := ToolWebRead(source, map[string]any{"url": "https://page.test", "offset": float64(2), "limit": float64(2)})
	want := "# https://page.test\nLignes 2–3 sur 5 (2 de plus en dessous)\n\n```\n    2 | alpha\n    3 | match\n```"
	if got != want {
		t.Fatalf("read: %q", got)
	}
	grep := ToolWebGrep(source, map[string]any{"pattern": "MATCH", "context": float64(1)})
	if strings.Count(grep, "```") != 2 || !strings.Contains(grep, "2 match(es)") || !strings.Contains(grep, "    5 | match") {
		t.Fatalf("grep: %q", grep)
	}
	open := ToolWebOpen(source, map[string]any{"refresh": true, "wait_for": "#main", "actions": []any{"expand()"}})
	if !source.options.Force || !source.options.DismissPopups || source.options.WaitFor != "#main" || len(source.options.Actions) != 1 || !strings.Contains(open, "# Ouvert : https://page.test") {
		t.Fatalf("open: %q %+v", open, source.options)
	}
	props := WebOpenTool(source).Function.Parameters.(map[string]any)["properties"].(map[string]any)
	if len(props) != 2 || props["url"] == nil || props["refresh"] == nil {
		t.Fatalf("native schema: %+v", props)
	}
}
