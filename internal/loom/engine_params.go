package loom

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
)

//go:embed params/llamacpp.json
var llamaCppParamJSON []byte // immutable embedded source, not runtime state

// ParamSpec is the public control contract. Control/When name the few model-
// dependent behaviors; labels, tiers, choices and generic controls are data.
type ParamSpec struct {
	ID             string     `json:"id"`
	Flag           string     `json:"flag,omitempty"`
	Key            string     `json:"key,omitempty"`
	Label          string     `json:"label"`
	Tip            string     `json:"tip,omitempty"`
	Tier           string     `json:"tier"`
	Kind           string     `json:"kind"`
	NativeKind     string     `json:"native_kind,omitempty"`
	Control        string     `json:"control,omitempty"`
	When           string     `json:"when,omitempty"`
	Choices        [][]string `json:"choices,omitempty"`
	Min            *float64   `json:"min,omitempty"`
	Max            *float64   `json:"max,omitempty"`
	Step           *float64   `json:"step,omitempty"`
	Placeholder    string     `json:"placeholder,omitempty"`
	Stack          bool       `json:"stack,omitempty"`
	AffectsVRAM    bool       `json:"affects_vram"`
	RequiresReload bool       `json:"requires_reload"`
	RequiresFlag   bool       `json:"requires_flag,omitempty"`
	Available      bool       `json:"available"`
	Supported      bool       `json:"supported"`
	Covers         []string   `json:"covers,omitempty"`
	Aliases        []string   `json:"aliases,omitempty"`
	Default        string     `json:"default,omitempty"`
}

type curatedParams struct {
	Params         []ParamSpec `json:"params"`
	ExpertExcludes []string    `json:"expert_excludes"`
}

func readCuratedParams() curatedParams {
	var c curatedParams
	if err := json.Unmarshal(llamaCppParamJSON, &c); err != nil {
		panic("invalid embedded llama.cpp ParamSpec: " + err.Error())
	}
	return c
}

func paramMatchesFlag(p ParamSpec, f LlamaFlag) bool {
	if p.ID == f.ID || p.Flag != "" && p.Flag == f.Flag {
		return true
	}
	for _, alias := range f.Aliases {
		if p.Flag != "" && p.Flag == alias || p.ID == strings.TrimPrefix(alias, "--") {
			return true
		}
	}
	return false
}

func mergeEngineParams(c curatedParams, flags []LlamaFlag) []ParamSpec {
	params := append([]ParamSpec(nil), c.Params...)
	covered := make(map[string]bool)
	for _, id := range c.ExpertExcludes {
		covered[id] = true
	}
	for i := range params {
		p := &params[i]
		covered[p.ID] = true
		for _, id := range p.Covers {
			covered[id] = true
		}
		for _, f := range flags {
			if f.Hidden || !paramMatchesFlag(*p, f) {
				continue
			}
			p.Supported = true
			p.NativeKind = f.Kind
			covered[f.ID] = true
			p.Flag, p.Aliases, p.Default = f.Flag, f.Aliases, f.Default
			if p.Key == "" {
				p.Key = f.Key
			}
			if p.Tip == "" {
				p.Tip = f.Help
			}
			if len(p.Choices) == 0 {
				for _, choice := range f.Choices {
					p.Choices = append(p.Choices, []string{choice, choice})
				}
			}
			break
		}
		// Keep model-owned/composite controls and existing dedicated controls;
		// conditional native flags appear only when the installed help has them.
		p.Available = !p.RequiresFlag || p.Supported
	}
	for _, f := range flags {
		if f.Hidden || f.Deprecated || f.ID == "" || covered[f.ID] {
			continue
		}
		p := ParamSpec{ID: f.ID, Flag: f.Flag, Key: f.Key, Label: f.Flag, Tip: f.Help,
			Tier: "expert", Kind: f.Kind, Default: f.Default, Aliases: f.Aliases,
			Min: f.Min, Max: f.Max, Available: true, Supported: true, RequiresReload: true}
		for _, choice := range f.Choices {
			p.Choices = append(p.Choices, []string{choice, choice})
		}
		params = append(params, p)
	}
	return params
}

func (llamaCppEngine) Params() []ParamSpec {
	bin := strings.TrimSpace(ReadConfig()["BIN"])
	var flags []LlamaFlag
	if bin != "" {
		flags = llamaFlagsPublic(bin)
	}
	return mergeEngineParams(readCuratedParams(), flags)
}

func handleEngineParams(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		sendJSON(w, 405, map[string]any{"ok": false, "error": "méthode non autorisée"})
		return
	}
	e := localEngine()
	sendJSON(w, 200, map[string]any{"ok": true, "engine_id": e.ID(), "params": e.Params()})
}
