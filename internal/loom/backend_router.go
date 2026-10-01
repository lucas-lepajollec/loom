package loom

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Loom supplies current configuration, installed paths and preset labels to
// engine/llamacpp. The supervisor lock is passed to every router view.

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
