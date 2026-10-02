package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type vllmDownload struct {
	Model     string `json:"model"`
	Revision  string `json:"revision"`
	File      string `json:"file"`
	Total     *int64 `json:"total"` // null when HF does not report every file size
	Done      int64  `json:"done"`
	Finished  bool   `json:"finished"`
	Canceled  bool   `json:"canceled"`
	Error     string `json:"error"`
	StartedAt int64  `json:"started_at"`
	counter   int64
	cancel    context.CancelFunc
}

var vllmDownloads struct {
	mu    sync.Mutex
	state *vllmDownload
}

func vllmPrefetchFile(name string) bool {
	if !vllmSafeFile(name) || strings.Contains(name, "/") {
		return false
	}
	for _, ext := range []string{".safetensors", ".json", ".model", ".txt", ".tiktoken", ".jinja", ".jinja2", ".py"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// HTTP transfer shares Loom's client, token and byte-copy/progress machinery.
// Incomplete files survive cancellation/restarts, unlike GGUF's explicit-cleanup
// policy. Requests are pinned to a commit, making persistent resume safe.
func fetchVLLMFile(ctx context.Context, root *os.Root, dest, rawURL string, expected int64, done *int64) error {
	if st, err := root.Stat(dest); err == nil && st.Mode().IsRegular() && (expected <= 0 || st.Size() == expected) {
		atomic.AddInt64(done, st.Size())
		return nil
	}
	tmp := dest + ".loom-incomplete"
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	pos := st.Size()
	if expected > 0 && pos > expected {
		if err := f.Truncate(0); err != nil {
			return err
		}
		pos = 0
	}
	atomic.AddInt64(done, pos)
	if expected > 0 && pos == expected {
		if err := f.Close(); err != nil {
			return err
		}
		return root.Rename(tmp, dest)
	}
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		rng := ""
		if pos > 0 {
			rng = fmt.Sprintf("bytes=%d-", pos)
		}
		req, err := dlRequest(ctx, rawURL, rng)
		if err != nil {
			return err
		}
		if token := vllmHFToken(); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := dlClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && pos > 0 && contentRangeTotal(resp.Header.Get("Content-Range")) == pos && (expected <= 0 || expected == pos) {
			resp.Body.Close()
			if err := f.Close(); err != nil {
				return err
			}
			return root.Rename(tmp, dest)
		}
		if resp.StatusCode != 200 && resp.StatusCode != 206 {
			resp.Body.Close()
			return fmt.Errorf("Hugging Face HTTP %d", resp.StatusCode)
		}
		if resp.StatusCode == 200 && pos > 0 {
			if err := f.Truncate(0); err != nil {
				resp.Body.Close()
				return err
			}
			atomic.AddInt64(done, -pos)
			pos = 0
		}
		if resp.StatusCode == 206 {
			var start, end, total int64
			if _, err := fmt.Sscanf(resp.Header.Get("Content-Range"), "bytes %d-%d/%d", &start, &end, &total); err != nil || start != pos || end < start || total <= end || expected > 0 && expected != total {
				resp.Body.Close()
				return errors.New("invalid Hugging Face resume range")
			}
			if expected <= 0 {
				expected = total
			}
		} else if resp.ContentLength >= 0 {
			if expected > 0 && expected != resp.ContentLength {
				resp.Body.Close()
				return errors.New("inconsistent Hugging Face size")
			}
			if expected <= 0 {
				expected = resp.ContentLength
			}
		}
		n, copyErr := dlCopy(f, resp.Body, pos, done)
		resp.Body.Close()
		pos += n
		if expected > 0 && pos > expected {
			return errors.New("Hugging Face file too large")
		}
		if copyErr == nil && (expected <= 0 || pos == expected) {
			if err := f.Sync(); err != nil {
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			return root.Rename(tmp, dest)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return io.ErrUnexpectedEOF
}

func runVLLMPrefetch(ctx context.Context, st *vllmDownload) {
	err := prefetchVLLM(ctx, st)
	vllmDownloads.mu.Lock()
	st.Finished, st.Canceled, st.Done = true, errors.Is(err, context.Canceled), atomic.LoadInt64(&st.counter)
	if err != nil && !st.Canceled {
		st.Error = err.Error()
	}
	st.cancel()
	st.cancel = nil
	vllmDownloads.mu.Unlock()
}

func prefetchVLLM(ctx context.Context, st *vllmDownload) error {
	var model vllmHFModel
	if err := vllmHFGet(ctx, strings.TrimRight(hubAPIBase, "/")+"/"+st.Model+"?blobs=true", &model); err != nil {
		return err
	}
	config, weights, _ := vllmHFWeights(model)
	if !config || !weights || !vllmRevision.MatchString(model.SHA) {
		return errors.New("repository missing config.json, safetensors weights or immutable revision")
	}
	files := []vllmHFSibling{}
	var total int64
	known := true
	seen := map[string]bool{}
	for _, f := range model.Siblings {
		if !vllmPrefetchFile(f.Name) || seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		if f.Size <= 0 && f.LFS != nil {
			f.Size = f.LFS.Size
		}
		if f.Size <= 0 {
			known = false
		} else {
			total += f.Size
		}
		files = append(files, f)
	}
	vllmDownloads.mu.Lock()
	st.Revision = model.SHA
	if known {
		st.Total = &total
	}
	vllmDownloads.mu.Unlock()
	if err := os.MkdirAll(vllmHFCache(), 0o755); err != nil {
		return err
	}
	cache, err := os.OpenRoot(vllmHFCache())
	if err != nil {
		return err
	}
	defer cache.Close()
	name := vllmCacheName(st.Model)
	if err := cache.MkdirAll(name, 0o755); err != nil {
		return err
	}
	info, err := cache.Lstat(name)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid cache directory")
	}
	root, err := cache.OpenRoot(name)
	if err != nil {
		return err
	}
	defer root.Close()
	snapshot := path.Join("snapshots", model.SHA)
	if err := root.MkdirAll(snapshot, 0o755); err != nil {
		return err
	}
	if err := root.MkdirAll("refs", 0o755); err != nil {
		return err
	}
	if known {
		remaining := total
		for _, f := range files {
			for _, suffix := range []string{"", ".loom-incomplete"} {
				if info, err := root.Stat(path.Join(snapshot, f.Name) + suffix); err == nil && info.Mode().IsRegular() && info.Size() <= f.Size {
					remaining -= info.Size()
					break
				}
			}
		}
		if err := checkDiskSpace(root.Name(), remaining); err != nil {
			return err
		}
	}
	marker := path.Join(snapshot, ".loom-prefetch")
	if err := root.WriteFile(marker, []byte("incomplete"), 0o600); err != nil {
		return err
	}
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		vllmDownloads.mu.Lock()
		st.File = f.Name
		vllmDownloads.mu.Unlock()
		rawURL := strings.TrimRight(hubWebBase, "/") + "/" + st.Model + "/resolve/" + model.SHA + "/" + url.PathEscape(f.Name)
		if err := fetchVLLMFile(ctx, root, path.Join(snapshot, f.Name), rawURL, f.Size, &st.counter); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.WriteFile("refs/main.loom-tmp", []byte(model.SHA), 0o600); err != nil {
		return err
	}
	if err := root.Rename("refs/main.loom-tmp", "refs/main"); err != nil {
		return err
	}
	return root.Remove(marker)
}

func handleVLLMDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		vllmDownloads.mu.Lock()
		var state *vllmDownload
		if st := vllmDownloads.state; st != nil {
			state = &vllmDownload{Model: st.Model, Revision: st.Revision, File: st.File, Total: st.Total,
				Done: atomic.LoadInt64(&st.counter), Finished: st.Finished, Canceled: st.Canceled,
				Error: st.Error, StartedAt: st.StartedAt}
		}
		vllmDownloads.mu.Unlock()
		sendJSON(w, 200, map[string]any{"ok": true, "download": state})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Model string `json:"model"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	if !validVLLMModel(req.Model) {
		sendJSON(w, 400, map[string]any{"ok": false, "error": "invalid model"})
		return
	}
	vllmDownloads.mu.Lock()
	defer vllmDownloads.mu.Unlock()
	if vllmDownloads.state != nil && !vllmDownloads.state.Finished {
		sendJSON(w, 409, map[string]any{"ok": false, "error": "download already in progress"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	st := &vllmDownload{Model: req.Model, StartedAt: time.Now().UnixMilli(), cancel: cancel}
	vllmDownloads.state = st
	go runVLLMPrefetch(ctx, st)
	sendJSON(w, 202, map[string]any{"ok": true, "model": req.Model})
}

func handleVLLMDownloadCancel(w http.ResponseWriter, r *http.Request) {
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
	vllmDownloads.mu.Lock()
	defer vllmDownloads.mu.Unlock()
	st := vllmDownloads.state
	if st == nil || st.Model != req.Model || st.cancel == nil {
		sendJSON(w, 404, map[string]any{"ok": false, "error": "download not found"})
		return
	}
	st.cancel()
	sendJSON(w, 200, map[string]any{"ok": true})
}
