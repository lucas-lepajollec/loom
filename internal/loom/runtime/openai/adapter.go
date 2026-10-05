// Chat Completions adapter. Optional tool access is supplied explicitly by the
// application; no model loading, compaction or hidden fallback is applied.
package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config contains only the provider settings consumed by a text turn.
// Persistent records, credential storage and model selection belong to Loom.
type Config struct {
	Endpoint  string
	Model     string
	UsageMode string
}

type ProviderSource interface{ ProviderConfig() Config }
type CredentialSource interface{ APIKey() string }
type ClientSource interface{ HTTPClient() *http.Client }

// Adapter consumes resolved application inputs without reading application state.
// The client is copied for each request to enforce the no-redirect boundary.
type Adapter struct {
	Provider    ProviderSource
	Credentials CredentialSource
	Client      ClientSource
}

// Messages are marshaled directly to preserve the caller's existing JSON shape.
type Turn struct {
	Messages  any
	MaxTokens int
	Tools     ToolAccess
}

type Event struct {
	Content string
	Usage   *Usage
	Tool    *ToolEvent
}

func (a Adapter) Run(ctx context.Context, turn Turn, emit func(Event) bool) (string, error) {
	if turn.Tools != nil {
		return a.runTools(ctx, turn, emit)
	}
	return a.runOnce(ctx, turn, emit, nil)
}
func (a Adapter) runOnce(ctx context.Context, turn Turn, emit func(Event) bool, pending *[]toolCall) (string, error) {
	provider := a.Provider.ProviderConfig()
	endpoint, err := ValidateEndpoint(provider.Endpoint)
	if err != nil {
		return "", err
	}
	key := a.Credentials.APIKey()
	if strings.TrimSpace(key) == "" {
		return "", errors.New("missing key: reconnect the provider in Models → Providers")
	}
	payload := map[string]any{
		"model": provider.Model, "messages": turn.Messages, "stream": true,
	}
	if turn.Tools != nil {
		payload["tools"] = turn.Tools.Definitions()
	}
	if turn.MaxTokens > 0 {
		payload["max_tokens"] = turn.MaxTokens
	}
	if provider.UsageMode != "none" {
		payload["stream_options"] = map[string]bool{"include_usage": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", errors.New("invalid messages")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", errors.New("invalid destination")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+key)
	client := a.Client.HTTPClient()
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	// Even same-origin redirects are rejected: an explicit endpoint is a consent
	// boundary. Never forward credentials or context to a redirected destination.
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := copyClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("provider unreachable or timed out; check the connection")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if turn.Tools != nil && resp.StatusCode == http.StatusBadRequest {
			return "", errors.New("provider rejected a request with web-search tools; check model function-call support or disable web search")
		}
		// Never echo a provider body: it may contain the credential or prompt.
		return "", fmt.Errorf("the provider rejected the request (HTTP %d); check the key, model and your quota", resp.StatusCode)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return "", errors.New("the provider is not returning a compatible SSE stream")
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 2<<20))
	scanner.Buffer(make([]byte, 4096), 512<<10)
	var answer strings.Builder
	finish := ""
	done := false
	var data []string
	calls := map[int]*toolCall{}
	consume := func() error {
		payload := strings.Join(data, "\n")
		data = nil
		if payload == "" {
			return nil
		}
		if payload == "[DONE]" {
			done = true
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string          `json:"content"`
					Refusal   string          `json:"refusal"`
					ToolCalls json.RawMessage `json:"tool_calls"`
				} `json:"delta"`
				Finish string `json:"finish_reason"`
			} `json:"choices"`
			Usage *Usage          `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			return errors.New("invalid provider event")
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return errors.New("the provider reported an error during the response")
		}
		if chunk.Usage != nil && !emit(Event{Usage: chunk.Usage}) {
			return context.Canceled
		}
		if len(chunk.Choices) > 0 {
			c := chunk.Choices[0]
			if len(c.Delta.ToolCalls) > 0 && string(c.Delta.ToolCalls) != "null" && string(c.Delta.ToolCalls) != "[]" {
				if pending == nil {
					return errors.New("this text connector does not execute tools")
				}
				var deltas []toolDelta
				if json.Unmarshal(c.Delta.ToolCalls, &deltas) != nil {
					return errors.New("invalid provider tool call")
				}
				for _, d := range deltas {
					if d.Index < 0 || d.Index >= 4 {
						return errors.New("too many provider tool calls")
					}
					if calls[d.Index] == nil {
						calls[d.Index] = &toolCall{Type: "function"}
					}
					call := calls[d.Index]
					if d.ID != "" {
						call.ID = d.ID
					}
					if d.Type != "" && d.Type != "function" {
						return errors.New("unsupported provider tool type")
					}
					call.Function.Name += d.Function.Name
					call.Function.Arguments += d.Function.Arguments
					if len(call.ID) > 200 || len(call.Function.Name) > 128 || len(call.Function.Arguments) > 16384 {
						return errors.New("provider tool call too large")
					}
				}
			}
			part := c.Delta.Content + c.Delta.Refusal
			if answer.Len()+len(part) > 256<<10 {
				return errors.New("response too long (maximum 256 KiB)")
			}
			if part != "" {
				answer.WriteString(part)
				if !emit(Event{Content: part}) {
					return context.Canceled
				}
			}
			if c.Finish != "" {
				finish = c.Finish
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := consume(); err != nil {
				return "", err
			}
			if done {
				break
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err := scanner.Err(); err != nil {
		return "", errors.New("stream interrupted: partial response preserved")
	}
	if len(data) > 0 {
		if err := consume(); err != nil {
			return "", err
		}
	}
	if !done && finish == "" {
		return "", errors.New("stream interrupted before the end of the response")
	}
	// Reaching an explicitly requested benchmark budget is a completed measurement.
	if pending != nil && finish == "tool_calls" && len(calls) > 0 {
		seen := map[string]bool{}
		for i := 0; i < len(calls); i++ {
			call := calls[i]
			if call == nil || call.ID == "" || seen[call.ID] || call.Function.Name == "" {
				return "", errors.New("incomplete provider tool call")
			}
			seen[call.ID] = true
			*pending = append(*pending, *call)
		}
		return answer.String(), nil
	}
	if len(calls) > 0 {
		return "", errors.New("incomplete provider tool response")
	}
	if finish != "" && finish != "stop" && !(finish == "length" && turn.MaxTokens > 0) {
		return "", fmt.Errorf("incomplete response (finish: %s)", SafeFinishReason(finish))
	}
	if answer.Len() == 0 {
		return "", errors.New("the provider finished without a text response")
	}
	return answer.String(), nil
}

func SafeFinishReason(reason string) string {
	switch reason {
	case "length":
		return "length limit"
	case "content_filter":
		return "provider filter"
	default:
		return "unsupported function"
	}
}
