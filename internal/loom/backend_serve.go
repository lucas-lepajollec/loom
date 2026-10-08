package loom

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// cmdServe supervise llama-server en enfant (127.0.0.1, port interne) et tient
// le front OpenAI sur HOST:PORT. Il peut tourner en mode veille sans modèle chargé
// (0 VRAM) tout en maintenant /v1 actif et prêt à charger à la première requête.
func cmdServe(args []string) error {
	cfg := ReadConfig()
	bin := strings.TrimSpace(cfg["BIN"])
	if bin == "" {
		return fmt.Errorf("BIN not set — run “loom edit”")
	}
	_ = os.Chdir(LoomHome())

	initOwnedLlamaSupervisor()
	defer shutdownOwnedLlamaSupervisor()

	model := strings.TrimSpace(cfg["MODEL"])
	if engineBin := resolvedEngineBin(); routerWanted(engineBin) {
		// Mode router : le moteur démarre une fois, sans modèle ; les modèles se
		// chargent ensuite par son API, sans jamais relancer ce process.
		setLibraryPath(filepath.Dir(engineBin))
		if v := cfg["CUDA_VISIBLE_DEVICES"]; v != "" {
			_ = os.Setenv("CUDA_VISIBLE_DEVICES", v)
			_ = os.Setenv("CUDA_DEVICE_ORDER", "PCI_BUS_ID")
		}
		if err := ensureRouterINI(); err != nil {
			return fmt.Errorf("router presets: %w", err)
		}
		rArgs := routerServerArgs(engineBin)
		fmt.Fprintf(os.Stderr, "[loom serve] llama-server router  /v1=:%d  llama=:%d  max models=%d\n",
			LLMPort(), llamaBackendPort(), routerModelsMax())
		if err := startOwnedLlama(rArgs[0], rArgs); err != nil {
			return fmt.Errorf("starting llama-server router: %w", err)
		}
		if model != "" {
			startup := currentEngineService()
			startup.mu.Lock()
			startup.loading = true
			startup.mu.Unlock()
			go func() {
				defer func() {
					startup.mu.Lock()
					defer startup.mu.Unlock()
					startup.loading = false
					_ = startup.refreshLocked()
					startup.dispatchLocked()
				}()
				if err := waitRouterUp(2 * time.Minute); err != nil {
					fmt.Fprintf(os.Stderr, "[loom serve] router unreachable: %v\n", err)
					return
				}
				if err := routerActivate(); err != nil {
					fmt.Fprintf(os.Stderr, "[loom serve] model not loaded at startup: %v\n", err)
				}
			}()
		} else {
			_ = putStr(bkState, routerStateActive, "")
			_ = putStr(bkState, routerStateCurrent, "")
		}
	} else if model != "" {
		llmArgs, err := buildLlamaServerArgs()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[loom serve] model not loaded at startup: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[loom serve] %s  model=%s  /v1=:%d  llama=:%d\n",
				llmArgs[0], filepath.Base(model), LLMPort(), llamaBackendPort())
			if err := startOwnedLlama(llmArgs[0], llmArgs); err != nil {
				fmt.Fprintf(os.Stderr, "[loom serve] failed to start llama-server: %v\n", err)
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "[loom serve] engine idle (no model loaded, 0 VRAM)  /v1=:%d\n", LLMPort())
	}

	errc := make(chan error, 2)
	go serveOAIFront(errc)
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		errc <- nil
	}()
	return <-errc
}

// waitRouterUp attend que le router réponde sur le port interne.
func waitRouterUp(budget time.Duration) error {
	return llamaOwner.WaitRouterUp(budget, routerReachable)
}
