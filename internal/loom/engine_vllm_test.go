package loom

import (
	"strings"
	"testing"
)

func TestVLLMArgsAndModelIDs(t *testing.T) {
	got := strings.Join(vllmArgs("Qwen/Qwen3-8B", 9000, 0.85, 16384), " ")
	if got != "serve Qwen/Qwen3-8B --host 127.0.0.1 --port 9000 --max-model-len 16384 --gpu-memory-utilization 0.85 --reasoning-parser qwen3" {
		t.Fatal(got)
	}
	if strings.Contains(strings.Join(vllmArgs("m", 1, 0, 0), " "), "--gpu") {
		t.Fatal("options vides transmises")
	}
	for _, bad := range []string{"", "-rf", "a b", "x;rm", "../etc"} {
		if vllmModelID.MatchString(bad) && !strings.Contains(bad, "..") {
			t.Fatalf("accepté: %q", bad)
		}
	}
	if !vllmModelID.MatchString("meta-llama/Llama-3.1-8B-Instruct") {
		t.Fatal("identifiant valide refusé")
	}
}
