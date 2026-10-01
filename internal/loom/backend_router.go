package loom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// backend_router.go — llama-server en mode router.
//
// Loom possède l'intention (quel modèle, quels réglages) ; llama.cpp possède
// l'exécution. En mode router, llama-server démarre UNE fois sans modèle et
// charge/décharge ses instances à la demande. Loom décrit chaque configuration
// voulue comme une section d'un fichier de presets INI, demande au router de
// relire ce fichier (GET /models?reload=1) puis de charger la section
// (POST /models/load). Changer de modèle ne redémarre donc plus le moteur, les
// requêtes concurrentes vont directement aux slots natifs, et une app externe
// qui demande d'autres drapeaux de chargement obtient une VARIANTE éphémère
// sans toucher à la configuration ni aux presets de l'utilisateur.
//
// Un moteur qui ne connaît pas --models-preset (build ancien, fork) garde le
// mode historique « un process par modèle ». ENGINE_MODE=single force ce mode.

const (
	routerMaxEntries   = 8
	routerLoadBudget   = 10 * time.Minute
	routerStateEntries = "router_entries"
	routerStateActive  = "router_active"  // section du modèle choisi par l'utilisateur
	routerStateCurrent = "router_current" // section qui sert réellement /v1 (active ou variante)
)

// routerModel est l'état d'une section vu par le router.
type routerModel struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Failed   bool   `json:"failed"`
	ExitCode int    `json:"exit_code"`
}

var routerMu sync.Mutex // sérialise écriture INI + chargements dans ce process

func engineModePref() string {
	return strings.ToLower(strings.TrimSpace(ReadConfig()["ENGINE_MODE"]))
}

// routerCapable dit si ce binaire sait tourner en router piloté par presets.
func routerCapable(bin string) bool {
	return bin != "" && llamaHasFlag(bin, "models-preset") && llamaHasFlag(bin, "models-max")
}

func routerWanted(bin string) bool {
	return engineModePref() != "single" && routerCapable(bin)
}

func routerINIPath() string { return filepath.Join(LoomHome(), "router-models.ini") }

func routerModelsMax() int {
	if n, err := strconv.Atoi(strings.TrimSpace(ReadConfig()["MODELS_MAX"])); err == nil && n >= 0 {
		return n
	}
	return 1
}

// resolvedEngineBin reproduit la résolution du binaire de buildLlamaServerArgs.
func resolvedEngineBin() string {
	bin := strings.TrimSpace(ReadConfig()["BIN"])
	if bin == "" {
		return ""
	}
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(LoomHome(), bin)
	}
	return prebuiltResolveBin(bin)
}

// routerServerArgs construit la ligne de commande du router lui-même.
func routerServerArgs(bin string) []string {
	args := []string{bin,
		"--models-preset", routerINIPath(),
		"--models-max", strconv.Itoa(routerModelsMax()),
		"--no-models-autoload",
	}
	if err := ensureAPIKeyIfRequired(); err == nil {
		if k := readAPIKey(); k != "" {
			args = append(args, "--api-key", k)
		} else if k := ReadConfig()["API_KEY"]; k != "" {
			args = append(args, "--api-key", k)
		}
	}
	return append(args, "--host", "127.0.0.1", "--port", strconv.Itoa(llamaBackendPort()))
}

// activeInstanceArgs : argv de l'instance pour la configuration courante,
// sans le binaire ni host/port (le router les impose).
func activeInstanceArgs() (string, []string, error) {
	args, err := buildLlamaServerArgs()
	if err != nil {
		return "", nil, err
	}
	return args[0], args[1:], nil
}

func activeEntryLabel() string {
	label := filepath.Base(strings.TrimSpace(ReadConfig()["MODEL"]))
	if pid := strings.TrimSpace(getStr(bkState, "active_preset")); pid != "" {
		if body, err := ReadPreset(pid); err == nil {
			label = presetDisplayName(body, pid)
		}
	}
	return label
}

