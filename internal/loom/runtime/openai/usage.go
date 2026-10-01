package openai

import "encoding/json"

type Usage struct {
	Input          int64 `json:"prompt_tokens"`
	Output         int64 `json:"completion_tokens"`
	Total          int64 `json:"total_tokens"`
	Thinking       int64 `json:"thinking_tokens,omitempty"`
	Cached         int64 `json:"cache_read_tokens,omitempty"`
	inputReported  bool
	outputReported bool
}

// Keep field presence for consumers that must distinguish missing usage from zero.
func (u *Usage) UnmarshalJSON(data []byte) error {
	type usageAlias Usage
	var wire struct {
		usageAlias
		Input  *int64 `json:"prompt_tokens"`
		Output *int64 `json:"completion_tokens"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*u = Usage(wire.usageAlias)
	if wire.Input != nil {
		u.Input, u.inputReported = *wire.Input, true
	}
	if wire.Output != nil {
		u.Output, u.outputReported = *wire.Output, true
	}
	return nil
}

// InputReported and OutputReported distinguish omitted token counts from zero.
func (u Usage) InputReported() bool  { return u.inputReported }
func (u Usage) OutputReported() bool { return u.outputReported }

// EstimateCost applies caller-supplied prices per million tokens. The caller
// decides whether usage and prices are known; this is not a provider charge.
func EstimateCost(input, output int64, inputPrice, outputPrice float64) float64 {
	return (float64(input)*inputPrice + float64(output)*outputPrice) / 1e6
}
