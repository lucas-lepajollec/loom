package loom

const benchSystemPrompt = "Answer the user request directly."

func benchPrompt(t benchTest) (string, int) {
	if t.Kind == "perf" || t.ID == benchTestPerf {
		return benchCorpusPrompt(2000), 300
	}
	n := t.MaxTokens
	if n <= 0 {
		n = 256
	}
	return t.Prompt, n
}
