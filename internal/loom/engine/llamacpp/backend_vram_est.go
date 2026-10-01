package llamacpp

import (
	"math"
	"strings"
)

const (
	EstCUDAReserveB         = 320 * 1024 * 1024
	EstComputeSafety        = 1.15
	EstDefaultUBatch        = 512
	EstCtxComputePerEmbd    = 2.25
	EstCtxComputePerEmbdMLA = 1.25
	EstCtxComputeF16Mask    = 1.5
	EstVRAMFitFrac          = 0.97
	EstDefaultVocab         = 128000
)

type EstimateOptions struct {
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

type VRAMEstimate struct {
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

func KVBPE(t string) float64 {
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

func LayerNKVHeads(m GGUFMeta, i int) int {
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

func LayerIsSWA(m GGUFMeta, i int) bool {
	n := len(m.SWAPattern)
	if n == 0 {
		return false
	}
	return m.SWAPattern[i%n] != 0
}

func KVHeadDims(m GGUFMeta, swa bool) (klen, vlen int) {
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

func EstimateKVBytesPerLayer(m GGUFMeta, ctx int, kvK, kvV string) int64 {
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
	bpeK, bpeV := KVBPE(kvK), KVBPE(kvV)
	var sum int64
	for i := 0; i < nLayers; i++ {
		swa := LayerIsSWA(m, i)
		c := ctx
		if swa && m.SlidingWindow > 0 && m.SlidingWindow < c {
			c = m.SlidingWindow
		}
		if c <= 0 {
			continue
		}
		nKV := LayerNKVHeads(m, i)
		klen, vlen := KVHeadDims(m, swa)
		sum += int64(float64(c) * float64(nKV) * (float64(klen)*bpeK + float64(vlen)*bpeV))
	}
	return sum
}

func EstimateKVBytes(m GGUFMeta, ctx int, kvK, kvV string) int64 {
	if ctx <= 0 {
		return 0
	}
	if len(m.SWAPattern) > 0 {
		return EstimateKVBytesPerLayer(m, ctx, kvK, kvV)
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
	bpeK, bpeV := KVBPE(kvK), KVBPE(kvV)
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

func EstimateComputeBytes(m GGUFMeta, ctx, ubatch, np int, kvK, kvV string) int64 {
	nEmbd := m.Embedding
	nVocab := m.VocabSize
	if nVocab <= 0 {
		nVocab = EstDefaultVocab
	}
	ub := ubatch
	if ub <= 0 {
		ub = EstDefaultUBatch
	}
	par := np
	if par <= 0 {
		par = 1
	}
	outBuf := int64(nVocab) * int64(ub) * 4
	act := int64(4) * int64(nEmbd) * int64(ub) * 4
	flat := int64(float64(act+outBuf) * EstComputeSafety)
	if nEmbd <= 0 || ctx <= 0 {
		return flat
	}
	var perTok float64
	if KVBPE(kvK) < 2 || KVBPE(kvV) < 2 {
		rate := EstCtxComputePerEmbd
		if m.KeyLengthMLA > 0 || m.KVLoraRank > 0 {
			rate = EstCtxComputePerEmbdMLA
		}
		perTok = rate * float64(nEmbd) * (float64(ub) / EstDefaultUBatch)
	} else {
		perTok = float64(ub) * 2 * EstCtxComputeF16Mask
	}
	return flat + int64(perTok*float64(ctx))
}

func EstimateMTPBytes(m GGUFMeta, ctx int, kvK, kvV string) int64 {
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
	bpeK, bpeV := KVBPE(kvK), KVBPE(kvV)
	if bpeK < 2 {
		bpeK = 2
	}
	if bpeV < 2 {
		bpeV = 2
	}
	return int64(float64(m.NextN) * float64(nKV) * (float64(m.KeyLength)*bpeK + float64(m.ValueLength)*bpeV) * float64(ctx))
}

func EstimateRun(m GGUFMeta, opt EstimateOptions) VRAMEstimate {
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
		ub = EstDefaultUBatch
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
	kv := EstimateKVBytes(m, ctx, opt.KVTypeK, opt.KVTypeV)
	comp := EstimateComputeBytes(m, ctx, ub, np, opt.KVTypeK, opt.KVTypeV)
	mtp := int64(0)
	if opt.MTP {
		mtp = EstimateMTPBytes(m, ctx, opt.KVTypeK, opt.KVTypeV)
	}
	gpu := wGPU + kv + comp + mtp + opt.MmprojB
	if wGPU > 0 {
		gpu += EstCUDAReserveB
	}
	total := gpu + wRAM
	return VRAMEstimate{
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

func MaxCtxThatFits(m GGUFMeta, opt EstimateOptions, budget int64) int {
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
		e := EstimateRun(m, o)
		if e.GPUBytes <= budget {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return best
}

func VRAMEstimateJSON(e VRAMEstimate) map[string]any {
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
