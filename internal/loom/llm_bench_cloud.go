package loom

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Resolve only registered cloud models, using the same IDs as /api/workspace.
func benchCloudProvider(choiceID string) (CloudProvider, bool) {
	for _, p := range workspaceSessions.providers() {
		for _, model := range append([]string{p.Model}, p.Models...) {
			if model != "" && cloudChoiceID(p.ID, model) == choiceID {
				p.Model = model
				return p, true
			}
		}
	}
	return CloudProvider{}, false
}

func runCloudBenchTest(ctx context.Context, t benchTest, choiceID string) (*benchResult, string, error) {
	if err := runtimeVaultError(); err != nil {
		return nil, "", err
	}
	p, ok := benchCloudProvider(choiceID)
	if !ok {
		return nil, "", fmt.Errorf("cloud model not found; check the provider in Models")
	}
	workspaceSessions.mu.Lock()
	key := workspaceSessions.keys[p.ID]
	workspaceSessions.mu.Unlock()
	if !p.Ready || strings.TrimSpace(key) == "" {
		return nil, "", fmt.Errorf("missing key: reconnect provider %s in Models → Providers", p.Name)
	}
	prompt, maxTokens := benchPrompt(t)
	res := &benchResult{BenchCloudMetrics: &BenchCloudMetrics{}}
	var preview strings.Builder
	tooLarge := false
	started := time.Now()
	_, err := (cloudRuntimeAdapter{provider: p, key: key}).Run(ctx, RuntimeTurn{
		Messages: []Message{{Role: "system", Content: benchSystemPrompt}, {Role: "user", Content: prompt}}, MaxTokens: maxTokens,
	}, func(e StreamEvent) bool {
		if e.Content != "" {
			if res.TTFT == nil {
				ttft := time.Since(started).Seconds()
				res.TTFT = &ttft
			}
			if preview.Len()+len(e.Content) > 64<<10 {
				tooLarge = true
				return false
			}
			preview.WriteString(e.Content)
		}
		if e.Usage != nil {
			if e.Usage.inputReported {
				n := e.Usage.Input
				res.PromptTokens = &n
			}
			if e.Usage.outputReported {
				n := e.Usage.Output
				res.CompletionTokens = &n
			}
		}
		return ctx.Err() == nil
	})
	res.Elapsed = time.Since(started).Seconds()
	if tooLarge {
		return nil, "", fmt.Errorf("benchmark response too large")
	}
	if err != nil {
		return nil, "", err
	}
	if res.CompletionTokens != nil && res.TTFT != nil && res.Elapsed > *res.TTFT {
		rate := float64(*res.CompletionTokens) / (res.Elapsed - *res.TTFT)
		res.PredictedPerSec = &rate
		res.RateBasis = "after_first_text"
	}
	return res, preview.String(), nil
}
