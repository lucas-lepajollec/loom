package loom

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const vllmAutoState = "vllm_auto_update"

var vllmAutoMu sync.Mutex

func vllmVersion() string {
	if !vllmInstalled() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Metadata only: importing vllm would initialize GPU libraries.
	out, err := hideCmd(exec.CommandContext(ctx, filepath.Join(vllmDir(), "bin", "python"), "-c", "from importlib.metadata import version; print(version('vllm'))")).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func loadVLLMAuto() engineAuto {
	var a engineAuto
	_ = getStoreJSON(bkState, vllmAutoState, &a)
	return a
}

func handleVLLMAuto(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "state": loadVLLMAuto()})
		return
	}
	if !workspaceMethod(w, r, http.MethodPost) {
		return
	}
	var req struct {
		Auto bool `json:"auto"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	vllmAutoMu.Lock()
	a := loadVLLMAuto()
	a.Auto = req.Auto
	if !a.Auto {
		a.Pending = ""
	}
	err := putStoreJSON(bkState, vllmAutoState, a)
	vllmAutoMu.Unlock()
	if err != nil {
		sendJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	sendJSON(w, 200, map[string]any{"ok": true, "state": a})
}

func vllmLatest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://pypi.org/pypi/vllm/json", nil)
	if err != nil {
		return "", err
	}
	resp, err := hubClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("PyPI HTTP %d", resp.StatusCode)
	}
	var data struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&data)
	if err == nil && data.Info.Version == "" {
		err = fmt.Errorf("unknown PyPI version")
	}
	return data.Info.Version, err
}

// Called every 30 minutes. Upstream at most every six hours; both reservation
// and the serving check use the lifecycle mutex, so a concurrent start wins
// or gets a conflict, never an environment upgraded beneath a live process.
func vllmAutoTick(now time.Time, check func(context.Context) (string, error)) {
	vllmAutoMu.Lock()
	a := loadVLLMAuto()
	if !a.Auto || !vllmInstalled() || now.UnixMilli()-a.CheckedAt < (6*time.Hour).Milliseconds() && a.Pending == "" {
		vllmAutoMu.Unlock()
		return
	}
	vllm.mu.Lock()
	busy := vllm.cmd != nil || vllm.job != ""
	vllm.mu.Unlock()
	if busy {
		vllmAutoMu.Unlock()
		return
	}
	to := a.Pending
	if now.UnixMilli()-a.CheckedAt >= (6 * time.Hour).Milliseconds() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		var err error
		to, err = check(ctx)
		cancel()
		a.CheckedAt = now.UnixMilli()
		if err != nil {
			a.LastError = err.Error()
			_ = putStoreJSON(bkState, vllmAutoState, a)
			vllmAutoMu.Unlock()
			return
		}
	}
	from := vllmVersion()
	if from == "" || strings.Split(from, "+")[0] == to {
		a.Pending, a.LastError = "", ""
		if err := vllmEnvironmentHealth(context.Background()); err != nil {
			a.LastError = err.Error()
		}
		_ = putStoreJSON(bkState, vllmAutoState, a)
		vllmAutoMu.Unlock()
		return
	}
	a.Pending = to
	if vllmRequirements() != "" {
		a.LastError = vllmRequirements()
		_ = putStoreJSON(bkState, vllmAutoState, a)
		vllmAutoMu.Unlock()
		return
	}
	if err := vllm.reserve("update", ""); err != nil {
		_ = putStoreJSON(bkState, vllmAutoState, a)
		vllmAutoMu.Unlock()
		return
	}
	_ = putStoreJSON(bkState, vllmAutoState, a)
	vllmAutoMu.Unlock()
	err := vllm.installOrUpdate(true)
	actual := vllmVersion()
	vllmAutoMu.Lock()
	a = loadVLLMAuto() // keep a user's toggle changed during the update
	a.Pending, a.LastAt, a.LastFrom, a.LastTo, a.LastError = "", now.UnixMilli(), from, actual, ""
	if err != nil {
		a.LastError = err.Error()
	}
	_ = putStoreJSON(bkState, vllmAutoState, a)
	vllmAutoMu.Unlock()
}

func vllmAutoLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for now := range ticker.C {
		vllmAutoTick(now, vllmLatest)
	}
}
