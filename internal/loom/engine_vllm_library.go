package loom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func vllmHFHome() string {
	if home := os.Getenv("HF_HOME"); home != "" {
		return home
	}
	if cache := os.Getenv("XDG_CACHE_HOME"); cache != "" {
		return filepath.Join(cache, "huggingface")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "huggingface")
}

func vllmHFCache() string {
	if cache := os.Getenv("HF_HUB_CACHE"); cache != "" {
		return cache
	}
	return filepath.Join(vllmHFHome(), "hub")
}

func vllmHFToken() string {
	if token := os.Getenv("HF_TOKEN"); token != "" {
		return token
	}
	p := os.Getenv("HF_TOKEN_PATH")
	if p == "" {
		p = filepath.Join(vllmHFHome(), "token")
	}
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func vllmCacheName(model string) string { return "models--" + strings.ReplaceAll(model, "/", "--") }

type vllmCachedModel struct {
	ID        string   `json:"id"`
	Size      int64    `json:"size"` // actual regular files; HF blob symlinks aren't counted twice
	Servable  bool     `json:"servable"`
	Snapshots []string `json:"snapshots"`
}

// Root confines reads to the repository, including HF's ordinary snapshot →
// blob symlinks. A link outside the repository never makes a model servable.
func vllmSnapshotServable(root *os.Root, snapshot string) bool {
	if _, err := root.Lstat(path.Join("snapshots", snapshot, ".loom-prefetch")); !os.IsNotExist(err) {
		return false
	}
	config := path.Join("snapshots", snapshot, "config.json")
	st, err := root.Stat(config)
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		return false
	}
	entries, err := fs.ReadDir(root.FS(), path.Join("snapshots", snapshot))
	if err != nil {
		return false
	}
	weights := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".safetensors") {
			st, err := root.Stat(path.Join("snapshots", snapshot, e.Name()))
			weights = weights || err == nil && st.Mode().IsRegular() && st.Size() > 0
		}
	}
	if !weights {
		return false
	}
	// A partial sharded download must not look ready because its first shard exists.
	index := path.Join("snapshots", snapshot, "model.safetensors.index.json")
	if _, err := root.Stat(index); err == nil {
		f, err := root.Open(index)
		if err != nil {
			return false
		}
		defer f.Close()
		var data struct {
			WeightMap map[string]string `json:"weight_map"`
		}
		if json.NewDecoder(io.LimitReader(f, 16<<20)).Decode(&data) != nil || len(data.WeightMap) == 0 {
			return false
		}
		for _, name := range data.WeightMap {
			if !vllmSafeFile(name) {
				return false
			}
			st, err := root.Stat(path.Join("snapshots", snapshot, name))
			if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
				return false
			}
		}
	}
	return true
}

func listVLLMCache(cache string) ([]vllmCachedModel, error) {
	out := []vllmCachedModel{}
	root, err := os.OpenRoot(cache)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "models--") {
			continue
		}
		id := strings.ReplaceAll(strings.TrimPrefix(e.Name(), "models--"), "--", "/")
		if !validVLLMModel(id) {
			continue
		}
		repo, err := root.OpenRoot(e.Name())
		if err != nil {
			continue
		}
		m := vllmCachedModel{ID: id, Snapshots: []string{}}
		err = fs.WalkDir(repo.FS(), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type().IsRegular() {
				// repo.Lstat, not d.Info(): with Go 1.25 a nested Root's
				// DirEntry.Info resolves the path from the wrong directory.
				st, err := repo.Lstat(p)
				if err != nil {
					return err
				}
				m.Size += st.Size()
			}
			return nil
		})
		if err != nil {
			repo.Close()
			return nil, err
		}
		snapshots, _ := fs.ReadDir(repo.FS(), "snapshots")
		for _, s := range snapshots {
			if s.IsDir() && vllmSnapshotServable(repo, s.Name()) {
				m.Servable = true
				m.Snapshots = append(m.Snapshots, s.Name())
			}
		}
		repo.Close()
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func handleVLLMModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	models, err := listVLLMCache(vllmHFCache())
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "models": models, "cache": vllmHFCache()})
}

type vllmHFSibling struct {
	Name string `json:"rfilename"`
	Size int64  `json:"size"`
	LFS  *struct {
		Size int64 `json:"size"`
	} `json:"lfs"`
}
type vllmHFModel struct {
	ID          string          `json:"id"`
	ModelID     string          `json:"modelId"`
	SHA         string          `json:"sha"`
	Pipeline    string          `json:"pipeline_tag"`
	Tags        []string        `json:"tags"`
	Siblings    []vllmHFSibling `json:"siblings"`
	Safetensors *struct {
		Total int64 `json:"total"`
	} `json:"safetensors"`
}

func vllmHFGet(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "loom/"+Version)
	if token := vllmHFToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := hubClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Hugging Face HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(out)
}

func vllmHFWeights(m vllmHFModel) (hasConfig, hasWeights bool, size int64) {
	known := true
	for _, f := range m.Siblings {
		if f.Name == "config.json" {
			hasConfig = true
		}
		if !strings.Contains(f.Name, "/") && strings.HasSuffix(f.Name, ".safetensors") {
			hasWeights = true
			n := f.Size
			if n <= 0 && f.LFS != nil {
				n = f.LFS.Size
			}
			if n <= 0 {
				known = false
			} else {
				size += n
			}
		}
	}
	if !known {
		size = 0
	}
	return
}

