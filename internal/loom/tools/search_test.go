package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type searchTransport func(*http.Request) (*http.Response, error)

func (f searchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSearchProvidersEncodeQueriesAndNormalizeResults(t *testing.T) {
	for _, provider := range []string{"searxng", "brave", "tavily"} {
		t.Run(provider, func(t *testing.T) {
			client := &http.Client{Transport: searchTransport(func(r *http.Request) (*http.Response, error) {
				if provider == "tavily" {
					var data map[string]any
					_ = json.NewDecoder(r.Body).Decode(&data)
					if data["query"] != "a & b" || data["include_answer"] != false || r.Method != "POST" {
						t.Fatal("wrong Tavily request")
					}
				} else if r.URL.Query().Get("q") != "a & b" {
					t.Fatal("query was not encoded")
				}
				if provider == "brave" {
					if r.Header.Get("X-Subscription-Token") != "fixture" {
						t.Fatal("missing Brave key")
					}
				} else if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Fatal("missing bearer key")
				}
				body := `{"results":[{"title":"Docs","url":"https://example.test/docs","content":"Snippet"},{"title":"Duplicate","url":"https://example.test/docs"},{"url":"javascript:alert(1)"}]}`
				if provider == "brave" {
					body = `{"web":{"results":[{"title":"Docs","url":"https://example.test/docs","description":"Snippet"}]}}`
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			rows, err := Search(context.Background(), client, SearchConfig{Provider: provider, URL: "http://127.0.0.1/search", Key: "fixture"}, "a & b", 8)
			if err != nil || len(rows) != 1 || rows[0].Snippet != "Snippet" {
				t.Fatalf("normalization: %+v %v", rows, err)
			}
		})
	}
}
func TestSearchFailuresDoNotExposeUpstreamBodies(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{403, `credential-private-fixture`}, {200, `<html>credential-private-fixture</html>`}, {200, `{"error":"credential-private-fixture"}`}} {
		client := &http.Client{Transport: searchTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}
		_, err := Search(context.Background(), client, SearchConfig{Provider: "searxng", URL: "http://127.0.0.1/search"}, "query", 8)
		if err == nil || strings.Contains(err.Error(), "credential-private-fixture") {
			t.Fatal("unsafe or missing error")
		}
	}
}
