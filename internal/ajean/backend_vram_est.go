package ajean

import (
	"math"
	"os"
	"strconv"
	"strings"
)

// Estimation VRAM d'un GGUF, calquée sur Unsloth Studio (llama_cpp.py) :
// poids du fichier + cache KV selon l'architecture (MLA / hybride Mamba /
// GQA / repli) + buffer de compute + réserve CUDA + tête MTP si elle est
// engagée. Ce n'est pas une mesure llama.cpp, mais ça suit les mêmes axes
// que le panneau (CTX, KV, NGL, batch, slots).

const (
	estCUDAReserveB         = 320 * 1024 * 1024
	estComputeSafety        = 1.15
	estDefaultUBatch        = 512
	estCtxComputePerEmbd    = 2.25
	estCtxComputePerEmbdMLA = 1.25
	estCtxComputeF16Mask    = 1.5
	estVRAMFitFrac          = 0.97
	estDefaultVocab         = 128000
)

type estimateOpt struct {
	Ctx     int
	NGL     int
	KVTypeK string
	KVTypeV string
	Batch   int
	UBatch  int
	NP      int
	MTP     bool
	MmprojB int64
}

type vramEst struct {
	GPUBytes     int64
	RAMOffload   int64
	TotalBytes   int64
	WeightsGPU   int64
	WeightsRAM   int64
	KVBytes      int64
	ComputeBytes int64
	MTPBytes     int64
	VRAMTotal    int64
	VRAMFree     int64
	RAMTotal     int64
	Ctx          int
	NativeCtx    int
	MaxCtxFit    int
	NLayers      int
	Arch         string
	Verdict      string
}

func kvBPE(t string) float64 {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "f32":
		return 4
	case "f16", "bf16", "":
		return 2
	case "q8_0":
		return 34.0 / 32.0
	case "q5_1":
		return 0.75
	case "q5_0":
		return 0.6875
	case "q4_1":
		return 0.625
	case "q4_0", "iq4_nl":
		return 0.5625
	default:
		return 2
	}
}

func layerNKVHeads(m ggufMeta, i int) int {
	if n := len(m.NKVHeadsPerLayer); n > 0 {
		if v := m.NKVHeadsPerLayer[i%n]; v > 0 {
			return v
		}
	}
	if m.NKVHeads > 0 {
		return m.NKVHeads
	}
	if m.NHeads > 0 {
		return m.NHeads
	}
	return 1
}

func layerIsSWA(m ggufMeta, i int) bool {
	n := len(m.SWAPattern)
	if n == 0 {
		return false
	}
	return m.SWAPattern[i%n] != 0
}

func kvHeadDims(m ggufMeta, swa bool) (klen, vlen int) {
	klen, vlen = m.KeyLength, m.ValueLength
	if swa {
		if m.KeyLengthSWA > 0 {
			klen = m.KeyLengthSWA
		}
		if m.ValueLengthSWA > 0 {
			vlen = m.ValueLengthSWA
		}
	}
	if klen > 0 && vlen > 0 {
		return klen, vlen
	}
	headDim := 0
	if m.NHeads > 0 && m.Embedding > 0 {
		headDim = m.Embedding / m.NHeads
	}
	if headDim <= 0 {
		headDim = 128
	}
	if klen <= 0 {
		klen = headDim
	}
	if vlen <= 0 {
		vlen = headDim
	}
	return klen, vlen
}

func estimateKVBytesPerLayer(m ggufMeta, ctx int, kvK, kvV string) int64 {
	nLayers := m.NLayers
	if nLayers <= 0 {
		nLayers = len(m.SWAPattern)
	}
	if nLayers <= 0 {
		nLayers = len(m.NKVHeadsPerLayer)
	}
	if nLayers <= 0 {
		return 0
	}
	bpeK, bpeV := kvBPE(kvK), kvBPE(kvV)
	var sum int64
	for i := 0; i < nLayers; i++ {
		swa := layerIsSWA(m, i)
		c := ctx
		if swa && m.SlidingWindow > 0 && m.SlidingWindow < c {
			c = m.SlidingWindow
		}
		if c <= 0 {
			continue
		}
		nKV := layerNKVHeads(m, i)
		klen, vlen := kvHeadDims(m, swa)
		sum += int64(float64(c) * float64(nKV) * (float64(klen)*bpeK + float64(vlen)*bpeV))
	}
	return sum
}

