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
	p, ok := benchCloudProvider(choiceID)
	if !ok {
		return nil, "", fmt.Errorf("modèle cloud introuvable ; vérifiez le provider dans Modèles")
	}
	workspaceSessions.mu.Lock()
	key := workspaceSessions.keys[p.ID]
	workspaceSessions.mu.Unlock()
	if !p.Ready || strings.TrimSpace(key) == "" {
		return nil, "", fmt.Errorf("clé absente : reconnectez le provider %s dans Modèles → Providers", p.Name)
	}
	prompt, maxTokens := t.Prompt, t.MaxTokens
	if t.Kind == "perf" || t.ID == benchTestPerf {
		prompt, maxTokens = benchCorpusPrompt(2000), 300
	} else if maxTokens <= 0 {
		maxTokens = 256
	}
	res := &benchResult{BenchCloudMetrics: &BenchCloudMetrics{}}
	var preview strings.Builder
	started := time.Now()
	_, err := (cloudRuntimeAdapter{provider: p, key: key}).Run(ctx, RuntimeTurn{
		Messages: []Message{{Role: "user", Content: prompt}}, MaxTokens: maxTokens,
	}, func(e StreamEvent) bool {
		if e.Content != "" {
			if res.TTFT == nil {
				ttft := time.Since(started).Seconds()
				res.TTFT = &ttft
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
	if err != nil {
		return nil, "", err
	}
	if res.CompletionTokens != nil && res.TTFT != nil && res.Elapsed > *res.TTFT {
		rate := float64(*res.CompletionTokens) / (res.Elapsed - *res.TTFT)
		res.PredictedPerSec = &rate
	}
	return res, preview.String(), nil
}
