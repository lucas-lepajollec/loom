package loom

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Overlay runtime /v1 : change le process llama-server pour les drapeaux
// demandés par l'app, sans WriteConfig / preset / souvenir.

var (
	oaiRunMu    sync.Mutex
	oaiRunKV    = map[string]string{}
	oaiRunExtra = map[string]string{}
)

func oaiRuntimeGet(key string) string {
	oaiRunMu.Lock()
	defer oaiRunMu.Unlock()
	return strings.TrimSpace(oaiRunKV[key])
}

func oaiRuntimeExtra() map[string]string {
	oaiRunMu.Lock()
	defer oaiRunMu.Unlock()
	out := make(map[string]string, len(oaiRunExtra))
	for k, v := range oaiRunExtra {
		out[k] = v
	}
	return out
}

func clearOAIRuntime() {
	oaiRunMu.Lock()
	defer oaiRunMu.Unlock()
	oaiRunKV = map[string]string{}
	oaiRunExtra = map[string]string{}
}

func oaiRuntimeApply(kv, extra map[string]string) error {
	if len(kv) == 0 && len(extra) == 0 {
		return nil
	}
	oaiRunMu.Lock()
	changed := false
	for k, v := range kv {
		v = strings.TrimSpace(v)
		if v == "" || oaiRunKV[k] == v {
			continue
		}
		oaiRunKV[k] = v
		changed = true
		fmt.Printf("[loom serve] /v1 overlay %s=%s (request, not the preset)\n", k, v)
	}
	for k, v := range extra {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || oaiRunExtra[k] == v {
			continue
		}
		oaiRunExtra[k] = v
		changed = true
		fmt.Printf("[loom serve] /v1 overlay --%s %s (request, not the preset)\n", k, v)
	}
	oaiRunMu.Unlock()
	if !changed {
		return nil
	}
	// Router : les drapeaux demandés deviennent une variante éphémère chargée à
	// côté de la configuration de l'utilisateur, jamais écrite dans ses réglages.
	if routerReachable() {
		_, err := routerActivateVariant()
		return err
	}
	if err := restartLlamaForOAI(); err != nil {
		return err
	}
	return waitLlamaReady(10 * time.Minute)
}

func appendOAIRuntimeArgs(args []string) []string {
	bin := ""
	if len(args) > 0 {
		bin = args[0]
	}
	for id, val := range oaiRuntimeExtra() {
		flag := "--" + id
		if llamaBooleanFlag(bin, id) {
			if val == "" || val == "on" || val == "true" || val == "1" {
				args = append(args, flag, "on")
			} else if val == "off" || val == "false" || val == "0" {
				args = append(args, flag, "off")
			} else {
				args = append(args, flag, val)
			}
			continue
		}
		if val == "" {
			args = append(args, flag)
			continue
		}
		args = append(args, flag, val)
	}
	return args
}
