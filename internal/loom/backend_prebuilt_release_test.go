package loom

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewestBinaryReleaseSkipsReleasesWithoutBinaries(t *testing.T) {
	rels := []struct {
		TagName string    `json:"tag_name"`
		Draft   bool      `json:"draft"`
		Assets  []ghAsset `json:"assets"`
	}{
		{TagName: "v0.5.0", Assets: []ghAsset{{Name: "nightly-tag.txt"}}},
		{TagName: "b2", Draft: true, Assets: []ghAsset{{Name: "llama-b2-bin-ubuntu-x64.tar.gz"}}},
		{TagName: "b1", Assets: []ghAsset{{Name: "llama-b1-bin-ubuntu-x64.tar.gz"}}},
	}
	tag, assets, err := newestBinaryRelease(rels)
	if err != nil || tag != "b1" || len(assets) != 1 {
		t.Fatal(tag, assets, err)
	}
	if _, _, err := newestBinaryRelease(rels[:1]); err == nil {
		t.Fatal("release without binaries accepted")
	}
}

func TestPickCudaPrefersHighestSupportedLinuxBuild(t *testing.T) {
	assets := []ghAsset{
		{Name: "cudart-llama-b9-bin-ubuntu-cuda-12.8-x64.tar.gz"},
		{Name: "llama-b9-bin-ubuntu-cuda-12.8-x64.tar.gz"},
		{Name: "llama-b9-bin-ubuntu-cuda-13.4-x64.tar.gz"},
		{Name: "llama-b9-bin-ubuntu-cuda-13.4-arm64.tar.gz"},
	}
	best, ver := pickCuda(assets, "bin-ubuntu-cuda-", "-x64.tar.gz")
	if best == nil || best.Name[:6] == "cudart" {
		t.Fatal(best)
	}
	if ver != "12.8" && ver != "13.4" {
		t.Fatal(ver)
	}
}

func TestLinkCudaRuntimePutsLibrariesNextToBinary(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "llama-b9")
	rt := filepath.Join(root, "cudart-llama-b9-bin-ubuntu-cuda-12.8-x64")
	for _, d := range []string{bin, rt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"libcudart.so.12", "libcublas.so.12", "libcublasLt.so.12", "README"} {
		if err := os.WriteFile(filepath.Join(rt, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if n := linkCudaRuntime(root, bin); n != 3 {
		t.Fatal(n)
	}
	if n := linkCudaRuntime(root, bin); n != 0 {
		t.Fatal("relinked", n)
	}
	if _, err := os.Stat(filepath.Join(bin, "libcublasLt.so.12")); err != nil {
		t.Fatal(err)
	}
}
