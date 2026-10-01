package loom

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// Engine is the in-package migration boundary. Existing service supervision,
// router, argument resolution and estimation remain the execution owners.
type Engine interface {
	ID() string
	Capabilities() EngineCaps
	Start(context.Context) error
	Stop() error
	Status() EngineStatus
	Load(context.Context, ModelConfig) error
	Unload(context.Context, string) error
	Endpoint() string
	Params() []ParamSpec
	Estimate(ModelConfig) Estimate
}

type EngineCaps struct {
	Router   bool `json:"router"`
	Presets  bool `json:"presets"`
	Slots    bool `json:"slots"`
	Estimate bool `json:"estimate"`
}

type EngineStatus struct {
	Active bool   `json:"active"`
	Ready  bool   `json:"ready"`
	Model  string `json:"model"`
}

// Empty fields select the current configuration. Explicit inputs are resolved
// in memory, never saved. ModelConfig is not a second argument-building path.
type ModelConfig struct {
	Model   string
	Content string
}

type Estimate = vramEst

type llamaCppEngine struct{}

var _ Engine = llamaCppEngine{}

// Stateless view over the existing owner; no second process or runtime global.
func localEngine() Engine { return llamaCppEngine{} }

func (llamaCppEngine) ID() string { return "llama.cpp" }

func (llamaCppEngine) Capabilities() EngineCaps {
	bin := resolvedEngineBin()
	return EngineCaps{Router: routerWanted(bin), Presets: true,
		Slots: llamaBooleanFlag(bin, "slots"), Estimate: true}
}

// Context is checked before entering the legacy service/router lifecycle. This
// first wrapper does not claim cancellation of an already-started service job.
func (llamaCppEngine) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureLoomEngineBind(); err != nil {
		return err
	}
	if err := preflightEngine(); err != nil {
		return err
	}
	return serviceAction("start")
}

func (llamaCppEngine) Stop() error {
	if ownedLlamaManaged() {
		stopOwnedLlama()
		return nil
	}
	return serviceAction("stop")
}

func (llamaCppEngine) Status() EngineStatus {
	active := serviceIsActive()
	model := strings.TrimSpace(ReadConfig()["MODEL"])
	if ownedLlamaManaged() && !ownedLlamaRunning() {
		model = ""
	}
	return EngineStatus{Active: active, Ready: active && healthCheck(), Model: model}
}

func resolveEngineConfig(c ModelConfig) map[string]string {
	cfg := ReadConfig()
	for k, v := range parseEnv(c.Content) {
		cfg[k] = v
	}
	if c.Model != "" {
		cfg["MODEL"] = c.Model
	}
	return cfg
}

func (llamaCppEngine) BuildArgs(c ModelConfig) ([]string, error) {
	return buildLlamaServerArgsForConfig(resolveEngineConfig(c))
}

func (e llamaCppEngine) Load(ctx context.Context, c ModelConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Model == "" && c.Content == "" {
		return restartLlamaEngine()
	}
	// Explicit transient input requires the existing router. Never write it to
	// config.env merely to make a service restart consume it.
	if !routerReachable() {
		return fmt.Errorf("configuration temporaire : moteur router requis")
	}
	args, err := e.BuildArgs(c)
	if err != nil {
		return err
	}
	opts, err := argsToPresetOptions(args[0], args[1:])
	if err != nil {
		return err
	}
	routerMu.Lock()
	defer routerMu.Unlock()
	entry := routerEntry{Name: routerEntryName(opts), Label: filepath.Base(resolveEngineConfig(c)["MODEL"]) + " · variante API", Options: opts}
	_ = putStr(bkState, routerStateCurrent, entry.Name)
	return routerEnsureLoaded(entry)
}

func (llamaCppEngine) Unload(ctx context.Context, model string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if model == "" {
		return unloadEngine()
	}
	if !routerReachable() {
		return fmt.Errorf("déchargement ciblé : moteur router requis")
	}
	routerMu.Lock()
	defer routerMu.Unlock()
	_, code, err := routerDo(http.MethodPost, "/models/unload", map[string]string{"model": model}, 30*time.Second)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("router /models/unload : HTTP %d", code)
	}
	return nil
}

func (llamaCppEngine) Endpoint() string { return llamaBackendURL().String() + "/v1" }

func (llamaCppEngine) Estimate(c ModelConfig) Estimate {
	if c.Model == "" && c.Content == "" {
		c.Content = formatEnv(ReadConfig())
	}
	return estimateModel(c.Model, c.Content)
}