func estimateKVBytes(m ggufMeta, ctx int, kvK, kvV string) int64 {
	if ctx <= 0 {
		return 0
	}
	if len(m.SWAPattern) > 0 {
		return estimateKVBytesPerLayer(m, ctx, kvK, kvV)
	}
	nLayers := m.NLayers
	if nLayers <= 0 {
		return 0
	}
	shared := m.SharedKVLayers
	nKVLayers := nLayers - shared
	if nKVLayers < 1 {
		nKVLayers = 1
	}
	nKV := m.NKVHeads
	if nKV <= 0 {
		nKV = m.NHeads
	}
	if nKV <= 0 {
		nKV = 1
	}
	bpeK, bpeV := kvBPE(kvK), kvBPE(kvV)
	keyLen, valLen := m.KeyLength, m.ValueLength

	if m.KVLoraRank > 0 {
		nKVMLA := m.NKVHeads
		if nKVMLA <= 0 {
			nKVMLA = 1
		}
		rope := m.KeyLengthMLA
		if rope <= 0 {
			rope = 64
		}
		klen := keyLen
		if klen <= 0 {
			klen = m.KVLoraRank + rope
		}
		return int64(float64(nKVLayers) * float64(ctx) * float64(nKVMLA) * float64(klen) * bpeK)
	}

	if m.SSMInner > 0 && m.FullAttnEvery > 0 {
		nAttn := (nLayers + m.FullAttnEvery - 1) / m.FullAttnEvery
		if keyLen > 0 && valLen > 0 {
			return int64(float64(nAttn) * float64(ctx) * float64(nKV) * (float64(keyLen)*bpeK + float64(valLen)*bpeV))
		}
		headDim := 0
		if m.NHeads > 0 && m.Embedding > 0 {
			headDim = m.Embedding / m.NHeads
		}
		if headDim <= 0 {
			headDim = 128
		}
		return int64(float64(nAttn) * float64(ctx) * float64(nKV) * 2 * float64(headDim) * ((bpeK + bpeV) / 2))
	}

	if keyLen > 0 && valLen > 0 {
		return int64(float64(nKVLayers) * float64(ctx) * float64(nKV) * (float64(keyLen)*bpeK + float64(valLen)*bpeV))
	}
	headDim := 0
	if m.NHeads > 0 && m.Embedding > 0 {
		headDim = m.Embedding / m.NHeads
	}
	if headDim <= 0 {
		headDim = 128
	}
	return int64(2 * float64(nKV) * float64(headDim) * float64(nKVLayers) * float64(ctx) * ((bpeK + bpeV) / 2))
}

func estimateComputeBytes(m ggufMeta, ctx, ubatch, np int, kvK, kvV string) int64 {
	nEmbd := m.Embedding
	nVocab := m.VocabSize
	if nVocab <= 0 {
		nVocab = estDefaultVocab
	}
	ub := ubatch
	if ub <= 0 {
		ub = estDefaultUBatch
	}
	par := np
	if par <= 0 {
		par = 1
	}
	outBuf := int64(nVocab) * int64(ub) * 4
	act := int64(4) * int64(nEmbd) * int64(ub) * 4
	flat := int64(float64(act+outBuf) * estComputeSafety)
	if nEmbd <= 0 || ctx <= 0 {
		return flat
	}
	var perTok float64
	if kvBPE(kvK) < 2 || kvBPE(kvV) < 2 {
		rate := estCtxComputePerEmbd
		if m.KeyLengthMLA > 0 || m.KVLoraRank > 0 {
			rate = estCtxComputePerEmbdMLA
		}
		perTok = rate * float64(nEmbd) * (float64(ub) / estDefaultUBatch)
	} else {
		perTok = float64(ub) * 2 * estCtxComputeF16Mask
	}
	return flat + int64(perTok*float64(ctx))
}

