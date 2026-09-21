package ajean

import (
	"os"
	"path/filepath"
	"strings"
)

type modelCaps struct {
	NativeCtx     int      `json:"native_ctx"`
	NLayers       int      `json:"n_layers"`
	Thinks        bool     `json:"thinks"`
	HasEffort     bool     `json:"has_effort"`
	Effort        []string `json:"effort,omitempty"`
	EffortDefault string   `json:"effort_default,omitempty"`
	Vision        bool     `json:"vision"`
	Mmproj        []string `json:"mmproj,omitempty"`
	NextN         int      `json:"nextn,omitempty"`
	Arch          string   `json:"arch,omitempty"`
}

func capsForModel(model string) modelCaps {
	meta := loadGGUFMetaForModel(model)
	c := modelCaps{
		NativeCtx:     meta.ContextLength,
		NLayers:       meta.NLayers,
		Thinks:        meta.Thinks,
		HasEffort:     meta.HasEffort,
		Effort:        meta.EffortLevels,
		EffortDefault: meta.EffortDefault,
		NextN:         meta.NextN,
		Arch:          meta.Arch,
	}
	c.Mmproj = findMmprojNear(model)
	c.Vision = len(c.Mmproj) > 0
	return c
}

func findMmprojNear(model string) []string {
	path := strings.TrimSpace(model)
	if path == "" {
		return nil
	}
	if p, err := resolveServeModelPath(path); err == nil {
		path = p
	}
	dir := filepath.Dir(path)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(strings.ToLower(n), ".gguf") {
			continue
		}
		if !strings.Contains(strings.ToLower(n), "mmproj") {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	return out
}
