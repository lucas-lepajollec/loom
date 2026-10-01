package llamacpp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RouterState is Loom-owned persistence; the router has no database singleton.
type RouterState interface {
	GetString(key string) string
	PutString(key, value string) error
	GetJSON(key string, dst any) bool
	PutJSON(key string, value any) error
}

// Router holds the explicit inputs for one external llama-server router.
// Lock is shared by all views of that owner and serializes INI writes/loads.
// Authorize and APIKey preserve Loom's credential policy; SetLastError reports
// loading failures to the existing service status. All accessors are required.
// Process startup, stop and service supervision belong to the caller.
type Router struct {
	BinaryPath   string
	BackendPort  int
	INIPath      string
	ModelsMax    int
	State        RouterState
	Lock         sync.Locker
	Authorize    func(*http.Request)
	APIKey       func() (string, error)
	SetLastError func(string)
	// Nil retains the default HTTP transport; tests can supply a fake router.
	Transport http.RoundTripper
}

const (
	RouterMaxEntries   = 8
	RouterLoadBudget   = 10 * time.Minute
	RouterStateEntries = "router_entries"
	RouterStateActive  = "router_active"  // section du modèle choisi par l'utilisateur
	RouterStateCurrent = "router_current" // section qui sert réellement /v1 (active ou variante)
)

// RouterModel est l'état d'une section vu par le router.
type RouterModel struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Failed   bool   `json:"failed"`
	ExitCode int    `json:"exit_code"`
}

func (r *Router) ServerArgs(fallbackKey func() string) []string {
	args := []string{r.BinaryPath,
		"--models-preset", r.INIPath,
		"--models-max", strconv.Itoa(r.ModelsMax),
		"--no-models-autoload",
	}
	if k, err := r.APIKey(); err == nil {
		if k != "" {
			args = append(args, "--api-key", k)
		} else if k := fallbackKey(); k != "" {
			args = append(args, "--api-key", k)
		}
	}
	return append(args, "--host", "127.0.0.1", "--port", strconv.Itoa(r.BackendPort))
}

func (r *Router) Entries() []RouterEntry {
	var list []RouterEntry
	_ = r.State.GetJSON(RouterStateEntries, &list)
	return list
}

// RememberEntry ajoute (ou rafraîchit) une section et borne la liste en
// gardant les plus récentes et celles qui servent encore.
func (r *Router) RememberEntry(e RouterEntry) ([]RouterEntry, error) {
	list := r.Entries()
	keep := map[string]bool{
		e.Name:                                true,
		r.State.GetString(RouterStateActive):  true,
		r.State.GetString(RouterStateCurrent): true,
	}
	e.Used = time.Now().UnixNano()
	next := []RouterEntry{e}
	for _, x := range list {
		if x.Name != e.Name {
			next = append(next, x)
		}
	}
	sort.SliceStable(next, func(i, j int) bool { return next[i].Used > next[j].Used })
	if len(next) > RouterMaxEntries {
		trimmed := next[:0]
		for i, x := range next {
			if i < RouterMaxEntries || keep[x.Name] {
				trimmed = append(trimmed, x)
			}
		}
		next = trimmed
	}
	return next, r.State.PutJSON(RouterStateEntries, next)
}

func (r *Router) WriteINI(entries []RouterEntry) error {
	path := r.INIPath
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, RenderRouterINI(entries), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EnsureINI écrit le fichier avant le démarrage du router, à partir des
// sections mémorisées (le fichier doit exister pour --models-preset).
func (r *Router) EnsureINI() error {
	return r.WriteINI(r.Entries())
}

// Do appelle l'API de gestion du router (loopback, jamais exposée).
func (r *Router) Do(method, path string, body any, timeout time.Duration) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, "http://127.0.0.1:"+strconv.Itoa(r.BackendPort)+path, rd)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	r.Authorize(req)
	resp, err := (&http.Client{Timeout: timeout, Transport: r.Transport}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return raw, resp.StatusCode, err
}

// Reachable dit si un llama-server en mode router répond sur le port interne.
func (r *Router) Reachable() bool {
	raw, code, err := r.Do(http.MethodGet, "/props", nil, 2*time.Second)
	if err != nil || code != http.StatusOK {
		return false
	}
	var p struct {
		Role string `json:"role"`
	}
	return json.Unmarshal(raw, &p) == nil && p.Role == "router"
}

func (r *Router) Models(reload bool) ([]RouterModel, error) {
	return r.ModelsWithTimeout(reload, 30*time.Second)
}