func vllmQuantFamily(m vllmHFModel) string {
	s := strings.ToLower(m.ID + " " + strings.Join(m.Tags, " "))
	for _, quant := range []string{"awq", "gptq", "fp8"} {
		if strings.Contains(s, quant) {
			return quant
		}
	}
	return ""
}

func searchVLLMHub(ctx context.Context, query, quant string, gpus []map[string]any) ([]map[string]any, error) {
	if quant != "" && quant != "awq" && quant != "gptq" && quant != "fp8" {
		return nil, errors.New("invalid quantization filter")
	}
	u, err := url.Parse(hubAPIBase)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("filter", "safetensors")
	if quant != "" {
		q.Add("filter", quant)
	}
	q.Set("pipeline_tag", "text-generation")
	q.Set("search", query)
	q.Set("sort", "downloads")
	q.Set("direction", "-1")
	q.Set("limit", "30")
	q.Set("full", "true")
	q.Set("config", "true")
	u.RawQuery = q.Encode()
	var raw []vllmHFModel
	if err := vllmHFGet(ctx, u.String(), &raw); err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, m := range raw {
		if m.ID == "" {
			m.ID = m.ModelID
		}
		config, weights, size := vllmHFWeights(m)
		family := vllmQuantFamily(m)
		if !validVLLMModel(m.ID) || m.Pipeline != "text-generation" || !config || !weights || quant != "" && family != quant || vllmForeignFormat(m) {
			continue
		}
		var needed any // unknown remains null
		var weightBytes float64
		if size > 0 {
			weightBytes = float64(size)
		} else {
			params := hubParseParamsB(m.ID) * 1e9
			if m.Safetensors != nil && m.Safetensors.Total > 0 {
				params = float64(m.Safetensors.Total)
			}
			bpp := 2.0
			if family == "awq" || family == "gptq" {
				bpp = 0.6
			} else if family == "fp8" {
				bpp = 1.1
			}
			weightBytes = params * bpp
		}
		verdict := "unknown"
		if weightBytes > 0 {
			mb := weightBytes/(1<<20)*1.2 + 2048 // rough runtime + short-context KV allowance
			needed = mb
			maxGPU, minGPU := 0.0, 0.0
			allKnown := true
			for i, g := range gpus {
				total, _ := asInt(g["total"])
				allKnown = allKnown && total > 0
				if float64(total) > maxGPU {
					maxGPU = float64(total)
				}
				if i == 0 || float64(total) < minGPU {
					minGPU = float64(total)
				}
			}
			if maxGPU > 0 {
				verdict = "no"
				if mb <= maxGPU*0.9 {
					verdict = "fits"
				} else if len(gpus) > 1 && allKnown && mb <= minGPU*float64(len(gpus))*0.9 {
					verdict = "tensor_parallel"
				}
			}
		}
		var knownSize any
		if size > 0 {
			knownSize = size
		}
		out = append(out, map[string]any{"id": m.ID, "size": knownSize, "quantization": family, "servable": true, "estimated_vram_mb": needed, "verdict": verdict, "estimate_only": true})
	}
	return out, nil
}

func handleVLLMSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodGet) {
		return
	}
	gpus := liveGPUs()
	models, err := searchVLLMHub(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("quantization"), gpus)
	if err != nil {
		sendJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "models": models, "gpus": gpus})
}

var vllmRevision = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

func vllmSafeFile(name string) bool {
	return name != "" && name != "." && path.Clean(name) == name && !strings.ContainsAny(name, "\\:\x00") && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../")
}

func deleteVLLMCache(model string) error {
	if !validVLLMModel(model) {
		return errors.New("invalid model")
	}
	vllm.mu.Lock()
	defer vllm.mu.Unlock()
	if (vllm.cmd != nil || vllm.job == "start") && vllm.model == model || vllm.job == "start" && vllm.pendingModel == model {
		return errors.New("model used by vLLM")
	}
	vllmDownloads.mu.Lock()
	defer vllmDownloads.mu.Unlock()
	if vllmDownloads.state != nil && !vllmDownloads.state.Finished && vllmDownloads.state.Model == model {
		return errors.New("download in progress")
	}
	root, err := os.OpenRoot(vllmHFCache())
	if err != nil {
		return err
	}
	defer root.Close()
	name := vllmCacheName(model)
	st, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid cache directory")
	}
	return root.RemoveAll(name)
}

func handleVLLMDelete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if err := deleteVLLMCache(req.Model); err != nil {
		sendJSON(w, 409, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true})
}

// vllmForeignFormat écarte les dépôts faits pour d'autres moteurs (MLX pour
// Apple, GGUF, bitsandbytes) : leurs safetensors ne se servent pas avec vLLM.
func vllmForeignFormat(m vllmHFModel) bool {
	id := strings.ToLower(m.ID)
	if strings.Contains(id, "mlx") || strings.Contains(id, "gguf") || strings.Contains(id, "bnb-4bit") {
		return true
	}
	for _, tag := range m.Tags {
		switch strings.ToLower(tag) {
		case "mlx", "gguf":
			return true
		}
	}
	return false
}
