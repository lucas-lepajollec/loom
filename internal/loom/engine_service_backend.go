package loom

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lucas-lepajollec/loom/internal/loom/engine/llamacpp"
)

var engineLaunchState struct {
	sync.Mutex
	home, key, single, selected string
	max                         int
	router                      bool
}

func noteEngineLaunch(args []string) {
	engineLaunchState.Lock()
	defer engineLaunchState.Unlock()
	engineLaunchState.home, engineLaunchState.key, engineLaunchState.max = LoomHome(), "", 1
	engineLaunchState.single, engineLaunchState.selected = ReadConfig()["MODEL"], ReadConfig()["MODEL"]
	engineLaunchState.router = false
	for i := 1; i+1 < len(args); i++ {
		switch args[i] {
		case "--api-key":
			engineLaunchState.key = args[i+1]
		case "--models-max":
			engineLaunchState.max, _ = strconv.Atoi(args[i+1])
			engineLaunchState.router = true
		}
	}
}
func backendInferenceKey(fallback string) string {
	engineLaunchState.Lock()
	defer engineLaunchState.Unlock()
	if engineLaunchState.home == LoomHome() {
		return engineLaunchState.key
	}
	if fallback != "" {
		return fallback
	}
	return legacyEngineKey()
}
func serviceCapacity() int {
	if !serviceRouterMode() {
		return 1
	}
	max := routerModelsMax()
	engineLaunchState.Lock()
	defer engineLaunchState.Unlock()
	if engineLaunchState.home == LoomHome() {
		if !engineLaunchState.router {
			return 1
		}
		if engineLaunchState.max > 0 && (max == 0 || max > engineLaunchState.max) {
			return engineLaunchState.max
		}
	}
	return max
}

func serviceRouterMode() bool {
	managed := ownedLlamaManaged()
	engineLaunchState.Lock()
	if engineLaunchState.home == LoomHome() && managed {
		router := engineLaunchState.router
		engineLaunchState.Unlock()
		return router
	}
	engineLaunchState.Unlock()
	return routerModeCached()
}

func serviceResidentModels() ([]string, error) {
	if serviceRouterMode() {
		list, err := routerModelsWithTimeout(false, 2*time.Second)
		if err != nil {
			return nil, err
		}
		out := []string{}
		for _, m := range list {
			if m.Status == "loading" {
				return nil, errEngineBusy
			}
			if m.Status == "loaded" {
				out = append(out, m.ID)
			}
		}
		return out, nil
	}
	if ownedLlamaManaged() && !ownedLlamaRunning() {
		return nil, nil
	}
	if !ownedLlamaManaged() {
		req, err := http.NewRequest(http.MethodGet, llamaBackendURL().String()+"/health", nil)
		if err != nil {
			return nil, err
		}
		localAuthHeader(req)
		resp, err := healthClient.Do(req)
		if err != nil {
			return nil, nil
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, nil
		}
	}
	if model := strings.TrimSpace(ReadConfig()["MODEL"]); model != "" {
		engineLaunchState.Lock()
		defer engineLaunchState.Unlock()
		if engineLaunchState.home == LoomHome() && engineLaunchState.single != "" {
			return []string{engineLaunchState.single}, nil
		}
		return []string{model}, nil
	}
	return nil, nil
}

func serviceIdleUnload() error {
	llamaOwner.SwitchLock().Lock()
	defer llamaOwner.SwitchLock().Unlock()
	if serviceRouterMode() {
		return routerUnloadAll()
	}
	// cmdServe conserve le front Loom et le choix enregistré pour le réveil.
	if ownedLlamaManaged() {
		stopOwnedLlama()
		return nil
	}
	return fmt.Errorf("idle unload requires the owned llama-server")
}