func estimateMTPBytes(m ggufMeta, ctx int, kvK, kvV string) int64 {
	if m.NextN <= 0 || ctx <= 0 {
		return 0
	}
	nKV := m.NKVHeads
	if nKV <= 0 {
		nKV = m.NHeads
	}
	if nKV <= 0 || m.KeyLength <= 0 || m.ValueLength <= 0 {
		return 0
	}
	bpeK, bpeV := kvBPE(kvK), kvBPE(kvV)
	if bpeK < 2 {
		bpeK = 2
	}
	if bpeV < 2 {
		bpeV = 2
	}
	return int64(float64(m.NextN) * float64(nKV) * (float64(m.KeyLength)*bpeK + float64(m.ValueLength)*bpeV) * float64(ctx))
}

func estimateRun(m ggufMeta, opt estimateOpt) vramEst {
	ctx := opt.Ctx
	if ctx <= 0 {
		ctx = m.ContextLength
	}
	np := opt.NP
	if np <= 0 {
		np = 1
	}
	ub := opt.UBatch
	if ub <= 0 {
		ub = estDefaultUBatch
	}
	layers := m.NLayers
	if layers <= 0 {
		layers = 1
	}
	ngl := opt.NGL
	if ngl <= 0 {
		ngl = 999
	}
	gpuFrac := 1.0
	if ngl < 999 && ngl < layers {
		gpuFrac = float64(ngl) / float64(layers)
		if gpuFrac < 0 {
			gpuFrac = 0
		}
	}
	weights := m.FileBytes
	wGPU := int64(math.Round(float64(weights) * gpuFrac))
	wRAM := weights - wGPU
	kv := estimateKVBytes(m, ctx, opt.KVTypeK, opt.KVTypeV)
	comp := estimateComputeBytes(m, ctx, ub, np, opt.KVTypeK, opt.KVTypeV)
	mtp := int64(0)
	if opt.MTP {
		mtp = estimateMTPBytes(m, ctx, opt.KVTypeK, opt.KVTypeV)
	}
	gpu := wGPU + kv + comp + mtp + opt.MmprojB
	if wGPU > 0 {
		gpu += estCUDAReserveB
	}
	total := gpu + wRAM
	return vramEst{
		GPUBytes:     gpu,
		TotalBytes:   total,
		WeightsGPU:   wGPU,
		WeightsRAM:   wRAM,
		KVBytes:      kv,
		ComputeBytes: comp,
		MTPBytes:     mtp,
		Ctx:          ctx,
		NativeCtx:    m.ContextLength,
		NLayers:      m.NLayers,
		Arch:         m.Arch,
	}
}

func attachHardware(est *vramEst) {
	vramTotal, vramUsed, ramTotal := hubVRAMTotals()
	est.VRAMTotal = int64(vramTotal) * 1024 * 1024
	est.VRAMFree = int64(vramTotal-vramUsed) * 1024 * 1024
	if est.VRAMFree < 0 {
		est.VRAMFree = 0
	}
	est.RAMTotal = int64(ramTotal) * 1024 * 1024
	budget := int64(float64(est.VRAMTotal) * estVRAMFitFrac)
	if budget <= 0 {
		est.Verdict = "unknown"
		return
	}
	if est.GPUBytes > budget {
		est.RAMOffload = est.GPUBytes - budget
		if est.RAMOffload < 0 {
			est.RAMOffload = 0
		}
	}
	neededMB := float64(est.GPUBytes) / (1024 * 1024)
	est.Verdict = hubVerdict(neededMB, float64(vramTotal), float64(vramTotal-vramUsed), float64(ramTotal))
}

