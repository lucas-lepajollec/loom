package loom

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/lucas-lepajollec/loom/internal/loom/runtime/openai"
)

// Shared discussion/harness usage retains its historical Loom type and private
// presence flags. The cloud protocol owns parsing; this boundary copies all fields.
type RuntimeUsage struct {
	Input          int64 `json:"prompt_tokens"`
	Output         int64 `json:"completion_tokens"`
	Total          int64 `json:"total_tokens"`
	Thinking       int64 `json:"thinking_tokens,omitempty"`
	Cached         int64 `json:"cache_read_tokens,omitempty"`
	inputReported  bool
	outputReported bool
}

func runtimeUsageFromOpenAI(u openai.Usage) RuntimeUsage {
	return RuntimeUsage{Input: u.Input, Output: u.Output, Total: u.Total,
		Thinking: u.Thinking, Cached: u.Cached,
		inputReported: u.InputReported(), outputReported: u.OutputReported()}
}

func (u *RuntimeUsage) UnmarshalJSON(data []byte) error {
	var wire openai.Usage
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*u = runtimeUsageFromOpenAI(wire)
	return nil
}

func validateCloudEndpoint(raw string) (string, error) { return openai.ValidateEndpoint(raw) }
func localNetworkHost(host string) bool                { return openai.LocalNetworkHost(host) }
func safeFinishReason(reason string) string            { return openai.SafeFinishReason(reason) }
func estimatedCloudCost(input, output int64, inputPrice, outputPrice float64) float64 {
	return openai.EstimateCost(input, output, inputPrice, outputPrice)
}

// Compatibility for registry, discussions and benchmarks: Loom resolves a
// provider/key snapshot before starting a turn and supplies the original client.
type cloudRuntimeAdapter struct {
	provider CloudProvider
	key      string
	client   *http.Client
}

func (cloudRuntimeAdapter) Descriptor() RuntimeDescriptor {
	return RuntimeDescriptor{ID: "openai-compatible", Name: "API compatible Chat Completions", Kind: "cloud", Description: "API cloud compatible Chat Completions, configurée explicitement.", Implemented: true, Capabilities: []string{"chat", "stream", "cancel", "usage"}}
}

func (a cloudRuntimeAdapter) ProviderConfig() openai.Config {
	return openai.Config{Endpoint: a.provider.Endpoint, Model: a.provider.Model, UsageMode: a.provider.UsageMode}
}
func (a cloudRuntimeAdapter) APIKey() string           { return a.key }
func (a cloudRuntimeAdapter) HTTPClient() *http.Client { return a.client }

func (a cloudRuntimeAdapter) Run(ctx context.Context, turn RuntimeTurn, emit ChatCallback) ([]Message, error) {
	answer, err := (openai.Adapter{Provider: a, Credentials: a, Client: a}).Run(ctx,
		openai.Turn{Messages: turn.Messages, MaxTokens: turn.MaxTokens}, func(e openai.Event) bool {
			event := StreamEvent{Content: e.Content}
			if e.Usage != nil {
				u := runtimeUsageFromOpenAI(*e.Usage)
				event.Usage = &u
			}
			return emit(event)
		})
	if err != nil {
		return nil, err
	}
	return []Message{{Role: "assistant", Content: answer}}, nil
}
