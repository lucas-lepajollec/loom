package llamacpp

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func writeArchGGUF(t *testing.T, path, arch string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	put := func(v any) {
		t.Helper()
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	putStr := func(s string) {
		t.Helper()
		put(uint64(len(s)))
		if _, err := f.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	u32 := func(key string, n uint32) {
		putStr(key)
		put(uint32(GGUFTypeUint32))
		put(n)
	}
	put(uint32(GGUFMagic))
	put(uint32(3))
	put(uint64(0))
	put(uint64(11))
	putStr("general.architecture")
	put(uint32(GGUFTypeString))
	putStr(arch)
	u32(arch+".context_length", 262144)
	u32(arch+".block_count", 33)
	u32(arch+".embedding_length", 4096)
	u32(arch+".attention.head_count", 16)
	u32(arch+".attention.head_count_kv", 4)
	u32(arch+".attention.key_length", 256)
	u32(arch+".attention.value_length", 256)
	u32(arch+".ssm.inner_size", 4096)
	u32(arch+".full_attention_interval", 4)
	u32(arch+".nextn_predict_layers", 1)
}

func TestGGUFMetaHybridQwen35(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qwen35.gguf")
	writeArchGGUF(t, path, "qwen35")
	m, err := ReadGGUFMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Arch != "qwen35" || m.ContextLength != 262144 || m.NLayers != 33 || m.NKVHeads != 4 {
		t.Fatalf("meta incomplète : %+v", m)
	}
	if m.SSMInner != 4096 || m.FullAttnEvery != 4 || m.NextN != 1 {
		t.Fatalf("hybride/MTP manquants : %+v", m)
	}
}

func TestEstimateHybridKVGrowsWithCtx(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qwen35.gguf")
	writeArchGGUF(t, path, "qwen35")
	m, err := ReadGGUFMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	small := EstimateKVBytes(m, 32768, "f16", "f16")
	big := EstimateKVBytes(m, 262144, "f16", "f16")
	if small <= 0 || big <= small*6 {
		t.Fatalf("KV 32k=%d 262k=%d, attendu ~8×", small, big)
	}
	q8 := EstimateKVBytes(m, 32768, "q8_0", "q8_0")
	if q8 >= small {
		t.Fatalf("q8_0 (%d) doit être plus léger que f16 (%d)", q8, small)
	}
}

func writeGemmaSWAGGUF(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	put := func(v any) {
		t.Helper()
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	putStr := func(s string) {
		t.Helper()
		put(uint64(len(s)))
		if _, err := f.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	u32 := func(key string, n uint32) {
		putStr(key)
		put(uint32(GGUFTypeUint32))
		put(n)
	}
	arrU32 := func(key string, vals []uint32) {
		putStr(key)
		put(uint32(GGUFTypeArray))
		put(uint32(GGUFTypeUint32))
		put(uint64(len(vals)))
		for _, v := range vals {
			put(v)
		}
	}
	arch := "gemma4"
	put(uint32(GGUFMagic))
	put(uint32(3))
	put(uint64(0))
	put(uint64(12))
	putStr("general.architecture")
	put(uint32(GGUFTypeString))
	putStr(arch)
	u32(arch+".context_length", 131072)
	u32(arch+".block_count", 6)
	u32(arch+".embedding_length", 3840)
	u32(arch+".attention.head_count", 16)
	arrU32(arch+".attention.head_count_kv", []uint32{8, 8, 8, 8, 8, 1})
	u32(arch+".attention.key_length", 512)
	u32(arch+".attention.value_length", 512)
	u32(arch+".attention.sliding_window", 1024)
	arrU32(arch+".attention.sliding_window_pattern", []uint32{1, 1, 1, 1, 1, 0})
	u32(arch+".attention.key_length_swa", 256)
	u32(arch+".attention.value_length_swa", 256)
}

func TestEstimateGemmaSWADoesNotStackFullCtx(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gemma4.gguf")
	writeGemmaSWAGGUF(t, path)
	m, err := ReadGGUFMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Arch != "gemma4" || m.NLayers != 6 || m.SlidingWindow != 1024 {
		t.Fatalf("meta SWA incomplète : %+v", m)
	}
	if len(m.SWAPattern) != 6 || len(m.NKVHeadsPerLayer) != 6 {
		t.Fatalf("tableaux par couche absents : pattern=%v nkv=%v", m.SWAPattern, m.NKVHeadsPerLayer)
	}
	if m.KeyLengthSWA != 256 || m.ValueLengthSWA != 256 {
		t.Fatalf("dims SWA : %+v", m)
	}
	atWindow := EstimateKVBytes(m, 1024, "f16", "f16")
	atNative := EstimateKVBytes(m, 131072, "f16", "f16")
	if atWindow <= 0 || atNative <= atWindow {
		t.Fatalf("KV 1k=%d 131k=%d", atWindow, atNative)
	}
	// Sans le motif SWA, 131k ferait ~128× le KV. Les couches locales
	// restent à 1024 jetons : ça ne doit plus s'empiler avec le contexte natif.
	if atNative > atWindow*20 {
		t.Fatalf("KV 131k=%d trop gros contre 1k=%d (SWA ignoré)", atNative, atWindow)
	}
	naive := int64(6) * 131072 * 8 * (512 + 512) * 2
	if atNative >= naive/2 {
		t.Fatalf("KV 131k=%d encore au niveau naïf %d", atNative, naive)
	}
}