func maxCtxThatFits(m ggufMeta, opt estimateOpt, budget int64) int {
	native := m.ContextLength
	if native <= 0 {
		native = 131072
	}
	if budget <= 0 {
		return 0
	}
	lo, hi, best := 256, native, 0
	for lo <= hi {
		mid := (lo + hi) / 2
		o := opt
		o.Ctx = mid
		e := estimateRun(m, o)
		if e.GPUBytes <= budget {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return best
}

func estimateOptFromEnv(cfg map[string]string) estimateOpt {
	if cfg == nil {
		cfg = map[string]string{}
	}
	atoi := func(k string, def int) int {
		v := strings.TrimSpace(cfg[k])
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return def
		}
		return n
	}
	opt := estimateOpt{
		Ctx:     atoi("CTX", 0),
		NGL:     atoi("NGL", 999),
		Batch:   atoi("BATCH", 2048),
		UBatch:  atoi("UBATCH", estDefaultUBatch),
		NP:      atoi("NP", serverNP()),
		KVTypeK: strings.TrimSpace(cfg["KV_TYPE_K"]),
		KVTypeV: strings.TrimSpace(cfg["KV_TYPE_V"]),
	}
	if opt.KVTypeK == "" && opt.KVTypeV == "" {
		base := strings.TrimSpace(cfg["KV_TYPE"])
		opt.KVTypeK, opt.KVTypeV = base, base
	}
	extra := strings.Fields(cfg["EXTRA_ARGS"])
	for i, t := range extra {
		switch t {
		case "-np", "--parallel":
			if i+1 < len(extra) {
				if n, err := strconv.Atoi(extra[i+1]); err == nil && n > 0 {
					opt.NP = n
				}
			}
		case "--spec-type":
			if i+1 < len(extra) && strings.Contains(extra[i+1], "mtp") {
				opt.MTP = true
			}
		case "--cache-type-k", "-ctk":
			if i+1 < len(extra) {
				opt.KVTypeK = extra[i+1]
			}
		case "--cache-type-v", "-ctv":
			if i+1 < len(extra) {
				opt.KVTypeV = extra[i+1]
			}
		}
	}
	if opt.NP <= 0 {
		opt.NP = 1
	}
	if mm := strings.TrimSpace(cfg["MMPROJ"]); mm != "" {
		if p, err := resolveServeModelPath(mm); err == nil {
			if st, e := os.Stat(p); e == nil {
				opt.MmprojB = st.Size()
			}
		}
	}
	return opt
}

func estimateModel(model, content string) vramEst {
	cfg := parseEnv(content)
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(cfg["MODEL"])
	}
	if strings.TrimSpace(cfg["MODEL"]) == "" && model != "" {
		cfg["MODEL"] = model
	}
	meta := loadGGUFMetaForModel(model)
	opt := estimateOptFromEnv(cfg)
	if opt.Ctx <= 0 {
		opt.Ctx = meta.ContextLength
	}
	est := estimateRun(meta, opt)
	attachHardware(&est)
	budget := int64(float64(est.VRAMTotal) * estVRAMFitFrac)
	est.MaxCtxFit = maxCtxThatFits(meta, opt, budget)
	return est
}

func vramEstJSON(e vramEst) map[string]any {
	mb := func(b int64) int {
		if b <= 0 {
			return 0
		}
		return int((b + 512*1024) / (1024 * 1024))
	}
	return map[string]any{
		"ok":             true,
		"gpu_mb":         mb(e.GPUBytes),
		"total_mb":       mb(e.TotalBytes),
		"ram_offload_mb": mb(e.RAMOffload),
		"weights_gpu_mb": mb(e.WeightsGPU),
		"weights_ram_mb": mb(e.WeightsRAM),
		"kv_mb":          mb(e.KVBytes),
		"compute_mb":     mb(e.ComputeBytes),
		"mtp_mb":         mb(e.MTPBytes),
		"vram_total_mb":  mb(e.VRAMTotal),
		"vram_free_mb":   mb(e.VRAMFree),
		"ram_total_mb":   mb(e.RAMTotal),
		"ctx":            e.Ctx,
		"native_ctx":     e.NativeCtx,
		"max_ctx_fit":    e.MaxCtxFit,
		"n_layers":       e.NLayers,
		"arch":           e.Arch,
		"verdict":        e.Verdict,
	}
}
