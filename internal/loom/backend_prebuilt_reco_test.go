package loom

import (
	"runtime"
	"testing"
)

// llama.cpp publie désormais des binaires CUDA pour Linux : le précompilé est
// conseillé partout ; sous Linux avec CUDA ou HIP, une note explique que
// compiler reste possible pour optimiser.
func TestRecommendedMode(t *testing.T) {
	cases := []struct {
		backend  string
		wantMode string
		wantWhy  bool
	}{
		{"cuda", "fast", true}, // binaire CUDA officiel, compilation possible
		{"hip", "fast", true}, // zip ROCm Ubuntu si publié, sinon compile HIP
		{"rocm", "fast", false},
		{"vulkan", "fast", false},
		{"cpu", "fast", false},
		{"metal", "fast", false},
	}
	for _, c := range cases {
		got := recommendedMode(c.backend)
		wantMode, wantWhy := c.wantMode, c.wantWhy
		// La règle ne vise que Linux : sur les autres OS le précompilé reste
		// conseillé même avec CUDA détecté.
		if runtime.GOOS != "linux" {
			wantMode, wantWhy = "fast", false
		}
		if got["mode"] != wantMode {
			t.Errorf("backend %q sur %s : mode = %v, attendu %q", c.backend, runtime.GOOS, got["mode"], wantMode)
		}
		if hasWhy := got["why"] != ""; hasWhy != wantWhy {
			t.Errorf("backend %q sur %s : why non vide = %v, attendu %v", c.backend, runtime.GOOS, hasWhy, wantWhy)
		}
	}
}
