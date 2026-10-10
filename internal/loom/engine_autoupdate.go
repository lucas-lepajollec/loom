package loom

import (
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Automatic engine updates. Opt-in, on the machine that owns the engine.
// An update restarts the engine, so it is applied only when that interrupts
// nothing: engine stopped, or no model loaded. Otherwise it waits ("prête") and
// the user can apply it at once from the Engine settings.

const engineAutoState = "engine_auto_update"

type engineAuto struct {
	Auto      bool   `json:"auto"`
	CheckedAt int64  `json:"checked_at,omitempty"`
	Pending   string `json:"pending,omitempty"` // version waiting for a free engine
	LastAt    int64  `json:"last_at,omitempty"`
	LastFrom  string `json:"last_from,omitempty"`
	LastTo    string `json:"last_to,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

var engineAutoMu sync.Mutex

func loadEngineAuto() engineAuto {
	var a engineAuto
	_ = getStoreJSON(bkState, engineAutoState, &a)
	return a
}

func saveEngineAuto(a engineAuto) { _ = putStoreJSON(bkState, engineAutoState, a) }

// engineFree reports whether restarting the engine would interrupt nothing.
// Unknown counts as busy.
func engineFree() bool {
	if !serviceIsActive() {
		return true
	}
	models, err := routerModelsWithTimeout(false, 5*time.Second)
	if err != nil {
		return false
	}
	for _, m := range models {
		if m.Status == "loaded" || m.Status == "loading" {
			return false
		}
	}
	return true
}

// engineUpdateAvailable says what an update would install: the latest
// official build for prebuilt installs, or new commits for source builds.
// kind is "prebuilt", "source" or "" (nothing Loom can update).
func engineUpdateAvailable() (kind, from, to string, err error) {
	bin := strings.TrimSpace(ReadConfig()["BIN"])
	if pb := prebuiltServerBin(); pb != "" && bin == pb {
		tag, assets, err := fetchLlamaLatest()
		if err != nil {
			return "prebuilt", "", "", err
		}
		cur, _ := prebuiltVersion()
		if cur == tag {
			_, _, label, _, err := pickPrebuilt(assets)
			if err != nil {
				return "prebuilt", cur, "", err
			}
			if llamaBuildHealth(context.Background(), pb, "", buildPlan{backend: prebuiltBackend(label)}).Healthy {
				return "prebuilt", cur, "", nil
			}
		}
		return "prebuilt", cur, tag, nil
	}
	repo := engineRepo(bin)
	if repo == "" || !isDir(filepath.Join(repo, ".git")) {
		return "", "", "", nil
	}
	branch := gitOutput(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" || branch == "HEAD" {
		branch = "master"
	}
	if err := runStep("git fetch", repo, "git", "fetch", "origin", "--quiet"); err != nil {
		return "source", "", "", err
	}
	behind, _ := strconv.Atoi(gitOutput(repo, "rev-list", "--count", "HEAD..origin/"+branch))
	from = gitOutput(repo, "rev-parse", "--short", "HEAD")
	if behind == 0 && llamaBuildHealth(context.Background(), llamaServerBin(repo), repo, detectBuildPlan()).Healthy {
		return "source", from, "", nil
	}
	return "source", from, gitOutput(repo, "rev-parse", "--short", "origin/"+branch), nil
}

// engineAutoTick runs one check; free and apply are injectable for tests.
func engineAutoTick(now time.Time, check func() (string, string, string, error), free func() bool, apply func(kind string) error) engineAuto {
	engineAutoMu.Lock()
	defer engineAutoMu.Unlock()
	a := loadEngineAuto()
	if !a.Auto || currentEngineNode() != nil {
		return a
	}
	kind, from, to, err := check()
	a.CheckedAt = now.UnixMilli()
	if err != nil || to == "" || kind == "" {
		a.Pending = ""
		if err != nil {
			a.LastError = err.Error()
		}
		saveEngineAuto(a)
		return a
	}
	if !free() {
		a.Pending = to
		saveEngineAuto(a)
		return a
	}
	a.Pending, a.LastAt, a.LastFrom, a.LastTo, a.LastError = "", now.UnixMilli(), from, to, ""
	if err := apply(kind); err != nil {
		a.LastError = err.Error()
	}
	saveEngineAuto(a)
	return a
}

func applyEngineUpdate(kind string) error {
	if kind == "prebuilt" {
		return startLcJob("prebuilt", lcRunPrebuilt)
	}
	return startLcJob("update", func() { lcRunUpdate(false) })
}

// engineAutoLoop checks every 30 minutes while enabled (a pending update is
// applied as soon as the engine becomes free); upstream is queried at most
// every 6 hours.
func engineAutoLoop() {
	var lastUpstream time.Time
	var cached struct{ kind, from, to string }
	for {
		time.Sleep(30 * time.Minute)
		check := func() (string, string, string, error) {
			if time.Since(lastUpstream) < 6*time.Hour {
				return cached.kind, cached.from, cached.to, nil
			}
			kind, from, to, err := engineUpdateAvailable()
			if err == nil {
				lastUpstream, cached.kind, cached.from, cached.to = time.Now(), kind, from, to
			}
			return kind, from, to, err
		}
		if a := engineAutoTick(time.Now(), check, engineFree, applyEngineUpdate); a.LastAt != 0 && a.Pending == "" {
			cached.to = "" // applied: re-check upstream next time
			lastUpstream = time.Time{}
		}
	}
}

// GET: setting and last result. POST {auto}: switch.
func handleEngineAuto(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sendJSON(w, 200, map[string]any{"ok": true, "state": loadEngineAuto()})
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
	engineAutoMu.Lock()
	a := loadEngineAuto()
	a.Auto = req.Auto
	if !req.Auto {
		a.Pending = ""
	}
	saveEngineAuto(a)
	engineAutoMu.Unlock()
	sendJSON(w, 200, map[string]any{"ok": true, "state": a})
}
