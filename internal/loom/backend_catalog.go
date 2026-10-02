package loom

// backend_catalog.go — catalogue local de modèles recommandés combiné avec
// les infos matérielles locales, pour proposer des modèles adaptés à la machine.

import (
	"encoding/json"
	"net/http"
	"runtime"
)

type catalogModel struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Params   string  `json:"params"`
	Quant    string  `json:"quant"`
	SizeGB   float64 `json:"size_gb"`
	MinRAMGB float64 `json:"min_ram_gb"`
	URL      string  `json:"url"`
	Note     string  `json:"note"`
}

type catalog struct {
	Version int            `json:"version"`
	Models  []catalogModel `json:"models"`
}

type hardwareInfo struct {
	OS    string  `json:"os"`
	Arch  string  `json:"arch"`
	RAMGB float64 `json:"ram_gb"`
}

// fetchCatalog charge le catalogue local embarqué.
func fetchCatalog() catalog {
	var c catalog
	_ = json.Unmarshal([]byte(fallbackCatalogJSON), &c)
	return c
}

func detectHardware() hardwareInfo {
	return hardwareInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, RAMGB: totalRAMGB()}
}

// handleCatalog : renvoie le catalogue + le matériel local. L'UI s'en sert pour
// marquer chaque modèle « tient / trop lourd » et proposer le bon par défaut.
func handleCatalog(w http.ResponseWriter, r *http.Request) {
	resp := struct {
		Hardware hardwareInfo   `json:"hardware"`
		Models   []catalogModel `json:"models"`
	}{Hardware: detectHardware(), Models: fetchCatalog().Models}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// fallbackCatalogJSON — repli minimal si loom.local/models.json est injoignable.
const fallbackCatalogJSON = `{
  "version": 1,
  "models": [
    {"id":"qwen2.5-3b-instruct-q4","name":"Qwen2.5 3B Instruct","params":"3B","quant":"Q4_K_M","size_gb":2.1,"min_ram_gb":6,"url":"https://huggingface.co/bartowski/Qwen2.5-3B-Instruct-GGUF/resolve/main/Qwen2.5-3B-Instruct-Q4_K_M.gguf","note":"Lightweight and fast — ideal for small machines."},
    {"id":"qwen2.5-7b-instruct-q4","name":"Qwen2.5 7B Instruct","params":"7B","quant":"Q4_K_M","size_gb":4.7,"min_ram_gb":10,"url":"https://huggingface.co/bartowski/Qwen2.5-7B-Instruct-GGUF/resolve/main/Qwen2.5-7B-Instruct-Q4_K_M.gguf","note":"More capable — recommended with 16 GB of RAM or a GPU."}
  ]
}`
