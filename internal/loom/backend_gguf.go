package loom

import (
	"encoding/binary"
	"fmt"
	"io"
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

const (
	ggufMagic       = 0x46554747 // "GGUF"
	ggufTypeUint8   = 0
	ggufTypeInt8    = 1
	ggufTypeUint16  = 2
	ggufTypeInt16   = 3
	ggufTypeUint32  = 4
	ggufTypeInt32   = 5
	ggufTypeFloat32 = 6
	ggufTypeBool    = 7
	ggufTypeString  = 8
	ggufTypeArray   = 9
	ggufTypeUint64  = 10
	ggufTypeInt64   = 11
	ggufTypeFloat64 = 12
	ggufMaxKV       = 65536
	ggufMaxString   = 16 << 20
	ggufMaxArrayLen = 8 << 20
)

// ggufMeta est le sous-ensemble d'en-tête GGUF dont on a besoin pour les
// défauts d'un modèle nu et l'estimation VRAM (formule Unsloth Studio :
// KV selon l'archi, buffer de compute, tête MTP).
type ggufMeta struct {
	Path           string
	Arch           string
	ContextLength  int
	NLayers        int
	NHeads         int
	NKVHeads       int
	Embedding      int
	FFN            int
	KeyLength      int
	ValueLength    int
	SlidingWindow     int
	FullAttnEvery     int
	KVLoraRank        int
	KeyLengthMLA      int
	SSMInner          int
	NextN             int
	VocabSize         int
	FileBytes         int64
	SharedKVLayers    int
	Thinks            bool
	HasEffort         bool
	EffortLevels      []string
	EffortDefault     string
	KeyLengthSWA      int
	ValueLengthSWA    int
	SWAPattern        []int
	NKVHeadsPerLayer  []int
}

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

func readGGUFMeta(path string) (ggufMeta, error) {
	var out ggufMeta
	path = strings.TrimSpace(path)
	if path == "" {
		return out, fmt.Errorf("chemin vide")
	}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()

	var magic, version uint32
	if err := binary.Read(f, binary.LittleEndian, &magic); err != nil {
		return out, err
	}
	if magic != ggufMagic {
		return out, fmt.Errorf("pas un GGUF")
	}
	if err := binary.Read(f, binary.LittleEndian, &version); err != nil {
		return out, err
	}
	if version < 2 || version > 3 {
		return out, fmt.Errorf("GGUF version %d non gérée", version)
	}
	var nTensors, nKV uint64
	if err := binary.Read(f, binary.LittleEndian, &nTensors); err != nil {
		return out, err
	}
	if err := binary.Read(f, binary.LittleEndian, &nKV); err != nil {
		return out, err
	}
	if nKV > ggufMaxKV {
		return out, fmt.Errorf("trop de métadonnées GGUF")
	}

	arch := ""
	archKeys := map[string]string{}
	ctxByKey := map[string]int{}
	setInt := func(attr string, n int) {
		if n <= 0 {
			return
		}
		switch attr {
		case "context_length":
			out.ContextLength = n
		case "n_layers":
			out.NLayers = n
		case "n_heads":
			out.NHeads = n
		case "n_kv_heads":
			out.NKVHeads = n
		case "embedding":
			out.Embedding = n
		case "ffn":
			out.FFN = n
		case "key_length":
			out.KeyLength = n
		case "value_length":
			out.ValueLength = n
		case "sliding_window":
			out.SlidingWindow = n
		case "full_attn":
			out.FullAttnEvery = n
		case "kv_lora":
			out.KVLoraRank = n
		case "key_length_mla":
			out.KeyLengthMLA = n
		case "ssm_inner":
			out.SSMInner = n
		case "nextn":
			out.NextN = n
		case "shared_kv":
			out.SharedKVLayers = n
		case "key_length_swa":
			out.KeyLengthSWA = n
		case "value_length_swa":
			out.ValueLengthSWA = n
		}
	}
	bindArch := func(a string) {
		arch = a
		out.Arch = a
		archKeys = map[string]string{
			a + ".context_length":             "context_length",
			a + ".block_count":                "n_layers",
			a + ".attention.head_count":       "n_heads",
			a + ".attention.head_count_kv":    "n_kv_heads",
			a + ".embedding_length":           "embedding",
			a + ".feed_forward_length":        "ffn",
			a + ".attention.key_length":       "key_length",
			a + ".attention.value_length":     "value_length",
			a + ".attention.sliding_window":          "sliding_window",
			a + ".attention.sliding_window_pattern":  "swa_pattern",
			a + ".attention.key_length_swa":          "key_length_swa",
			a + ".attention.value_length_swa":        "value_length_swa",
			a + ".full_attention_interval":           "full_attn",
			a + ".attention.kv_lora_rank":     "kv_lora",
			a + ".attention.key_length_mla":   "key_length_mla",
			a + ".attention.shared_kv_layers": "shared_kv",
			a + ".ssm.inner_size":             "ssm_inner",
			a + ".nextn_predict_layers":       "nextn",
		}
	}

	for i := uint64(0); i < nKV; i++ {
		key, err := ggufReadString(f)
		if err != nil {
			break
		}
		var typ uint32
		if err := binary.Read(f, binary.LittleEndian, &typ); err != nil {
			break
		}
		switch {
		case key == "general.architecture" && typ == ggufTypeString:
			a, err := ggufReadString(f)
			if err != nil {
				return out, err
			}
			bindArch(a)
		case key == "tokenizer.ggml.tokens" && typ == ggufTypeArray:
			n, err := ggufSkipArrayKeepLen(f)
			if err != nil {
				return out, err
			}
			if n > 0 {
				out.VocabSize = int(n)
			}
		case (key == "tokenizer.chat_template" || key == "tokenizer.ggml.chat_template") && typ == ggufTypeString:
			s, err := ggufReadString(f)
			if err != nil {
				return out, err
			}
			tc := inspectChatTemplate(s)
			out.Thinks = tc.Thinks
			out.HasEffort = tc.HasEffort
			out.EffortLevels = tc.EffortLevels
			out.EffortDefault = tc.EffortDefault
		case typ == ggufTypeArray:
			attr := archKeys[key]
			if attr == "n_kv_heads" || attr == "swa_pattern" {
				vals, err := ggufReadIntArray(f)
				if err != nil {
					_ = ggufSkipValue(f, typ)
					continue
				}
				if attr == "swa_pattern" {
					out.SWAPattern = vals
					continue
				}
				out.NKVHeadsPerLayer = vals
				max := 0
				for _, v := range vals {
					if v > max {
						max = v
					}
				}
				setInt(attr, max)
			} else if err := ggufSkipValue(f, typ); err != nil {
				return out, err
			}
		default:
			if strings.HasSuffix(key, ".context_length") {
				n, err := ggufReadInt(f, typ)
				if err != nil {
					_ = ggufSkipValue(f, typ)
					continue
				}
				if n > 0 {
					ctxByKey[key] = n
					if arch != "" && key == arch+".context_length" {
						out.ContextLength = n
					} else if out.ContextLength == 0 {
						out.ContextLength = n
					}
				}
				continue
			}
			if attr := archKeys[key]; attr != "" {
				n, err := ggufReadInt(f, typ)
				if err != nil {
					_ = ggufSkipValue(f, typ)
					continue
				}
				setInt(attr, n)
				continue
			}
			if err := ggufSkipValue(f, typ); err != nil {
				return out, err
			}
		}
	}
	if out.ContextLength == 0 {
		out.ContextLength = pickGGUFContext(arch, ctxByKey)
	}
	return out, nil
}

func pickGGUFContext(arch string, ctxByKey map[string]int) int {
	if arch != "" {
		if n := ctxByKey[arch+".context_length"]; n > 0 {
			return n
		}
	}
	for k, n := range ctxByKey {
		if strings.HasSuffix(k, ".context_length") && n > 0 {
			return n
		}
	}
	return 0
}

func ggufReadString(r io.Reader) (string, error) {
	var n uint64
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	if n > ggufMaxString {
		return "", fmt.Errorf("chaîne GGUF trop longue")
	}
	if n == 0 {
		return "", nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func ggufReadInt(r io.Reader, typ uint32) (int, error) {
	switch typ {
	case ggufTypeUint8:
		var v uint8
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int(v), nil
	case ggufTypeInt8:
		var v int8
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int(v), nil
	case ggufTypeUint16:
		var v uint16
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int(v), nil
	case ggufTypeInt16:
		var v int16
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int(v), nil
	case ggufTypeUint32:
		var v uint32
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int(v), nil
	case ggufTypeInt32:
		var v int32
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int(v), nil
	case ggufTypeUint64:
		var v uint64
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		if v > uint64(^uint(0)>>1) {
			return 0, fmt.Errorf("entier GGUF trop grand")
		}
		return int(v), nil
	case ggufTypeInt64:
		var v int64
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int(v), nil
	case ggufTypeBool:
		var v uint8
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return 0, err
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("type GGUF %d non entier", typ)
	}
}

func ggufSkipValue(r io.Reader, typ uint32) error {
	if n := ggufScalarSize(typ); n > 0 {
		_, err := io.CopyN(io.Discard, r, n)
		return err
	}
	switch typ {
	case ggufTypeString:
		_, err := ggufReadString(r)
		return err
	case ggufTypeArray:
		_, err := ggufSkipArrayKeepLen(r)
		return err
	default:
		return fmt.Errorf("type GGUF %d inconnu", typ)
	}
}

func ggufSkipArrayKeepLen(r io.Reader) (uint64, error) {
	var elem uint32
	if err := binary.Read(r, binary.LittleEndian, &elem); err != nil {
		return 0, err
	}
	var count uint64
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return 0, err
	}
	if count > ggufMaxArrayLen {
		return 0, fmt.Errorf("tableau GGUF trop long")
	}
	if sz := ggufScalarSize(elem); sz > 0 {
		_, err := io.CopyN(io.Discard, r, sz*int64(count))
		return count, err
	}
	for i := uint64(0); i < count; i++ {
		if err := ggufSkipValue(r, elem); err != nil {
			return count, err
		}
	}
	return count, nil
}

func ggufReadIntArray(r io.Reader) ([]int, error) {
	var elem uint32
	if err := binary.Read(r, binary.LittleEndian, &elem); err != nil {
		return nil, err
	}
	var count uint64
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	if count > 4096 {
		count = 4096
	}
	out := make([]int, 0, count)
	for i := uint64(0); i < count; i++ {
		n, err := ggufReadInt(r, elem)
		if err != nil {
			return out, err
		}
		out = append(out, n)
	}
	return out, nil
}

func ggufScalarSize(typ uint32) int64 {
	switch typ {
	case ggufTypeUint8, ggufTypeInt8, ggufTypeBool:
		return 1
	case ggufTypeUint16, ggufTypeInt16:
		return 2
	case ggufTypeUint32, ggufTypeInt32, ggufTypeFloat32:
		return 4
	case ggufTypeUint64, ggufTypeInt64, ggufTypeFloat64:
		return 8
	}
	return 0
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

func chatTemplateThinks(s string) bool {
	return inspectChatTemplate(s).Thinks
}

// inspectChatTemplate lit le gabarit Jinja du GGUF : est-ce que le modèle
// pense, et quels niveaux d'effort il accepte vraiment (Qwen : low/medium/xhigh ;
// Gemma 4 : enable_thinking sans effort). On ne recopie pas la liste llama.cpp.
type chatTemplateCaps struct {
	Thinks        bool
	HasEffort     bool
	EffortLevels  []string
	EffortDefault string
}

var effortLevelOrder = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

func inspectChatTemplate(s string) chatTemplateCaps {
	var c chatTemplateCaps
	if s == "" {
		return c
	}
	low := strings.ToLower(s)
	for _, n := range []string{
		"<think>", "</think>", "enable_thinking", "reasoning_content",
		"<|channel|>analysis", "reasoning_effort", "thinking_mode",
	} {
		if strings.Contains(low, n) {
			c.Thinks = true
			break
		}
	}
	c.HasEffort = strings.Contains(low, "reasoning_effort")
	if !c.HasEffort {
		return c
	}
	found := map[string]bool{}
	for idx := 0; ; {
		i := strings.Index(low[idx:], "reasoning_effort")
		if i < 0 {
			break
		}
		i += idx
		start := i - 120
		if start < 0 {
			start = 0
		}
		end := i + 280
		if end > len(low) {
			end = len(low)
		}
		chunk := low[start:end]
		for _, k := range effortLevelOrder {
			if strings.Contains(chunk, "'"+k+"'") || strings.Contains(chunk, `"`+k+`"`) {
				found[k] = true
			}
		}
		if c.EffortDefault == "" {
			c.EffortDefault = effortDefaultIn(chunk)
		}
		idx = i + 1
	}
	for _, k := range effortLevelOrder {
		if found[k] {
			c.EffortLevels = append(c.EffortLevels, k)
		}
	}
	return c
}

func effortDefaultIn(chunk string) string {
	for _, k := range effortLevelOrder {
		if strings.Contains(chunk, "default('"+k+"')") || strings.Contains(chunk, `default("`+k+`")`) {
			return k
		}
	}
	return ""
}
