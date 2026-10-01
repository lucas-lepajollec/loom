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
