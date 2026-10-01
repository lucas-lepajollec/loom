package loom

import (
	"os"
	"strconv"
	"strings"
)

// Estimation VRAM d'un GGUF, calquée sur Unsloth Studio (llama_cpp.py) :
// poids du fichier + cache KV selon l'architecture (MLA / hybride Mamba /
// GQA / repli) + buffer de compute + réserve CUDA + tête MTP si elle est
// engagée. Ce n'est pas une mesure llama.cpp, mais ça suit les mêmes axes
// que le panneau (CTX, KV, NGL, batch, slots).

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
