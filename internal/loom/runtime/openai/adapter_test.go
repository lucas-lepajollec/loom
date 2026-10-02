package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type inputs struct {
	config Config
	key    string
	client *http.Client
}

func (i inputs) ProviderConfig() Config   { return i.config }
func (i inputs) APIKey() string           { return i.key }
func (i inputs) HTTPClient() *http.Client { return i.client }
func adapterWithTransport(f roundTripFunc) Adapter {
	i := inputs{config: Config{Endpoint: "https://provider.example/v1/", Model: "fixture"}, key: "fixture-key", client: &http.Client{Transport: f}}
	return Adapter{Provider: i, Credentials: i, Client: i}
}
func sseResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func textChunk(text, finish string) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": text}, "finish_reason": finish}}})
	return "data: " + string(b) + "\n\n"
}

func TestRequestAndUsagePresence(t *testing.T) {
	for _, mode := range []string{"", "none"} {
		t.Run(mode, func(t *testing.T) {
			a := adapterWithTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "POST" || r.URL.String() != "https://provider.example/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("request changed: %v", r)
				}
				var got map[string]any
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				want := map[string]any{"model": "fixture", "stream": true, "messages": nil, "max_tokens": float64(12)}
				if mode != "none" {
					want["stream_options"] = map[string]any{"include_usage": true}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("payload = %#v, want %#v", got, want)
				}
				return sseResponse(": ignored\ndata: {\"choices\": [],\n" + "data: \"usage\": {\"prompt_tokens\":0,\"total_tokens\":9,\"thinking_tokens\":2,\"cache_read_tokens\":3}}\n\n" + textChunk("Hello", "length")), nil
			})
			i := a.Provider.(inputs)
			i.config.UsageMode = mode
			a.Provider = i
			var events []Event
			var messages []struct {
				Role string `json:"role"`
			}
			answer, err := a.Run(context.Background(), Turn{Messages: messages, MaxTokens: 12}, func(e Event) bool { events = append(events, e); return true })
			if err != nil || answer != "Hello" || len(events) != 2 {
				t.Fatalf("answer=%q events=%+v err=%v", answer, events, err)
			}
			u := events[0].Usage
			if u == nil || !u.InputReported() || u.OutputReported() || u.Total != 9 || u.Thinking != 2 || u.Cached != 3 {
				t.Fatalf("usage=%+v", u)
			}
		})
	}
}

func TestStreamFailuresAndPartialEvents(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		abort            bool
	}{
		{"truncated", textChunk("Partial", ""), "stream interrupted before the end of the response", false},
		{"malformed", "data: garbage\n\n", "invalid provider event", false},
		{"upstream", "data: {\"error\":{\"message\":\"secret\"}}\n\n", "the provider reported an error during the response", false},
		{"tools", "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{}]}}]}\n\n", "this text connector does not execute tools", false},
		{"empty", "data: [DONE]\n\n", "the provider finished without a text response", false},
		{"length", textChunk("Partial", "length"), "incomplete response (finish: length limit)", false},
		{"unknown finish", textChunk("Partial", "secret"), "incomplete response (finish: unsupported function)", false},
		{"text limit", textChunk(strings.Repeat("x", (256<<10)+1), "stop"), "response too long (maximum 256 KiB)", false},
		{"scan limit", "data: " + strings.Repeat("x", 512<<10), "stream interrupted: partial response preserved", false},
		{"abort text", textChunk("Partial", "stop"), context.Canceled.Error(), true},
		{"abort usage", "data: {\"usage\":{\"completion_tokens\":0}}\n\n", context.Canceled.Error(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := adapterWithTransport(func(*http.Request) (*http.Response, error) { return sseResponse(tc.body), nil })
			var partial strings.Builder
			answer, err := a.Run(context.Background(), Turn{}, func(e Event) bool { partial.WriteString(e.Content); return !tc.abort })
			if answer != "" || err == nil || err.Error() != tc.want {
				t.Fatalf("answer=%q err=%v", answer, err)
			}
			if tc.name == "truncated" && partial.String() != "Partial" {
				t.Fatal("lost partial event")
			}
		})
	}
}

func TestRedirectAndClientIsolation(t *testing.T) {
	calls := 0
	a := adapterWithTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 307, Header: http.Header{"Location": {"https://redirect.example/secret"}}, Body: io.NopCloser(strings.NewReader("secret"))}, nil
	})
	client := a.Client.HTTPClient()
	originalRedirect := func(*http.Request, []*http.Request) error { t.Fatal("caller redirect policy used"); return nil }
	client.CheckRedirect = originalRedirect
	_, err := a.Run(context.Background(), Turn{}, func(Event) bool { return true })
	if calls != 1 || err == nil || strings.Contains(err.Error(), "secret") || client.CheckRedirect == nil || reflect.ValueOf(client.CheckRedirect).Pointer() != reflect.ValueOf(originalRedirect).Pointer() {
		t.Fatalf("calls=%d err=%v client mutated", calls, err)
	}
}

func TestCancellationAndSanitizedTransportError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := adapterWithTransport(func(*http.Request) (*http.Response, error) { cancel(); return nil, errors.New("secret") })
	if _, err := a.Run(ctx, Turn{}, func(Event) bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), Turn{}, func(Event) bool { return true }); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}
