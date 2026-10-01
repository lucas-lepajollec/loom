package loom

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Lecture minimale du métadonnées GGUF : on n'ouvre que l'en-tête KV pour
// récupérer `{arch}.context_length`. Ça évite de plaquer le 32768 de l'UI
// sur un modèle nu dont la fenêtre native est ailleurs (Qwen 3.5, etc.).

type ggufMetaCacheEnt struct {
	mod  time.Time
	size int64
	meta ggufMeta
}

var (
	ggufMetaMu    sync.Mutex
	ggufMetaCache = map[string]ggufMetaCacheEnt{}
)

// ggufContextLength lit n_ctx_train du .gguf. 0 = illisible ou absent :
// l'appelant laisse alors llama-server décider (pas de -c de repli 32k).
func ggufContextLength(path string) int {
	return loadGGUFMeta(path).ContextLength
}

func loadGGUFMeta(path string) ggufMeta {
	path = strings.TrimSpace(path)
	if path == "" {
		return ggufMeta{}
	}
	st, err := os.Stat(path)
	if err != nil {
		return ggufMeta{}
	}
	ggufMetaMu.Lock()
	if ent, ok := ggufMetaCache[path]; ok && ent.mod.Equal(st.ModTime()) && ent.size == st.Size() {
		m := ent.meta
		ggufMetaMu.Unlock()
		return m
	}
	ggufMetaMu.Unlock()
	m, _ := readGGUFMeta(path)
	m.Path = path
	m.FileBytes = ggufWeightBytes(path)
	ggufMetaMu.Lock()
	ggufMetaCache[path] = ggufMetaCacheEnt{mod: st.ModTime(), size: st.Size(), meta: m}
	ggufMetaMu.Unlock()
	return m
}

func ggufWeightBytes(path string) int64 {
	base := filepath.Base(path)
	if _, total, ok := shardInfo(base); ok && total > 1 {
		if n := shardFamilySize(filepath.Dir(path), base); n > 0 {
			return n
		}
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func loadGGUFMetaForModel(model string) ggufMeta {
	model = strings.TrimSpace(model)
	if model == "" {
		return ggufMeta{}
	}
	p := model
	if r, err := resolveServeModelPath(model); err == nil {
		p = r
	}
	return loadGGUFMeta(p)
}

// modelNativeCtx résout MODEL= puis lit le context_length du GGUF. 0 si
// le fichier n'est pas là ou n'a pas la clé.
func modelNativeCtx(model string) int {
	return loadGGUFMetaForModel(model).ContextLength
}

// effectiveCtx est la fenêtre vraiment utilisée : CTX= du preset / souvenir
// s'il est posé, sinon le GGUF, sinon 0 (inconnu — pas 32768 inventé).
func effectiveCtx(cfg map[string]string) int {
	if cfg == nil {
		cfg = ReadConfig()
	}
	if v := strings.TrimSpace(cfg["CTX"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return modelNativeCtx(cfg["MODEL"])
}

// serveCtxArg est la valeur passée à llama-server -c. Chaîne vide = on
// omet le drapeau, le moteur prend n_ctx_train du modèle.
func serveCtxArg(cfg map[string]string, modelPath string) string {
	if v := oaiRuntimeGet("CTX"); v != "" {
		return v
	}
	if v := strings.TrimSpace(cfg["CTX"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return strconv.Itoa(n)
		}
	}
	if strings.EqualFold(cfg["FIT"], "on") {
		return ""
	}
	if n := ggufContextLength(modelPath); n > 0 {
		return strconv.Itoa(n)
	}
	return ""
}
