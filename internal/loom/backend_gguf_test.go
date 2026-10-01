package loom

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func writeMiniGGUF(t *testing.T, path, arch string, ctx uint32) {
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
	put(uint32(ggufMagic))
	put(uint32(3))
	put(uint64(0))
	put(uint64(4))
	putStr("general.architecture")
	put(uint32(ggufTypeString))
	putStr(arch)
	putStr("tokenizer.ggml.tokens")
	put(uint32(ggufTypeArray))
	put(uint32(ggufTypeString))
	put(uint64(2))
	putStr("<s>")
	putStr("</s>")
	putStr("general.name")
	put(uint32(ggufTypeString))
	putStr("demo")
	putStr(arch + ".context_length")
	put(uint32(ggufTypeUint32))
	put(ctx)
}

func TestGGUFContextLength(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qwen.gguf")
	writeMiniGGUF(t, path, "qwen3", 262144)
	got := ggufContextLength(path)
	if got != 262144 {
		t.Fatalf("context_length = %d, attendu 262144", got)
	}
}

func TestGGUFContextLengthRejectsGarbage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pas.gguf")
	if err := os.WriteFile(path, []byte("not a gguf"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := ggufContextLength(path); n != 0 {
		t.Fatalf("garbage → %d, attendu 0", n)
	}
}

func TestLoadNakedModelLeavesMemorySettingsAutomatic(t *testing.T) {
	home := testHome(t)
	setConfig(t, "BIN=/usr/bin/llama-server\nHOST=127.0.0.1\nPORT=8081\nCTX=8192\n")
	_ = putStr(bkState, "active_preset", "")
	path := filepath.Join(home, "models", "qwen.gguf")
	writeMiniGGUF(t, path, "qwen3", 262144)
	if err := loadNakedModel("qwen.gguf"); err != nil {
		t.Fatal(err)
	}
	cfg := ReadConfig()
	if cfg["CTX"] != "" || cfg["NGL"] != "" || cfg["FIT"] != "on" {
		t.Fatal("naked load must leave memory settings automatic")
	}
	if cfg["MODEL"] != "qwen.gguf" {
		t.Fatalf("MODEL = %q", cfg["MODEL"])
	}
}

func TestApplyLiveKeepsLinkedBIN(t *testing.T) {
	_ = testHome(t)
	setConfig(t, "BIN=/usr/bin/llama-server\nHOST=127.0.0.1\nPORT=8081\nMODEL=old.gguf\n")
	if err := applyLiveConfig("MODEL=qwen.gguf\nCTX=60000\nEXTRA_ARGS=--flash-attn on\n", ""); err != nil {
		t.Fatal(err)
	}
	if got := ReadConfig()["BIN"]; got != "/usr/bin/llama-server" {
		t.Fatalf("BIN effacé par apply : %q", got)
	}
}

func TestRememberedNakedKeepsExtraArgs(t *testing.T) {
	home := testHome(t)
	setConfig(t, "BIN=/usr/bin/llama-server\n")
	path := filepath.Join(home, "models", "qwen.gguf")
	writeMiniGGUF(t, path, "qwen3", 262144)
	if err := rememberNakedFromContent("qwen.gguf", "MODEL=qwen.gguf\nCTX=60000\nEXTRA_ARGS=--flash-attn on\n"); err != nil {
		t.Fatal(err)
	}
	if err := loadNakedModel("qwen.gguf"); err != nil {
		t.Fatal(err)
	}
	if got := ReadConfig()["EXTRA_ARGS"]; got != "--flash-attn on" {
		t.Fatalf("EXTRA_ARGS du souvenir perdu : %q", got)
	}
}

func TestApplyLiveRefreshesRememberedExtras(t *testing.T) {
	home := testHome(t)
	setConfig(t, "BIN=/usr/bin/llama-server\n")
	path := filepath.Join(home, "models", "qwen.gguf")
	writeMiniGGUF(t, path, "qwen3", 262144)
	if err := rememberNakedFromContent("qwen.gguf", "MODEL=qwen.gguf\nCTX=60000\n"); err != nil {
		t.Fatal(err)
	}
	body := "BIN=/usr/bin/llama-server\nMODEL=qwen.gguf\nCTX=60000\nEXTRA_ARGS=--flash-attn on\n"
	if err := applyLiveConfig(body, ""); err != nil {
		t.Fatal(err)
	}
	refreshRememberedNakedIfAny(body)
	if err := loadNakedModel("qwen.gguf"); err != nil {
		t.Fatal(err)
	}
	if got := ReadConfig()["EXTRA_ARGS"]; got != "--flash-attn on" {
		t.Fatalf("appliquer n'a pas figé EXTRA_ARGS dans le souvenir : %q", got)
	}
}

func TestRememberedNakedOverridesGGUF(t *testing.T) {
	home := testHome(t)
	setConfig(t, "BIN=/usr/bin/llama-server\n")
	path := filepath.Join(home, "models", "qwen.gguf")
	writeMiniGGUF(t, path, "qwen3", 262144)
	if err := rememberNakedFromContent("qwen.gguf", "MODEL=qwen.gguf\nCTX=4096\nNGL=40\n"); err != nil {
		t.Fatal(err)
	}
	if err := loadNakedModel("qwen.gguf"); err != nil {
		t.Fatal(err)
	}
	cfg := ReadConfig()
	if cfg["CTX"] != "4096" || cfg["NGL"] != "40" {
		t.Fatalf("souvenir ignoré : %v", cfg)
	}
}

func TestLiveNakedTweakDoesNotStickWithoutRemember(t *testing.T) {
	home := testHome(t)
	setConfig(t, "BIN=/usr/bin/llama-server\n")
	path := filepath.Join(home, "models", "qwen.gguf")
	writeMiniGGUF(t, path, "qwen3", 262144)
	if err := loadNakedModel("qwen.gguf"); err != nil {
		t.Fatal(err)
	}
	if err := applyLiveConfig("BIN=/usr/bin/llama-server\nMODEL=qwen.gguf\nCTX=4096\n", ""); err != nil {
		t.Fatal(err)
	}
	if ReadConfig()["CTX"] != "4096" {
		t.Fatal("appliquer à chaud n'a pas pris")
	}
	if err := loadNakedModel("qwen.gguf"); err != nil {
		t.Fatal(err)
	}
	if got := ReadConfig()["CTX"]; got != "" {
		t.Fatalf("sans souvenir, reload doit reprendre le fit automatique, CTX=%q", got)
	}
}

func TestServeCtxArgOmitsWhenUnknown(t *testing.T) {
	if got := serveCtxArg(map[string]string{}, filepath.Join(t.TempDir(), "absent.gguf")); got != "" {
		t.Fatalf("sans CTX ni GGUF, -c doit être omis, got %q", got)
	}
	path := filepath.Join(t.TempDir(), "m.gguf")
	writeMiniGGUF(t, path, "llama", 131072)
	if got := serveCtxArg(map[string]string{}, path); got != "131072" {
		t.Fatalf("natif GGUF : %q", got)
	}
	if got := serveCtxArg(map[string]string{"CTX": "8192"}, path); got != "8192" {
		t.Fatalf("CTX explicite : %q", got)
	}
}

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
		put(uint32(ggufTypeUint32))
		put(n)
	}
	put(uint32(ggufMagic))
	put(uint32(3))
	put(uint64(0))
	put(uint64(11))
	putStr("general.architecture")
	put(uint32(ggufTypeString))
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
	m := loadGGUFMeta(path)
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
	m := loadGGUFMeta(path)
	small := estimateKVBytes(m, 32768, "f16", "f16")
	big := estimateKVBytes(m, 262144, "f16", "f16")
	if small <= 0 || big <= small*6 {
		t.Fatalf("KV 32k=%d 262k=%d, attendu ~8×", small, big)
	}
	q8 := estimateKVBytes(m, 32768, "q8_0", "q8_0")
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
		put(uint32(ggufTypeUint32))
		put(n)
	}
	arrU32 := func(key string, vals []uint32) {
		putStr(key)
		put(uint32(ggufTypeArray))
		put(uint32(ggufTypeUint32))
		put(uint64(len(vals)))
		for _, v := range vals {
			put(v)
		}
	}
	arch := "gemma4"
	put(uint32(ggufMagic))
	put(uint32(3))
	put(uint64(0))
	put(uint64(12))
	putStr("general.architecture")
	put(uint32(ggufTypeString))
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
	m := loadGGUFMeta(path)
	if m.Arch != "gemma4" || m.NLayers != 6 || m.SlidingWindow != 1024 {
		t.Fatalf("meta SWA incomplète : %+v", m)
	}
	if len(m.SWAPattern) != 6 || len(m.NKVHeadsPerLayer) != 6 {
		t.Fatalf("tableaux par couche absents : pattern=%v nkv=%v", m.SWAPattern, m.NKVHeadsPerLayer)
	}
	if m.KeyLengthSWA != 256 || m.ValueLengthSWA != 256 {
		t.Fatalf("dims SWA : %+v", m)
	}
	atWindow := estimateKVBytes(m, 1024, "f16", "f16")
	atNative := estimateKVBytes(m, 131072, "f16", "f16")
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