func loadRouterEntries() []routerEntry {
	var list []routerEntry
	_ = getJSON(bkState, routerStateEntries, &list)
	return list
}

// rememberRouterEntry ajoute (ou rafraîchit) une section et borne la liste en
// gardant les plus récentes et celles qui servent encore.
func rememberRouterEntry(e routerEntry) ([]routerEntry, error) {
	list := loadRouterEntries()
	keep := map[string]bool{
		e.Name:                              true,
		getStr(bkState, routerStateActive):  true,
		getStr(bkState, routerStateCurrent): true,
	}
	e.Used = time.Now().UnixNano()
	next := []routerEntry{e}
	for _, x := range list {
		if x.Name != e.Name {
			next = append(next, x)
		}
	}
	sort.SliceStable(next, func(i, j int) bool { return next[i].Used > next[j].Used })
	if len(next) > routerMaxEntries {
		trimmed := next[:0]
		for i, x := range next {
			if i < routerMaxEntries || keep[x.Name] {
				trimmed = append(trimmed, x)
			}
		}
		next = trimmed
	}
	return next, putJSON(bkState, routerStateEntries, next)
}

func writeRouterINI(entries []routerEntry) error {
	path := routerINIPath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, renderRouterINI(entries), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ensureRouterINI écrit le fichier avant le démarrage du router, à partir des
// sections mémorisées (le fichier doit exister pour --models-preset).
func ensureRouterINI() error {
	return writeRouterINI(loadRouterEntries())
}

// routerDo appelle l'API de gestion du router (loopback, jamais exposée).
func routerDo(method, path string, body any, timeout time.Duration) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, llamaBackendURL().String()+path, rd)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	authHeader(req)
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return raw, resp.StatusCode, err
}

// routerReachable dit si un llama-server en mode router répond sur le port interne.
func routerReachable() bool {
	raw, code, err := routerDo(http.MethodGet, "/props", nil, 2*time.Second)
	if err != nil || code != http.StatusOK {
		return false
	}
	var p struct {
		Role string `json:"role"`
	}
	return json.Unmarshal(raw, &p) == nil && p.Role == "router"
}

func routerModels(reload bool) ([]routerModel, error) {
	return routerModelsWithTimeout(reload, 30*time.Second)
}

func routerModelsWithTimeout(reload bool, timeout time.Duration) ([]routerModel, error) {
	path := "/models"
	if reload {
		path += "?reload=1"
	}
	raw, code, err := routerDo(http.MethodGet, path, nil, timeout)
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
	out := make([]routerModel, 0, len(payload.Data))
	for _, d := range payload.Data {
		out = append(out, routerModel{ID: d.ID, Status: d.Status.Value, Failed: d.Status.Failed, ExitCode: d.Status.ExitCode})
	}
	return out, nil
}

func routerModelStatus(name string) (routerModel, bool) {
	list, err := routerModels(false)
	if err != nil {
		return routerModel{}, false
	}
	for _, m := range list {
		if m.ID == name {
			return m, true
		}
	}
	return routerModel{}, false
}

// routerEnsureLoaded publie la section puis attend qu'elle serve.
func routerEnsureLoaded(e routerEntry) error {
	entries, err := rememberRouterEntry(e)
	if err != nil {
		return err
	}
	if err := writeRouterINI(entries); err != nil {
		return err
	}
	list, err := routerModels(true)
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
			if _, code, err := routerDo(http.MethodPost, "/models/load", map[string]string{"model": e.Name}, 30*time.Second); err != nil {
				return err
			} else if code != http.StatusOK {
				return fmt.Errorf("le router refuse de charger %s (HTTP %d)", e.Label, code)
			}
		}
	}
	if !found {
		return fmt.Errorf("le router ne voit pas la configuration %s — preset invalide pour ce moteur ?", e.Name)
	}
	deadline := time.Now().Add(routerLoadBudget)
	for time.Now().Before(deadline) {
		m, ok := routerModelStatus(e.Name)
		switch {
		case ok && m.Status == "loaded":
			setLlamaLastError("")
			return nil
		case ok && m.Failed:
			msg := fmt.Sprintf("Le modèle n'a pas pu se charger (code %d) — mémoire VRAM insuffisante ou modèle incompatible avec ce moteur", m.ExitCode)
			setLlamaLastError(msg)
			return fmt.Errorf("%s", msg)
		}
		time.Sleep(400 * time.Millisecond)
	}
	return fmt.Errorf("le chargement de %s dépasse %s", e.Label, routerLoadBudget)
}