func serviceEnsureRouterLoaded(e routerEntry, selectCurrent bool) error {
	s := currentEngineService()
	s.mu.Lock()
	protected := make([]string, 0, len(s.resident))
	for model := range s.resident {
		protected = append(protected, model)
	}
	s.mu.Unlock()
	r := llamaRouter()
	r.ProtectedEntries = protected
	if selectCurrent {
		if err := putStr(bkState, routerStateActive, e.Name); err != nil {
			return err
		}
		if err := putStr(bkState, routerStateCurrent, e.Name); err != nil {
			return err
		}
	}
	return r.EnsureLoaded(e)
}

func serviceApplyCapacity() error {
	llamaOwner.SwitchLock().Lock()
	defer llamaOwner.SwitchLock().Unlock()
	s := currentEngineService()
	s.mu.Lock()
	resident := make([]engineResident, 0, len(s.resident))
	for _, model := range s.resident {
		resident = append(resident, *model)
	}
	s.mu.Unlock()
	sort.Slice(resident, func(i, j int) bool { return resident[i].LastUsed.Before(resident[j].LastUsed) })
	entries := loadRouterEntries()
	bin := resolvedEngineBin()
	args := routerServerArgs(bin)
	stopOwnedLlama()
	if err := startOwnedLlama(bin, args); err != nil {
		return err
	}
	if err := waitRouterUp(2 * time.Minute); err != nil {
		return err
	}
	// Les inférences sont drainées ; on restaure les plus récents en dernier
	// afin que le LRU natif respecte la nouvelle limite.
	llamaOwner.RouterLock().Lock()
	defer llamaOwner.RouterLock().Unlock()
	for _, model := range resident {
		for _, entry := range entries {
			if entry.Name == model.Model {
				if err := routerEnsureLoaded(entry); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

// Résout une configuration sans changer la sélection ni l'overlay global.
// Chaque stream garde sa propre section, y compris avec MODELS_MAX > 1.
func serviceModelRequest(id string, run, extra map[string]string, router bool, priority string) (string, string, func() error, error) {
	cfg := ReadConfig()
	if router {
		for _, e := range loadRouterEntries() {
			if e.Name == id && len(run) == 0 && len(extra) == 0 {
				model := cfg["MODEL"]
				for _, option := range e.Options {
					if option[0] == "m" || option[0] == "model" {
						model = option[1]
					}
				}
				return e.Name, model, func() error {
					llamaOwner.RouterLock().Lock()
					defer llamaOwner.RouterLock().Unlock()
					return serviceEnsureRouterLoaded(e, false)
				}, nil
			}
		}
	}
	var entry *oaiEntry
	if !oaiKeepCurrent(id) {
		e, ok := resolveOAIModel(id)
		if !ok {
			return "", "", nil, fmt.Errorf("unknown model: %s", id)
		}
		entry = &e
		if !oaiAlreadyLoaded(id) {
			if e.Kind == "preset" {
				body, err := ReadPreset(strings.TrimSuffix(filepath.Base(e.Preset), ".env"))
				if err != nil {
					return "", "", nil, err
				}
				cfg = parseEnv(body)
			} else {
				cfg = nakedLoadEnv(e.Model)
			}
			cur := ReadConfig()
			for _, k := range preservedKeys {
				if v, ok := cur[k]; ok {
					if _, claimed := cfg[k]; !softPreservedKeys[k] || !claimed {
						cfg[k] = v
					}
				}
			}
			cfg["PORT"] = cur["PORT"]
		}
	}
	model := strings.TrimSpace(cfg["MODEL"])
	if model == "" {
		return "", "", nil, fmt.Errorf("no model selected")
	}
	if router && oaiKeepCurrent(id) && len(run) == 0 && len(extra) == 0 {
		if cur := routerCurrentName(); cur != "" {
			for _, e := range loadRouterEntries() {
				if e.Name == cur {
					return cur, model, func() error {
						llamaOwner.RouterLock().Lock()
						defer llamaOwner.RouterLock().Unlock()
						return serviceEnsureRouterLoaded(e, true)
					}, nil
				}
			}
		}
	}
	// Sans surcharge ni swap, le mode historique ne nécessite pas de build.
	if !router && oaiAlreadyLoaded(id) && len(run) == 0 && len(extra) == 0 {
		target := model
		running := ownedLlamaRunning()
		engineLaunchState.Lock()
		if running && engineLaunchState.home == LoomHome() && engineLaunchState.selected == model && engineLaunchState.single != "" {
			target = engineLaunchState.single
		}
		engineLaunchState.Unlock()
		return target, model, func() error {
			if err := restartOwnedLlama(); err != nil {
				return err
			}
			return waitLlamaReady(10 * time.Minute)
		}, nil
	}
	for k, v := range run {
		cfg[k] = v
	}
	args, err := buildServiceModelArgs(cfg, extra)
	if err != nil {
		return "", "", nil, err
	}
	if router {
		opts, err := argsToPresetOptions(args[0], args[1:])
		if err != nil {
			return "", "", nil, err
		}
		e := routerEntry{Name: routerEntryName(opts), Label: filepath.Base(model), Options: opts}
		return e.Name, model, func() error {
			llamaOwner.SwitchLock().Lock()
			defer llamaOwner.SwitchLock().Unlock()
			llamaOwner.RouterLock().Lock()
			defer llamaOwner.RouterLock().Unlock()
			return serviceEnsureRouterLoaded(e, oaiKeepCurrent(id) && len(run) == 0 && len(extra) == 0)
		}, nil
	}
	// L'ancien serveur ne peut héberger qu'une configuration : les variantes
	// ont une identité distincte et attendent donc elles aussi le drain.
	target := model
	if len(run) > 0 || len(extra) > 0 {
		opts, _ := argsToPresetOptions(args[0], args[1:])
		target = model + ":" + routerEntryName(opts)
	}
	return target, model, func() error {
		llamaOwner.SwitchLock().Lock()
		defer llamaOwner.SwitchLock().Unlock()
		if entry != nil && priority == "interactive" {
			if err := applyOAIEntry(*entry); err != nil {
				return err
			}
		}
		if ownedLlamaManaged() {
			stopOwnedLlama()
			if err := llamaOwner.Start(func() llamacpp.Launch {
				launch := ownedLlamaLaunch(args[0], args)
				launch.Command.Env = libraryPathEnv(filepath.Dir(args[0]))
				if devices := cfg["CUDA_VISIBLE_DEVICES"]; devices != "" {
					launch.Command.Env = append(launch.Command.Env, "CUDA_VISIBLE_DEVICES="+devices, "CUDA_DEVICE_ORDER=PCI_BUS_ID")
				}
				return launch
			}); err != nil {
				return err
			}
			engineLaunchState.Lock()
			engineLaunchState.single, engineLaunchState.selected = target, model
			engineLaunchState.Unlock()
		} else {
			if len(run) > 0 || len(extra) > 0 {
				return fmt.Errorf("temporary configuration requires an owned engine")
			}
			if err := restartLlamaForOAI(); err != nil {
				return err
			}
		}
		return waitLlamaReady(10 * time.Minute)
	}, nil
}

func buildServiceModelArgs(cfg map[string]string, extra map[string]string) ([]string, error) {
	return buildLlamaServerArgsWithRuntime(cfg, func(string) string { return "" }, func(args []string) []string {
		ids := make([]string, 0, len(extra))
		for id := range extra {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			val := extra[id]
			if llamaBooleanFlag(args[0], id) {
				if val == "off" || val == "false" || val == "0" {
					val = "off"
				} else {
					val = "on"
				}
			}
			args = append(args, "--"+id, val)
		}
		return args
	}, false)
}

func runEngineIdleTimer(stop <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			currentEngineService().idleTick()
		}
	}
}

func enginePriority(r *http.Request, key engineAPIIdentity) string {
	if key.Priority == "background" || strings.EqualFold(r.Header.Get("X-Loom-Priority"), "background") {
		return "background"
	}
	return "interactive"
}
