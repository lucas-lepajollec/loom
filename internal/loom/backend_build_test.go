package loom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheMismatchCatchesLostGPUBackend(t *testing.T) {
	build := t.TempDir()
	write := func(cache string) {
		if err := os.WriteFile(filepath.Join(build, "CMakeCache.txt"), []byte(cache), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plan := buildPlan{backend: "cuda", cudaCXX: "/usr/local/cuda/bin/nvcc", flags: []string{"-DGGML_CUDA=ON"}}
	// The state Lucas's update left: CUDA switched off after CMake's re-run.
	write("GGML_CUDA:BOOL=OFF\nCMAKE_CUDA_COMPILER:UNINITIALIZED=/usr/local/cuda/bin/nvcc\n")
	if !cacheMismatch(build, plan) {
		t.Fatal("CUDA off in cache not detected")
	}
	write("GGML_CUDA:BOOL=ON\nCMAKE_CUDA_COMPILER:FILEPATH=/usr/local/cuda-13.3/bin/nvcc\n")
	if !cacheMismatch(build, plan) {
		t.Fatal("changed CUDA compiler not detected")
	}
	write("GGML_CUDA:BOOL=ON\nCMAKE_CUDA_COMPILER:FILEPATH=/usr/local/cuda/bin/nvcc\n")
	if cacheMismatch(build, plan) {
		t.Fatal("matching cache flagged")
	}
	if cacheMismatch(build, buildPlan{backend: "cpu"}) != true {
		t.Fatal("CPU plan over a CUDA cache must reconfigure")
	}
}

func TestGPUListedNeedsADeviceLine(t *testing.T) {
	if gpuListed("Available devices:\n  (none)\nggml_cuda_init: failed to initialize CUDA", "CUDA") {
		t.Fatal("error text counted as a device")
	}
	if !gpuListed("Available devices:\n  CUDA0: NVIDIA GeForce RTX 5060 Ti (16310 MiB, 15000 MiB free)", "CUDA") {
		t.Fatal("CUDA device missed")
	}
	if !gpuListed("Available devices:\n  MTL0: Apple M3", "Metal") {
		t.Fatal("Metal device missed")
	}
}

func TestPickCudaHostCompilerFallsBackToAcceptedGCC(t *testing.T) {
	calls := []string{}
	compiles := func(nvcc, host string) bool {
		calls = append(calls, host)
		return host != "" && strings.HasSuffix(host, "-15")
	}
	got := pickCudaHostCompiler("/usr/local/cuda/bin/nvcc", func(n, h string) bool { return compiles(n, h) })
	// The default compiler is tried first; a fallback is only returned when one
	// of the installed candidates is accepted by nvcc.
	if len(calls) == 0 || calls[0] != "" {
		t.Fatalf("default compiler not tried first: %v", calls)
	}
	if got != "" && !strings.HasSuffix(got, "-15") {
		t.Fatalf("picked %q", got)
	}
	if pickCudaHostCompiler("nvcc", func(string, string) bool { return true }) != "" {
		t.Fatal("a working default compiler must not be overridden")
	}
}