func (r *Router) ModelsWithTimeout(reload bool, timeout time.Duration) ([]RouterModel, error) {
	path := "/models"
	if reload {
		path += "?reload=1"
	}
	raw, code, err := r.Do(http.MethodGet, path, nil, timeout)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("router /models : HTTP %d", code)
	}
	var payload struct {
		Data []struct {
			ID     string `json:"id"`
			Status struct {
				Value    string `json:"value"`
				Failed   bool   `json:"failed"`
				ExitCode int    `json:"exit_code"`
			} `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("router /models illisible : %w", err)
	}
	out := make([]RouterModel, 0, len(payload.Data))
	for _, d := range payload.Data {
		out = append(out, RouterModel{ID: d.ID, Status: d.Status.Value, Failed: d.Status.Failed, ExitCode: d.Status.ExitCode})
	}
	return out, nil
}

func (r *Router) ModelStatus(name string) (RouterModel, bool) {
	list, err := r.Models(false)
	if err != nil {
		return RouterModel{}, false
	}
	for _, m := range list {
		if m.ID == name {
			return m, true
		}
	}
	return RouterModel{}, false
}

// EnsureLoaded publishes the entry and waits for it to serve. The caller
// must hold Lock when using it outside Activate or ActivateVariant.
func (r *Router) EnsureLoaded(e RouterEntry) error {
	entries, err := r.RememberEntry(e)
	if err != nil {
		return err
	}
	if err := r.WriteINI(entries); err != nil {
		return err
	}
	list, err := r.Models(true)
	if err != nil {
		return err
	}
	found := false
	for _, m := range list {
		if m.ID != e.Name {
			continue
		}
		found = true
		if m.Status == "loaded" {
			return nil
		}
		if m.Status != "loading" {
			if _, code, err := r.Do(http.MethodPost, "/models/load", map[string]string{"model": e.Name}, 30*time.Second); err != nil {
				return err
			} else if code != http.StatusOK {
				return fmt.Errorf("le router refuse de charger %s (HTTP %d)", e.Label, code)
			}
		}
	}
	if !found {
		return fmt.Errorf("le router ne voit pas la configuration %s — preset invalide pour ce moteur ?", e.Name)
	}
	deadline := time.Now().Add(RouterLoadBudget)
	for time.Now().Before(deadline) {
		m, ok := r.ModelStatus(e.Name)
		switch {
		case ok && m.Status == "loaded":
			r.SetLastError("")
			return nil
		case ok && m.Failed:
			msg := fmt.Sprintf("Le modèle n'a pas pu se charger (code %d) — mémoire VRAM insuffisante ou modèle incompatible avec ce moteur", m.ExitCode)
			r.SetLastError(msg)
			return fmt.Errorf("%s", msg)
		}
		time.Sleep(400 * time.Millisecond)
	}
	return fmt.Errorf("le chargement de %s dépasse %s", e.Label, RouterLoadBudget)
}

func (r *Router) unloadLocked() error {
	_ = r.State.PutString(RouterStateActive, "")
	_ = r.State.PutString(RouterStateCurrent, "")
	list, err := r.Models(false)
	if err != nil {
		return err
	}
	for _, m := range list {
		if m.Status == "loaded" || m.Status == "loading" {
			_, _, _ = r.Do(http.MethodPost, "/models/unload", map[string]string{"model": m.ID}, 30*time.Second)
		}
	}
	return nil
}

// UnloadAll libère la VRAM sans arrêter le moteur.
func (r *Router) UnloadAll() error {
	r.Lock.Lock()
	defer r.Lock.Unlock()
	return r.unloadLocked()
}

// CurrentName : section vers laquelle /v1 doit router maintenant.
func (r *Router) CurrentName() string {
	if n := strings.TrimSpace(r.State.GetString(RouterStateCurrent)); n != "" {
		return n
	}
	return strings.TrimSpace(r.State.GetString(RouterStateActive))
}

// ObservedContext reads native allocation, never a requested/native GGUF
// fallback. autoload=false prevents a status read from starting a model.
func (r *Router) ObservedContext() *int {
	name := r.CurrentName()
	active := r.State.GetString(RouterStateActive)
	if name == "" || name != active {
		return nil
	}
	list, err := r.ModelsWithTimeout(false, 500*time.Millisecond)
	if err != nil {
		return nil
	}
	loaded := false
	for _, m := range list {
		if m.ID == name && m.Status == "loaded" && !m.Failed {
			loaded = true
		}
	}
	if !loaded {
		return nil
	}
	raw, code, err := r.Do(http.MethodGet, "/props?model="+url.QueryEscape(name)+"&autoload=false", nil, 500*time.Millisecond)
	if err != nil || code != http.StatusOK || r.CurrentName() != name || r.State.GetString(RouterStateActive) != active {
		return nil
	}
	var props struct {
		Settings struct {
			Context int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	if json.Unmarshal(raw, &props) != nil || props.Settings.Context <= 0 {
		return nil
	}
	return &props.Settings.Context
}

// Activate builds the user's current entry while holding the owner's lock.
// Selection is recorded before load so health is never ready on the old model.
func (r *Router) Activate(hasModel func() bool, build func() (RouterEntry, error)) error {
	r.Lock.Lock()
	defer r.Lock.Unlock()
	if !hasModel() {
		return r.unloadLocked()
	}
	e, err := build()
	if err != nil {
		r.SetLastError(err.Error())
		return err
	}
	_ = r.State.PutString(RouterStateActive, e.Name)
	_ = r.State.PutString(RouterStateCurrent, e.Name)
	return r.EnsureLoaded(e)
}

// ActivateVariant loads a transient entry without replacing the user selection.
func (r *Router) ActivateVariant(build func() (RouterEntry, error)) (string, error) {
	r.Lock.Lock()
	defer r.Lock.Unlock()
	e, err := build()
	if err != nil {
		return "", err
	}
	_ = r.State.PutString(RouterStateCurrent, e.Name)
	return e.Name, r.EnsureLoaded(e)
}

// Unload targets one native instance without changing the saved selection.
func (r *Router) Unload(model string) error {
	r.Lock.Lock()
	defer r.Lock.Unlock()
	_, code, err := r.Do(http.MethodPost, "/models/unload", map[string]string{"model": model}, 30*time.Second)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("router /models/unload : HTTP %d", code)
	}
	return nil
}
