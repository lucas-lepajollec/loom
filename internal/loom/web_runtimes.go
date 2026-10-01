package loom

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Optional actions dispatch through registered adapters. All routes, including
// legacy aliases, retain web auth, strict JSON, no-store and vault-lock checks.
func handleRuntimeConnect(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	adapter, ok := runtimeForAction(w, r.PathValue("id"))
	if !ok {
		return
	}
	connector, ok := adapter.(Connectable)
	if !ok || !hasRuntimeCapability(adapter.Descriptor(), "connect") {
		sendJSON(w, http.StatusNotImplemented, map[string]any{"ok": false, "error": "connexion non disponible pour ce runtime"})
		return
	}
	var req struct {
		Consent bool `json:"consent"`
	}
	if !workspaceDecode(w, r, &req) {
		return
	}
	// Refuse before invoking any adapter, even if it forgets its own check.
	if !req.Consent {
		sendJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "confirmez la lecture du catalogue natif"})
		return
	}
	models, err := connector.Connect(r.Context(), req.Consent)
	if !usageVaultAccess(w) {
		return
	}
	if err != nil {
		sendRuntimeActionError(w, err)
		return
	}
	sendJSON(w, http.StatusOK, map[string]any{"ok": true, "models": models})
}

func handleRuntimeQuota(w http.ResponseWriter, r *http.Request) {
	if !workspaceMethod(w, r, http.MethodPost) || !usageVaultAccess(w) {
		return
	}
	adapter, ok := runtimeForAction(w, r.PathValue("id"))
	if !ok {
		return
	}
	var req struct{}
	if !workspaceDecode(w, r, &req) {
		return
	}
	refreshRuntimeQuota(w, r.Context(), adapter)
}

func runtimeForAction(w http.ResponseWriter, id string) (RuntimeAdapter, bool) {
	adapter, ok := registeredRuntimes.lookup(id)
	if !ok {
		sendJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "runtime introuvable"})
	}
	return adapter, ok
}

// Shared by the path-based action and the historical body-based refresh API.
// The existing 30-second throttle, cache and stale readings remain unchanged.
func refreshRuntimeQuota(w http.ResponseWriter, ctx context.Context, adapter RuntimeAdapter) {
	reader, ok := adapter.(QuotaReader)
	d := adapter.Descriptor()
	if !ok || !hasRuntimeCapability(d, "quota") {
		sendJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "lecture des quotas non disponible pour ce harness"})
		return
	}
	quotaCache.Lock()
	defer quotaCache.Unlock()
	if time.Since(quotaCache.attempts[d.ID]) < 30*time.Second {
		sendJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "attendez 30 secondes entre deux lectures"})
		return
	}
	quotaCache.attempts[d.ID] = time.Now()
	q, err := reader.Quota(ctx)
	if !usageVaultAccess(w) {
		return
	}
	if err != nil {
		previous := quotaCache.items[d.ID]
		previous.RuntimeID = d.ID
		previous.Error = err.Error()
		quotaCache.items[d.ID] = previous
		sendRuntimeActionError(w, err)
		return
	}
	q.RuntimeID = d.ID
	quotaCache.items[d.ID] = q
	sendJSON(w, http.StatusOK, map[string]any{"ok": true, "quota": q})
}

// Keep adapter storage errors' historical HTTP status without exposing a store
// error or credential value. This in-package bridge precedes the package split.
type runtimeActionError struct {
	status  int
	message string
}

func (err runtimeActionError) Error() string { return err.message }

func sendRuntimeActionError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	var actionError runtimeActionError
	if errors.As(err, &actionError) {
		status = actionError.status
	}
	sendJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
}

func runtimeVaultError() error {
	if memEncActive() && !memUnlocked() {
		return runtimeActionError{status: http.StatusLocked, message: "déverrouillez le coffre Loom pour consulter les comptes"}
	}
	return nil
}