// buildRouterEntry fabrique la section de la configuration courante, avec
// d'éventuelles surcharges de chargement éphémères (kv config + drapeaux).
func buildRouterEntry(label string) (routerEntry, error) {
	bin, args, err := activeInstanceArgs()
	if err != nil {
		return routerEntry{}, err
	}
	opts, err := argsToPresetOptions(bin, args)
	if err != nil {
		return routerEntry{}, err
	}
	return routerEntry{Name: routerEntryName(opts), Label: label, Options: opts}, nil
}

// routerActivate charge dans le router la configuration courante (celle que
// l'utilisateur a choisie). Aucun redémarrage du moteur.
func routerActivate() error {
	routerMu.Lock()
	defer routerMu.Unlock()
	if strings.TrimSpace(ReadConfig()["MODEL"]) == "" {
		return routerUnloadLocked()
	}
	e, err := buildRouterEntry(activeEntryLabel())
	if err != nil {
		setLlamaLastError(err.Error())
		return err
	}
	// L'état actif est posé AVANT le chargement : /health répond 503 pendant
	// que la nouvelle configuration charge, jamais « prêt » sur l'ancienne.
	_ = putStr(bkState, routerStateActive, e.Name)
	_ = putStr(bkState, routerStateCurrent, e.Name)
	return routerEnsureLoaded(e)
}

// routerActivateVariant charge une variante éphémère de la configuration
// courante pour une requête /v1 qui impose ses propres drapeaux de chargement.
func routerActivateVariant() (string, error) {
	routerMu.Lock()
	defer routerMu.Unlock()
	e, err := buildRouterEntry(activeEntryLabel() + " · variante API")
	if err != nil {
		return "", err
	}
	_ = putStr(bkState, routerStateCurrent, e.Name)
	return e.Name, routerEnsureLoaded(e)
}

func routerUnloadLocked() error {
	_ = putStr(bkState, routerStateActive, "")
	_ = putStr(bkState, routerStateCurrent, "")
	list, err := routerModels(false)
	if err != nil {
		return err
	}
	for _, m := range list {
		if m.Status == "loaded" || m.Status == "loading" {
			_, _, _ = routerDo(http.MethodPost, "/models/unload", map[string]string{"model": m.ID}, 30*time.Second)
		}
	}
	return nil
}

// routerUnloadAll libère la VRAM sans arrêter le moteur.
func routerUnloadAll() error {
	routerMu.Lock()
	defer routerMu.Unlock()
	return routerUnloadLocked()
}

// routerCurrentName : section vers laquelle /v1 doit router maintenant.
func routerCurrentName() string {
	if n := strings.TrimSpace(getStr(bkState, routerStateCurrent)); n != "" {
		return n
	}
	return strings.TrimSpace(getStr(bkState, routerStateActive))
}

// observedEngineCtx reads native allocation, never a requested/native GGUF
// fallback. autoload=false prevents a status read from starting a model.
func observedEngineCtx() *int {
	name := routerCurrentName()
	active := getStr(bkState, routerStateActive)
	if name == "" || name != active {
		return nil
	}
	list, err := routerModelsWithTimeout(false, 500*time.Millisecond)
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
	raw, code, err := routerDo(http.MethodGet, "/props?model="+url.QueryEscape(name)+"&autoload=false", nil, 500*time.Millisecond)
	if err != nil || code != http.StatusOK || routerCurrentName() != name || getStr(bkState, routerStateActive) != active {
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
